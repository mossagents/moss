# 🚀 快速开始

本文档介绍如何使用 Moss——既可以作为终端 AI 助手直接使用，也可以作为**第三方库**嵌入到其他应用中。

---

## 安装

```bash
go install github.com/mossagi/moss/cmd/moss@latest
```

或从源码构建：

```bash
git clone https://github.com/mossagi/moss.git
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
| `/model <name>` | 切换模型 |
| `/config` | 查看配置 |
| `/config set <key> <value>` | 修改配置 |

---

## 示例应用

仓库内置示例：

- `examples/minicode`：代码助手（默认 TUI）
- `examples/miniwork`：多 Agent 编排
- `examples/miniclaw`：Web 抓取 Agent
- `examples/miniloop`：有状态自主循环 Agent（内置 trading 领域适配器）

快速体验（以 minicode 为例）：

```bash
cd examples/minicode
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

    "github.com/mossagi/moss/adapters/openai"
    "github.com/mossagi/moss/kernel"
    "github.com/mossagi/moss/kernel/port"
    "github.com/mossagi/moss/kernel/sandbox"
    "github.com/mossagi/moss/kernel/session"
)

func main() {
    ctx := context.Background()

    // 1. 创建 Kernel
    k := kernel.New(
        kernel.WithLLM(openai.New(os.Getenv("OPENAI_API_KEY"))),
        kernel.WithUserIO(port.NewPrintfIO(os.Stdout)),
        kernel.WithSandbox(must(sandbox.NewLocal("."))),
    )

    // 2. 一键注册所有标准技能
    k.SetupWithDefaults(ctx, ".")

    // 3. 启动并运行
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
    // result.Output 包含最终回复
    _ = result
}

func must[T any](v T, err error) T {
    if err != nil { panic(err) }
    return v
}
```

### 自定义 Setup

`SetupWithDefaults` 支持选择性禁用：

```go
// 只注册核心工具，不加载 MCP 和 Skill
k.SetupWithDefaults(ctx, ".",
    kernel.WithoutMCPServers(),
    kernel.WithoutSkills(),
)

// 完全自定义：不使用 SetupWithDefaults
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

使用 `kernel/testing` 包中的 Mock 适配器编写测试：

```go
import kt "github.com/mossagi/moss/kernel/testing"

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
