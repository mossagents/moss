# 内核级单路径 Agent Runtime 统一设计

日期：2026-04-21  
状态：Draft / Ready for review

## 1. 背景

当前 moss 在权限、prompt、上下文、会话持久化四个核心机制上存在多条并行实现路径。

典型表现包括：

- 产品层与内核层同时拥有 prompt 组装能力；
- 入口层已经解析出细粒度 session spec，但 runtime 首轮执行仍可能退化成粗粒度 approval mode；
- 模型可见上下文既来自真实消息历史，也来自 fragment 式临时投影；
- 持久化同时承担“事实来源”和“结果快照”两种职责，resume 与 replay 依赖结果态而不是事件态。

这些问题的共同根因不是某个局部 bug，而是**同一机制缺少单一真相源**。只要这种结构不改，继续修补局部实现只会让系统更复杂，模型行为也会越来越不稳定。

codex 只作为参考实现。本设计不追求对齐 codex 的具体代码，而是只保留其背后的生产原则：

1. 同一机制只允许一条实现路径；
2. 模型可见输入必须与 runtime 执行语义同源；
3. 可恢复状态必须来自事件事实，而不是结果快照；
4. 产品层只能做入口适配，不能拥有运行时语义真相。

本设计明确**不考虑向后兼容性**。

若本设计与更早的 spec 在 session JSONL、prompt fragment、approval mode、checkpoint 恢复来源等问题上冲突，以本设计为准。

## 2. 本次目标

本次设计目标是建立一套以内核为中心的统一 agent runtime。

本次希望达成：

1. 建立会话蓝图作为唯一运行时真相源；
2. 删除 approval mode、string prompt hook、fragment prompt state、snapshot-first persistence 这些旧范式的核心地位；
3. 让权限、prompt、上下文、持久化都从同一套结构化状态派生；
4. 让 exec、TUI、resume、fork、checkpoint、review 共享同一条运行时链；
5. 明确 kernel、harness、product 的职责边界，防止未来再次长出旁路实现。

## 3. 非目标

本设计明确不做：

1. 保留旧字段、旧 metadata、旧 fallback 语义；
2. 保留 JSON session snapshot 作为运行时事实源；
3. 继续支持“外层 composer + 内层 prompt hook”双提示词路径；
4. 继续支持“compiled policy 与 approval mode 并存”的运行时语义；
5. 在长期架构中允许多个生产级事件存储后端并存。

## 4. 设计原则

### 4.1 单一真相源优先于便捷入口

所有运行时决策都必须来自结构化真相源。CLI flag、TUI posture、product config 只能是输入，不是事实。

### 4.2 运行时语义必须先编译再执行

不得再出现“先 build kernel，再根据 session 或 UI 回填 posture”的补丁式流程。所有执行前语义必须先编译成可消费的 blueprint。

### 4.3 模型可见输入与执行权限必须同源

模型看到的权限说明、上下文说明、模式说明，必须由执行侧实际使用的结构派生，不能再分别从不同字段或不同路径猜测。

### 4.4 上下文只有三种合法形态

上下文只允许三种合法形态：

1. persistent history item
2. task-scoped history item
3. turn-local materialization layer

三者都必须走同一条编译链，不允许再出现 fragment state 这种侧信道。

规则：

1. 会影响整个 session 的重放、resume、fork、checkpoint 语义的上下文，必须进入 persistent history；
2. 只对当前任务（task 或 planning 边界内）有意义、跨轮但不跨任务的上下文，进入 task-scoped history；
3. 只服务当前 turn 的 recall、runtime notice、预算告警等临时内容，停留在 turn-local materialization layer；
4. 无论哪种形态，都必须能通过 prompt_materialized 事件审计模型当时实际看到了什么。

task-scoped 规则：

1. task-scoped item 必须绑定到一个明确的 task boundary 事件（如 task_started），绑定字段为 `bound_to_task_event_id`，必须写入 EventStore，不允许只在内存中约定；
2. task 结束时，task-scoped items 必须通过显式 task_completed 或 task_abandoned 事件被 retire；
3. retire 时系统必须决定：是否把摘要提升为 persistent history，决策通过 task_completed / task_abandoned 事件的 `promote_scoped_items` 字段声明；
4. fork / resume 时，task-scoped items 随 task boundary 一起继承或丢弃，不得偷偷遗漏；
5. projection engine 必须自动处理 retire，不得由各个 projector 各自实现 retire 逻辑；
6. projection engine replay 结束时必须执行不变量检查：若存在 `bound_to_task_event_id` 对应的 task 已结束（task_completed 或 task_abandoned），但 task-scoped item 仍为 active 状态，则必须报错而非静默通过。

### 4.5 事件是事实，快照只是投影

resume、checkpoint、fork、review 必须以事件流为事实来源。任何快照都只能是缓存或派生视图。

### 4.6 扩展必须走结构化 provider

bootstrap、capability、skill、memory、planning 这类扩展只能注册结构化 provider，不能直接拼接字符串或直接篡改 session message。

## 5. 核心领域模型

### 5.1 RuntimeRequest

RuntimeRequest 是所有入口进入 runtime 的唯一输入对象。

职责：

1. 表达入口收集到的原始选择；
2. 不表达任何已编译的运行时语义；
3. 允许来自 CLI、TUI、API、resume、fork、checkpoint replay。

建议字段：

1. run_mode
2. collaboration_mode
3. workspace_trust
4. permission_profile
5. prompt_pack
6. session_policy
7. model_profile
8. workspace
9. restore_source
10. user_goal

说明：approval mode 仅允许作为入口层快捷别名，进入 RuntimeRequest 前必须被映射为 permission profile 或其他结构化输入，之后立即消失。

### 5.2 SessionBlueprint

