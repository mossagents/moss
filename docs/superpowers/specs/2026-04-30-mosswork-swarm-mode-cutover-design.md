# mosswork Swarm 模式切换设计

日期：2026-04-30  
状态：Draft / Ready for review

## 1. 背景

`apps\mosswork` 当前同时存在两套语义：

- 用户可见界面将第二种聊天模式称为“专家模式”
- 后端实际执行链路已经是 swarm pipeline
- 代码与持久化仍大量使用 `expert` 作为模式名、参数名和会话来源值

这种状态会造成三个问题：

1. 产品语义与实现语义分裂，用户看到的是“专家模式”，但内部已经是 swarm。
2. 代码维护成本高，模式值、API 名称、文件名和日志名不一致，后续继续扩展时容易混淆。
3. 旧命名会把“专家”误当作稳定域概念，阻碍 `mosswork` 将该模式明确收敛为 swarm 协作形态。

本次需求不是单纯改 UI 文案，而是将 `mosswork` 中的该模式**完整切换为 Swarm 模式**，并且**不保留旧 `expert` 值兼容**。

## 2. 目标

本设计目标如下：

1. 将 `mosswork` 的聊天模式模型统一为 `normal | swarm`。
2. 将用户可见文案统一为“普通模式 / Swarm 模式”。
3. 将 `mosswork` 内部与该模式相关的 API、字段、函数、文件名、内部 agent 名称、输出文件名前缀统一迁移到 `swarm` 语义。
4. 将会话持久化中的模式来源值改为 `swarm`，不再写入或识别 `expert`。
5. 保持现有 swarm pipeline 的执行语义不变，本次只做模式和命名层的切换。

## 3. 非目标

本设计明确不做：

- 保留旧 `expert` 会话来源值的兼容读取
- 自动迁移历史 `expert` 会话数据到 `swarm`
- 改变 swarm pipeline 的 planner / worker / synthesizer / reviewer 流程
- 调整 `fast / standard / deep` 或 `brief / standard / detailed / comprehensive` 这些参数值
- 借此次改动重构与模式切换无关的 UI、存储或 runtime 行为

## 4. 设计原则

### 4.1 产品语义与运行时语义必须一致

对用户展示为 `Swarm` 的模式，在代码、API、持久化和值域上也必须是 `swarm`，不能继续以 `expert` 作为真实模式名。

### 4.2 本次是硬切换，不保留 alias

`mosswork` 不再把 `expert` 视为有效模式值。历史数据若仍带有 `expert`，恢复时按普通模式对待，不做专门兼容。

### 4.3 改名应覆盖完整边界

不能只改界面或只改持久化值。前端状态、后端字段、Wails bindings、注释、内部 agent 名称、调试产物名都要同步收敛，否则仍会留下双重语义。

### 4.4 只改模式命名，不改执行协议

这次不重新设计 swarm pipeline 的并行研究逻辑，只把“以前被命名为 expert 的那条路径”正式收敛为 Swarm 模式。

## 5. 模式模型与持久化

### 5.1 模式值

`mosswork` 最终只保留以下两种聊天模式：

- `normal`
- `swarm`

前端 `ChatMode`、后端 `SetChatMode` 使用的值、会话摘要 `source` 的特殊标识以及持久化的 `thread_source` 值都统一到这两个枚举值。

### 5.2 持久化规则

当前会话一旦提交模式：

- 普通模式写入 `normal`
- Swarm 模式写入 `swarm`

`sessionChatMode` 在恢复会话时只识别 `swarm`，不再识别 `expert`。

### 5.3 历史会话行为

历史会话若 `thread_source == "expert"`：

- 不做自动迁移
- 不做兼容别名处理
- 恢复到前端时按普通模式表现
- 侧边栏不再显示该会话的 Swarm 标识
- 恢复后若用户直接继续发送消息，则继续走普通模式路径，不隐式把历史 `expert` 值改写为 `normal`
- 只有用户在新版交互里重新提交模式时，后续持久化写入才会落到 `normal` 或 `swarm`

