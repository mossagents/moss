# Moss

**Agent harness for Go: compose fast, run safely.**  
**面向 Go 的 Agent Harness：快速装配，安全运行。**

Moss gives you a ready-to-run agent stack (CLI + runtime + extension surface) while keeping the core composable and library-first.  
Moss 提供可直接运行的智能体栈（CLI + Runtime + 扩展面），同时保持核心可组合、可嵌入（library-first）。

> **MossCode**: *A coding-agent harness grounded in your workspace.*  
> **MossCode**：*扎根于你的工作区上下文的代码 Agent Harness。*

---

## Why Moss / 为什么选择 Moss

- **Start fast**: run a coding agent in terminal with `moss` in minutes.  
  **快速启动**：几分钟内用 `moss` 在终端跑起代码智能体。
- **Build your own**: integrate as a Go library and control runtime behavior.  
  **可深度集成**：作为 Go 库嵌入现有系统，按需控制运行时行为。
- **Production-minded defaults**: policy, sandbox, sessions, tools, and extension hooks.  
  **面向生产的默认能力**：策略、沙箱、会话、工具系统与扩展钩子齐备。

---

## What's included / 开箱包含

- **Planning & task tracking**: built-in task-flow capabilities (e.g. deepagent preset tools).  
  **任务规划与追踪**：内置任务流能力（如 deepagent 预设工具）。
- **Filesystem + command execution**: file tools and command execution with trust-level policy.  
  **文件与命令执行**：文件工具与命令执行，支持 trust-level 策略控制。
- **Sub-agent delegation**: task delegation patterns for multi-agent workflows.  
  **子代理委派**：支持多代理工作流中的任务拆分与委派。
- **Interactive TUI + headless run**: use `moss` for TUI, `moss run --goal ...` for scripted flow.  
  **交互式 TUI + 非交互运行**：`moss` 启动 TUI，`moss run --goal ...` 用于脚本化执行。
- **Extension-friendly architecture**: middleware, adapters, and appkit assembly APIs.  
  **扩展友好架构**：中间件、适配器与 appkit 装配 API。

---

## Quickstart / 快速开始

### 1) Install CLI / 安装 CLI

```bash
go install github.com/mossagents/moss/cmd/moss@latest
```

### 2) Run in terminal / 在终端运行

```bash
# Interactive TUI (default)
# 交互式 TUI（默认）
moss

# Non-interactive run
# 非交互执行
moss run --goal "Fix the bug in main.go" --workspace .

# Version
# 版本
moss version
```

### 3) Embed as a Go library / 作为 Go 库集成

```go
package main

import (
	"context"
	"os"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func main() {
	ctx := context.Background()

	k, err := appkit.BuildKernel(ctx, &appkit.AppFlags{
		Provider:  "openai",
		Model:     "gpt-4o",
		Workspace: ".",
		APIKey:    os.Getenv("OPENAI_API_KEY"),
	}, port.NewConsoleIO())
	if err != nil {
		panic(err)
	}

	if err := k.Boot(ctx); err != nil {
		panic(err)
	}
	defer k.Shutdown(ctx)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     "Fix the bug in main.go",
		MaxSteps: 50,
	})
	if err != nil {
		panic(err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: "Fix the bug in main.go"})

	result, err := k.Run(ctx, sess)
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

> For extension-first assembly, use `appkit.BuildKernelWithExtensions(...)`.  
> 如果你希望按官方推荐路径装配扩展，使用 `appkit.BuildKernelWithExtensions(...)`。

---

## CLI at a glance / CLI 能力速览

### `moss`

Launches interactive TUI.  
启动交互式 TUI。

### `moss run --goal "..."`

Runs one goal with flags like `--workspace`, `--provider`, `--model`, `--trust`.  
以单目标执行，支持 `--workspace`、`--provider`、`--model`、`--trust` 等参数。

### `moss version`

Prints CLI version.  
输出 CLI 版本。

---

## Configuration / 配置

Global config path: `~/.moss/config.yaml`  
全局配置路径：`~/.moss/config.yaml`

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
skills:
  - name: my-mcp-server
    transport: stdio
    command: npx
    args: ["-y", "@example/mcp-server"]
```

Priority: CLI flags > config file > env vars  
优先级：CLI 参数 > 配置文件 > 环境变量

