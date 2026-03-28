# 📋 开发日志 (Changelog)

---

## v0.4.0 — 动态模型路由 (2026-03-25)

### 新增

- **ModelRouter (`adapters/router.go`)**：支持按任务需求动态选择模型
  - 支持在一个 YAML 文件中声明多个模型配置
  - 支持能力标签匹配（如 `image_generation`、`reasoning`、`function_calling`）
  - 支持成本约束（`cost_tier` + `max_cost_tier`）
  - 支持选择偏好（`prefer_cheap`）
  - 无可用模型时返回详细诊断错误
- **模型能力类型 (`kernel/port/capability.go`)**
  - 新增 `ModelCapability` 能力枚举
  - 新增 `TaskRequirement` 任务需求结构

### 变更

- `ModelConfig` 新增 `requirements` 字段（`kernel/port/llm.go`）
- `SessionConfig` 新增 `model_config` 字段（`kernel/session/session.go`）
- Agent Loop 在调用 LLM 时透传 `session.Config.ModelConfig`（`kernel/loop/loop.go`）
- README 与 Getting Started 文档同步新增动态模型路由配置与使用示例

### 兼容性

- 向后兼容：现有代码若不设置 `model_config` / `requirements`，行为与此前一致（使用默认模型）

---

## Unreleased — Progressive Skills (Phase 3)

### 新增

- `skill.DiscoverSkillManifests(workspace)`：仅发现 Skill 元信息（名称/描述/来源），不加载正文
- `defaults.WithProgressiveSkills()`：启用按需技能加载模式（默认仍为 eager）
- 按需技能工具：
  - `list_skills`：列出可用技能与加载状态
  - `activate_skill`：按名称激活并加载 `SKILL.md`

### 变更

- `defaults.Setup` 的 Skill 发现逻辑改为 manifest 驱动：
  - eager 模式：发现 manifest 后立即加载全部 skill（兼容旧行为）
  - progressive 模式：仅注册清单，运行时按需激活

### 兼容性

- 向后兼容：未启用 `WithProgressiveSkills()` 时行为与此前一致。

---

## Unreleased — Runtime Consolidation (Phase 1/2)

### 新增

- 新增 `appkit/runtime` 作为运行时装配入口：
  - `runtime.Setup(ctx, k, workspaceDir, opts...)`
  - `runtime.WithBuiltinTools(bool)`
  - `runtime.WithMCPServers(bool)`
  - `runtime.WithSkills(bool)`
  - `runtime.WithProgressiveSkills(bool)`
  - `runtime.WithAgents(bool)`
- 新增 runtime facade API：
  - `runtime.SkillsManager`
  - `runtime.SkillManifests`
  - `runtime.SetSkillManifests`
  - `runtime.EnableProgressiveSkills`
  - `runtime.RegisterProgressiveSkillTools`
  - `runtime.AgentRegistry`

### 变更

- `appkit`、`cmd/moss`、`userio/tui` 已切换到 `appkit/runtime` API 路径。
- 旧 `extensions/*` shim 已移除，统一通过 `appkit/runtime` 与 `appkit` 装配。

### 迁移说明

- 推荐从旧 API 迁移到新 API：
  - `defaults.Setup` -> `runtime.Setup`
  - `defaults.WithoutBuiltin` -> `runtime.WithBuiltinTools(false)`
  - `defaults.WithoutMCPServers` -> `runtime.WithMCPServers(false)`
  - `defaults.WithoutSkills` -> `runtime.WithSkills(false)`
  - `defaults.WithProgressiveSkills` -> `runtime.WithProgressiveSkills(true)`
  - `skillsx.Manager` -> `runtime.SkillsManager`
  - `agentsx.Registry` -> `runtime.AgentRegistry`

---

## Unreleased — Persistent Memories (Phase 4, partial)

### 新增

- `appkit/runtime`：持久 memory 命名空间工具
  - 工具：`read_memory` / `write_memory` / `list_memories` / `delete_memory`
  - prompt 提示：声明 `/memories` 持久语义
- `appkit.WithPersistentMemories(memoriesDir)`：按目录装配持久记忆工具
- `BuildDeepAgentKernel` 默认接入持久记忆（可配置关闭）

### 变更

- `DeepAgentConfig` 新增：
  - `EnablePersistentMemories`
  - `MemoryDir`
- restricted 模式默认审批策略新增 `write_memory` 与 `delete_memory`

### 兼容性

- 向后兼容：memory 能力为扩展层加法，不影响 kernel 核心 API。

---

## Unreleased — Context Offload (Phase 5, partial)

