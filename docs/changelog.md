# Changelog

这份变更日志记录当前主线仓库已经落地、且仍与代码一致的关键演进。由于仓库近期以持续主线演进为主，以下内容按主题与时间窗口整理，而不是强行维护已经失真的旧版本清单。

## 2026-04

### Harness Patterns 包

新增 `harness/patterns/` 包，提供可组合的 Agent 编排原语：

- **SequentialAgent** — 顺序执行多个子 Agent，按事件顺序物化结果到父 session
- **ParallelAgent** — 并发执行多个子 Agent，支持自定义聚合函数
- **LoopAgent** — 迭代执行，支持最大迭代次数和退出条件
- **SupervisorAgent** — 动态路由，支持 RoundRobin 和 FirstMatch 策略
- **ResearchAgent** — 研究型编排模式（Query → Parallel Search → Synthesis 循环）
- `harness/patterns` 保持为轻量 workflow primitive 层；deep-agent 产品预设装配入口位于 `appkit.BuildDeepAgent(...)`
- `SupervisorAgent` 现在会记录路由决策状态，并支持在 worker 失败时 failover 到剩余 worker
- `ParallelAgent` 现在会为每个并发分支复制 child session，避免共享 session 竞态，并只把聚合后的事件提交回父 session
- `SequentialAgent`、`LoopAgent`、`SupervisorAgent` 与 `ResearchAgent` 现在统一采用 event-to-session materialization 语义：child session 内部副作用不会直接泄漏，只有 yielded `event.Content` / `event.Actions.StateDelta` 会提交回父 session
- `ResearchAgent` 现在会把 query 显式传入 SearchAgent，并把聚合 findings 显式传入 SynthesisAgent
- 这套 contract 已进一步上提到 kernel 层：`InvocationContext.RunChild(...)` 为 custom agent 提供统一的 branch-local child-run 语义，`Kernel.RunAgent(...)` 作为唯一顶层执行入口会对 root 级 generic event 做同样的 materialization
- `session.EventActions` 的单布尔 `Materialized` 已进一步结构化为 `MaterializedIn` domain；同一 event 现在可以按 child -> parent -> root 逐级提交，但在同一 session domain 内绝不重复提交
- `SupervisorAgent` 进一步强化为真正的策略控制面：支持 per-worker timeout、worker health 记账与冷却、基于剩余 budget 的 worker 过滤，以及在 no-match / budget-exhausted / timeout / failure 场景下可选地向父级 `Escalate`

### appkit → harness Feature 迁移

- `appkit.Extension` 变为 `harness.Feature` 类型别名
- 新增 `BuildKernelWithFeatures` 作为主要 API
- `BuildKernelWithExtensions` 标记为 deprecated
- `RuntimeSetup` Feature 替代了旧的 `WithRuntimeOptions`
- Harness Feature 新增 phase / dependency 元数据，`Harness.Install()` 会在安装前做受控排序与依赖校验
- `BuildDeepAgent` 中的 `patch-tool-calls` 与默认 restricted policy 已收回到 Feature 安装链
- `BuildDeepAgent` 已进一步拆成声明式 preset packs：state catalog、session/context、checkpoint、task runtime、persistent memories、execution surface、runtime setup、post-runtime governance

### Release gates 与观测面

主线新增了可直接用于发布守门和运行态自检的两组能力：

- `kernel\observe\ReleaseGateMeter`
- `kernel\observe\NormalizedMetricsSnapshot`
- `testing\arch_guard.ps1` 的环境参数、override 审计与 gate 校验流程

默认 gate：

- `success_rate >= 0.95`
- `llm_latency_avg <= 10000ms`
- `tool_latency_avg <= 5000ms`
- `tool_error_rate <= 0.05`

### 上下文压缩与中间件注入

主线继续强化了 context compression 能力，使长会话的压缩与中间件接线更适合产品面直接使用。

### 依赖升级

同步更新了部分间接依赖（例如 `xxhash` 与 OpenTelemetry 相关链路），以保持当前 runtime 与观测栈可维护。

## 2026-03 下旬

### mosscode 产品面成熟

`apps\mosscode` 从基础 coding assistant 演进到当前的核心应用入口，已包含：

- profile / trust / approval 解析
- doctor / debug-config / config / review 等 operator 命令
- checkpoint / fork / apply / rollback / changes 管理
- 更完整的 TUI 交互与外部上下文入口
- model router、retry、breaker、failover 等治理能力

### deepagent 预设成型

`presets\deepagent` 成为当前仓库里最重要的产品预设能力层，默认接入：

- session / checkpoint / task runtime
- persistent memories
- context offload / compact conversation
- delegated agent
- workspace isolation
- repo state / patch apply / patch revert / snapshot

### 运行时装配收敛到 `harness.Feature` + `appkit` builders

仓库逐步完成了从旧兼容入口到当前装配路径的收敛：

- `appkit.BuildKernel`
- `appkit.BuildKernelWithFeatures`（取代 `BuildKernelWithExtensions`）
- `harness.RuntimeSetup`

这也意味着文档和示例不再应围绕旧的兼容入口叙述。

### Skills / MCP / builtins / subagents 边界明确

能力加载体系被重新梳理为：

- builtin tools
- prompt skills (`SKILL.md`)
- MCP servers
- subagents

并支持 progressive skill manifests、按 trust 加载 project assets、以及项目级 MCP 审批。

### 记忆、上下文与异步协作能力落地

主线在这一阶段新增或强化了：

- persistent memories（含 sqlite-backed memory store）
- `offload_context`
- `compact_conversation`
- `update_task`
- task graph / mailbox / workspace isolation 相关能力

## 更早阶段仍保留的长期方向

以下方向依然能在当前代码里看到延续，但具体实现已经被更新后的主线结构取代：

- library-first kernel
- runtime capability loading
- profile-based execution posture
- core apps + reference examples 作为产品入口与参考实现

旧版 changelog 中那些已经失真的版本号、旧入口和废弃 API，不再在这里重复保留。
