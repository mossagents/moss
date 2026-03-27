# 🌿 Moss

**Minimal Agent Runtime Kernel** — 5 核心概念 + 2 Port 接口，零外部依赖。

> 类比 Linux Kernel：核心最小化、接口稳定、可扩展。  
> Kernel 只提供 Agent 运行的不可约原语，所有业务逻辑在上层应用中实现。  
> **设计为库优先 (library-first)**，可以嵌入到任何 Go 应用中作为 AI Agent 基座。

## 架构

```
┌──────────────────────────────────────────────────────┐
│              Applications / Agents                    │
│  (CLI, TUI, Web 服务, 自定义 Agent, ...)              │
├──────────────────────────────────────────────────────┤
│              Middleware Chain                          │
│  (PolicyCheck, EventEmitter, Logger, 自定义)          │
├──────────────────────────────────────────────────────┤
│  KERNEL: Loop + Tool + Session + Middleware           │
├──────────────────────────────────────────────────────┤
│  Ports: LLM (Complete/Stream) + UserIO (Send/Ask)     │
├──────────────────────────────────────────────────────┤
│  Adapters: Claude / OpenAI / 自定义                    │
└──────────────────────────────────────────────────────┘
```

### 核心概念

| 概念 | 职责 | 类比 |
|---|---|---|
| **Loop** | Agent 执行循环 (think→act→observe) | Process Scheduler |
| **Tool** | 能力注册、查找、执行 | System Calls |
| **Session** | 执行上下文 (消息+状态+预算) | Process + Memory |
| **Middleware** | 统一扩展点 (Policy/Events/Logger) | Kernel Modules |
| **Workspace/Executor** | 文件与命令执行抽象 | Filesystem + Process Isolation |

### Port 接口

| Port | 职责 |
|---|---|
| **LLM** | 模型调用 (Complete + Stream) |
| **UserIO** | 结构化交互协议 (Send + Ask) |

## 快速开始

### 安装

```bash
go install github.com/mossagents/moss/cmd/moss@latest
```

### CLI 使用

```bash
# 交互式 TUI（默认）
moss

# 带参数启动
moss --provider openai --model gpt-4o

# 非交互式
moss run --goal "Fix the bug in main.go" --workspace ./project

# 版本信息
moss version
```

### 作为库集成（3 步）

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
    k, _ := appkit.BuildKernel(ctx, &appkit.AppFlags{
        Provider:  "openai",
        Workspace: ".",
        APIKey:    os.Getenv("OPENAI_API_KEY"),
    }, port.NewPrintfIO(os.Stdout))

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
    _ = result // result.Output 包含最终回复
}
```

### 动态模型路由（按任务能力选择模型）

Moss 支持在一个配置文件中声明多个模型，并按任务需求动态路由：

- 可配置模型能力（如 `image_generation`、`reasoning`）
- 可配置成本等级（`cost_tier`）
- 可按任务约束选择低成本或高能力模型
- 找不到可用模型时返回详细错误

示例配置 `models.yaml`：

```yaml
models:
    - name: gpt-4o
        provider: openai
        model: gpt-4o
        cost_tier: 3
        capabilities: [text_generation, code_generation, image_understanding, function_calling, reasoning]
        is_default: true

    - name: gpt-4o-mini
        provider: openai
        model: gpt-4o-mini
        cost_tier: 1
        capabilities: [text_generation, code_generation, function_calling]

    - name: image-gen
        provider: openai
        model: gpt-image-1
        cost_tier: 2
        capabilities: [image_generation]
```

在 Kernel 中启用路由器：

```go
router, err := adapters.NewModelRouterFromFile("models.yaml")
if err != nil {
        panic(err)
}