SessionBlueprint 是 resolver 编译后的唯一运行时真相源。

职责：

1. 作为 session 初始化的唯一配置输入；
2. 作为 prompt、policy、context、persistence 的共同源头；
3. 作为 resume、fork、checkpoint、review 的解释基线。

建议字段：

1. identity
2. model_config
3. effective_tool_policy
4. context_budget
5. prompt_plan
6. persistence_plan
7. checkpoint_plan
8. collaboration_contract
9. session_budget
10. provenance
11. execution_affinity（可选）

其中 execution_affinity 用于声明 session 对执行节点的亲和性偏好，至少包含：

1. affinity_mode：none（无约束，任意节点可执行）/ node_pinned（需与特定节点亲和，如持有本地 git worktree 的 sandbox session）/ region_pinned（限定区域）
2. preferred_node_id：仅 node_pinned 时有意义，集群调度器优先选择该节点
3. sticky_reason：可读说明，供调度器和审计使用（如 "has_local_worktree"）

缺省值为 affinity_mode = none，不影响现有单节点部署。

其中 prompt_plan 至少必须包含：

1. prompt_pack_id
2. role_overlay_id
3. enabled_provider_ids
4. prompt_budget_policy

provenance 至少必须包含：

1. blueprint_schema_version
2. resolver_build_version
3. resolver_catalog_digest
4. provider_set_digest

### 5.3 RuntimeEvent

RuntimeEvent 是唯一事实来源。

建议事件族：

1. session_created
2. turn_started
3. turn_completed
4. prompt_materialized
5. tool_called
6. tool_completed
7. approval_requested
8. approval_resolved
9. permissions_amended
10. context_compacted
11. session_forked
12. checkpoint_created
13. session_completed
14. session_failed
15. task_started
16. task_completed
17. task_abandoned
18. role_transitioned
19. plan_updated
20. memory_consolidated
21. budget_exhausted
22. subagent_spawned
23. subagent_completed

budget_exhausted 约束：

1. TurnEngine 在任何预算（token / step / time）耗尽时必须先写 budget_exhausted 事件再停止执行，不得静默截断；
2. budget_exhausted 必须携带 budget_kind（token / step / time）、consumed_value、limit_value；
3. budget_exhausted 是可审计事件，UI 与 review 页面必须能展示。

turn_completed 约束：

1. 每个 turn 必须以 turn_completed 事件结束，无论结束原因是模型停止输出、审批挂起还是 budget 耗尽；
2. turn_completed 必须携带 turn_outcome 字段，取值：completed（正常结束）/ suspended_for_approval（等待审批）/ budget_exhausted（预算耗尽中断）/ error（异常中断）；
3. turn_completed 必须携带 model_response_ref，指向本 turn 模型原始响应内容的可寻址引用（包含模型输出文本及工具调用声明），以便 replay 和 review 时重建模型当时说了什么；
4. 若 turn 以 error 结束，turn_completed 必须携带 error_kind，并与 session_failed（如有）形成因果关联；
5. turn_started 和 turn_completed 之间的 seq 范围定义了该 turn 的事件边界，projection engine 在重放时必须以此边界为单位处理 turn 内部事件顺序。

session_failed 约束：

1. runtime 因不可恢复错误（如 EventStore 写入失败、blueprint 校验失败、TurnEngine panic recovery）中断时，必须写 session_failed 事件；
2. session_failed 必须携带 error_kind、error_message、last_seq（中断时最后成功写入的事件 seq）；
3. replay 时必须能通过 session_failed 区分"正常完成"与"意外中断"，不允许让事件流在某个 turn 事件后静默断掉；
4. session_failed 之后不允许再追加新事件，除非通过 session_forked 或 session_created 开启新 lineage。

subagent_spawned / subagent_completed 约束：

1. agent A 派生 agent B 时，agent A 的 session 必须写 subagent_spawned 事件，携带 child_session_id、parent_task_event_id（派生任务的 task boundary 事件 id）；
2. agent B 的 session 创建时，session_created 事件必须携带 parent_session_id，以建立父子 session 关联；
3. agent B 完成后，agent A 的 session 必须写 subagent_completed 事件，携带 child_session_id、result_ref、outcome（success / failed / abandoned）；
4. subagent 派生链必须可从事件流完整追溯，不允许只在内存或调度层维护父子关系。

task_completed / task_abandoned 的 promote_scoped_items 约束：

1. task_completed 和 task_abandoned 必须携带 `promote_scoped_items` 字段，类型为列表，每条包含：item_id、promote（bool）、summary（可选，promote 为 true 时必填）；
2. promote = true 的 item 必须在对应事件写入后由 projection engine 提升为 persistent history item；
3. promote = false 的 item 由 projection engine 标记为 retired，不再出现在 session 上下文中；
4. 若 task_completed / task_abandoned 未携带 promote_scoped_items，projection engine 必须把该 task 下所有 task-scoped item 默认 retire（不提升），不得静默保留。

plan_updated 约束：

1. update_plan 工具执行成功后必须写 plan_updated 事件，携带完整 planning state 快照；
2. planning state 只允许从 plan_updated 事件 projection 读取，不得再从 session key-value 内存独立维护；
3. session resume / fork / checkpoint replay 时，planning state 必须从事件流中的最新 plan_updated 事件重建；
4. plan_updated 事件必须携带 task_boundary_event_id（若当前在某 task 内）以建立关联。

memory_consolidated 约束：

1. 任何会写入跨 session MemoryStore（如 memory_save 工具）的操作完成后必须写 memory_consolidated 事件；
2. memory_consolidated 事件必须携带 memory_record_id、memory_path、session_id；
3. 这样 resume 时可从事件流得知该 session 产生了哪些持久记忆，fork 和 review 可据此决定是否继承；
4. 跨 session MemoryStore 本身不属于 EventStore，memory_consolidated 只起审计与关联作用，不替代 MemoryStore 的存储职责。

