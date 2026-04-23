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
4. 默认以真实 LLM 运行，但提供 deterministic demo/mock 模式，方便本地演示和回归。
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

### 3. Workflow Layer

workflow 层负责 research-first 业务流程：

- 创建或恢复 swarm run
- materialize root / planner / supervisor / worker / synthesizer / reviewer 线程
- 派发任务
- 收集 findings / source sets / synthesis drafts
- 处理 review / redirect / takeover
- 决定何时完成或失败

workflow 层是 example 的核心业务逻辑，但只通过 swarm runtime 提供的事实面工作，不直接依赖旧 example 的自定义 event/parallel 结构。

### 4. Surface Layer

surface 层复用现有产品能力，但提供 swarm-first 默认行为：

- `inspect`：默认从 swarm run/root session 出发，而不是要求用户先知道底层 session ID
- `export`：默认导出与当前 run 相关的 artifacts / runtime events / final report

这层可以调用现有 `harness\appkit\product` builder/rendering，但不能把 example 变成 `mosscode` 的别名。

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

### Inspect

`inspect` 默认展示：

- run 级摘要：threads / tasks / messages / artifacts / governance
- thread 级摘要：role、status、artifact count、关键消息
- event 级摘要：swarm execution events、runtime events 中的 swarm family

如果底层仍复用通用 inspect builder，example 需要提供 swarm-first 参数解释和默认目标解析。

### Export

`export` 导出一个自解释 bundle，首版至少包括：

- run summary
- final report
- artifact refs（必要时附带 artifact payload）
- runtime events（json 或 jsonl）

可选附加：

- markdown 报告
- inspect 摘要快照

## Demo / Mock Mode

example 默认走真实 LLM，但提供显式 `--demo` 或 `--mock` 模式。

约束如下：

1. demo/mock 不能绕过 swarm runtime、stores、event bridge。
2. demo/mock 只替换“任务产出来源”，不替换 run/resume/inspect/export 的事实链路。
3. deterministic 输出应稳定，方便测试断言 artifact/governance/event 顺序。

这保证：

- 用户可以先本地体验完整命令面
- 仓库测试可以稳定覆盖示例主链路
- 真实 LLM 与 demo/mock 只在产出来源上不同，不会变成两套产品

## Error Handling

1. 单个 worker 失败时，优先记录 failed task，并通过 governance message 触发 redirect/takeover，而不是立即终止整个 run。
2. synthesis/review 无法继续时，可将 root run 置为 failed。
3. `resume` 必须优先恢复已有 run，而不是静默创建一个新 run。
4. inspect/export 对缺失数据应显式提示事实不完整，而不是伪造“成功”视图。

## Testing

首版至少覆盖：

1. **demo/mock end-to-end**
   - `run -> resume -> inspect -> export`
   - 校验 artifact、governance、runtime events 都存在
2. **真实运行装配**
   - 校验真实 LLM 模式下的 runtime 装配和路径分离
3. **恢复链路**
   - 中途停下后能依靠 stores + recovery 继续
4. **导出正确性**
   - 导出 bundle 包含最终报告、artifact refs、runtime events

## Design Constraints

1. example 必须清楚展示“如何使用新 swarm 能力”，因此不能把关键逻辑藏进旧 demo helper。
2. 必须保持 example 边界清晰：可运行、可学习，但不复制完整 `apps\mosscode` 的全部功能。
3. inspect/export 优先复用已有产品能力，避免重造一套平行 viewer。
4. demo/mock 与真实 LLM 共用同一事实链路。

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
- demo/mock provider
- 文档

## Recommendation

按“独立的小型 swarm app”路线实现 `examples\agent-swarm`。它应成为新 swarm 的官方参考样板：

- 用新的 swarm substrate 做 research app
- 展示真实的 run/resume/inspect/export 流程
- 又通过 deterministic demo/mock 保持可演示、可测试、可回归

这是当前仓库里最能向贡献者说明“新 agent-swarm 应该怎样被真正使用”的示例形态。
