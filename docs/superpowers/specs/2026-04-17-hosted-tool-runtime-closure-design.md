# Stage 6 设计：Hosted Tool Runtime Closure（只读型 Hosted Tools）

日期：2026-04-17  
状态：Draft / Ready for review

## 1. 背景

阶段 5 已经完成两件关键升级：

- OpenAI Responses 已切换到真正的事件级流式适配；
- 公共流协议已能表达 reasoning、refusal、tool_call、hosted_tool 等 richer event。

当前系统已经可以：

- 在统一流协议中看到 hosted tool 事件；
- 将 hosted tool 作为 assistant transcript 的最终快照保存；
- 在 UI 输出桥中把 hosted tool 作为独立展示事件发出。

但仍存在一个关键缺口：**hosted tool 还不是运行时治理、审计、回放层的一等 execution fact**。

这导致：

- audit / trace / state journal 无法稳定表示 hosted tool 生命周期；
- 回放只能看到最终状态，不能准确恢复中间过程；
- 后续产品策略、风控、统计、debugging 无法稳定依赖 hosted tool 时间线。

## 2. 本阶段目标

阶段 6 聚焦于：**把只读型 hosted tools 纳入完整 runtime 闭环**。

首批范围：

- web_search
- file_search
- image_generation（仅作为 provider 侧托管动作记录，不引入本地执行）

本阶段目标：

1. hosted tool 成为一等 execution event；
2. assistant transcript 与 audit timeline 明确分层；
3. trace / inspect / state journal / audit.jsonl 能稳定记录 hosted tool 生命周期；
4. 不把 hosted tool 错误伪装成普通 tool call；
5. 为后续高风险 hosted tools（如 code_interpreter / computer_use）的审批与策略接口留下扩展点。

## 3. 非目标

本阶段明确不做：

- 高风险 hosted tools 的审批执行闭环；
- 本地沙箱或 computer-use 权限控制；
- 删除现有 StreamChunk 的兼容字段；
- 改写现有 session store 结构；
- 把 hosted tool 折叠回 assistant 文本正文。

## 4. 设计原则

### 4.1 双轨事实源

Hosted tools 保持双轨语义：

- **Transcript 轨**：assistant 消息中保留每轮 hosted tool 的最终状态快照；
- **Execution 轨**：运行时以 execution events 记录完整生命周期。

两者职责分离：

- transcript 回答“这一轮最后发生了什么”；
- execution timeline 回答“运行过程中它是怎么发生的”。

### 4.2 Provider-agnostic 收口

生命周期事件从 kernel loop 发，不从 provider 层直接发。

原因：

- loop 已消费统一 StreamChunk；
- 可避免 OpenAI 特定事件名泄漏到 observe 层；
- 便于 Claude / Gemini / future provider 未来共用同一治理协议。

### 4.3 Hosted tool 不是普通 tool call

Hosted tools 代表 provider 托管动作，而非本地 tool registry 里的函数调用。

因此：

- 不复用本地 tool.started / tool.completed 的产品语义；
- 不伪装成 tool result；
- 在 execution event 中通过 payload_kind=hosted_tool 显式区分。

## 5. 核心设计

### 5.1 新增 execution 事件类型

在 execution event 总线中新增 hosted tool 生命周期类型：

- hosted_tool.started
- hosted_tool.progress
- hosted_tool.completed
- hosted_tool.failed

这些事件统一走现有 ExecutionEvent 基础设施，无需新建 observer 总线。

### 5.2 事件字段约定

继续复用现有 ExecutionEvent 结构：

- phase = llm
- actor = provider
- payload_kind = hosted_tool
- tool_name = hosted tool 名称
- call_id = hosted tool 实例 ID
- event_id = 每一次事件的唯一 ID

metadata 里补充：

- status：原始或归一化状态
- provider：如 openai-responses
- read_only：是否为只读型 hosted tool
- input：有则保留
- output：有则保留
- raw_event_type：provider 原始事件名
- synthetic_terminal：是否为 loop 收尾时补发的合成终态

### 5.3 生命周期归一化

在 loop 层提供统一状态映射函数，将 provider 的原始状态统一归类为稳定语义：

- searching / in_progress / interpreting → progress
- completed / done → completed
- failed / errored / cancelled → failed
- 首次看到某个 hosted tool 实例 → started

这样不同 provider 即便事件命名不同，也能落成统一治理语义。

### 5.4 去重与状态推进

loop 内维护 hosted tool 运行态索引：

- 主键优先使用 hosted tool ID；
- 没有 ID 时回退到名称；
- 重复进度事件做轻量去重；
- 只有状态推进时才发出新的 execution event。

这样可以避免 timeline 被重复的 progress 噪音刷满。

### 5.5 UI/transport 语义

保留现有 assistant.hosted_tool 作为面向交互展示的轻量事件。

同时在 runtime / transport 语义中新增 hosted tool 生命周期事件，供：

- trace timeline
- inspect
- audit
- state catalog
- 后续策略与治理面板

统一消费。