task 事件与 planning Item 的对应关系：

1. task_started 对应 planning.Item 状态从 pending 切换到 in_progress；
2. task_completed 对应 planning.Item 状态切换到 completed；
3. task_abandoned 对应 planning.Item 状态切换到 blocked 或被移除；
4. planning.Item 是用户可见的任务语义单元，task_started 等事件是对应的运行时事实；
5. planning state 中 Item 的状态变更必须通过写 plan_updated 事件完成，不得绕过事件直接修改投影；
6. task_started 事件必须携带对应的 planning_item_id（若 task 来自某个 planning.Item），以建立双向关联。

role_transitioned 约束：

1. role_transitioned 只允许在 collaboration_contract 定义了对应 transition rule 的情况下发生；
2. role_transitioned 事件必须携带 from_role_overlay_id、to_role_overlay_id 与 transition_reason；
3. role_transitioned 发生后，PromptCompiler 必须在下一轮物化时使用新的 role overlay，并在 prompt_materialized 事件中体现；
4. 不需要通过 fork 来切换角色；fork 只用于需要独立 session lineage 的场景。

要求：

1. 每个事件必须带 session_id、seq、timestamp、event_type；
2. 关键事件必须带 blueprint hash 或 policy hash；
3. prompt 与权限类事件必须能追溯其来源 provider 或 compiler。

模型调用相关事件至少必须带：

1. prompt_materialized_id
2. prompt_hash
3. policy_hash
4. provider_id（实际使用的 LLM provider 标识，如 openai / claude / gemini 及具体 endpoint）
5. model_id（实际调用的模型名称，包含 failover 后实际生效的模型）

恢复相关硬约束：

1. session_created 必须持久化 canonical blueprint payload；
2. session_forked 必须持久化 fork 后 canonical blueprint payload；
3. checkpoint_created 必须持久化 checkpoint 对应的 canonical blueprint payload 或其稳定引用；
4. resume、fork、checkpoint replay 默认只读取持久化 blueprint 与事件流；
5. RuntimeRequest 仅用于新建会话或显式 re-resolve，不得作为默认恢复来源。

checkpoint 边界要求：

1. checkpoint_created 必须记录 event boundary；
2. replay 默认只重放到该 boundary，不得偷读 checkpoint 之后的新事件；
3. 若要在 replay 后继续接上新事件，必须先形成新的 session lineage，再写新的 session_created 或 session_forked 事件；
4. checkpoint_created 必须可选携带 workspace_snapshot_ref（git commit hash 或 worktree snapshot id），以支持 checkpoint replay 时同时恢复代码工作区状态；
5. workspace_snapshot_ref 只存引用键，不存内容本身；内容由 Sandbox / git 层维护；EventStore 不承担工作区内容的存储职责。

### 5.4 MaterializedState

MaterializedState 是从事件流投影出的运行时状态。

职责：

1. 提供 session 当前视图；
2. 提供 prompt debug、resume summary、checkpoint summary、review summary；
3. 不得越权成为事实来源。

### 5.5 恢复与物化契约

本设计把恢复与 prompt 物化收紧为一套明确契约。

#### 恢复契约

1. 新建 session：由 RuntimeRequest 经过 resolver 生成 canonical blueprint；
2. resume：默认读取持久化 blueprint + event stream；
3. fork：默认复制或派生新的 canonical blueprint，再写 fork 事件；
4. checkpoint replay：默认读取 checkpoint 固定下来的 canonical blueprint + event stream；
5. 只有用户显式要求 re-resolve 时，系统才允许重新走 RuntimeRequest -> resolver。

#### 物化契约

1. persistent history item 会进入可重放 history，并影响未来 turn；
2. task-scoped history item 在 task 存活期间跨轮可见，task retire 后按决策写入或丢弃；
3. turn-local materialization layer 只影响当前 turn，不写入可重放 history；
4. prompt_materialized 事件必须记录本次真正采纳的 history snapshot、selected layer ids、content hash、budget snapshot、provider provenance；
5. prompt_materialized 事件必须记录 truncated_layer_ids，即因 budget 超限而被截断或丢弃的 layer id 列表；
6. 实际模型调用事件必须显式回指对应的 prompt_materialized_id 或等价强关联键；
7. 任何 UI 或 review 页面都必须优先展示 prompt_materialized 的审计结果，而不是自行重算 prompt。

## 6. 单一路径执行模型

目标执行链如下：

1. Entry Adapter 将外部输入转换为 RuntimeRequest；
2. Request Resolver 将 RuntimeRequest 编译为 SessionBlueprint；
3. Kernel 根据 SessionBlueprint 初始化 session aggregate；
4. Prompt Compiler 从 SessionBlueprint 与当前 MaterializedState 生成唯一模型输入；
5. Turn Engine 执行模型调用、工具调用、审批挂起与恢复；
6. 所有状态变化先写 RuntimeEvent，再异步或同步更新投影；
7. 产品层只读取投影结果显示给用户。

禁止流程：

1. 入口层直接拼 prompt；
2. 入口层直接套用 approval mode 到 kernel；
3. TUI 或 exec 在 session 创建后再补写 posture；
4. 持久化层把整份 session 快照当作真相源。

## 7. 四个核心机制的唯一实现路径

### 7.1 权限机制

只保留一套 EffectiveToolPolicy。

设计要求：

1. 静态 permission profile、workspace trust、session grant、project amendment 必须经过同一个 PolicyCompiler 合并；
2. 执行侧工具授权只消费 EffectiveToolPolicy；
3. 模型侧权限说明只从 EffectiveToolPolicy 渲染，不允许再只显示 read-only、workspace-write 这类标签；
4. approval_requested 与 approval_resolved 事件必须携带 policy hash；
5. permissions_amended 必须是显式事件，不允许隐式修改内存态。

