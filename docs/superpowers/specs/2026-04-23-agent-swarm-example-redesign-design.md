# Agent Swarm Example Redesign

## Problem

`examples\agent-swarm` 目前仍是旧的并行研究 demo：它直接在 example 内实现 persona worker、分解、并行批处理和综合流程，能展示“多 agent 一起工作”，但不能体现新的 swarm 一等公民能力，包括：

- `harness\swarm` 的 runtime / role pack / research orchestrator
- session / task / message / artifact 的持久化事实面
- recovery / inspect / export 的产品化入口
- governance message 和 swarm event bridge

目标不是把旧示例“翻新一下”，而是用新的 swarm substrate **重新实现一个完整的小型产品示例**，让仓库贡献者可以直接看到：如果要基于新 swarm 做一个 research app，推荐的命令面、装配方式、恢复方式和调试方式是什么。

## Goals

1. 将 `examples\agent-swarm` 重做为一个独立的小型 swarm app，而不是单次运行 demo。
2. 首版命令面提供：
   - `run`
   - `resume`
   - `inspect`
   - `export`
3. 运行主线基于新的 swarm runtime：
   - `harness\swarm.RuntimeFeature()`
   - `harness\swarm.ResearchOrchestrator`
   - file-backed session / task / artifact store
   - runtime event store + swarm execution events
4. 默认以真实 LLM 运行，但提供 deterministic demo 模式，方便本地演示和回归。
5. `inspect` / `export` 必须消费和 `run` 同一套持久化事实，而不是各自维护平行数据。

## Non-Goals

1. 不兼容旧 `examples\agent-swarm` 的 persona worker 内部实现。
2. 不在首版引入新的通用 swarm orchestration 策略；首版只聚焦 research-first。
3. 不把 example 做成第二个 `apps\mosscode`；它是小型产品示例，不是通用 IDE/assistant。
4. 不在首版覆盖分布式 swarm 或跨节点调度。

## User-Facing Shape

`examples\agent-swarm` 作为独立 example app，保留 `go run .` 的体验，但命令面改为产品式子命令：

- `run`：启动一个 research swarm run，输出 root session / swarm run / artifact 摘要
- `resume`：恢复最近一次或指定的一次 swarm run，继续未完成任务
- `inspect`：按 swarm run 优先展示线程、任务、artifact、governance、events
- `export`：导出最终报告、artifact refs、runtime events、可选 markdown/json bundle

示例使用单独 app name 和独立运行目录，避免与 `mosscode` 的默认存储冲突。

### CLI Contract

| Command | Required Input | Optional Input | Default Targeting | Failure Behavior |
|---|---|---|---|---|
| `run` | `--topic`（真实 LLM 模式）；demo 模式下可省略并使用内置 topic | `--run-id`, `--output`, `--demo`, provider/model flags | 总是创建新 swarm run；若 `--run-id` 已存在则报冲突 | 不覆盖已有 run；若 runtime 装配失败直接退出 |
| `resume` | 无；也可显式传 `--run-id` 或 `--session` | `--latest`, `--output`, `--demo`, `--force-degraded-resume` | 优先级：`--session` > `--run-id` > `--latest` > 最近可恢复 run | 无匹配 run 时返回明确错误；目标 run 不可恢复时拒绝继续 |
| `inspect` | 无；也可显式传 `--run-id` 或 `--session` | `--latest`, `--json`, `--view run|threads|thread|events`, `--thread-id` | 优先级：`--session` > `--run-id` > `--latest` > 最近一个 run（不要求 recoverable） | 目标不存在或事实不完整时输出结构化诊断 |
| `export` | 无；也可显式传 `--run-id` 或 `--session` | `--latest`, `--output`, `--format json|jsonl|bundle`, `--include-payloads` | 优先级：`--session` > `--run-id` > `--latest` > 最近一个 run（不要求 recoverable） | 输出路径不可写或目标格式所需的事实缺失时显式失败，不生成“成功”外观 |

`resume` 和 `inspect/export` 使用同一套目标解析逻辑，避免一个按 session-first、另一个按 run-first 的语义漂移。

`--output` 语义统一如下：