Common env vars / 常用环境变量：

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY` (or `GOOGLE_API_KEY`)
- `MOSS_DEBUG=1`（开启 `~/.moss/debug.log` 调试日志落盘）

---

## Architecture / 架构

Moss is organized into a minimal runtime core plus top-level feature packages.  
Moss 由最小运行时核心与顶层功能包组成。

- `kernel/`: runtime primitives (loop, tool, session, middleware, port).  
  `kernel/`：运行时原语（loop、tool、session、middleware、port）。
- `appkit/`: high-level assembly helpers.  
  `appkit/`：高层装配工具。
- `agent/`, `skill/`, `bootstrap/`, `knowledge/`, `scheduler/`, `gateway/`: feature/support packages.  
  `agent/`、`skill/`、`bootstrap/`、`knowledge/`、`scheduler/`、`gateway/`：功能与支撑包。
- `cmd/moss/`: terminal CLI and TUI entrypoints.  
  `cmd/moss/`：终端 CLI 与 TUI 入口。

---

## Presets & customization / 预设与定制

- Use `presets/deepagent` for deepagents-style defaults (planning, context compaction, task lifecycle).  
  使用 `presets/deepagent` 可获得 deepagents 风格默认能力（规划、上下文压缩、任务生命周期）。
- Add middleware for policy, audit, events, and guardrails.  
  通过 middleware 扩展策略、审计、事件与防护逻辑。
- Add custom tools/skills/MCP servers via runtime setup and config.  
  通过 runtime setup 与配置扩展自定义工具、技能和 MCP 服务。

---

## Examples / 示例

Reference applications live in `examples/`:
示例应用位于 `examples/`：

- `examples/mosscode/` - coding assistant
- `examples/mossresearch/` - deep research orchestrator with delegated web research
- `examples/mosswriter/` - content builder agent with filesystem-driven writing workflows
- `examples/mosswork/` - multi-agent orchestration
- `examples/mossclaw/` - web automation/scraping workflows
- `examples/mossquant/` - stateful autonomous loop patterns
- `examples/mossroom/` - realtime multi-user agent game

Run any example:
运行任一示例：

```bash
cd examples/mosscode
go run .
```

---

## Documentation / 文档导航

- [Getting Started](docs/getting-started.md) / [快速开始](docs/getting-started.md)
- [Architecture](docs/architecture.md) / [架构设计](docs/architecture.md)
- [Skills](docs/skills.md) / [技能系统](docs/skills.md)
- [Kernel Design](docs/kernel-design.md) / [内核设计](docs/kernel-design.md)
- [Production Readiness](docs/production-readiness.md) / [生产准备度](docs/production-readiness.md)
- [Changelog](docs/changelog.md) / [变更日志](docs/changelog.md)
- [Roadmap](docs/roadmap.md) / [路线图](docs/roadmap.md)

---

## FAQ

### Is Moss only for CLI usage? / Moss 只能用于 CLI 吗？

No. CLI is one entrypoint. Moss is designed to be embedded as a Go library as well.  
不是。CLI 只是入口之一。Moss 也被设计为可嵌入的 Go 库。

### Can I control risky operations? / 我能控制高风险操作吗？

Yes. Use trust levels and policy middleware to require approvals for sensitive tools/commands.  
可以。通过 trust level 与策略中间件可对敏感工具/命令启用审批。

### Can I bring my own model/provider? / 我能接入自定义模型或供应商吗？

Yes. Moss provides adapter-based integration and model routing patterns.  
可以。Moss 提供基于适配器的模型集成与路由模式。

---

## Security model / 安全模型

Moss follows a tool-boundary model: the agent can do what exposed tools allow.  
Moss 采用工具边界安全模型：Agent 能做的事由你暴露的工具能力决定。

Enforce constraints at sandbox, policy, and tool levels instead of relying on prompt-only restrictions.  
建议在 sandbox、policy 与 tool 层实施约束，而不是仅依赖提示词限制。

---

## Development checks / 开发校验

```bash
go test ./...
go build ./...
```

For multi-module verification (root + `examples/*`), prefer validating each example at module root:

```bash
cd examples/<name>
go test ./...
go build .
```

Note: avoid using `go build ./...` as a strict pass/fail gate for every example module, because some packaging helper directories (for example `examples/mosswork-desktop/build/ios`) are not standalone runnable `main` packages.

---

## License

MIT