结论：approval mode 不再是运行时概念，只是入口输入的语法糖。

### 7.2 Prompt 机制

只保留一套 PromptCompiler。

PromptCompiler 只能消费结构化 PromptLayerProvider 输出。

产品场景差异只允许通过两种结构化输入进入 PromptCompiler：

1. PromptPack
2. RoleOverlay

二者职责必须正交。

#### PromptPack

PromptPack 解决“这个产品或会话场景总体是做什么的”。

它负责提供产品级基线指令，例如：

1. mosscode 对应 coding pack；
2. mosswork 对应 desktop pack。

PromptPack 允许表达：

1. 产品目标
2. 领域约束
3. 输出风格基线
4. 产品级工作流契约

PromptPack 不允许表达：

1. 当前 agent 的具体角色
2. 当前 turn 的权限状态
3. 当前 session 的临时上下文

#### RoleOverlay

RoleOverlay 解决“同一产品内当前 agent 正在扮演什么角色”。

例如：

1. mosswork manager
2. mosswork worker
3. mosscode root
4. mosscode review
5. mosscode planner

RoleOverlay 允许表达：

1. 角色目标
2. 角色边界
3. 角色级协作方式
4. 对同一 PromptPack 的局部覆写

RoleOverlay 不允许表达：

1. 独立于 PromptPack 存在的完整产品基线
2. 绕过统一权限说明机制的额外授权
3. 绕过 ContextProjector 的临时上下文拼接

#### 子 Agent 约束

子 agent 不得直接传 raw system prompt。

子 agent 只能通过 child blueprint 指定：

1. prompt_pack_id
2. role_overlay_id
3. collaboration_mode
4. 其他结构化 runtime inputs

collaboration_contract 允许定义 role transition rules，格式为：

1. from_role
2. to_role
3. trigger_condition
4. transition_type（immediate / next_turn_boundary）

同一 session 内的角色切换通过 role_transitioned 事件完成，不需要 fork。

结论：同一产品内 manager / worker / reviewer / planner 这类差异，必须通过 RoleOverlay 解决，而不是再长出第二套 prompt 模板拼接机制；同一 session 内的动态角色切换通过 role_transitioned 事件完成，不增加 session lineage 代价。

PromptLayerProvider 最小结构：

1. layer_id
2. scope
3. priority
4. content_parts（类型为 []ContentPart，对齐 model.ContentPart，支持文本、图片、文件等多模态内容）
5. dedupe_key
6. provenance
7. persistence_scope

说明：content_parts 替代旧的 content_kind + content 单字段设计。ContentPart 是已有的 model 层类型，PromptLayerProvider 直接复用，不另起标准。纯文本 layer 使用单个 TextPart；含图片或文件附件的 layer 使用多个 ContentPart；PromptCompiler 在物化时负责按模型 API 格式展平。

ContextInjector 适配要求：

1. 现有 ContextInjector 接口返回 string，必须通过适配层包装为单个 TextPart 的 PromptLayerProvider，再交由 PromptCompiler 处理；
2. 不得在 PromptCompiler 之外直接把 ContextInjector 输出拼入模型 prompt；
3. 长期演进方向是 ContextInjector 直接返回 []ContentPart，但在接口迁移完成前，适配包装是唯一合法过渡路径。

persistence_scope 只允许三类值：

1. persistent
2. task-scoped
3. turn-local

task-scoped layer 必须额外携带 task_boundary_event_id，用于绑定到对应的 task boundary 事件。

允许的 provider 来源：

1. bootstrap
2. capability
3. memory
4. planning
5. collaboration contract
6. permissions summary
7. runtime notices
8. prompt pack
9. role overlay

memory provider scope 规则：

1. 跨 session 长期记忆召回（来自持久 MemoryStore 的 recall）默认使用 persistent scope，代表该 memory 对整个会话历史有意义；
2. 仅对当前任务有效的临时 memory context 使用 task-scoped scope，并绑定当前 task boundary 事件；
3. 单轮相关性检索结果（如 RAG 召回的片段）使用 turn-local scope；
4. 实现必须为每次注入的 memory layer 显式声明 scope，不得留空由实现自行决定。

planning provider scope 规则：

1. planning state layer（plan 结构、item 列表、current_focus）使用 persistent scope，代表 plan 在整个 session 历史中始终可见；
2. 单个 task 内的执行进度提示使用 task-scoped scope；
3. planning provider 必须从 plan_updated 事件的 projection 读取 planning state，不得从 session key-value 内存独立读取；
4. 若当前 session 无任何 plan_updated 事件，planning provider 不输出 layer，而不是输出空内容。

禁止：

1. raw string prompt hook；
2. 产品层自行二次拼接 system prompt；
3. 模型输入和 prompt debug 来自不同路径。
4. 产品层以 raw system prompt 形式把 manager、worker、reviewer 等角色模板直接塞进 session。

额外要求：

1. prompt_materialized 事件必须记录 prompt hash；
2. prompt_materialized 事件必须记录 selected layer ids 与 layer hash；
3. prompt_materialized 事件必须记录 budget snapshot 与 compiler provenance；
4. prompt_materialized 事件必须记录 truncated_layer_ids，列出因 budget 超限被截断或丢弃的 layer；
5. 同一轮若发生重试，必须能区分是同一 prompt 重发还是 prompt 已发生变化。

budget 保障层约束：

