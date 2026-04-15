# 快速开始

这份仓库当前是 **library-first runtime + apps 核心应用 + examples 参考示例** 的结构：最直接的体验方式是运行 `apps\mosscode`，要嵌入到自己的 Go 应用里则使用 `appkit`（包括 `appkit.BuildDeepAgent(...)` 这条完整预设路径）。

## 先决条件

- Go `1.25.0`
- 至少一个模型提供商的 API Key（例如 OpenAI、Anthropic 或 Gemini）

## 1. 运行仓库自带产品入口

当前仓库树里最完整的交互式产品面是 `apps\mosscode`。打包后的 `moss` CLI 入口也指向 `mosscode`。

```powershell
Set-Location apps\mosscode
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

其它核心应用和示例也遵循同样的模式，例如 `mosswork` 使用 `~\.mosswork`，`mossresearch` 使用 `~\.mossresearch`。

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

	"github.com/mossagents/moss/harness/appkit"
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
- `harness.RuntimeSetup(...)` 默认能力装配
- 内置工具、MCP、`SKILL.md`、subagent 注册

### Feature 优先路径：`appkit.BuildKernelWithFeatures`

当你需要持久会话、调度、知识库、记忆或额外安装逻辑时，使用：

```go
k, err := appkit.BuildKernelWithFeatures(ctx, flags, io,
	harness.SessionPersistence(store),
	harness.PersistentMemories(".\\.moss\\memories"),
	harness.ContextOffload(store),
	harness.Scheduling(sched),
	harness.FeatureFunc{FeatureName: "my-tools", InstallFunc: func(_ context.Context, h *harness.Harness) error {
		return registerMyTools(h.Kernel().ToolRegistry())
	}},
	harness.RuntimeSetup(flags.Workspace, flags.Trust),
)
```

官方 Feature 会按 phase / dependency 元数据做受控安装；未标注元数据的自定义 Feature 则保持 configure 阶段语义，并在同阶段内按传入顺序安装。

`appkit` 的默认 builder 现在也会通过 managed backend factory 注入本地执行后端；如果你显式提供了 `Workspace/Executor`，builder 会尊重这些端口，而不是再额外覆盖一层默认 local backend。

### Deep Agent 预设：`appkit`

如果你需要更完整的“coding / research / writer”式产品能力，直接使用：

```go
import "github.com/mossagents/moss/harness/appkit"

k, err := appkit.BuildDeepAgent(ctx, flags, io, nil)
```

默认会接入：

- Session store / checkpoint store / task runtime
- 持久记忆与上下文压缩
- workspace isolation / repo state / patch apply / rollback
- 通用委派代理 `general-purpose`
- planning、task、mailbox 等协作能力

这条路径现在由声明式 preset packs 组合而成：`BuildDeepAgent(...)` 负责按 `DeepAgentConfig` 选择 pack，再交给 `BuildKernelWithFeatures(...)` 做受控安装。

## 4. 你应该选哪条路径

| 场景 | 推荐入口 |
|---|---|
| 想马上体验当前仓库能力 | `apps\mosscode` |
| 想构建最小可运行应用 | `appkit.BuildKernel` |
| 想按官方 Feature 方式组合能力 | `appkit.BuildKernelWithFeatures` |
| 想做 deep-agent 风格应用 | `appkit.BuildDeepAgent` |

## 5. 应用与示例一览

`apps\` 目录里的核心应用：

| 目录 | 用途 |
|---|---|
| `apps\mosscode` | 代码代理核心应用，含 TUI、治理、评审、检查点与变更回滚 |
| `apps\mosswork` | 桌面协作核心应用 |

`examples\` 目录里的参考入口：

| 目录 | 用途 |
|---|---|
| `examples\mossresearch` | 深度研究编排，偏研究与证据收集 |
| `examples\mosswriter` | 内容生成工作流 |
| `examples\mossclaw` | 个人助理 / Web 抓取 / Gateway 模式 |
| `examples\mossquant` | 有状态分析循环 |
| `examples\mossroom` | 多人实时房间 |
| `examples\basic` | 最小示例 |
| `examples\custom-tool` | 自定义工具接入 |
| `examples\websocket` | 自定义 `UserIO` / WebSocket 场景 |

## 6. 开发检查

项目由多个独立 Go 模块组成，使用 `go.work` 工作区管理：

```powershell
# 构建和测试 kernel 模块
Set-Location kernel; go build ./...; go test ./...

# 构建和测试 harness 模块
Set-Location harness; go build ./...; go test ./...
```
