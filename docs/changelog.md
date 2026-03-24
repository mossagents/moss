# 📋 开发日志 (Changelog)

---

## v0.1.0 — 库友好 API (2026-03-24)

> Commit: `9ca8b7c`

### 新增

- **`Kernel.SetupWithDefaults()`** — 一行代码替代 30+ 行手动注册
  - 自动注册 CoreSkill（6 个内置工具）
  - 自动加载 MCP Skills（从 `~/.moss/config.yaml` 和 `./moss.yaml`）
  - 自动发现 PromptSkills（从标准目录的 `SKILL.md`）
  - 支持 `SetupOption`：`WithoutCoreSkill()`, `WithoutMCPSkills()`, `WithoutPromptSkills()`, `WithWarningWriter()`
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
- **CoreSkill** — 内置 6 工具的 Skill 封装
- **PromptSkill** — 兼容 skills.sh 的 `SKILL.md` 系统提示词注入
- `DiscoverPromptSkills()` — 从标准目录自动发现
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
