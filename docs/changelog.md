# 📋 开发日志 (Changelog)

---

## v0.3.0 — 架构审查与文档更新 (2026-03-25)

### 变更

- **architecture.md**：新增应用模式指南（多轮会话复用、Per-Instance Kernel、自定义 UserIO 适配器）和子系统成熟度表
- **kernel-design.md**：更新目录结构（补充 agent/, skill/, appkit/, gateway/, knowledge/, scheduler/ 等实际包），新增 Session 多轮复用说明，新增 miniroom 架构验证案例
- **roadmap.md**：更新已完成模块表（补充 Agent 委派、Session 持久化、Gateway/Knowledge 实验性标注），更新示例列表
- **README.md**：示例应用表补充 miniroom 和 minitrade，项目结构树补充 agent/、appkit/、gateway/、knowledge/、scheduler/ 等包
- **getting-started.md**：更新示例应用列表

---

## v0.2.0 — 示例完善与 Prompt 模板化 (2026-03-24)

> Recent commits: `5ef1541`, `a8d3d99`, `2eb9182`, `d78b0ed`, `6e11e75`, `f211059`

### 新增

- 新增 4 个示例应用：
  - `examples/minicode`（代码助手）
  - `examples/miniwork`（多 Agent 编排）
  - `examples/miniclaw`（Web 抓取）
  - `examples/miniloop`（有状态自主循环 Agent，内置 trading 领域）
- 新增应用名配置能力：`skill.SetAppName(name)` / `skill.AppName()`
  - 支持将全局配置目录从 `~/.moss` 切换到 `~/.<appName>`
- 新增 system prompt 模板机制（Moss 和 examples）
  - 项目级覆盖：`./.<appName>/system_prompt.tmpl`
  - 全局级覆盖：`~/.<appName>/system_prompt.tmpl`

### 变更

- `minicode` 由 REPL 迁移为默认 TUI 交互方式
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
  - 自动注册 BuiltinTool（6 个内置工具）
  - 自动加载 MCP Skills（从 `~/.moss/config.yaml` 和 `./moss.yaml`）
  - 自动发现 Skills（从标准目录的 `SKILL.md`）
  - 支持 `SetupOption`：`WithoutBuiltin()`, `WithoutMCPServers()`, `WithoutSkills()`, `WithWarningWriter()`
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
- **BuiltinTool** — 内置 6 工具的 Provider 封装
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
