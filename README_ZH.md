# Moss

**面向 Go 的 Agent Harness：快速装配，安全运行。**

Moss 提供开箱可用的智能体技术栈（CLI + Runtime + 扩展能力），同时保持核心可组合、可嵌入（library-first）。

英文文档请查看 [`README.md`](README.md)。

## 为什么选择 Moss

- 启动快：几分钟内即可使用 `moss` 运行代码智能体。
- 可深度集成：可作为 Go 库嵌入你的系统，并精细控制运行时行为。
- 面向生产：默认具备策略、沙箱、会话和工具边界控制能力。

## 开箱包含

- 任务规划与追踪能力（含 deepagent 风格流程）。
- 文件系统与命令执行工具，支持 trust-level 风险控制。
- 子代理委派能力，适合多代理协作场景。
- 交互式 TUI 与非交互执行模式。
- 扩展友好的架构（middleware + appkit 装配 API）。

## 快速开始

### 1) 安装 CLI

```bash
go install github.com/mossagents/moss/cmd/moss@latest
```

### 2) 在终端运行

```bash
# 交互式 TUI
moss

# 非交互执行
moss run --goal "修复 main.go 中的 bug" --workspace .

# 查看版本
moss version
```

### 3) 作为 Go 库集成

```go
package main

import (
	"context"
	"os"

	"github.com/mossagents/moss/appkit"
	intr "github.com/mossagents/moss/kernel/interaction"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func main() {
	ctx := context.Background()

	k, err := appkit.BuildKernel(ctx, &appkit.AppFlags{
		Provider:  "openai",
		Model:     "gpt-4o",
		Workspace: ".",
		APIKey:    os.Getenv("OPENAI_API_KEY"),
	}, intr.NewConsoleIO())
	if err != nil {
		panic(err)
	}

	if err := k.Boot(ctx); err != nil {
		panic(err)
	}
	defer k.Shutdown(ctx)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:     "Fix the bug in main.go",
		Mode:     "oneshot",
		MaxSteps: 50,
	})
	if err != nil {
		panic(err)
	}
	sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart("Fix the bug in main.go")}})

	result, err := k.Run(ctx, sess)
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

如需按扩展优先方式装配，请使用 `appkit.BuildKernelWithExtensions(...)`。

## CLI 速览

- `moss`：启动交互式 TUI。
- `moss run --goal "..."`：执行单目标任务，支持 `--workspace`、`--provider`、`--model`、`--trust` 等参数。
- `moss version`：输出 CLI 版本。

## 配置

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

优先级：CLI 参数 > 配置文件 > 环境变量

常用环境变量：

- `OPENAI_API_KEY`
- `OPENAI_BASE_URL`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY`（或 `GOOGLE_API_KEY`）
- `MOSS_DEBUG=1`（将调试日志写入 `~/.moss/debug.log`）

## 架构

Moss 由最小运行时核心与顶层功能包组成：

- `kernel/`：运行时原语（loop、tool、session、middleware、port）。
- `appkit/`：高层装配工具。
- `agent/`、`skill/`、`bootstrap/`、`knowledge/`、`scheduler/`、`gateway/`：功能与支撑包。
- `cmd/moss/`：终端 CLI 与 TUI 入口。

## 预设与定制

- 使用 `presets/deepagent` 获取 deepagent 风格默认能力（规划、上下文压缩、任务生命周期）。
- 可通过 middleware 扩展策略、审计、事件和防护逻辑。
- 可通过 runtime setup 与配置扩展自定义工具、技能和 MCP 服务。

## 示例

示例应用位于 `examples/`：

- `examples/mosscode/` - 代码助手
- `examples/mossresearch/` - 深度研究编排（含委派 Web 研究）
- `examples/mosswriter/` - 基于文件系统的内容构建工作流
- `examples/mosswork-desktop/` - 桌面协作助理（含委派代理和持久运行时状态）
- `examples/mossclaw/` - Web 自动化与抓取工作流
- `examples/mossquant/` - 有状态自主循环模式
- `examples/mossroom/` - 多用户实时 Agent 游戏

运行任一示例：

```bash
cd examples/mosscode
go run .
```

## 文档导航

- [快速开始](docs/getting-started.md)
- [架构设计](docs/architecture.md)
- [技能系统](docs/skills.md)
- [内核设计](docs/kernel-design.md)
- [生产准备度](docs/production-readiness.md)
- [变更日志](docs/changelog.md)
- [路线图](docs/roadmap.md)

## 安全模型

Moss 采用工具边界安全模型：Agent 的能力由你显式暴露的工具决定。

建议在沙箱、策略与工具层施加约束，而不是只依赖提示词限制。

## 开发校验

```bash
go vet ./...
go test ./...
pwsh ./testing/validate_examples.ps1
go build ./...
```

按示例逐个校验：

```bash
cd examples/<name>
go test ./...
go build .
```

说明：`go build ./...` 不能作为所有示例模块的严格通过门槛，因为部分打包辅助目录（例如 `examples/mosswork-desktop/build/ios`）并非可独立运行的 `main` 包。

## 兼容性

- 模块路径：`github.com/mossagents/moss`
- `go.mod` 目标版本：Go `1.25.0`

## 许可证

MIT

