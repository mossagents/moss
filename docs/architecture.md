# 架构概览

Moss 采用 **三层架构**：Kernel（核心运行时）→ Harness（编排层）→ Applications（应用层）。

核心原则：**把稳定的运行时原语留在 `kernel\`，把可组合编排能力放在 `harness\`，把产品预设和应用逻辑放在最上层。**

## 三层架构

```text
┌────────────────────────────────────────────────────┐
│ Layer 3: Applications                              │
│   apps\mosscode, apps\mosswork                     │
│   examples\mossresearch, mosswriter, mossclaw, ... │
│   → 面向终端用户，组合 Kernel + Harness            │
└────────────────────────────────────────────────────┘
                         ↕
┌────────────────────────────────────────────────────┐
│ Layer 2: Harness + Assembly                        │
│   harness (Feature/Backend/Harness)                │
│   harness\patterns (Agent 编排原语)                 │
│   appkit + appkit\runtime                          │
│   appkit.BuildDeepAgent (产品预设装配路径)          │
│   → 可复用的 Agent 编排与装配能力                   │
└────────────────────────────────────────────────────┘
                         ↕
┌────────────────────────────────────────────────────┐
│ Layer 1: Kernel                                    │
│   Agent 接口 + RunAgent + Session + Event          │
│   LLM 抽象 + Tool 系统 + Plugin 系统               │
│   → 最小核心运行时原语                              │
└────────────────────────────────────────────────────┘
```

## 关键职责边界

| 层 | 主要职责 |
|---|---|
| `kernel\` | Agent 接口、`RunAgent`、Session、Event、Tool、Plugin、Model、UserIO、Workspace/Executor、Task、Observe、Checkpoint、Artifact |
| `harness\` | Feature 接口、Backend 接口、Harness 组合器 — 将能力可组合地安装到 Kernel |
| `kernel\patterns\` | Agent 编排原语：Sequential、Parallel、Loop、Supervisor、Research |
| `appkit\runtime\` | 默认能力装配：builtin tools、MCP、`SKILL.md`、subagent、context、memory、knowledge、scheduling |
| `appkit\` | 面向应用的构建入口、扩展组合 API，以及 `BuildDeepAgent(...)` 产品预设路径 |
| `extensions\` | 扩展能力命名空间：`skill\`、`mcp\`、`agent\`、`knowledge\`、`capability\`（五个子域）|
| `apps\` | 核心应用入口 |
| `examples\` | 参考实现与集成示例 |

## Harness 层

`harness\` 包引入了 **Feature / Backend / Harness** 三个核心概念：

- **Feature**：一个可组合的能力单元，实现 `Name() string` + `Install(ctx, *Harness) error`。官方 Feature 还可以通过元数据声明安装 phase 与依赖，交由 `Harness.Install()` 做受控排序与校验，再把 Plugin、工具、系统提示词等注入 Kernel。
- **Backend**：统一的部署后端抽象，组合 `workspace.Workspace` + `workspace.Executor`，并可选参与 factory / install / boot / shutdown 生命周期。`LocalBackendFactory` + `LocalBackend` 是默认实现。
- **Harness**：组合器，持有 Kernel + Backend + 已安装 Feature 列表。

内置 Feature 包括：

| Feature | 作用 |
|---|---|
| `BootstrapContext` | 加载工作区上下文（AGENTS.md/SOUL.md）到系统提示词 |
| `SessionPersistence` | 注入 session 持久化存储 |
| `Checkpointing` | 启用 session 快照与恢复 |
| `TaskDelegation` | 启用异步 sub-agent 委派（Mailbox 通信） |
| `LLMResilience` | 注入 LLM 重试与熔断策略 |
| `ExecutionPolicy` | 注入工具级访问控制 Policy |
| `PatchToolCalls` | 启用工具调用修补（invalid JSON/name 纠正） |

## Agent 编排模式 (`kernel\patterns\`)

`kernel\patterns\` 包提供可组合的 Agent 编排原语，所有模式均实现 `kernel.Agent` 接口：

| Pattern | 用途 | 核心参数 |
|---|---|---|
| `SequentialAgent` | 顺序执行多个子 Agent，并按顺序提交已物化结果 | `Agents []Agent` |
| `ParallelAgent` | 并发执行多个子 Agent，结果聚合后提交 | `Agents []Agent`, `Aggregator func` |
| `LoopAgent` | 迭代执行，支持退出条件，并在每轮提交结果 | `Agent`, `MaxIterations`, `ShouldExit func` |
| `SupervisorAgent` | 带状态记录/失败恢复/策略约束的动态路由控制面 | `Workers []Agent`, `Router func`, `FailoverOnError`, `WorkerTimeout`, `WorkerBudgets` |
| `ResearchAgent` | 研究型编排（显式传递 Query/Findings 上下文） | `QueryAgent`, `SearchAgent`, `SynthesisAgent` |
编排模式支持任意嵌套组合，例如 `Sequential[Prepare, Parallel[Search1, Search2], Summarize]`。

内置路由策略：
- `RoundRobinRouter(stateKey)` — 轮询分配
- `FirstMatchRouter(predicate)` — 按条件匹配首个 Agent

当前 pattern 层的 canonical contract 是：**子 Agent 一律在 branch-local session 上运行；非 partial 的 yielded event 会按 pattern 语义 materialize 回父 session。**

- `SequentialAgent` / `LoopAgent` / `SupervisorAgent`：按执行顺序把 `event.Content` 与 `event.Actions.StateDelta` 提交到父 session
- `ParallelAgent`：每个并发分支隔离运行，只有聚合后的 event 会按聚合顺序提交到父 session
- `ResearchAgent`：query / search / synthesis 阶段都遵循同一提交语义，同时继续显式传递 query 与 findings 输入

这意味着 branch 内部直接改动 child session 的副作用不会泄漏到父 session；只有通过 yielded event 暴露出来的结果才会成为共享历史与共享状态。

这套 contract 现在也被提升到了更通用的 kernel 执行面：

- `InvocationContext.RunChild(...)` 成为 custom agent / orchestration code 的标准子调用入口：默认 branch-local 运行并提交 yielded 结果，必要时可关闭本级 materialization，交给更外层聚合器统一提交
- `Kernel.RunAgent(...)` 是唯一 canonical 顶层执行入口，并负责 root 级 generic event 的 session materialization
- `AgentLoop` 会把自己已经写入 session 的 LLM / tool event 标记到当前 `materialization domain`；canonical run path 只会跳过“已在同一 domain 提交过”的事件，而不会阻止事件继续向更外层 domain 逐级提交

`SupervisorAgent` 当前已经不只是“failover 一次”的简单路由器，还具备更完整的 control-plane 语义：

- per-worker timeout：单个 worker 卡住时可超时回收并继续 failover
- worker health：按 session 维度记录成功/失败/超时、连续失败数与冷却窗口
- budget-aware admission：可按剩余 token / steps 预算过滤高成本 worker
- escalation hooks：可在 no-match / budget-exhausted / timeout / failure 场景下向父级显式发出 `Escalate`

## 推荐装配路径

### 1. 标准装配

`appkit.BuildKernel(...)`：

1. 根据 `AppFlags` 构建 LLM adapter
2. 创建本地 `Sandbox`
3. 建立 `kernel.Kernel`
4. 安装 `harness.RuntimeSetup(flags.Workspace, flags.Trust)`
5. 通过底层 runtime capability assembly 加载 builtin tools / MCP / skills / agents

这是最短的“库优先”入口。

### 2. Feature 装配

`appkit.BuildKernelWithFeatures(...)` 在上述基础上再拼装 `harness.Feature`：

- `SessionPersistence`
- `Planning`
- `ContextOffload`
- `ContextManagement`
- `BootstrapContext`
- `Scheduling`
- `Knowledge`
- `PersistentMemories`
- `FeatureFunc` / `KernelOptions`

这也是当前核心应用和较完整示例最常用的装配方式。官方 Feature 会按 phase / dependency
元数据做受控安装；未标注元数据的自定义 Feature 则保持 configure 阶段语义，并在同阶段内按传入顺序安装。

### 3. 产品预设

`appkit.BuildDeepAgent(...)` 基于 `appkit` 扩展出一条完整产品路径，内部通过声明式 preset packs 组合默认能力，默认接入：

- session / checkpoint / task runtime
- 持久记忆
- `offload_context` + `compact_conversation`
- workspace isolation / repo state / patch apply / patch revert
- 通用 delegated agent
- planning / mailbox / task graph 相关能力

`apps\mosscode`、`examples\mossresearch`、`examples\mosswriter` 都是在这条路径上继续叠加产品能力。

## 运行时能力加载模型

`harness.RuntimeSetup(...)` 默认加载四类能力：

1. **Builtin tools**
2. **MCP servers**
3. **Prompt skills (`SKILL.md`)**
4. **Subagents**

并通过 harness-owned setup options 控制是否启用：

- `harness.WithBuiltinTools(false)`
- `harness.WithMCPServers(false)`
- `harness.WithSkills(false)`
- `harness.WithProgressiveSkills(true)`
- `harness.WithAgents(false)`

## 当前产品面位置

仓库已经不是“单个 `cmd\moss` 二进制 + 所有能力都在根 CLI” 的布局。当前真实入口分为 `apps\` 和 `examples\`：

- `apps\mosscode`：最完整的 coding agent 核心应用，也是 `moss` CLI 的目标产品面
- `apps\mosswork`：桌面协作核心应用
- `examples\mossresearch`：研究型 orchestrator
- `examples\mosswriter`：写作型 orchestrator
- `examples\mossclaw`：assistant / gateway / schedule / knowledge 组合示例

这意味着主文档也应围绕 **library API + core apps + reference examples** 叙述，而不是围绕旧的单体 CLI 叙述。

## 配置与信任边界

配置由 `config\` 统一管理：

- 每个应用通过 `config.SetAppName(...)` 绑定自己的目录
- 全局配置在 `~\.<app>\config.yaml`
- project assets 是否允许加载，取决于 trust：
  - `trusted`：允许项目级配置、skill、bootstrap、MCP 资产
  - `restricted`：只允许安全默认面

旧的 `runtime\profile` 兼容层已经删除。当前执行姿态由 typed `SessionSpec/ResolvedSessionSpec`、`collaboration_mode`、`permission_profile` 与 runtime policy 直接驱动。

## 扩展桥接

`kernel\ExtensionBridge` 是当前扩展层与 Kernel 的正式桥：

- `OnBoot`
- `OnShutdown`
- `OnSystemPrompt`
- `OnSessionLifecycle`
- `OnToolLifecycle`

这让扩展可以按顺序接入生命周期，而不把业务语义塞回内核。

## Go 模块结构

仓库包含两个核心模块和若干独立模块：

| 模块 | 路径 | 说明 |
|---|---|---|
| `github.com/mossagents/moss/kernel` | `kernel/` | 核心运行时原语，零外部依赖（仅 stdlib + uuid/yaml/otel） |
| `github.com/mossagents/moss/harness` | `harness/` | 编排层 + 全部上层能力，依赖 kernel |
| `github.com/mossagents/moss/contrib/*` | `contrib/` | 可选扩展（TUI、telemetry、tools） |
| `github.com/mossagents/moss/apps/*` | `apps/` | 核心应用（mosscode、mosswork） |
| `github.com/mossagents/moss/examples/*` | `examples/` | 参考实现与集成示例 |

本地开发通过根目录 `go.work` 统一管理所有模块。

## 当前包布局

| 目录 | 说明 |
|---|---|
| `kernel\` | 核心运行时原语（Agent、`RunAgent`、Session、Event、Tool、Plugin） |
| `kernel\patterns\` | Agent 编排原语（Sequential、Parallel、Loop、Supervisor、Research） |
| `kernel\artifact\` | Artifact 存储接口与内存实现 |
| `harness\` | 可组合编排层（Feature、Backend、Harness） |
| `harness\appkit\` | 构建器、扩展 API 与 `BuildDeepAgent(...)` 预设装配 |
| `harness\bootstrap\` | 启动上下文加载 |
| `harness\config\` | 配置、模板上下文与项目级规则 |
| `harness\providers\` | LLM / embedder provider 构建 |
| `harness\extensions\` | 扩展能力命名空间：`skill\`（`SKILL.md` 解析与发现）、`mcp\`（外部 MCP server 桥接）、`agent\`（委派代理注册与任务运行时协作）、`knowledge\`（知识库抽象）、`capability\`（扩展 Provider 注册与版本管理）|
| `harness\runtime\` | Layer 2 实现层；子包：`policy\`（工具策略编译）、`hooks\governance\`（policy/rbac/ratelimit/rag）、`hooks\declarative\`（YAML 驱动的声明式 hook）、`memory\`（记忆存储）、`scheduling\`（调度）、`events\`（事件分发）、`state\`、`builtintools\`|
| `harness\runtime\assembly\` | 装配链 |
| `harness\runtime\capstate\` | 能力状态绑定 |
| `harness\runtime\runctx\` | 执行上下文 |
| `harness\runtime\execution\` | 执行基础设施 |
| `harness\runtime\planning\` | 规划实现 |
| `harness\scheduler\` | 调度器 |
| `harness\gateway\` | Channel / Router / Serve 相关能力 |
| `harness\distributed\` | 分布式实现原型 |
| `harness\sandbox\` | 本地 / Docker 等执行隔离实现 |

**一句话总结**

**Kernel 保持最小运行时原语（含编排原语 `patterns\`），Harness 提供可组合编排能力，appkit 负责应用拼装与 `BuildDeepAgent(...)` 产品预设，apps 提供核心入口，examples 提供参考实现。**
