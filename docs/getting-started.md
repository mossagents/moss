# 🚀 快速开始

> **Moss**: *Agent harness for Go: compose fast, run safely.*  
> **Moss**：*面向 Go 的 Agent Harness：快速装配，安全运行。*
>
> **MossCode**: *A coding-agent harness grounded in your workspace.*  
> **MossCode**：*扎根于你的工作区上下文的代码 Agent Harness。*

本文档介绍如何使用 Moss——既可以作为终端 AI 助手直接使用，也可以作为**第三方库**嵌入到其他应用中。

---

## 安装

```bash
go install github.com/mossagents/moss/cmd/moss@latest
```

或从源码构建：

```bash
git clone https://github.com/mossagents/moss.git
cd moss
go build -o moss ./cmd/moss/
```

## 配置

Moss 使用 `~/.moss/config.yaml` 作为全局配置文件：

```yaml
provider: openai          # 或 claude
model: gpt-4o
base_url: ""              # 自定义 API 端点（可选）
api_key: ""               # API 密钥（可选，也可用环境变量）
skills:
  - name: my-mcp-server
    transport: stdio
    command: npx
    args: ["-y", "@example/mcp-server"]
```

**配置优先级**：CLI 参数 > 配置文件 > 环境变量

**环境变量**：

| 变量 | 说明 |
|---|---|
| `ANTHROPIC_API_KEY` | Claude API 密钥 |
| `OPENAI_API_KEY` | OpenAI API 密钥 |

### 动态模型路由配置（可选）

如果你希望根据任务能力自动选择模型（例如图片生成任务走图像模型），可单独提供一个模型路由配置文件：

```yaml
models:
  - name: claude-sonnet
    provider: claude
    model: claude-sonnet-4-20250514
    cost_tier: 2
    capabilities: [text_generation, code_generation, reasoning, function_calling]
    is_default: true

  - name: image-gen
    provider: openai
    model: gpt-image-1
    cost_tier: 3
    capabilities: [image_generation]
```

在代码中加载并注入 `ModelRouter`：

```go
router, err := adapters.NewModelRouterFromFile("models.yaml")
if err != nil {
    panic(err)
}

k := kernel.New(
    kernel.WithLLM(router),
    kernel.WithUserIO(port.NewPrintfIO(os.Stdout)),
)
```

创建 Session 时可传入任务要求：

```go
sess, _ := k.NewSession(ctx, session.SessionConfig{
    Goal: "生成一张产品海报",
    ModelConfig: port.ModelConfig{
        Requirements: &port.TaskRequirement{
            Capabilities: []port.ModelCapability{port.CapImageGeneration},
            MaxCostTier:  3,
            PreferCheap:  false,
        },
    },
})
```

当没有任何模型满足要求时，`ModelRouter` 会返回可读错误并列出已注册模型与能力。

### 第三方应用配置目录

若你基于 Moss 构建自己的应用，可设置应用名以隔离配置目录：

```go
skill.SetAppName("myagent")
_ = skill.EnsureMossDir()
```

此时全局配置文件路径为 `~/.myagent/config.yaml`。

### System Prompt 模板覆盖

可通过模板覆盖默认 system prompt：

- 项目级（优先）：`./.<appName>/system_prompt.tmpl`
- 全局级：`~/.<appName>/system_prompt.tmpl`

模板语法使用 Go `text/template`。

## CLI 用法

```bash
# 交互式 TUI（默认）
moss

# 带参数启动
moss --provider openai --model gpt-4o

# CLI 模式（非交互式）
moss run --goal "Fix the bug in main.go" --workspace ./project

# 版本信息
moss version
```

### TUI 斜杠命令