- `run` / `resume`：指定最终用户输出目录（默认使用 app data 目录下的 run 输出根）
- `export`：指定导出目录或导出文件根
- `inspect`：不支持 `--output`，只输出到 stdout

### Shared Target Resolution

target 解析是独立共享单元，不属于 entry 层，也不属于 inspect/export surface 本身。

- 名称：`TargetResolver`
- 输入：`ResolveMode(resume|inspect|export)` + `--session`、`--run-id`、`--latest`
- 输出：`ResolvedTarget{RootSessionID, SwarmRunID, ResolutionSource}`
- 被 `resume` / `inspect` / `export` 共同调用

entry 层只负责把 flag 交给 `TargetResolver`；workflow 和 surface 只消费已经 resolve 完成的 target。

职责边界进一步约束如下：

- 若用户显式传 `--session` 或 `--run-id`，`TargetResolver` 只做 identity 解析，不判断 recoverable。
- 若用户显式传 `--latest`：
  - `resume` 选择最近一个 `recoverable=true` 的 run。
  - `inspect` / `export` 选择最近一个 run，不要求 recoverable。
- 若用户完全不传 target：
  - `resume` 的默认回退仍是最近可恢复 run。
  - `inspect` / `export` 的默认回退是最近一个 run。
- `TargetResolver` 在需要“最近可恢复 run”时，必须调用 `RecoveryResolver` 做 recoverable 判断，而不是自己重复实现恢复规则。

当 `--view thread` 时，必须显式传 `--thread-id`；否则 `inspect` 直接报错，而不是猜测“最重要线程”。

## Architecture

### 1. Entry Layer

入口层只负责：

- flag 和 subcommand 解析
- 输出格式（plain/json）
- 真实 LLM / demo mode 的开关
- 默认路径与 app name 选择

入口层不直接编排 worker/synthesis/review 流程。

### 2. Runtime Assembly Layer

装配层负责统一创建 example 所需 runtime：

- `appkit.BuildDeepAgent(...)`
- 显式启用 swarm preset
- 注入独立的 session / task / checkpoint / artifact / event store 路径
- 打开 `harness\swarm.Runtime`
- 打开 inspect/export 需要的 `runtimeenv` 依赖

这一层的职责是把“完整 swarm app 所需基础设施”一次性准备好，供 `run` / `resume` / `inspect` / `export` 共享。

**对外接口：**

- 输入：
  - app flags（provider/model/workspace/trust/demo）
  - command mode（run/resume/inspect/export）
- 输出：
  - configured kernel
  - `harness\swarm.Runtime`
  - runtime storage handles（session/task/checkpoint/artifact/event store）
  - `RecoveryResolver` / `RunLockService`
- 不负责：
  - 研究任务编排
  - CLI target 解析
  - 最终报告格式化

### 2.5. Recovery and Lock Services

这两个单元虽然由装配层提供实例，但职责独立，不与 workflow 或 surface 混在一起。

**`RecoveryResolver`**

- 输入：`ResolvedTarget` + session/task/message/artifact/event stores
- 输出：`RecoveredRunSnapshot`
- 负责：
  - 从持久化事实重建 run/thread/task/message/artifact/governance 视图
  - 计算 `recoverable` / `degraded` / `events_partial`
  - 产出 inspect/export 可直接消费的统一快照
- 不负责：
  - 修改事实
  - 获取 CLI flags
  - 执行任务恢复

`RecoveredRunSnapshot` 至少包含：

- run/thread/task/message/artifact/governance 汇总视图
- `recoverable` / `degraded` / `events_partial`
- persisted `execution_mode`（`real|demo`）
- diagnostics（例如 `events_last_error`）

**`RunLockService`**

- 输入：`SwarmRunID`
- 输出：lease handle 或冲突错误
- 负责：
  - 对 `run` / `resume` 进行单-run 排他保护
  - 维护 TTL 和过期接管规则
- 不负责：
  - 决定 run 是否 recoverable
  - 渲染冲突输出格式

### 3. Workflow Layer

workflow 层负责 research-first 业务流程：