## 6. 错误语义

### 6.1 hosted tool 失败不等于 run 失败

某个 hosted tool 实例失败，只表示这一步 provider 托管动作失败；
不自动等价于整个 run 或整个 LLM turn 失败。

只有当 provider 因该失败直接中断本轮生成时，才进一步落到 llm.completed/error 或 run.failed。

### 6.2 终态保证

每个 hosted tool 实例必须最终进入：

- completed；或
- failed

如果流中只出现中间状态，但本轮 LLM 已结束，则 loop 在收尾时补发一个合成终态事件，并在 metadata 中标记 synthetic_terminal=true。

### 6.3 实例 ID 与事件 ID 分离

必须区分：

- event_id：单条生命周期事件唯一标识；
- call_id：同一个 hosted tool 实例在 started/progress/completed/failed 之间的关联键。

这可以避免 state journal / trace timeline 互相覆盖或误聚合。

## 7. 持久化与回放

### 7.1 Session 持久化

现有 session JSONL 快照继续只保存 assistant transcript 的最终状态，不改结构。

assistant message 中的 HostedToolCalls 继续作为“每轮最终快照”。

### 7.2 审计持久化

audit.jsonl、trace timeline、state journal 已经消费 ExecutionEvent，因此阶段 6 只需要发出稳定 hosted tool execution events，持久化链路即可自动获得支持。

### 7.3 回放优先级

回放和诊断时采用如下优先级：

1. 优先读取 execution timeline，恢复 hosted tool 生命周期；
2. 没有 timeline 时，再回退到 transcript 里的 HostedToolCalls 最终快照；
3. 两者冲突时，以 execution timeline 为准。

## 8. 影响面

### 8.1 预计主要改动文件

- kernel/observe/execution_event.go
- kernel/loop/loop_llm.go
- kernel/observe/events.go
- harness/runtime/events/events.go
- harness/appkit/product/trace.go
- harness/runtime/state/state_journal.go
- kernel/hooks/builtins/audit.go
- 相关测试文件

### 8.2 向后兼容策略

本阶段不要求向后兼容旧 hosted tool 表达方式。

但为了控制波动：

- 保留 assistant.hosted_tool 展示事件；
- 新增 execution hosted tool 事件作为增量能力；
- 不破坏现有普通 tool call 与 transcript 逻辑。

## 9. 测试矩阵

### 9.1 单元测试

1. hosted tool 状态归一化：
   - 原始状态到 started/progress/completed/failed 的映射正确；
2. 去重与状态推进：
   - 连续重复 progress 不应产生多条等价 execution event；
3. synthetic terminal：
   - 未显式结束时收尾补 completed/failed。

### 9.2 loop 集成测试

1. 一次 hosted tool 生命周期可生成：
   - assistant.hosted_tool 展示事件；
   - hosted_tool.started/progress/completed execution events；
2. assistant 最终消息仍能保留 HostedToolCalls 最终快照；
3. 普通 tool_call 路径无回归。

### 9.3 审计与 trace 测试

1. audit.jsonl 应出现 hosted tool execution event；
2. RunTrace timeline 能正确显示 hosted tool 事件；
3. state journal 能按 call_id / event_id 稳定索引；
4. inspect 输出不出现 hosted tool 生命周期丢失或覆盖。

### 9.4 UI / transport 测试

1. runtime event bridge 能输出 hosted tool 生命周期事件；
2. TUI timeline 不被重复 progress 噪音淹没；
3. 运行结束后 transcript 与 progress 面板语义一致。

## 10. 建议提交拆分

### Commit 1

主题：hosted tool execution lifecycle 基础设施

范围：

- 新增 execution event types；
- loop 内 hosted tool 生命周期发射；
- 状态归一化与去重；
- 基础回归测试。

### Commit 2

主题：runtime trace / inspect / state journal 对 hosted tool 生命周期的呈现与消费

范围：

- runtime events bridge 补 hosted tool 生命周期类型；
- trace timeline / inspect 输出补 hosted tool 展示；
- state catalog 与 audit 相关测试补齐。

### Commit 3

主题：产品层收口与文档/测试完善

范围：

- TUI 或产品层展示优化；
- synthetic terminal 和错误语义回归；
- 文档、说明与最终测试矩阵收尾。

## 11. 推荐执行顺序

建议按以下顺序实施：

1. 先做 execution event 类型与 loop 发射；
2. 再做 trace / inspect / state catalog 消费；
3. 最后做 UI 呈现和测试收口。

这样可以先把“事实源”做对，再让产品层消费，避免先改展示、后改语义造成返工。

## 12. 验收标准

阶段 6 结束时应满足：

- hosted tool 生命周期能在 audit / trace / state journal 中稳定看见；
- assistant transcript 仍保持简洁，不被生命周期噪音污染；
- inspect / replay 优先依赖 execution timeline 而非快照猜测；
- 普通 tool_call 行为无回归；
- provider 未来新增 hosted tool 类型时，只需接入统一状态归一化逻辑即可被治理层消费。
