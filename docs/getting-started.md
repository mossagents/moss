# 快速开始

这份仓库当前是 **library-first runtime + examples 产品入口** 的结构：最直接的体验方式是运行 `examples\mosscode`，要嵌入到自己的 Go 应用里则使用 `appkit` 或 `presets\deepagent`。

## 先决条件

- Go `1.25.0`
- 至少一个模型提供商的 API Key（例如 OpenAI、Anthropic 或 Gemini）

## 1. 运行仓库自带产品入口

当前仓库树里最完整的交互式产品面是 `examples\mosscode`。

```powershell
Set-Location examples\mosscode
go run . --provider openai --model gpt-4o
```

常见用法：

```powershell
# 进入交互式 TUI
go run .

# 一次性执行
go run . --prompt "Summarize the repository structure"

# 环境诊断
go run . doctor

# 查看当前配置
go run . debug-config
```

`mosscode` 会使用独立的应用目录，例如：

- 全局配置：`~\.mosscode\config.yaml`
- 会话/检查点/任务/记忆：`~\.mosscode\...`

其它示例应用也遵循同样的模式，例如 `mossresearch` 使用 `~\.mossresearch`，`mosswriter` 使用 `~\.mosswriter`。

## 2. 配置模型与运行参数

每个应用都会先合并全局配置、环境变量和命令行参数。以 `mosscode` 为例，可在 `~\.mosscode\config.yaml` 中设置：

```yaml
provider: openai
model: gpt-4o
base_url: ""
api_key: ""
default_profile: coding

profiles:
  coding:
    label: Coding
    task_mode: coding
    trust: trusted
    approval: confirm

skills:
  - name: github
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
```

优先级保持一致：

**命令行参数 > 环境变量 > 全局配置文件**

常见环境变量：

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `GEMINI_API_KEY` / `GOOGLE_API_KEY`
- `MOSSCODE_ROUTER_CONFIG`
- `MOSSCODE_LLM_FAILOVER`

## 3. 作为 Go 库嵌入

### 最短路径：`appkit.BuildKernel`

如果你只需要一个带默认运行时装配的 Kernel，直接使用 `appkit.BuildKernel(...)`：

```go
package main

import (
	"context"
	"os"

	"github.com/mossagents/moss/appkit"
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
	sess.AppendMessage(mdl.Message{
		Role: mdl.RoleUser,
		ContentParts: []mdl.ContentPart{
			mdl.TextPart("Read README.md and summarize it"),
		},
	})

	result, err := k.Run(ctx, sess)
	if err != nil {
		panic(err)
	}
	println(result.Output)
}
```

这条路径会自动完成：

- LLM adapter 构建
- 本地 `Sandbox` / `Workspace` / `Executor`
- `runtime.Setup(...)` 默认能力装配
- 内置工具、MCP、`SKILL.md`、subagent 注册

### 扩展优先路径：`appkit.BuildKernelWithExtensions`

当你需要持久会话、调度、知识库、记忆或额外安装逻辑时，使用：

```go
k, err := appkit.BuildKernelWithExtensions(ctx, flags, io,
	appkit.WithSessionStore(store),
	appkit.WithPersistentMemories(".\\.moss\\memories"),
	appkit.WithContextOffload(store),
	appkit.WithScheduling(sched),
	appkit.AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
		return registerMyTools(k.ToolRegistry())
	}),
)
```

### Deep Agent 预设：`presets\deepagent`

如果你需要更完整的“coding / research / writer”式产品能力，直接使用：

```go
import "github.com/mossagents/moss/presets/deepagent"

k, err := deepagent.BuildKernel(ctx, flags, io, nil)
```

默认会接入：

- Session store / checkpoint store / task runtime
- 持久记忆与上下文压缩
- workspace isolation / repo state / patch apply / rollback
- 通用委派代理 `general-purpose`
- planning、task、mailbox 等协作能力

## 4. 你应该选哪条路径

| 场景 | 推荐入口 |
|---|---|
| 想马上体验当前仓库能力 | `examples\mosscode` |
| 想构建最小可运行应用 | `appkit.BuildKernel` |
| 想按官方扩展方式组合能力 | `appkit.BuildKernelWithExtensions` |
| 想做 deep-agent 风格应用 | `presets\deepagent.BuildKernel` |

## 5. 示例应用一览

`examples\` 目录里的参考入口：

| 目录 | 用途 |
|---|---|
| `examples\mosscode` | 代码代理产品面，含 TUI、治理、评审、检查点与变更回滚 |
| `examples\mossresearch` | 深度研究编排，偏研究与证据收集 |
| `examples\mosswriter` | 内容生成工作流 |
| `examples\mossclaw` | 个人助理 / Web 抓取 / Gateway 模式 |
| `examples\mossquant` | 有状态分析循环 |
| `examples\mossroom` | 多人实时房间 |
| `examples\mosswork-desktop` | 桌面协作助理 |
| `examples\basic` | 最小示例 |
| `examples\custom-tool` | 自定义工具接入 |
| `examples\websocket` | 自定义 `UserIO` / WebSocket 场景 |

## 6. 开发检查

仓库根目录常用校验命令：

```powershell
go test ./...
go build ./...
```
