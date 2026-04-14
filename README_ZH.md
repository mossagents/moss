# Moss

**面向 Go 的 Agent Harness：快速装配，安全运行。**

Moss 是一个以库优先（library-first）为核心的 Go Agent Runtime。当前仓库围绕一个可复用的最小内核、一个带默认能力装配的运行时层、`apps\` 下的两个核心应用，以及 `examples\` 下的参考示例组织。

英文说明请看 [`README.md`](README.md)。

## 当前仓库提供什么

- 三层运行时架构，用于构建 Go AI Agent：
  - **Kernel** — 核心运行时原语（Agent 接口、request-shaped `RunAgent`、Session、Event、Tool、Plugin）。
  - **Harness** — 可组合编排层（Feature/Backend/Harness），将能力装配到 Kernel。
  - **Applications** — 面向终端用户的产品（`apps\mosscode`、`apps\mosswork`）及参考示例。
- `appkit`：按 `AppFlags` 构建完整 Kernel 的推荐入口。
- `appkit.BuildDeepAgent(...)`：适合 coding / research / writer 产品面的完整预设路径。
- `apps\`：当前仓库里的核心应用入口，其中 `apps\mosscode` 是主交互产品面，打包后的 `moss` CLI 入口指向 `mosscode`。
- `examples\`：普通参考示例目录。

## 快速开始

### 1. 先运行主应用

当前仓库里最完整的交互式产品面是 `apps\mosscode`。

```powershell
Set-Location apps\mosscode
go run . --provider openai --model gpt-4o
```

常见变体：

```powershell
# 交互式 TUI
go run .

# 一次性执行
go run . --prompt "Summarize the repository structure"

# 环境诊断
go run . doctor
```

### 2. 作为 Go 库集成

```go
package main

import (
	"context"
	"os"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/io"
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
		Goal:     "Read README.md and summarize it",
		Mode:     "oneshot",
		MaxSteps: 20,
	})
	if err != nil {
		panic(err)
	}
	userMsg := mdl.Message{
		Role: mdl.RoleUser,
		ContentParts: []mdl.ContentPart{
			mdl.TextPart("Read README.md and summarize it"),
		},
	}
	sess.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &userMsg,
	})
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

如果要按 Feature 优先方式装配，使用 `appkit.BuildKernelWithFeatures(...)`；如果要做更完整的 deep-agent 产品，使用 `appkit.BuildDeepAgent(...)`。

### 3. 使用 Harness 层

`harness` 包提供可组合的 Feature，将工具、Hook 和系统提示词扩展安装到 Kernel：

```go
package main

import (
	"context"
	"time"

	"github.com/mossagents/moss/harness"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/retry"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
)

func main() {
	ctx := context.Background()

	sb, _ := sandbox.NewLocal(".")
	k := kernel.New(
		kernel.WithLLM(myLLM),
		kernel.WithSandbox(sb),
		kernel.WithUserIO(myIO),
	)

	backend := &harness.LocalBackend{
		Workspace: k.Workspace(),
		Executor:  k.Executor(),
	}
	h := harness.New(k, backend)
	_ = h.Install(ctx,
		harness.BootstrapContext(".", "myapp", "trusted"),
		harness.LLMResilience(&retry.Config{
			MaxRetries:   3,
			InitialDelay: 500 * time.Millisecond,
		}, nil),
		harness.PatchToolCalls(),
	)

	_ = k.Boot(ctx)
	defer k.Shutdown(ctx)

	sess, _ := k.NewSession(ctx, session.SessionConfig{
		Goal: "help me", MaxSteps: 50,
	})
	userMsg := mdl.Message{
		Role: mdl.RoleUser,
		ContentParts: []mdl.ContentPart{mdl.TextPart("Hello")},
	}
	sess.AppendMessage(userMsg)
	result, _ := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("root"),
		UserContent: &userMsg,
	})
	println(result.Output)
}
```

如果希望由 harness 托管 backend 的构建与生命周期，优先使用 `harness.NewWithBackendFactory(ctx, k, harness.NewLocalBackendFactory(workspace))`；`appkit.BuildKernel(...)` 和 `appkit.BuildDeepAgent(...)` 现在默认走这条路径。

## 仓库结构

| 路径 | 作用 |
|---|---|
| `kernel\` | 核心运行时原语（Agent、`RunAgent`、Session、Event、Tool、Plugin） |
| `harness\` | 可组合编排层（Feature、Backend、Harness） |
| `appkit\` | 推荐构建器、扩展组合 API，以及 deep-agent 预设装配路径 |
| `appkit\runtime\` | 默认能力装配（builtin tools、MCP、skills、subagents、memory、context、scheduling） |
| `skill\` / `mcp\` / `agent\` | 能力 provider、MCP 桥接、委派代理 |
| `bootstrap\`、`config\`、`providers\`、`logging\` | 支撑包 |
| `knowledge\`、`scheduler\`、`gateway\`、`distributed\`、`sandbox\` | 更高层运行时积木 |
| `apps\` | 核心应用入口（`mosscode`、`mosswork`） |
| `examples\` | 可运行参考示例与集成样例 |

## 配置

核心配置包默认应用名是 `moss`，因此如果你直接以默认命名嵌入库，配置路径通常是：

```text
~\.moss\config.yaml
```

核心应用和示例都会覆盖应用名，因此会使用各自目录，例如：

- `~\.mosscode\config.yaml`
- `~\.mossresearch\config.yaml`
- `~\.mosswriter\config.yaml`

典型配置：

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
default_profile: coding

skills:
  - name: github
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
```

优先级：

**命令行参数 > 环境变量 > 配置文件**

## 应用与示例

`apps\` 下的核心应用：

- `mosscode` - 代码代理产品面，也是打包后 `moss` CLI 的目标应用
- `mosswork` - 桌面协作助理

`examples\` 目录中的参考入口：

- `mossresearch` - 深度研究编排
- `mosswriter` - 写作工作流代理
- `mossclaw` - assistant / gateway / scheduling / knowledge 示例
- `mossquant` - 有状态分析循环
- `mossroom` - 实时多人房间
- `basic`、`custom-tool`、`websocket` - 聚焦型集成示例

## 文档导航

- [快速开始](docs/getting-started.md)
- [架构设计](docs/architecture.md)
- [技能系统](docs/skills.md)
- [内核设计](docs/kernel-design.md)
- [生产准备度](docs/production-readiness.md)
- [变更日志](docs/changelog.md)
- [路线图](docs/roadmap.md)

## 开发校验

```powershell
go test ./...
go build ./...
```

## 兼容性

- 模块路径：`github.com/mossagents/moss`
- `go.mod` 目标版本：Go `1.25.0`

## 许可证

MIT