这是一次明确的兼容性切断，属于本次设计接受的行为变化。

## 6. 前端设计

### 6.1 模式切换与模式展示

以下前端展示统一改为 `Swarm` 语义：

- 模式切换按钮：`专家模式` -> `Swarm 模式`
- 会话信息面板中的模式标签
- 侧边栏会话列表中用于特殊模式的图标 title 和来源判断

对应状态值从 `"expert"` 切换为 `"swarm"`。

### 6.2 参数状态与组件命名

与该模式相关的前端状态统一迁移：

- `expertBreadth` -> `swarmBreadth`
- `expertDepth` -> `swarmDepth`
- `expertOutputLength` -> `swarmOutputLength`

相关类型与组件同样迁移到 `swarm` 命名，例如：

- `ExpertDepth` -> `SwarmDepth`
- `ExpertOutputLength` -> `SwarmOutputLength`
- `ExpertParamsBar` -> `SwarmParamsBar`

参数值本身保持不变：

- 深度：`fast | standard | deep`
- 输出长度：`brief | standard | detailed | comprehensive`

### 6.3 前端调用链

消息发送前的参数同步逻辑继续保留，但调用链统一改名：

- `chatMode === "swarm"` 时才同步 swarm 参数
- `ChatService.setExpertParams(...)` 改为 `ChatService.setSwarmParams(...)`
- Wails 生成 bindings 的 TypeScript 导出同步更新

## 7. 后端设计

### 7.1 ChatService 模式路由

后端 `ChatService` 中与模式相关的字段和判断全部收敛到 `swarm`：

- `chatMode` 只使用 `normal` 或 `swarm`
- `routeMessage` 只在 `mode == "swarm"` 且 swarm runtime 可用时进入 swarm pipeline
- 其他情况保持现有普通会话发送路径

本设计不修改 swarm runtime 不可用时的现有降级行为；本次变更重点是模式和命名切换，而非路由容错策略重构。

额外约束：

- 若当前会话模式已提交为 `swarm`，其持久化值仍保持 `swarm`
- runtime 不可用只影响本次消息实际走哪条执行路径，不会反向改写模式语义或持久化来源值

### 7.2 参数接口与内部字段

以下后端字段和接口统一改名：

- `expertBreadth` -> `swarmBreadth`
- `expertDepth` -> `swarmDepth`
- `expertOutputLength` -> `swarmOutputLength`
- `SetExpertParams(...)` -> `SetSwarmParams(...)`

字段含义不变，仍表示：

- 并行研究方向数
- 单个方向的探索深度
- 综合回答输出长度

### 7.3 Pipeline 相关函数与文件

当前 `expertswarm.go` 中实现的整条流程统一迁移到 `swarm` 命名边界。

以下改名属于本次实现范围内的必改项，至少包括：

- 文件：`expertswarm.go` -> `swarmpipeline.go`
- `runExpertSwarm` -> `runSwarmPipeline`
- `expertPlanQuestions` -> `swarmPlanQuestions`
- `expertRunWorkers` -> `swarmRunWorkers`
- `expertRunWorker` -> `swarmRunWorker`
- `expertSynthesize` -> `swarmSynthesize`
- `expertReview` -> `swarmReview`
- 其他 `expert*` helper 一并迁移到 `swarm*`

目标不是重新设计边界，而是让这条已有 swarm 流程从名字到职责都与真实语义一致。

## 8. 内部 agent、日志与落盘产物

以下内部标识也统一迁移：

- `expert-worker` -> `swarm-worker`
- `expert-synthesizer` -> `swarm-synthesizer`
- 其他内部 `expert-*` agent 名称同步改为 `swarm-*`

落盘产物同样改名：

- 输出文件名前缀：`expert-YYYYMMDD-HHMMSS.md` -> `swarm-YYYYMMDD-HHMMSS.md`
- 相关日志文案、调试注释、注释说明统一改为 `swarm`

