# 🌿 moss

**Minimal Agent Runtime Kernel** — 5 核心概念 + 2 Port 接口，零外部依赖。

> 类比 Linux Kernel：核心最小化、接口稳定、可扩展。  
> Kernel 只提供 Agent 运行的不可约原语，所有业务逻辑在上层应用中实现。

## 架构

```
┌─────────────────────────────────────────────┐
│           Applications / Agents              │
├─────────────────────────────────────────────┤
│     Middleware Chain (Policy, Events, ...)    │
├─────────────────────────────────────────────┤
│  KERNEL: Loop + Tool + Session + Sandbox     │
├─────────────────────────────────────────────┤
│  Ports: LLM (Complete/Stream) + UserIO       │
└─────────────────────────────────────────────┘
```

### 核心概念

| 概念 | 职责 | 类比 |
|---|---|---|
| **Loop** | Agent 执行循环 (think→act→observe) | Process Scheduler |
| **Tool** | 能力注册、查找、执行 | System Calls |
| **Session** | 执行上下文 (消息+状态+预算) | Process + Memory |
| **Middleware** | 统一扩展点 (Policy/Events/Logger) | Kernel Modules |
| **Sandbox** | 执行隔离 (文件+命令) | Namespaces/cgroups |

### Port 接口

| Port | 职责 |
|---|---|
| **LLM** | 模型调用 (Complete + Stream) |
| **UserIO** | 结构化交互协议 (Send + Ask) |

## 快速开始

```go
package main

import (
    "context"

    "github.com/mossagi/moss/kernel"
    "github.com/mossagi/moss/kernel/middleware/builtins"
    "github.com/mossagi/moss/kernel/port"
    "github.com/mossagi/moss/kernel/sandbox"
    "github.com/mossagi/moss/kernel/session"
    "github.com/mossagi/moss/kernel/tool"
)

func main() {
    ctx := context.Background()

    k := kernel.New(
        kernel.WithLLM(myLLMAdapter),
        kernel.WithSandbox(mustSandbox(sandbox.NewLocal("/workspace"))),
        kernel.WithUserIO(myCLIIO),
    )

    // 注册工具
    k.ToolRegistry().Register(tool.ToolSpec{
        Name:        "read_file",
        Description: "Read file contents",
        Risk:        tool.RiskLow,
    }, readFileHandler)

    // 设置策略
    k.WithPolicy(
        builtins.RequireApprovalFor("write_file", "run_command"),
        builtins.DefaultAllow(),
    )

    // 监听事件
    k.OnEvent("tool.*", func(e builtins.Event) {
        log.Printf("[%s] %v", e.Type, e.Data)
    })

    k.Boot(ctx)
    defer k.Shutdown(ctx)

    sess, _ := k.NewSession(ctx, session.SessionConfig{
        Goal:     "Fix the bug in main.go",
        MaxSteps: 50,
    })
    sess.AppendMessage(port.Message{
        Role:    port.RoleUser,
        Content: "Fix the bug in main.go",
    })

    result, _ := k.Run(ctx, sess)
    fmt.Println(result.Output)
}
```

## 项目结构

```
moss/
├── cmd/moss/              # CLI 入口 (run/tui/version)
│   ├── main.go            # 命令路由 + buildKernel
│   ├── tui.go             # 交互式 TUI + cliUserIO
│   └── tui_test.go
├── kernel/                # Agent Runtime Kernel (零外部依赖)
│   ├── kernel.go          # Kernel 入口 (New/Boot/Run/Shutdown)
│   ├── option.go          # 函数式选项 (WithLLM/WithSandbox/Use)
│   ├── port/              # Port 接口
│   │   ├── types.go       # Message, Role, ToolCall, ToolResult
│   │   ├── llm.go         # LLM, StreamingLLM, CompletionRequest
│   │   └── io.go          # UserIO (Send/Ask), OutputMessage
│   ├── tool/              # Tool System
│   │   ├── tool.go        # ToolSpec, ToolHandler, RiskLevel
│   │   └── registry.go    # Registry 接口 + map 实现
│   ├── session/           # Session Management
│   │   ├── session.go     # Session, Budget, SessionConfig
│   │   └── manager.go     # Manager 接口 + 内存实现
│   ├── middleware/         # Middleware Chain
│   │   ├── middleware.go   # Middleware 类型, Phase, Context
│   │   ├── chain.go       # Chain 洋葱模型执行
│   │   └── builtins/      # 内置 Middleware
│   │       ├── policy.go  # PolicyCheck + DenyTool
│   │       ├── events.go  # EventEmitter + glob 匹配
│   │       └── logger.go  # Logger (phase 耗时)
│   ├── loop/              # Agent Loop
│   │   └── loop.go        # think→act→observe + streaming
│   ├── sandbox/           # Sandbox
│   │   ├── sandbox.go     # Sandbox 接口
│   │   └── local.go       # LocalSandbox (路径逃逸保护)
│   └── testing/           # Mock 适配器
│       ├── mock_llm.go    # MockLLM
│       ├── mock_sandbox.go # MemorySandbox
│       └── mock_io.go     # RecorderIO
└── docs/
    └── kernel-design.md   # 详细设计文档
```

## CLI 用法

```bash
# 运行任务
moss run --goal "Fix the null pointer in main.go" --workspace ./project

# 交互式 TUI
moss tui

# 版本信息
moss version
```

## 扩展

### 自定义 Middleware

```go
func myMiddleware(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
    if mc.Phase == middleware.BeforeToolCall {
        log.Printf("About to call tool: %s", mc.Tool.Name)
    }
    return next(ctx)
}

k := kernel.New(kernel.Use(myMiddleware))
```

### 实现 LLM Adapter

```go
type MyLLM struct{}

func (m *MyLLM) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
    // 调用你的 LLM API
    return &port.CompletionResponse{
        Message:    port.Message{Role: port.RoleAssistant, Content: "..."},
        StopReason: "end_turn",
    }, nil
}
```

### 实现 UserIO Adapter

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

## 测试

```bash
go test ./... -count=1
```

## 设计文档

完整设计文档见 [docs/kernel-design.md](docs/kernel-design.md)，包含：
- 设计哲学与第一性原理
- 分层架构与依赖规则
- 所有接口的详细定义
- Agent Loop 执行流程图
- 架构验证（OpenClaw + Claude Code 映射）

## License

MIT