1. bootstrap layer 不可被 budget 截断；
2. permissions summary layer 不可被 budget 截断；
3. prompt pack base layer 不可被 budget 截断；
4. role overlay layer 不可被 budget 截断；
5. 以上四类 layer 的 priority 在 PromptCompiler 内部必须高于所有其他 layer，且对 budget policy 免疫。

产品层约束：

1. mosscode / mosswork 只能选择 prompt_pack_id 与 role_overlay_id；
2. 产品层不得构造最终 system prompt；
3. 产品层不得通过自定义模板旁路覆盖 PromptCompiler；
4. 若产品需要新增使用场景，必须新增 PromptPack 或 RoleOverlay，而不是新增另一套 prompt 组装路径。

### 7.3 上下文机制

上下文机制只保留一条编译路径，但允许三种合法输出：persistent history item、task-scoped history item 与 turn-local materialization layer。

设计要求：

1. ContextProjector 是唯一允许把运行时状态转换成模型可见上下文的组件；
2. startup context、可重放 memory context、可重放 planning context 必须进入 persistent history；
3. 任务范围内的中程上下文（如任务进度、中间推理结论）进入 task-scoped history，绑定 task boundary 事件；
4. planning state 是 persistent history 的一部分，通过 plan_updated 事件 projection 维护，不是独立的 session key-value 侧信道；
5. recall、runtime notice、预算告警等当前轮临时上下文可以生成 turn-local layer；
6. compaction 只能压缩 persistent history，不允许同时维护 fragment state；
7. compaction 结果必须写入 context_compacted 事件，并在 history 中留下明确 summary item；
8. resume、fork、checkpoint replay 必须能从事件重建相同 persistent history 与对应的 task-scoped history；
9. task-scoped item 必须随 task_completed 或 task_abandoned 事件被 retire，retire 决策通过 promote_scoped_items 字段写入事件，由 projection engine 统一执行，不得由各 projector 分散实现；
10. turn-local layer 不得被偷偷提升为持久状态，除非有明确 RuntimeEvent 与对应 projector 规则；
11. projection engine 在 replay 结束后必须执行不变量检查：若存在 bound_to_task_event_id 对应的 task 已通过 task_completed / task_abandoned 事件结束，但 task-scoped item 仍为 active 状态，必须报错，不得静默通过。

layer 提升规则：

persistence_scope 声明了 layer 的生命周期意图，turn 结束后 ContextProjector 在处理 turn_completed 事件时必须执行以下提升动作：

1. persistence_scope = persistent 的 layer：必须作为 persistent history item 写入，影响后续所有 turn；
2. persistence_scope = task-scoped 的 layer：必须作为 task-scoped history item 写入，绑定当前 task boundary 事件，task retire 前跨轮可见；
3. persistence_scope = turn-local 的 layer：turn_completed 后丢弃，不写入 history，不影响后续 turn；
4. 提升动作必须在 turn_completed 事件写入后由 projection engine 触发，不得在 turn 内部提前提升；
5. 任何绕过 turn_completed 事件直接把 turn-local layer 提升为 history 的行为都是违反本设计的旁路。

结论：fragment 式 PromptContextState 不再作为长期架构保留；prompt 物化只允许通过 ContextProjector + PromptCompiler 完成。

### 7.4 持久化机制

运行时持久化只允许通过统一的 EventStore 接口写入和读取，不允许绕过接口直接访问底层存储。

EventStore 接口是唯一的事实层入口。

提供两种标准实现：

1. SQLite 实现：面向生产和长期会话场景，支持高效 projection rebuild 与多 session 并发；
2. JSONL 实现：面向测试、轻量场景与单会话 export / import。

两种实现必须满足相同的接口契约，runtime 不得感知底层实现差异。

JSONL 实现的适用约束：

1. 不适合并发多 session 写入；
2. 不支持高效的 projection rebuild；
3. 适合测试套件与单 session debug；
4. 可作为 export / import 的实现载体。

禁止：

1. 绕过 EventStore 接口直接读写底层存储文件；
2. 在同一 session 内混用两种实现；
3. runtime 通过检测底层实现类型推导运行时行为。

建议最小存储结构：

1. sessions
2. session_events
3. session_views
4. checkpoint_views
5. review_views

checkpoint_views 至少必须持有：

1. blueprint reference
2. event boundary
3. lineage metadata

说明：session_views 等 projection 表允许重建，允许丢弃后从事件流重新 materialize。

## 8. 分层与归属

### 8.1 Kernel

Kernel 持有以下核心组件：

1. RequestResolver
2. PolicyCompiler
3. PromptCompiler
4. ContextProjector
5. TurnEngine
6. EventStore interface
7. Projection interfaces

Kernel 同时持有 versioned resolver catalogs 的解释权。catalog 的结构、默认值、版本与 digest 均属于 kernel 语义层。

Kernel 是运行时语义的唯一归属层。

### 8.2 Harness

Harness 只负责：

1. 注册 provider；
2. 安装扩展；
3. 连接底层设施；
4. 组合默认装配。

Harness 不再拥有运行时真相源，不得直接拼 prompt、直接决定 posture、直接定义持久化真相格式。

Harness 可以提供 catalog 数据源，但不得定义语义默认值，不得覆写 resolver 规则，不得让同一 RuntimeRequest 在不同 harness bundle 下 resolve 出不同 blueprint。

### 8.3 Product

Product 层只负责：

1. 输入适配；
2. 视图展示；
3. 用户交互；
4. 调用 runtime API。

Product 层不得：

1. 解释权限真相；
2. 维护独立 prompt 路径；
3. 维护独立 resume 语义；
4. 用 UI state 补丁修复 runtime posture。

Product 层只能传递标识符与输入，不得携带第二套 registry 解释逻辑。

## 9. 删除与替换清单

1. 删除 approvalMode 作为运行时真相源。  
   替换为入口层权限快捷别名解析 + PolicyCompiler。

