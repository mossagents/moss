# 架构概览

Moss 当前采用 **最小内核 + runtime 装配层 + appkit 扩展层 + apps 核心应用 + examples 参考示例** 的结构。核心原则是：**把稳定的运行时原语留在 `kernel\`，把可组合能力放在顶层包和预设里。**

## 分层

```text
Applications / Products
  apps\mosscode, apps\mosswork
  examples\mossresearch, mosswriter, mossclaw, ...

Assembly / Presets
  appkit
  presets\deepagent

Runtime capability loading
  appkit\runtime
  skill
  mcp
  agent

Core runtime
  kernel

Infrastructure / support packages
  bootstrap  config  providers  logging
  knowledge  scheduler  gateway  distributed  sandbox
```

## 关键职责边界

| 层 | 主要职责 |
|---|---|
| `kernel\` | `Kernel`、Session、Tool、Middleware、Model、UserIO、Workspace/Executor、Task、Observe、Checkpoint |
| `appkit\runtime\` | 默认能力装配：builtin tools、MCP、`SKILL.md`、subagent、context、memory、knowledge、scheduling |
| `appkit\` | 面向应用的构建入口与扩展组合 API |
| `presets\deepagent\` | 深代理预设，组合持久化、上下文压缩、委派与工作区隔离 |
| `skill\` / `mcp\` / `agent\` | 能力提供者、外部工具桥接、委派代理注册 |
| `apps\` | 核心应用入口 |
| `examples\` | 参考实现与集成示例 |

## 推荐装配路径

### 1. 标准装配

`appkit.BuildKernel(...)`：

1. 根据 `AppFlags` 构建 LLM adapter
2. 创建本地 `Sandbox`
3. 建立 `kernel.Kernel`
4. 调用 `runtime.Setup(...)`
5. 加载 builtin tools / MCP / skills / agents

这是最短的“库优先”入口。

### 2. 扩展装配

`appkit.BuildKernelWithExtensions(...)` 在上述基础上再拼装：

- `WithSessionStore`
- `WithPlanning`
- `WithContextOffload`
- `WithContextManagement`
- `WithLoadedBootstrapContextWithTrust`
- `WithScheduling`
- `WithKnowledge`
- `WithPersistentMemories`
- `AfterBuild`

这也是当前核心应用和较完整示例最常用的装配方式。

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
| `kernel\` | 核心运行时原语 |
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

**Kernel 保持最小，runtime 负责默认能力装配，appkit 负责应用拼装，deepagent 负责产品级预设，apps 提供核心入口，examples 提供参考实现。**