- 创建或恢复 swarm run
- materialize root / planner / supervisor / worker / synthesizer / reviewer 线程
- 派发任务
- 收集 findings / source sets / synthesis drafts
- 处理 review / redirect / takeover
- 决定何时完成或失败

workflow 层是 example 的核心业务逻辑，但只通过 swarm runtime 提供的事实面工作，不直接依赖旧 example 的自定义 event/parallel 结构。

**对外接口：**

- 输入：
  - root session / target run identity
  - topic
  - execution mode（real/demo）
  - resolved storage/runtime handles
  - run lease（来自 `RunLockService`）
  - recovered snapshot（仅 `resume` 路径需要）
- 输出：
  - updated swarm facts（sessions/tasks/messages/artifacts/events）
  - run summary（root session id, swarm run id, final report ref, status）
- 不负责：
  - provider/model 选择
  - inspect/export 渲染
  - target 解析

### 4. Surface Layer

surface 层复用现有产品能力，但提供 swarm-first 默认行为：

- `inspect`：默认从 swarm run/root session 出发，而不是要求用户先知道底层 session ID
- `export`：默认导出与当前 run 相关的 artifacts / runtime events / final report

这层可以调用现有 `harness\appkit\product` builder/rendering，但不能把 example 变成 `mosscode` 的别名。

**对外接口：**

- 输入：
  - resolved run/session target
  - `RecoveredRunSnapshot`
  - output format flags
- 输出：
  - inspect report
  - export bundle
- 不负责：
  - 改写 swarm 事实
  - 补写缺失 artifacts/events
  - 重新执行恢复判定

## Lifecycle and Status Model

### Run Status

| Status | Meaning | Recoverable |
|---|---|---|
| `running` | run 正在执行或等待下一线程推进 | 是 |
| `completed` | 已生成最终报告并完成 | 否 |
| `failed` | root run 无法继续推进 | 否（仅允许 inspect/export） |

### Task Status

| Status | Meaning | Resume Behavior |
|---|---|---|
| `pending` | 尚未开始 | 继续调度 |
| `running` | 中断前正在执行 | 视为可恢复，resume 时重新认领 |
| `completed` | 已完成 | 不重复执行 |
| `failed` | 执行失败 | 由 governance 决定 redirect/takeover/终止 |
| `cancelled` | 被人工/治理取消 | 不自动恢复 |

### Recoverable Rule

`recoverable=true` 不是 task status，而是 `RecoveryResolver` 从快照派生出的 run 级诊断位。判定规则：

- 至少存在一个 `pending` 或 `running` task；或
- 存在 `failed` task，但最近一条 governance action 明确把它标记为可继续（redirect/takeover 后等待重新调度）

因此“failed-but-recoverable”不是新的持久化状态，只是恢复时根据 failed task + governance facts 计算出的结果。

### Diagnostic Markers

| Marker | Meaning | Persistence |
|---|---|---|
| `recoverable=true` | root run 可由 `resume` 继续 | root session metadata |
| `degraded=true` | stores 不完整，但仍可读取部分事实 | root session metadata |
| `events_partial=true` | swarm facts 成功写入，但 runtime event store 记录不完整 | root session metadata |

当 event bridge 写 runtime event store 失败时，命令必须同步更新 root session metadata（如 `events_partial=true`、`events_last_error=<message>`），这样 inspect/export 才能只读同一事实面也看见该诊断。

## Data Flow

### Run

`run` 的最小主线如下：

1. 创建 root session
2. 用 `ResearchOrchestrator.Seed(...)` 生成 run / threads / initial tasks
3. 将线程和任务写入持久化 stores
4. 执行 worker / synthesizer / reviewer 流程
5. 持久化 artifacts 和 governance messages
6. 通过 event bridge 自动记录 swarm execution events，并同步写入 runtime event store
7. 输出最终报告摘要与 resume/inspect 提示

### Resume

`resume` 不重新 seed。它必须：

1. 解析用户指定的 run/session，或选择最近可恢复 run
2. 用 `RecoveryResolver.LoadRun(...)` 重建 swarm snapshot
3. 分析尚未完成的 tasks、已有 artifacts、已有 governance actions
4. 只恢复剩余工作，而不是重复执行已完成线程