k := kernel.New(
        kernel.WithLLM(router),
        kernel.WithUserIO(port.NewPrintfIO(os.Stdout)),
        kernel.WithSandbox(must(sandbox.NewLocal("."))),
)
```

为单次任务声明能力需求：

```go
sess, _ := k.NewSession(ctx, session.SessionConfig{
        Goal: "生成一张海报图",
        ModelConfig: port.ModelConfig{
                Requirements: &port.TaskRequirement{
                        Capabilities: []port.ModelCapability{port.CapImageGeneration},
                        MaxCostTier:  2,
                        PreferCheap:  true,
                },
        },
})
```

### 标准 UserIO 实现

| 实现 | 场景 | 用法 |
|---|---|---|
| `NoOpIO` | 后台任务、纯自动化 | `&port.NoOpIO{}` |
| `PrintfIO` | CLI、日志输出 | `port.NewPrintfIO(os.Stdout)` |
| `BufferIO` | 测试 | `port.NewBufferIO()` |

## 配置

全局配置文件 `~/.moss/config.yaml`：

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

**优先级**：CLI 参数 > 配置文件 > 环境变量 (`OPENAI_API_KEY` / `ANTHROPIC_API_KEY`)

### 第三方应用自定义配置目录

作为库集成时，可在启动早期设置应用名：

```go
skill.SetAppName("mosscode")
_ = skill.EnsureMossDir()
```

此时全局配置目录会从 `~/.moss` 变为 `~/.mosscode`（全局配置文件固定为 `config.yaml`）。

### System Prompt 模板覆盖

Moss 与 examples 现已支持通过模板文件覆盖默认 system prompt：

- 项目级（优先）：`./.<appName>/system_prompt.tmpl`
- 全局级：`~/.<appName>/system_prompt.tmpl`

未提供覆盖模板时，使用内置默认模板。

## 示例应用

仓库内置 5 个示例应用：

| 示例 | 说明 | 入口 |
|---|---|---|
| `mosscode` | 代码助手（默认 TUI） | `examples/mosscode/main.go` |
| `mosswork` | 多 Agent 任务编排（Manager/Worker） | `examples/mosswork/main.go` |
| `mossclaw` | Web 抓取 Agent | `examples/mossclaw/main.go` |
| `mossquant` | 有状态自主循环 Agent（可插拔领域适配器） | `examples/mossquant/main.go` |
| `mossroom` | 多人实时 Agent 游戏（WebSocket + Per-Room Kernel） | `examples/mossroom/main.go` |

运行方式（示例）：

```bash
cd examples/mosscode
go run .
```

每个示例目录下均提供独立 README 说明。

## 项目结构

```
moss/
├── cmd/moss/                # CLI 入口
│   ├── main.go              # 命令路由 + 配置加载 + Kernel 构建
│   ├── tui/                 # Bubble Tea 交互式 TUI
│   │   ├── app.go           # 状态机 (Welcome → Chat)
│   │   ├── welcome.go       # 配置输入页
│   │   ├── chat.go          # 聊天页 + 斜杠命令
│   │   ├── message.go       # 消息渲染 (8 种类型)
│   │   ├── userio.go        # BridgeIO (TUI ↔ Kernel 桥接)
│   │   ├── systemprompt.go  # 系统提示词构建
│   │   └── styles.go        # Lipgloss 样式
│   └── tui_test.go
├── adapters/                # LLM Adapter 实现
│   ├── claude/              # Anthropic Claude (SDK)
│   └── openai/              # OpenAI 兼容 (SDK)
│   ├── router.go             # 多模型动态路由 (ModelRouter)
│   └── router_test.go
├── examples/                # 示例应用
│   ├── mosscode/            # 代码助手（TUI）
│   ├── mosswork/            # 多 Agent 编排
│   ├── mossclaw/            # Web 抓取
│   ├── mossquant/           # 有状态循环 Agent（模拟交易）
│   └── mossroom/            # 多人实时 Agent 游戏（WebSocket）
├── kernel/                  # Agent Runtime Kernel (零外部依赖)
│   ├── kernel.go            # Kernel 入口 (New/Boot/Run/Shutdown)
│   ├── option.go            # 函数式选项 (WithLLM/WithSandbox/Use...)
│   ├── ...
│   ├── port/                # Port 接口 (纯类型定义)
│   │   ├── types.go         # Message, Role, ToolCall, ToolResult
│   │   ├── llm.go           # LLM, StreamingLLM, CompletionRequest
│   │   ├── capability.go    # 模型能力标签 + TaskRequirement
│   │   ├── io.go            # UserIO, OutputMessage, InputRequest
│   │   ├── io_std.go        # NoOpIO, PrintfIO, BufferIO
│   │   ├── channel.go       # Channel, InboundMessage, OutboundMessage
│   │   └── embedder.go      # Embedder 接口
│   ├── tool/                # Tool System
│   │   ├── tool.go          # ToolSpec, ToolHandler, RiskLevel
│   │   ├── registry.go      # Registry 接口 + map 实现
│   │   └── scoped.go        # ScopedRegistry (工具白名单视图)
│   ├── session/             # Session Management
│   │   ├── session.go       # Session, Budget, SessionConfig
│   │   ├── manager.go       # Manager 接口 + 内存实现
│   │   ├── router.go        # Router (DMScope 路由)
│   │   ├── store.go         # SessionStore 接口
│   │   └── store_file.go    # 文件持久化实现
│   ├── middleware/           # Middleware Chain (洋葱模型)
│   │   ├── middleware.go     # Middleware 类型, Phase, Context
│   │   ├── chain.go         # Chain 执行
│   │   └── builtins/        # PolicyCheck, EventEmitter, Logger, PatchToolCalls
│   ├── loop/                # Agent Loop
│   │   └── loop.go          # think→act→observe + streaming + 重试
│   ├── sandbox/             # Sandbox (执行隔离)
│   │   ├── sandbox.go       # Sandbox 接口
│   │   └── local.go         # LocalSandbox (路径逃逸保护)
│   ├── agent/               # Agent 委派系统
│   │   ├── config.go        # AgentConfig (YAML)
│   │   ├── registry.go      # Agent Registry
│   │   ├── tools.go         # delegate_agent / spawn_agent
│   │   └── depth.go         # 委派深度限制
│   ├── skill/               # 技能系统
│   │   ├── skill.go         # Skill 接口 + Manager
│   │   ├── config.go        # Config 加载/保存/合并
│   │   └── prompt.go        # Skill (SKILL.md 注入)
│   ├── mcp/                 # MCP 协议集成
│   │   └── mcp.go           # MCP Provider（外部工具服务器）
│   ├── gateway/             # 消息网关 [实验性]
│   ├── knowledge/           # 知识系统 [实验性]
│   ├── scheduler/           # 定时任务调度器
│   └── testing/             # Mock 适配器
│       ├── mock_llm.go      # MockLLM, MockStreamingLLM
│       ├── mock_sandbox.go  # MemorySandbox
│       └── mock_io.go       # RecorderIO
├── extensions/              # 官方维护的可选扩展
│   ├── toolbuiltins/        # 内置工具实现（read_file/write_file/...）
│   └── defaults/            # 默认装配（core tool skill / MCP / skills / agents）
├── appkit/                # 应用脚手架工具箱
│   ├── appkit.go          # ContextWithSignal, AppFlags, Banner
│   ├── repl.go              # REPL 引擎
│   └── serve.go             # Gateway Serve 脚手架
├── presets/                 # 官方预设装配（如 deepagent）
└── docs/                    # 文档
    ├── architecture.md      # 架构设计
    ├── getting-started.md   # 快速开始 & 库集成指南
    ├── skills.md            # 技能系统详解
    ├── kernel-design.md     # 原始内核设计文档
    ├── changelog.md         # 开发日志
    └── roadmap.md           # 路线图