2. 删除双 prompt 路径。  
   替换为 PromptCompiler + PromptLayerProvider 注册表。

3. 删除 fragment 式 PromptContextState 作为长期上下文机制。  
   替换为 ContextProjector + 统一 history item。

4. 删除“先 build runtime，再回填 session posture”的流程。  
   替换为先 resolve blueprint，再创建 session。

5. 删除整份 session snapshot 反复追加作为主持久化方案。  
   替换为 SQLite 事件仓库 + projection 表。

6. 删除产品层对 prompt、permission、resume posture 的直接解释权。  
   替换为产品层纯消费 SessionView、ResumeView、ReviewView。

## 10. EventStore 接口约束

EventStore 是唯一允许写入和读取运行时事实的接口。所有实现必须满足本节约束。

最小接口定义：

1. AppendEvents(session_id, expected_seq, request_id, events)
2. LoadEvents(session_id, after_seq)
3. LoadSessionView(session_id)
4. RebuildProjections(session_id)
5. ListResumeCandidates(filter)
6. Export(session_id, format) → 用于 JSONL / audit 输出
7. Import(session_id, source, format) → 用于 JSONL 导入

预留集群化扩展方法（当前实现可不支持，但接口设计不得阻塞其后续添加）：

8. SubscribeEvents(session_id, after_seq) → <-chan RuntimeEvent → 流式事件推送，供集群内跨节点实时感知

所有实现的共同一致性要求：

1. 事件追加必须原子化；
2. 同一 session 的 seq 必须严格单调；
3. 关键 projection 更新必须与事件写入有明确因果关系；
4. projection 出错时允许延迟修复，但不得影响事实层写入；
5. runtime 不允许从 projection 反写事实层；
6. AppendEvents 必须支持 optimistic concurrency；
7. request_id 或 turn_id 必须提供幂等保障，避免重试写出重复事件；
8. prompt_materialized 必须带 prompt hash、selected layer ids、budget snapshot、provider provenance；
9. approval 与 permission amendment 事件必须带 policy hash；
10. event store 不得接受无 blueprint version 的可恢复事件；
11. 一次 AppendEvents 调用允许同时原子追加多条事件（如 task_completed 同时触发 plan_updated），多条事件的 seq 必须连续递增，由 EventStore 实现负责分配，调用方不得预设具体 seq 值；多条事件的幂等保障必须基于整批的 request_id，不得仅对单条事件保障。

SQLite 实现额外要求：

1. 事件追加必须在事务内完成；
2. projection 表允许重建，允许延迟物化；
3. 多 session 并发写入必须通过行级锁或 WAL 隔离。

JSONL 实现额外要求：

1. 每次 AppendEvents 必须以追加方式写入，不得覆盖已有行；
2. seq 必须在读取时校验；
3. LoadSessionView 允许在加载时全量 replay 构建；
4. RebuildProjections 必须支持但允许全量扫描实现；
5. 不得用于多 session 并发写入场景。

## 11. 测试基线

### 11.1 编译一致性

1. 相同 RuntimeRequest 在不同入口必须生成相同 SessionBlueprint；
2. blueprint hash 必须稳定；
3. PolicyCompiler 与 PromptCompiler 的输出必须可重复。

### 11.2 执行一致性

1. 模型侧权限说明必须与执行侧 EffectiveToolPolicy 对应；
2. 一次 grant 或 amendment 后，模型侧与执行侧必须同步变化；
3. prompt input 只能由 PromptCompiler 生成；
4. 每个 turn_started 必须对应一个 turn_completed，不得出现有 turn_started 但无 turn_completed 的悬挂 turn；
5. turn_completed.model_response_ref 必须可寻址，review 页面必须能通过该引用重建模型原始响应。

### 11.3 可恢复性

1. 同一事件流必须能重建相同 SessionView；
2. resume、fork、checkpoint replay 不得依赖产品层补丁；
3. 删除 projection 后必须能从事件重建；
4. task 结束后（task_completed 或 task_abandoned），该 task 下所有 task-scoped item 必须不再出现在 session 上下文中；replay 结束后如有 active task-scoped item 对应的 task 已结束，必须报告不变量违反错误；
5. session_failed 必须能被 replay 识别并以"意外中断"状态呈现，不得与正常完成混淆；
6. turn-local layer 在 turn_completed 后必须不再出现在任何后续 turn 的上下文中；persistence_scope = persistent 的 layer 必须在 turn_completed 后作为 persistent history item 可重建。

### 11.4 反旁路测试

1. 产品层不得直接写 session messages 影响模型输入；
2. 不允许注册 raw string prompt hook；
3. 不允许绕过 EventStore 接口直接访问底层存储文件；
4. 不允许 runtime 从 approval mode 推导最终 policy；
5. 不允许在同一 session 内混用两种 EventStore 实现；
6. subagent 派生关系不得只存在于内存或调度层，必须通过 subagent_spawned / subagent_completed 事件可追溯；
7. task-scoped item 的 bound_to_task_event_id 必须写入 EventStore，不得仅在内存中约定。

## 12. 迁移阶段

虽然不考虑向后兼容，但实现上仍建议分阶段 cutover，避免在重构过程中长期维持双路径。

### 阶段 1：建立新真相源

1. 引入 RuntimeRequest、SessionBlueprint、RuntimeEvent、SessionView；
2. 引入 EventStore 接口及 SQLite 实现，JSONL 实现可同步或稍后跟进；
3. 建立 PolicyCompiler、PromptCompiler、ContextProjector 接口；
4. EventStore schema 必须在本阶段包含 `bound_to_task_event_id` 字段（task-scoped item 绑定声明）以及 task_completed / task_abandoned 的 `promote_scoped_items` 字段，不得推迟到后续阶段；
5. projection engine 的 task-scoped retire 逻辑与不变量检查必须在本阶段实现，防止 task-scoped 泄漏问题在新路径中重演。