| 命令 | 说明 |
|---|---|
| `/help` | 显示帮助 |
| `/exit` | 退出 |
| `/clear` | 清空对话 |
| `/status` | 查看当前运行时摘要（模型/工作区/姿态/会话） |
| `/model <name>` | 切换模型 |
| `/config` | 查看配置 |
| `/config set <key> <value>` | 修改配置 |
| `/resume [session_id|latest]` | 查看可恢复会话或恢复指定会话 |
| `/fork [session <id>\|checkpoint <id\|latest>\|latest] [restore]` | 从已有会话/检查点分叉新会话 |
| `/plan [prompt]` | 切换到 planning 模式，并可直接附带计划提示 |
| `/diff [path]` | 查看当前 git diff |
| `/review [mode]` | 查看当前仓库评审摘要 |
| `/mcp [list\|show <name>]` | 查看 MCP 服务状态 |
| `/compact [keep_recent] [note]` | 手动触发上下文压缩（调用 `offload_context`） |
| `/tasks [status] [limit]` | 列出后台任务（支持状态过滤） |
| `/task <id>` | 查询单个后台任务详情 |
| `/task cancel <id> [reason]` | 取消后台任务 |

---

## 示例应用

仓库内置示例：

- `examples/mosscode`：代码助手（默认 TUI）
- `examples/mosswork`：多 Agent 编排
- `examples/mossclaw`：Web 抓取 Agent
- `examples/mossquant`：有状态自主循环 Agent（内置 trading 领域适配器）
- `examples/mossroom`：多人实时 Agent 游戏（WebSocket + Per-Room Kernel）

快速体验（以 mosscode 为例）：

```bash
cd examples/mosscode
go run .
```

详细说明见各目录 README。

---

## 作为第三方库使用

Moss 设计为**库优先** (library-first)，核心 API 简洁且可扩展。

### 最简集成（3 行代码）

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

    // 1. 用 appkit 构建推荐配置的 Kernel
    k, _ := appkit.BuildKernel(ctx, &appkit.AppFlags{
        Provider:  "openai",
        Workspace: ".",
        APIKey:    os.Getenv("OPENAI_API_KEY"),
    }, port.NewPrintfIO(os.Stdout))

    // 2. 启动并运行
    k.Boot(ctx)
    defer k.Shutdown(ctx)

    sess, _ := k.NewSession(ctx, session.SessionConfig{
        Goal:     "Read and summarize README.md",
        MaxSteps: 20,
    })
    sess.AppendMessage(port.Message{
        Role:    port.RoleUser,
        Content: "Read and summarize README.md",
    })

    result, _ := k.Run(ctx, sess)
    _ = result
}
```

### 自定义 Setup

推荐做法是继续使用 `appkit.BuildKernelWithConfig` / `appkit.BuildKernelWithExtensions`，只在需要更底层控制时直接调用 `runtime.Setup`。

### Deep Agent 预设（推荐）

对于需要 deepagents 风格能力（规划 TODO、上下文压缩、异步子任务生命周期）的应用，推荐使用：

```go
import "github.com/mossagents/moss/presets/deepagent"

k, err := deepagent.BuildKernel(ctx, flags, io, &deepagent.Config{
    AppName: "myapp",
})
```

该预设默认接入：

- `write_todos` 规划工具
- `compact_conversation` + `offload_context` 上下文管理
- `task` / `update_task` / `list_tasks` / `cancel_task` 异步任务生命周期
- `plan_task` / `claim_task` 任务图协作工具
- `send_mail` / `read_mailbox` 代理异步邮箱
- `acquire_workspace` / `release_workspace` 任务级工作区隔离
- `PatchToolCalls` 中间件（自动补齐缺失 tool result）

```go
// 统一通过 appkit 装配官方扩展
k, err := appkit.BuildKernelWithExtensions(ctx, flags, io,
    appkit.WithSessionStore(store),
    appkit.WithContextOffload(store),
    appkit.WithPersistentMemories("./.moss/memories"),
    appkit.WithScheduling(sched),
    appkit.AfterBuild(func(_ context.Context, k *kernel.Kernel) error {
        return registerMyTools(k.ToolRegistry())
    }),
)
```

`WithContextOffload` 依赖可持久化的 `SessionStore`，会注册 `offload_context` 工具，用于手动压缩长会话并把历史快照保存到 store。

更底层时，`runtime.Setup` 仍支持选择性禁用：

```go
import runtime "github.com/mossagents/moss/appkit/runtime"