```

## 扩展

### 自定义 Middleware

```go
func myMiddleware(ctx context.Context, mc *middleware.Context, next middleware.Next) error {
    if mc.Phase == middleware.BeforeToolCall {
        log.Printf("Tool: %s", mc.Tool.Name)
    }
    return next(ctx)
}

k := kernel.New(kernel.Use(myMiddleware))
```

### 自定义 LLM Adapter

```go
type MyLLM struct{}

func (m *MyLLM) Complete(ctx context.Context, req port.CompletionRequest) (*port.CompletionResponse, error) {
    return &port.CompletionResponse{
        Message:    port.Message{Role: port.RoleAssistant, Content: "..."},
        StopReason: "end_turn",
    }, nil
}
```

### 自定义 UserIO Adapter

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

### 策略与事件

```go
// 权限策略
k.WithPolicy(
    builtins.RequireApprovalFor("write_file", "run_command"),
    builtins.DefaultAllow(),
)

// 事件监听
k.OnEvent("tool.*", func(e builtins.Event) {
    log.Printf("[%s] %v", e.Type, e.Data)
})
```

### Deep Agent 预设

如果你希望获得 deepagents 风格默认能力（规划、上下文压缩、异步任务生命周期），可使用：

```go
import "github.com/mossagents/moss/presets/deepagent"

k, _ := deepagent.BuildKernel(ctx, flags, io, &deepagent.Config{
    AppName: "myapp",
})
```

默认包含 `write_todos`、`compact_conversation`、`update_task`，并启用 `PatchToolCalls` 以自动修补不完整工具调用历史。

## 测试

```bash
go test ./... -count=1
```

## 文档

| 文档 | 说明 |
|---|---|
| [架构设计](docs/architecture.md) | 分层架构、核心概念、依赖图 |
| [快速开始](docs/getting-started.md) | 安装、CLI 用法、库集成指南 |
| [Examples 指南](examples/mosscode/README.md) | 示例应用入口（其余示例 README 在对应目录） |
| [技能系统](docs/skills.md) | Core Tool Skill、MCP（mcp 包）、Skill 详解 |
| [内核设计](docs/kernel-design.md) | 原始设计文档（第一性原理、接口定义、流程图） |
| [开发日志](docs/changelog.md) | 版本变更记录 |
| [路线图](docs/roadmap.md) | 后续规划 (P1/P2/P3) |

## License

MIT