这可以保证日志、artifact 和 UI 行为不会继续泄露旧模式名。

## 9. 绑定与生成物

由于 Wails frontend bindings 暴露了旧接口名和注释，改动需要覆盖以下生成链：

- Go 后端导出方法名变化（`SetSwarmParams`）
- 重新生成 `apps\mosswork\frontend\bindings\...` 下的 TypeScript bindings
- 更新 `frontend\src\lib\api.ts` 中对生成绑定的封装名称

如果还有依赖旧接口名的前端导入，也必须同步清理，确保代码库中不再保留 `expert` 作为 mosswork 模式相关接口名。

## 10. 错误处理与兼容性说明

### 10.1 无旧值兼容

本设计明确取消 `expert` 的兼容读取，因此以下现象是预期行为：

- 恢复旧 `expert` 会话时不会自动进入 Swarm 模式
- 旧会话列表不会再显示为特殊模式会话
- 旧数据若仍保留 `expert` 字符串，不会触发 swarm pipeline

### 10.2 运行时错误处理

本次改名不引入新的错误处理路径，保留现有行为：

- swarm 参数仍按原有约束做值域裁剪
- 发送消息时仍先同步参数，再走消息发送链
- swarm pipeline 内部的错误上报方式不变

### 10.3 非模式语义字符串不强制改动

只有在 `mosswork` 中承担“模式语义”的 `expert` 才必须迁移。若某处字符串不再表示该模式，而是历史文件内容、与别处共享的无关领域术语，则不强制纳入本次范围。

## 11. 实施边界

本次代码改动至少覆盖以下文件族：

- `apps\mosswork\chatservice.go`
- `apps\mosswork\expertswarm.go`（及其改名后的新文件）
- `apps\mosswork\frontend\src\App.tsx`
- `apps\mosswork\frontend\src\components\ModeToggleBar.tsx`
- `apps\mosswork\frontend\src\components\ChatInfoPanel.tsx`
- `apps\mosswork\frontend\src\components\ChatSidebar.tsx`
- `apps\mosswork\frontend\src\components\ExpertParamsBar.tsx`（及其改名后的新文件）
- `apps\mosswork\frontend\src\lib\api.ts`
- `apps\mosswork\frontend\bindings\...chatservice.ts`

是否存在额外引用，以全仓搜索结果为准；但改动应尽量限制在 `apps\mosswork` 相关面，避免扩散到无关产品。

## 12. 验收标准

实现完成后，以下结果必须同时成立：

1. `mosswork` UI 中不再出现“专家模式”文案，统一显示 `Swarm 模式`。
2. `mosswork` 前后端模式值统一为 `normal | swarm`。
3. `thread_source` 新写入值只会是 `normal` 或 `swarm`。
4. `mosswork` 中不再保留 `SetExpertParams`、`expertBreadth`、`expert-worker` 这类模式相关旧命名。
5. 旧 `expert` 会话不会再被识别为特殊模式会话。
6. Wails 绑定与前端封装可正常使用新的 `swarm` 接口名。
7. 代码中与该模式直接相关的 `expert` 语义已在 `apps\mosswork` 范围内被清理完毕。

## 13. 测试与验证

实现阶段至少需要覆盖以下验证：

- `apps\mosswork` 相关 Go 构建通过
- 前端类型检查/构建通过（按仓库现有脚本执行）
- Swarm 模式发送消息仍能进入现有 swarm pipeline
- 普通模式发送消息行为不变
- 新建会话并提交 Swarm 模式后，恢复会话时仍显示为 Swarm
- 历史 `expert` 会话恢复时按普通模式表现

## 14. 实施建议

建议按以下顺序实施：

1. 先改模式值与持久化判断，建立 `normal | swarm` 的新单一真相。
2. 再改前后端参数 API、组件状态与 bindings。
3. 最后统一重命名 pipeline helper、内部 agent 名称、文件名前缀和文案。

这样可以先完成语义收敛，再清理外围命名，降低局部改动与生成物不同步的风险。