// 只注册核心工具，不加载 MCP 和 Skill
runtime.Setup(ctx, k, ".",
    runtime.WithMCPServers(false),
    runtime.WithSkills(false),
)

// 启用按需 Skill 加载（启动时不注入全部 SKILL.md 正文）
runtime.Setup(ctx, k, ".",
    runtime.WithProgressiveSkills(true),
)

// 完全自定义：不使用 runtime.Setup
k := kernel.New(
    kernel.WithLLM(myLLM),
    kernel.WithUserIO(&port.NoOpIO{}),
)
k.ToolRegistry().Register(myTool, myHandler)
k.Boot(ctx)
```

### 选择 UserIO 实现

| 场景 | 实现 | 用法 |
|---|---|---|
| 后台自动化 | `NoOpIO` | `&port.NoOpIO{}` |
| CLI 日志 | `PrintfIO` | `port.NewPrintfIO(os.Stdout)` |
| 测试 | `BufferIO` | `port.NewBufferIO()` |
| Web/IM | 自定义 | 实现 `port.UserIO` 接口 |

### 实现自定义 LLM Adapter

```go
type MyLLM struct{}

func (m *MyLLM) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
    // 调用你的 LLM API
    return &port.CompletionResponse{
        Message:    port.Message{Role: port.RoleAssistant, Content: "..."},
        StopReason: "end_turn",
    }, nil
}

// 可选：实现 StreamingLLM 接口以支持流式输出
func (m *MyLLM) Stream(ctx context.Context, req port.CompletionRequest) (port.StreamIterator, error) {
    // ...
}
```

### 实现自定义 UserIO Adapter

```go
type WebSocketIO struct{ conn *websocket.Conn }

func (ws *WebSocketIO) Send(ctx context.Context, msg port.OutputMessage) error {
    return ws.conn.WriteJSON(msg)
}

func (ws *WebSocketIO) Ask(ctx context.Context, req port.InputRequest) (port.InputResponse, error) {
    ws.conn.WriteJSON(req)
    var resp port.InputResponse
    ws.conn.ReadJSON(&resp)
    return resp, nil
}
```

### 添加自定义 Middleware

```go
func myMiddleware(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
    if mc.Phase == middleware.BeforeToolCall {
        log.Printf("Tool: %s", mc.Tool.Name)
    }
    return next(ctx)
}

k := kernel.New(kernel.Use(myMiddleware))
```

### 策略与事件

```go
// 设置权限策略
k.WithPolicy(
    builtins.RequireApprovalFor("write_file", "run_command"),
    builtins.DefaultAllow(),
)

// 监听事件
k.OnEvent("tool.*", func(e builtins.Event) {
    log.Printf("[%s] %v", e.Type, e.Data)
})
```

---

## Boot 验证

`Boot()` 会检查必要组件是否已设置，并给出明确的修复建议：

```
kernel boot failed:
  - LLM port is required (use kernel.WithLLM())
  - UserIO port is not set (use kernel.WithUserIO(), or port.NoOpIO{} / port.NewPrintfIO())
```

---

## 测试

```bash
go test ./... -count=1
```

使用顶层 `testing` 包中的 Mock 适配器编写测试：

```go
import kt "github.com/mossagents/moss/testing"

mock := &kt.MockLLM{
    Responses: []port.CompletionResponse{
        {Message: port.Message{Role: port.RoleAssistant, Content: "Hello!"}},
    },
}

k := kernel.New(
    kernel.WithLLM(mock),
    kernel.WithUserIO(port.NewBufferIO()),
)
```