### 阶段 2：先接管恢复面

1. resume、fork、checkpoint、review 的 reader 先切到事件仓库与 projection；
2. 建立新 blueprint 持久化与恢复契约；
3. 在此阶段结束前，不允许新 runtime writer 对旧恢复链路可见。

### 阶段 3：原子接管主链路

1. exec 与 TUI 改为先 resolve blueprint，再创建 session；
2. 权限与 prompt 改为完全消费新编译器；
3. 持久化改为事件写入 + projection 读取。

要求：主链路 writer 切换与恢复 reader 切换必须同一阶段完成，不允许出现“新 writer + 旧 reader”长期并存窗口。

### 阶段 4：删除旧路径

1. 删除 approval mode runtime application；
2. 删除 raw PromptAssembler string hook 模式；
3. 删除 fragment prompt context；
4. 删除 snapshot-first session store 作为事实层。

### 阶段 5：收紧外部格式

1. JSONL 作为 EventStore 实现仅用于测试和轻量场景，export / audit / debug 通过 EventStore.Export 接口统一输出；
2. 产品层彻底停止 posture patch-up；
3. 所有 review 与 debug 页面改为读取 prompt_materialized 审计结果。

## 13. 最终结论

moss 下一阶段不应该继续在现有路径上修补，而应该直接切换到**以内核为中心的结构化单路径 runtime**。

核心判断如下：

1. 权限只保留一套 EffectiveToolPolicy；
2. prompt 只保留一套 PromptCompiler；
3. 上下文只保留一份统一 history；
4. 持久化只允许通过 EventStore 接口访问事实层，接口提供 SQLite 和 JSONL 两种实现；
5. harness 与 product 均不得再拥有运行时语义真相。

这不是“优化现有实现”的级别，而是一次必要的 runtime 收口。若继续允许双路径并存，moss 会持续表现为：入口越多，模型越笨，恢复越弱，解释性越差。

## 14. 子系统对接契约

本节记录各子系统与新设计的对接要求。未计入本节的子系统不得自行创造与主设计并行的第二套路径。

### 14.1 Observer 可观测性子系统

Observer 是 EventStore 写入的下游派生，不是独立的真相源。

要求：

1. Observer 回调（LLMObserver、ToolObserver、ApprovalObserver、SessionObserver 等）必须在 EventStore.AppendEvents 成功后再触发；
2. 不得先触发 Observer 再写事件；
3. Observer 输出（OTel span、Prometheus metrics、slog 日志）是事件的派生视图，不得成为另一个真相源；
4. Observer 分层实现与事件投影输出之间必须有一致的事件标识关联（如 session_id + seq），不得两套标识体系。

### 14.2 Guardian 审批子系统

Guardian 是 LLM-as-reviewer 自动审批机制，必须纳入 approval 事件流，不得旁路。

要求：

1. Guardian 必须是 EffectiveToolPolicy 的消费者，不得绕过 PolicyCompiler 直接做出权限判断；
2. Guardian 审批结果必须通过写 approval_resolved 事件入库；
3. approval_resolved 必须携带 resolver_type 字段，取值：human / guardian / policy，以便审计区分人工审批与自动审批；
4. Guardian 自身不得写事件之外的审批决策到内存；
5. 若 Guardian 不可用，必须 fallback 到 human approval，不得默认自动放行。

### 14.3 MCP 外部工具子系统

MCP 工具通过 MCPServer 桥接到本地 ToolRegistry，必须纳入 EffectiveToolPolicy 管辖范围。

要求：

1. MCP 工具必须在 PolicyCompiler 阶段纳入 EffectiveToolPolicy，不得通过默认限制外的单独授权机制运行；
2. tool_called / tool_completed 事件对 MCP 工具必须额外携带 mcp_server_id 和 mcp_tool_name，以与本地工具区分；
3. MCPServer 的 ToolGuard 负责 I/O 校验与约束（输入大小、JSON 深度等），PolicyCompiler 负责访问决策与审计；二者职责正交，不得混用；
4. MCP 工具的权限说明必须从 EffectiveToolPolicy 渲染，不得单独对模型描述未经 PolicyCompiler 类型的权限。

### 14.4 Budget 预算子系统

Budget Governor 负责 token / step / time 预算管理，其行为必须可观测。

要求：

1. 预算耗尽必须写 budget_exhausted 事件（见 5.3 节），不得静默截断或退出；
2. SessionBlueprint.context_budget 必须区分 main_token_budget 与 thinking_token_budget，支持 extended thinking 的独立计量；
3. prompt_materialized 事件的 budget_snapshot 必须同时记录主 token 消耗与 thinking token 消耗；
4. Budget Governor 的预算分配结果必须在 SessionBlueprint 编译阶段确定，不得在运行时隐式修改。

### 14.5 调度子系统

定时调度触发的 session 必须通过标准 RuntimeRequest 路径创建。

要求：

1. 调度触发必须构造标准 RuntimeRequest，run_mode 设为 scheduled，不得绕过 RequestResolver 直接创建 session；
2. session_created 事件必须携带 trigger_source 字段，取值：interactive / scheduled / api / resume，以区分交互式 session 与定时 session；
3. 调度 session 的 SessionBlueprint 必须包含完整运行时语义，不得简化。

### 14.6 分布式 TaskRuntime 子系统

多 worker 场景下 task 认领必须通过 EventStore 的 optimistic concurrency 解决冲突。

要求：

