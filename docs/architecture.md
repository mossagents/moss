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
│ Layer 2: Harness + Presets                         │
│   harness (Feature/Backend/Harness)                │
│   harness\patterns (Agent 编排原语)                 │
│   appkit + appkit\runtime                          │
│   presets\deepagent (thin wrapper)                  │
│   → 可复用的 Agent 编排模式                         │
└────────────────────────────────────────────────────┘
                         ↕
┌────────────────────────────────────────────────────┐
│ Layer 1: Kernel                                    │
│   Agent 接口 + Runner + Session + Event            │
│   LLM 抽象 + Tool 系统 + Plugin 系统               │
│   → 最小核心运行时原语                              │
└────────────────────────────────────────────────────┘
```

## 关键职责边界

| 层 | 主要职责 |
|---|---|
| `kernel\` | Agent 接口、Runner、Session、Event、Tool、Plugin、Model、UserIO、Workspace/Executor、Task、Observe、Checkpoint |
| `harness\` | Feature 接口、Backend 接口、Harness 组合器 — 将能力可组合地安装到 Kernel |
| `harness\patterns\` | Agent 编排原语：Sequential、Parallel、Loop、Supervisor、Research、DeepAgent |
| `appkit\runtime\` | 默认能力装配：builtin tools、MCP、`SKILL.md`、subagent、context、memory、knowledge、scheduling |
| `appkit\` | 面向应用的构建入口与扩展组合 API |
| `presets\deepagent\` | 深代理预设，组合持久化、上下文压缩、委派与工作区隔离 |
| `skill\` / `mcp\` / `agent\` | 能力提供者、外部工具桥接、委派代理注册 |
| `apps\` | 核心应用入口 |
| `examples\` | 参考实现与集成示例 |

## Harness 层

`harness\` 包引入了 **Feature / Backend / Harness** 三个核心概念：

- **Feature**：一个可组合的能力单元，实现 `Name() string` + `Install(ctx, *Harness) error`。Feature 通过 `Harness.Install()` 安装，将 Plugin、工具、系统提示词等注入 Kernel。
- **Backend**：统一的后端抽象，组合 `workspace.Workspace` + `workspace.Executor`。`LocalBackend` 是默认实现。
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

## Agent 编排模式 (`harness\patterns\`)

`harness\patterns\` 包提供可组合的 Agent 编排原语，所有模式均实现 `kernel.Agent` 接口：

| Pattern | 用途 | 核心参数 |
|---|---|---|
| `SequentialAgent` | 顺序执行多个子 Agent | `Agents []Agent` |
| `ParallelAgent` | 并发执行多个子 Agent，结果聚合 | `Agents []Agent`, `Aggregator func` |
| `LoopAgent` | 迭代执行，支持退出条件 | `Agent`, `MaxIterations`, `ShouldExit func` |
| `SupervisorAgent` | 动态路由到工作 Agent | `Workers []Agent`, `Router func` |
| `ResearchAgent` | 研究型编排（Query→Search→Synthesis 循环） | `QueryAgent`, `SearchAgent`, `SynthesisAgent` |
| `BuildDeepAgent` | 完整 deep-agent 预设（coding agent） | `DeepAgentConfig` |

编排模式支持任意嵌套组合，例如 `Sequential[Prepare, Parallel[Search1, Search2], Summarize]`。

内置路由策略：
- `RoundRobinRouter(stateKey)` — 轮询分配
- `FirstMatchRouter(predicate)` — 按条件匹配首个 Agent

## 推荐装配路径

### 1. 标准装配

`appkit.BuildKernel(...)`：

1. 根据 `AppFlags` 构建 LLM adapter
2. 创建本地 `Sandbox`
3. 建立 `kernel.Kernel`
4. 调用 `runtime.Setup(...)`
5. 加载 builtin tools / MCP / skills / agents

这是最短的“库优先”入口。

### 2. Feature 装配

`appkit.BuildKernelWithFeatures(...)` 在上述基础上再拼装 `harness.Feature`：

- `WithSessionStore`
- `WithPlanning`
- `WithContextOffload`
- `WithContextManagement`
- `WithLoadedBootstrapContextWithTrust`
- `WithScheduling`
- `WithKnowledge`
- `WithPersistentMemories`
- `AfterBuild`

这也是当前核心应用和较完整示例最常用的装配方式。Feature 按传入顺序依次安装到
Harness 上，确保正确的初始化顺序。

### 3. 产品预设

`presets\deepagent.BuildKernel(...)` 基于 `appkit` 扩展出一条完整产品路径，默认接入：

- session / checkpoint / task runtime
- 持久记忆
- `offload_context` + `compact_conversation`
- workspace isolation / repo state / patch apply / patch revert
- 通用 delegated agent
- planning / mailbox / task graph 相关能力

`apps\mosscode`、`examples\mossresearch`、`examples\mosswriter` 都是在这条路径上继续叠加产品能力。

## 运行时能力加载模型

`appkit\runtime.Setup(...)` 默认加载四类能力：

1. **Builtin tools**
2. **MCP servers**
3. **Prompt skills (`SKILL.md`)**
4. **Subagents**

并通过 `runtime.Option` 控制是否启用：

- `WithBuiltinTools(false)`
- `WithMCPServers(false)`
- `WithSkills(false)`
- `WithProgressiveSkills(true)`
- `WithAgents(false)`
- `WithPlanning(false)`
- `WithWorkspaceTrust(...)`

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
  - `trusted`：允许项目级 profile、skill、bootstrap、MCP 配置
  - `restricted`：只允许安全默认面

`runtime\profile.go` 负责把 `profile + trust + approval` 解析为实际执行姿态。

## 扩展桥接

`kernel\ExtensionBridge` 是当前扩展层与 Kernel 的正式桥：

- `OnBoot`
- `OnShutdown`
- `OnSystemPrompt`
- `OnSessionLifecycle`
- `OnToolLifecycle`

这让扩展可以按顺序接入生命周期，而不把业务语义塞回内核。

## 当前包布局

| 目录 | 说明 |
|---|---|
| `kernel\` | 核心运行时原语（Agent、Runner、Session、Event、Tool、Plugin） |
| `harness\` | 可组合编排层（Feature、Backend、Harness） |
| `harness\patterns\` | Agent 编排原语（Sequential、Parallel、Loop、Supervisor、Research、DeepAgent） |
| `appkit\` | 构建器与扩展 API |
| `bootstrap\` | 启动上下文加载 |
| `config\` | 配置、profile、模板上下文 |
| `providers\` | LLM / embedder provider 构建 |
| `skill\` | provider 抽象、`SKILL.md` 解析与发现 |
| `mcp\` | 外部 MCP server 桥接 |
| `agent\` | 委派代理注册与任务运行时协作 |
| `knowledge\` | 知识库抽象 |
| `scheduler\` | 调度器 |
| `gateway\` | Channel / Router / Serve 相关能力 |
| `distributed\` | 分布式实现原型 |
| `sandbox\` | 本地 / Docker 等执行隔离实现 |

## 一句话总结

**Kernel 保持最小运行时原语，Harness 提供可组合编排能力，appkit/deepagent 负责应用拼装与产品预设，apps 提供核心入口，examples 提供参考实现。**
