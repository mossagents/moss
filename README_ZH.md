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

	"github.com/mossagents/moss/harness/appkit"
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
	"github.com/mossagents/moss/harness/sandbox"
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
| `kernel\` | 核心运行时原语（Agent、`RunAgent`、Session、Event、Tool、Plugin）— 独立 Go 模块 |
| `kernel\patterns\` | Agent 编排原语（Sequential、Parallel、Loop、Supervisor、Research） |
| `harness\` | 可组合编排层（Feature、Backend、Harness）— 独立 Go 模块，依赖 kernel |
| `harness\appkit\` | 推荐构建器、扩展组合 API，以及 deep-agent 预设装配路径 |
| `harness\appkit\runtime\` | 默认能力装配（builtin tools、MCP、skills、subagents、memory、context、scheduling） |
| `harness\extensions\` | 面向扩展的能力模块命名空间（`skill\`、`mcp\`、`agent\`、`knowledge\`） |
| `harness\bootstrap\`、`config\`、`providers\`、`logging\` | 支撑包 |
| `harness\scheduler\`、`gateway\`、`distributed\`、`sandbox\` | 更高层运行时积木 |
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
default_preset: code

collaboration_modes:
	execute:
		builtin: execute
	plan:
		builtin: plan
	investigate:
		builtin: investigate

presets:
	code:
		prompt_pack: coding
		collaboration_mode: execute
		permission_profile: workspace-write

skills:
  - name: github
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
```

面向产品层的三种协作模式如下：

- `Agent`（`execute`）：直接实现与交付
- `Plan`（`plan`）：只读规划与决策支持
- `Ask`（`investigate`）：阅读、溯源、证据优先分析

旧的 `--profile` 和 `--approval` 选择器已不再支持。请统一使用 `--preset`、`--mode`、`--permissions`。

优先级：

**命令行参数 > 环境变量 > 配置文件**

## 观测与审计

Moss 现在有两层互补的观测面：

- `kernel/observe` 中的结构化运行事件与归一化指标
- `contrib/telemetry/otel` 与 `contrib/telemetry/prometheus` 中的可选 exporter

归一化指标现在除了 success / latency / cost / tool-error 之外，也直接覆盖 context 管理和 guardian review，包括：

- `context.compactions_total`
- `context.compaction_tokens_reclaimed_sum`
- `context.trim_retry_total`
- `context.normalize_total`
- `guardian.review_total`
- `guardian.fallback_rate`
- `guardian.error_rate`

OTEL 和 Prometheus exporter 也会把这些路径导出为一等指标族（`moss.context.*` 与 `moss.guardian.*`），因此 dashboard 不需要再从原始事件流侧推导。

在 operator 侧，产品层也不再把 `audit.jsonl` 仅当作追加日志：`moss inspect run ...` 与 `moss inspect thread ...` 现在会直接展示由 audit log 聚合出来的 context / guardian 审计摘要。

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
# 构建和测试 kernel 模块
Set-Location kernel; go build ./...; go test ./...

# 构建和测试 harness 模块
Set-Location harness; go build ./...; go test ./...

# 或使用 go workspace 从仓库根目录
go build ./kernel/... ./harness/...
```

## 兼容性

- Kernel 模块路径：`github.com/mossagents/moss/kernel`
- Harness 模块路径：`github.com/mossagents/moss/harness`
- Go 版本：`1.25.0`
- 本地开发通过 `go.work` 工作区管理

## 许可证

MIT