1. task_started 事件必须携带 claimed_by 字段（worker id）；
2. task_completed / task_abandoned 必须携带 result_ref（执行结果的可寻址引用）；
3. 认领冲突必须在 AppendEvents 的 optimistic concurrency 层解决，不得由 HTTP 层静默覆盖；
4. RemoteTaskRuntime 作为 HTTP 适配层，必须把认领操作映射为带 expected_seq 的 AppendEvents 调用，不得在 HTTP 层绯过 EventStore 序列化语义。

### 14.7 Sandbox / 工作区快照子系统

checkpoint 快照必须支持工作区状态的可选关联。

要求：

1. checkpoint_created 必须可选携带 workspace_snapshot_ref（见 5.3 节）；
2. Sandbox 层的 git snapshot 、patch journal 、worktree 快照必须提供稳定的引用键（如 git commit hash）供 EventStore 引用；
3. checkpoint replay 时如需恢复工作区，必须先进行 workspace_snapshot_ref 验证，恢复失败时必须记录明确错误事件，不得静默回退到不一致状态。

### 14.8 Provider Failover 子系统

模型调用的 provider 选择和 failover 必须对审计可见。

要求：

1. 模型调用事件必须携带 provider_id 和 model_id（见 5.3 节）；
2. failover 发生时，必须写一个辅助字段或附加属性记录 original_provider_id，以便 audit 能区分首选 provider 与实际使用 provider；
3. provider 路由策略（router_config）必须在 SessionBlueprint.model_config 内声明，不得在运行时隐式修改。

### 14.9 Skills 子系统

Skill 是 PromptPack 的一种实现形式，不是独立的 prompt 注入机制。

要求：

1. skill 注册后必须以 PromptPack provider 的形式贡献 layer，skill_id 对应 prompt_pack_id；
2. skill 不得绵过 PromptCompiler 直接拼接 system prompt；
3. skill 的内容可以包含 markdown 指令或结构化 layer，不得包含未经 PolicyCompiler 授权的工具声明；
4. 若 skill 需要注入工具，必须通过 capability provider 注入，不得直接写入 ToolRegistry。

### 14.10 Thinking （Extended Thinking）子系统

Extended thinking 有独立的 token 预算计量，必须在预算与审计两个维度与主 token 区分。

要求：

1. SessionBlueprint.context_budget 必须包含 thinking_token_budget 字段，与主 token budget 独立设置与计量；
2. prompt_materialized 事件的 budget_snapshot 必须同时记录 main_tokens 和 thinking_tokens 两个维度的已用和剩余值；
3. thinking budget 耗尽必须写独立的 budget_exhausted 事件（budget_kind = thinking_token），不得与主 token 耗尽事件合并；
4. Thinking 扩展的启用与禁用必须在 SessionBlueprint.model_config 中声明，不得在运行时隐式修改。

## 15. Agent 集群化预留扩展点

本节记录新设计对 agent 集群化的影响分析与预留扩展规则。**本节内容为前瞻性指引，不属于当前 POC 或 Phase 1 的交付范围。**

### 15.1 集群化基础：新设计的正向支撑

新设计在以下四个方面天然支持未来集群化：

1. **SessionBlueprint 可序列化**：Blueprint 是纯结构数据，无隐式内存引用，可序列化后发送至任意节点，接收节点无需与原始节点通信即可独立恢复完整运行时语义；
2. **EventStore 接口可替换后端**：AppendEvents + optimistic concurrency 语义与分布式存储（如 PostgreSQL、TiKV、etcd）天然对齐，未来只需替换实现，上层逻辑不变；
3. **无状态编译器可水平扩展**：RequestResolver、PolicyCompiler、PromptCompiler、ContextProjector 均为无状态编译器，可作为独立服务水平扩展，不需要亲和性调度；
4. **task claimed_by 语义**：task_started 的 claimed_by 字段加 AppendEvents optimistic concurrency 构成无锁任务认领协议，可直接用于多 worker 集群。

### 15.2 集群化预留扩展规则

**EventStore 接口**：

1. SubscribeEvents 方法（见 §10）是集群化的第一个必要扩展，当前可不实现，但接口定义不得阻塞其添加；
2. 分布式 EventStore 实现（如 PostgreSQL WAL-based）必须满足 §10 全部一致性要求，并额外保障跨节点 seq 单调性（通过序列生成器或 CAS 实现）；
3. 同一 session 的 seq 单调性在多节点写入时必须由存储层保障，不得由应用层乐观假设。

**SessionBlueprint**：

1. execution_affinity 字段（见 §5.2）是集群调度的声明入口，调度器必须尊重 node_pinned 约束；
2. 持有本地 git worktree 的 sandbox session 必须声明 affinity_mode = node_pinned，不得在多节点间迁移；
3. affinity_mode = none 的 session 可在集群内任意节点调度，调度器可基于负载均衡策略分配。

**Guardian 审批路由**：

1. 集群场景下 approval_requested 事件的路由规则由集群调度层负责，Guardian 实例本身不感知节点拓扑；
2. approval_resolved 的 resolver_type 字段（§14.2）在集群审计中同样有效，不需要额外扩展。

**MemoryStore 共享**：

1. 跨节点共享 MemoryStore 的网络化访问契约不在本 spec 范围内，由 harness/memory 子系统单独定义；
2. memory_consolidated 事件（§5.3）作为审计锚点在集群场景下保持有效，集群各节点写入 memory_consolidated 事件时必须带 session_id，以便跨节点追溯。

### 15.3 当前实现的集群化边界

以下限制在 POC / Phase 1 阶段是可接受的，不得被视为需要立即解决的缺陷：

1. SQLite EventStore 不支持多节点并发写入，仅用于单节点部署；
2. SubscribeEvents 不需要在 Phase 1 实现，Observer 回调可作为临时替代；
3. MemoryStore 暂时是单节点本地存储，跨节点共享留待集群阶段设计。