### 新增

- `appkit/runtime`：上下文 offload 能力
  - 工具：`offload_context`
  - 能力：将旧对话快照保存到 `SessionStore`，并压缩当前 session 消息
- `kernel/session/context.go`：
  - `LastNDialogMessages`
  - `BuildCompactedMessages`
- `appkit.WithContextOffload(store)`：统一装配 context offload 工具
- `BuildDeepAgentKernel` 默认启用 context offload（在启用 session store 时）

### 变更

- `DeepAgentConfig` 新增 `EnableContextOffload`
- restricted 模式默认审批策略新增 `offload_context`

### 兼容性

- 向后兼容：offload 为扩展层能力，显式调用工具时才生效；可通过配置关闭。

---

## Unreleased — Async Orchestration + TUI UX (Phase 6/7, partial)

### 新增

- Agent 异步委派生命周期工具：
  - `list_tasks`
  - `cancel_task`
- `task` 工具扩展模式：
  - `mode=list`
  - `mode=cancel`
- TUI 新增斜杠命令：
  - `/session`
  - `/offload`
  - `/tasks`
  - `/task`
  - `/task cancel`

### 变更

- `agent.Task` 增加 `created_at` / `updated_at`
- `TaskTracker` 支持任务列表查询与可取消后台任务的 cancel hook
- `BuildDeepAgentKernel` 默认排除 `list_tasks` / `cancel_task` 不注入 general-purpose 子代理
- restricted 模式默认审批策略新增 `cancel_task`

### 兼容性

- 向后兼容：原有 `delegate_agent` / `spawn_agent` / `query_agent` / `task`（sync/background/query）行为保持不变。

---

## Unreleased — Deepagents parity hardening

### 新增

- `appkit/runtime`（planning）：
  - `write_todos` 规划工具（会话级 todo 状态写入 `session.State["planning.todos"]`）
- `appkit/runtime`（context）：
  - `compact_conversation` 工具
  - `AutoCompactMiddleware`（阈值触发总结 + 快照 offload）
- `agent` 异步生命周期增强：
  - `update_task` 工具
  - `task mode=update`
  - Task `revision` 防并发回写覆盖
- `kernel/middleware/builtins.PatchToolCalls()`：
  - 在 `BeforeLLM` 阶段补齐缺失的 tool result（修复孤儿 tool_call 历史）
- `presets/deepagent` 包：
  - 新入口 `deepagent.BuildKernel` / `deepagent.DefaultConfig`

### 变更

- `kernel/loop` 在执行工具时注入 `ToolCallContext`（session_id/tool_name/call_id）
- `run_command` 在输出过大时自动 offload 到 `.moss/large_tool_results/*.json`，并返回路径与预览
- `BuildDeepAgentKernel` 默认挂载 `PatchToolCalls`，并将 `update_task` 从 general-purpose 工具白名单中排除
- 示例 `examples/mosscode` 与 `examples/mosswork-desktop` 迁移到 `presets/deepagent`

### 兼容性

- 兼容：`appkit.BuildDeepAgentKernel` 保留，`presets/deepagent` 为新增推荐入口。

---

## v0.3.0 — 架构审查与文档更新 (2026-03-25)

### 变更

- **architecture.md**：新增应用模式指南（多轮会话复用、Per-Instance Kernel、自定义 UserIO 适配器）和子系统成熟度表
- **kernel-design.md**：更新目录结构（补充 agent/, skill/, appkit/, gateway/, knowledge/, scheduler/ 等实际包），新增 Session 多轮复用说明，新增 mossroom 架构验证案例
- **roadmap.md**：更新已完成模块表（补充 Agent 委派、Session 持久化、Gateway/Knowledge 实验性标注），更新示例列表
- **README.md**：示例应用表补充 mossroom 和 mossquant，项目结构树补充 agent/、appkit/、gateway/、knowledge/、scheduler/ 等包
- **getting-started.md**：更新示例应用列表

---

## v0.2.0 — 示例完善与 Prompt 模板化 (2026-03-24)

> Recent commits: `5ef1541`, `a8d3d99`, `2eb9182`, `d78b0ed`, `6e11e75`, `f211059`

### 新增

- 新增 4 个示例应用：
  - `examples/mosscode`（代码助手）
  - `examples/mosswork`（多 Agent 编排）
  - `examples/mossclaw`（Web 抓取）
  - `examples/mossquant`（有状态自主循环 Agent，内置 trading 领域）
- 新增应用名配置能力：`skill.SetAppName(name)` / `skill.AppName()`
  - 支持将全局配置目录从 `~/.moss` 切换到 `~/.<appName>`