恢复模式约束：

- `run --demo` 创建的 run 必须把 `execution_mode=demo` 持久化到 root session metadata。
- 真实 LLM 运行创建的 run 必须持久化 `execution_mode=real`。
- `resume` 默认复用已持久化的 `execution_mode`，不允许把已有 run 从 `real` 切到 `demo`，也不允许反向切换。
- `resume --demo` 只用于显式确认目标 run 本来就是 demo；若目标 run 是 `real`，命令直接失败并报告模式不匹配。

补充约束：

- 目标解析优先级固定为 `--session` > `--run-id` > `--latest` > 最近可恢复 run。
- “最近可恢复 run”定义为：root session 未 completed，且 `RecoveryResolver` 计算结果为 `recoverable=true` 的最新 swarm run。
- 若没有任何可恢复 run，`resume` 返回明确错误并提示使用 `run`。
- 若目标 run 已 completed，`resume` 拒绝继续并建议使用 `inspect`/`export`。
- 若目标 root session metadata 含 `degraded=true`，默认拒绝继续，仅在显式 `--force-degraded-resume` 下允许。

### Inspect

`inspect` 默认展示：

- run 级摘要：threads / tasks / messages / artifacts / governance
- thread 级摘要：role、status、artifact count、关键消息
- event 级摘要：swarm execution events、runtime events 中的 swarm family

如果底层仍复用通用 inspect builder，example 需要提供 swarm-first 参数解释和默认目标解析。

### Export

`export` 导出一个自解释 bundle，首版至少包括：

- run summary
- artifact refs（必要时附带 artifact payload）
- runtime events（json 或 jsonl）
- `bundle` 模式下的 markdown 最终报告

可选附加：

- inspect 摘要快照

默认契约：

- 默认输出为目录 bundle，而不是单文件。
- 默认路径：`<app-data>\exports\<swarm-run-id>\`
- `--output` 为目录语义：`bundle` 直接使用该目录；`json` / `jsonl` 使用该目录作为输出根，并在其中生成对应文件。
- 目录结构：

```text
<swarm-run-id>/
├── summary.json
├── final-report.md
├── artifacts.json
├── events.jsonl
└── payloads/                # 仅 --include-payloads 时生成
    └── <artifact-id>.<ext>