- 新增 system prompt 模板机制（Moss 和 examples）
  - 项目级覆盖：`./.<appName>/system_prompt.tmpl`
  - 全局级覆盖：`~/.<appName>/system_prompt.tmpl`

### 变更

- `mosscode` 由 REPL 迁移为默认 TUI 交互方式
- 全局配置策略统一为 `config.yaml`（不再使用 `config.yml`）
- 配置目录初始化时自动创建配置模板文件（首次创建）

### 修复

- 修复 `MergeConfigs` 在配置加载异常场景下的空指针风险
- 修复自动生成配置模板中的 YAML 缩进问题
- 修复配置读取失败时导致默认 provider 回退行为不透明的问题

---

## v0.1.0 — 库友好 API (2026-03-24)

> Commit: `9ca8b7c`

### 新增

- **`Kernel.SetupWithDefaults()`** — 一行代码替代 30+ 行手动注册
  - 自动注册 runtime builtin tools provider（现为 8 个内置工具）
  - 自动加载 MCP Skills（从 `~/.moss/config.yaml` 和 `./moss.yaml`）
  - 自动发现 Skills（从标准目录的 `SKILL.md`）
  - 支持 `SetupOption`：`WithoutBuiltin()`, `WithoutMCPServers()`, `WithoutSkills()`
  - 警告和错误通过 slog 输出
- **标准 `UserIO` 实现** (`kernel/port/io_std.go`)
  - `NoOpIO` — 静默模式，Ask 返回安全默认值
  - `PrintfIO` — 格式化输出到 `io.Writer`，自动批准确认
  - `BufferIO` — 线程安全的测试用缓冲，支持 `AskFunc` 钩子
- **增强 `Boot()` 验证** — 同时检查 LLM 和 UserIO，报错包含修复建议

### 变更

- `cmd/moss/main.go` 重构为使用 `SetupWithDefaults`
- 移除 `cmd/moss` 对 `kernel/tool/builtins` 的直接依赖

---

## v0.0.4 — 配置中心化 (2026-03-24)

> Commit: `599a8d5`

### 新增

- 全局配置文件 `~/.moss/config.yaml`
  - 持久化 Provider / Model / BaseURL / APIKey
  - CLI 参数 > 配置文件 > 环境变量
- `--base-url` 和 `--api-key` CLI 参数
- `NewWithBaseURL()` 构造函数（Claude 和 OpenAI adapter）
- TUI Welcome 页面自动保存配置
- TUI `/config` 斜杠命令（查看和修改配置）

---

## v0.0.3 — 技能系统 (2026-03-23)

> Commits: `868f4e9` ~ `a147d57`

### 新增

- **Skill 接口** + `SkillManager` — 统一扩展入口
- **MCP Client** — 通过 MCP 协议连接外部工具服务器（stdio / SSE）
- **Builtin Tools Provider** — runtime 内置工具的默认 Provider 封装
- **Skill** — 兼容 skills.sh 的 `SKILL.md` 系统提示词注入
- `DiscoverSkills()` — 从标准目录自动发现
- CLI 输出已加载的 Skills 信息

---

## v0.0.2 — TUI 交互 (2026-03-22)

> Commits: `94f028e` ~ `832e0d9`

### 新增

- Bubble Tea 交互式 TUI
  - Welcome 页面（选择 Provider / Model / Workspace）
  - Chat 页面（流式输出、工具调用显示）
  - 消息渲染（8 种消息类型）
- 斜杠命令：`/exit`, `/model`, `/clear`, `/help`
- BridgeIO — TUI ↔ Kernel 的线程安全桥接

---

## v0.0.1 — Kernel 核心 (2026-03-21)

### 新增

- **Kernel** — 5 核心概念 + 2 Port 接口
  - Loop: Agent 执行循环 (think→act→observe)
  - Tool: 能力注册、查找、执行
  - Session: 执行上下文 (消息+状态+预算)
  - Middleware: 统一扩展点（洋葱模型）
  - Sandbox: 执行隔离 (路径逃逸保护)
- **Port 接口**
  - LLM: Complete + Stream（支持流式）
  - UserIO: Send + Ask（结构化交互协议）
- **内置 Middleware** — PolicyCheck, EventEmitter, Logger
- **Adapters** — Claude (anthropic-sdk-go) + OpenAI (openai-go)
- **LocalSandbox** — 路径逃逸保护 + 资源限制
- **Mock 适配器** — MockLLM, MemorySandbox, RecorderIO