```

- `summary.json`：run id、root session id、status、thread/task/artifact/governance 统计。
- `final-report.md`：`bundle` 模式下对 `completed` run 为必选文件；对未完成或失败 run 可缺省，并在 `summary.json` 中给出原因。
- `artifacts.json`：artifact refs 列表，默认不内联 payload。
- `events.jsonl`：runtime event store 中属于该 run 的事件流。
- `--include-payloads` 为真时，导出 artifact payload 文件，并在 `artifacts.json` 中写入相对路径。
- `--format json` 仅导出 `summary.json + artifacts.json`；`--format jsonl` 仅导出 `events.jsonl`；`--format bundle` 导出完整目录（默认）。

导出失败语义：

- `bundle`：若 `summary` 或 artifact refs 缺失，则失败；对 `completed` run 若最终报告缺失也失败；`events.jsonl` 缺失时仅在 `events_partial=true` 场景下降级为 warning，并把诊断写入 `summary.json`。
- `json`：只要求 `summary + artifacts` 可用；events 缺失不阻塞。
- `jsonl`：只要求 events 可用；若 `events_partial=true` 或无 event store 记录，则失败并给出明确诊断。

## Demo Mode

example 默认走真实 LLM，但提供显式 `--demo` 模式；demo 默认 topic 为 `Summarize the trade-offs of local-first agent swarms.`。

约束如下：

1. demo 不能绕过 swarm runtime、stores、event bridge。
2. demo 只替换“任务产出来源”，不替换 run/resume/inspect/export 的事实链路。
3. deterministic 输出应稳定，方便测试断言 artifact/governance/event 顺序。

`inspect` 和 `export` 不需要额外的 demo 标记；它们始终只读取目标 run 已写入的事实。

这保证：

- 用户可以先本地体验完整命令面
- 仓库测试可以稳定覆盖示例主链路
- 真实 LLM 与 demo 只在产出来源上不同，不会变成两套产品

## Error Handling

1. 单个 worker 失败时，优先记录 failed task，并通过 governance message 触发 redirect/takeover，而不是立即终止整个 run。
2. synthesis/review 无法继续时，可将 root run 置为 failed。
3. `resume` 必须优先恢复已有 run，而不是静默创建一个新 run。
4. inspect/export 对缺失数据应显式提示事实不完整，而不是伪造“成功”视图。
5. session/task/artifact store 写失败时，本轮命令直接失败并输出失败阶段；不得把 run 标成 completed。
6. event bridge 写 runtime event store 失败时，命令不回滚已写入的 swarm facts，但必须把该错误暴露为 warning/diagnostic，使 inspect 能看到“events partial”状态。
7. 恢复时若发现 store 部分缺失（例如 task/artifact 缺一部分），`inspect` 标记 run 为 degraded，`resume` 只允许在显式 `--force-degraded-resume` 下继续。
8. 同一 run 并发执行 `run/resume` 时，后进入者必须失败，不能双写。首版通过 run-level lock file / lock record 解决；锁持有者退出或超时后才允许下一次 resume。

event bridge 契约：

- 输入：session/task/message/artifact 持久化动作
- 输出：execution events + runtime event store records
- 若写 runtime event store 失败，必须返回可观测错误给 workflow，并要求 workflow 更新 root session metadata 诊断位
- event bridge 不负责补写历史事件，也不负责决定 run 是否失败

run-level lock 契约：

- 锁名：`<swarm-run-id>.lock`
- 默认 TTL：5 分钟
- `run/resume` 获取不到锁时立即失败，并提示当前持有者/剩余 TTL
- 崩溃恢复时，若 TTL 过期，则允许新的进程接管

## Testing

首版至少覆盖：

1. **demo end-to-end**
   - `run` 在 seed 完成、首批 task/artifact 落盘后人为中断，再执行 `resume -> inspect -> export`
   - 校验 artifact、governance、runtime events 都存在
2. **真实运行装配**
   - 作为 opt-in 集成验证执行（显式环境变量或手动脚本）
   - 校验真实 LLM 模式下的 runtime 装配和路径分离
3. **恢复链路**
   - 中途停下后能依靠 stores + recovery 继续
   - 验证 `completed` run 会拒绝 `resume`
4. **导出正确性**
   - 导出 bundle 包含最终报告、artifact refs、runtime events

真实运行装配的验收方式：

- 不作为默认 CI 必跑项。
- 作为 opt-in 集成验证（例如显式环境变量或手动脚本）执行。
- 普通仓库回归默认走 deterministic demo 模式，保证稳定。

## Design Constraints

1. example 必须清楚展示“如何使用新 swarm 能力”，因此不能把关键逻辑藏进旧 demo helper。
2. 必须保持 example 边界清晰：可运行、可学习，但不复制完整 `apps\mosscode` 的全部功能。
3. inspect/export 优先复用已有产品能力，避免重造一套平行 viewer。
4. demo 与真实 LLM 共用同一事实链路。

## Recommended Implementation Shape

建议目录按职责拆开，而不是继续堆在单一 `main.go`/`swarm.go`：

```text
examples/agent-swarm/
├── main.go
├── config.go
├── commands_run.go
├── commands_resume.go
├── commands_inspect.go
├── commands_export.go
├── runtime.go
├── workflow.go
├── demo_mode.go
└── README.md
```

这不是强制文件名清单，但要求边界保持一致：

- 命令入口
- runtime 装配
- swarm workflow
- demo provider
- 文档

## Recommendation

按“独立的小型 swarm app”路线实现 `examples\agent-swarm`。它应成为新 swarm 的官方参考样板：

- 用新的 swarm substrate 做 research app
- 展示真实的 run/resume/inspect/export 流程
- 又通过 deterministic demo 保持可演示、可测试、可回归

这是当前仓库里最能向贡献者说明“新 agent-swarm 应该怎样被真正使用”的示例形态。
