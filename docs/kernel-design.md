# Agent Runtime Kernel — 设计文档

> 最小稳定核心 · 5 概念 + 2 Port · 零外部依赖

---

## 1 设计哲学

类比 Linux Kernel：**核心最小化、接口稳定、可扩展**。  
Kernel 只提供 Agent 运行的不可约原语，所有业务逻辑（Agent 角色、Task 编排、Plan 生成等）在上层应用中实现。

### 1.1 第一性原理

一个 Agent 的本质行为：

```
loop {
    observe(context)  → 感知当前状态
    think(llm)        → 推理下一步行动
    act(tool)         → 执行动作
    check(policy)     → 安全检查
}
```

这个循环是**唯一的不可约内核**。其他一切都是为这个循环服务的基础设施。

### 1.2 核心概念

| 概念 | 职责 | Linux Kernel 类比 |
|---|---|---|
| **Loop** | Agent 执行循环（think→act→observe） | Process Scheduler |
| **Tool** | 能力注册、查找、执行 | System Calls |
| **Session** | 执行上下文（消息 + 状态 + 预算） | Process + Memory |
| **Middleware** | 统一扩展点（合并 Hook/Policy/Event） | Kernel Modules |
| **Sandbox** | 执行隔离（文件 + 命令） | Namespaces / cgroups |

### 1.3 Port 接口

| Port | 职责 |
|---|---|
| **LLM** | 模型调用（Complete + Stream） |
| **UserIO** | 用户交互协议（Send + Ask） |

### 1.4 概念数选择的权衡

| 方案 | 概念数 | 学习曲线 | 可发现性 | 能力 |
|---|---|---|---|---|
| 3 原语（极简） | 5 | 最低 | 低（Middleware 万能箱） | 完整 |
| **5 概念 + 便利 API** | **7** | **低** | **高（语义化方法）** | **完整** |
| 7 子系统（完整） | 10 | 高 | 高 | 完整 |

选择 5 概念方案，通过便利 API（`OnEvent()`、`WithPolicy()`）保持可发现性，
内部全部由 Middleware 实现，消除 Hook/Policy/EventBus 的选择焦虑。

---

## 2 分层架构

```
┌──────────────────────────────────────────────────────────────┐
│                   Applications / Agents                       │
│  (OpenClaw Gateway, Claude Code REPL, Custom Agents, ...)    │
├──────────────────────────────────────────────────────────────┤
│                   Middleware Chain                             │
│  (PolicyCheck, EventEmitter, Logger, Custom Middleware)       │
├──────────────────────────────────────────────────────────────┤
│                         KERNEL                                │
│  ┌────────┐  ┌────────┐  ┌──────────┐  ┌──────────┐        │
│  │  Loop  │  │  Tool  │  │ Session  │  │ Sandbox  │        │
│  └────────┘  └────────┘  └──────────┘  └──────────┘        │
├──────────────────────────────────────────────────────────────┤
│                     Ports (Interfaces)                         │
│  ┌─────────────────────┐  ┌─────────────────────┐           │
│  │    LLM Port         │  │    UserIO Port      │           │
│  │  Complete / Stream  │  │    Send / Ask       │           │
│  └─────────────────────┘  └─────────────────────┘           │
├──────────────────────────────────────────────────────────────┤
│                 Adapters (Infrastructure)                      │
│  Anthropic / OpenAI / Ollama   CLI / TUI / WS / Telegram    │
│  Local Sandbox / Docker Sandbox                               │
└──────────────────────────────────────────────────────────────┘
```

**依赖规则**:  
`Adapters → Applications → Kernel → Ports`

Kernel 层零外部依赖（仅 Go stdlib + 自身子包）。

---

## 3 Port 接口详细设计

### 3.1 共享消息类型 (`port/types.go`)

```go
type Role string

const (
    RoleSystem    Role = "system"
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleTool      Role = "tool"
)

type Message struct {
    Role        Role          // 消息角色
    Content     string        // 文本内容
    ToolCalls   []ToolCall    // LLM 请求的工具调用（仅 assistant）
    ToolResults []ToolResult  // 工具执行结果（仅 tool）
}

type ToolCall struct {
    ID        string          // 唯一调用 ID
    Name      string          // 工具名称
    Arguments json.RawMessage // 工具参数（JSON）
}

type ToolResult struct {
    CallID  string // 对应 ToolCall.ID
    Content string // 执行结果
    IsError bool   // 是否为错误
}

type TokenUsage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
}
```

### 3.2 LLM Port (`port/llm.go`)

```go
// LLM 是模型调用的核心接口（同步模式）
type LLM interface {
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

type CompletionRequest struct {
    Messages []Message     // 对话历史
    Tools    []ToolSpec    // 可用工具声明
    Config   ModelConfig   // 模型配置
}

type ModelConfig struct {
    Model       string  // 模型名称
    MaxTokens   int     // 最大生成 token
    Temperature float64 // 温度参数
    Extra       map[string]any // 供应商特定参数
}

type CompletionResponse struct {
    Message    Message    // LLM 的回复消息
    ToolCalls  []ToolCall // 请求的工具调用
    Usage      TokenUsage // token 用量
    StopReason string     // 停止原因: "end_turn", "tool_use", "max_tokens"
}

// ToolSpec 用于描述工具给 LLM（与 tool.ToolSpec 共享定义）
type ToolSpec struct {
    Name        string          // 工具名称
    Description string          // 工具描述
    InputSchema json.RawMessage // JSON Schema
}
```

**Streaming 扩展**（可选实现）：

```go
// StreamingLLM 是流式模型调用接口
type StreamingLLM interface {
    Stream(ctx context.Context, req CompletionRequest) (StreamIterator, error)
}

type StreamIterator interface {
    Next() (StreamChunk, error) // 返回下一个 chunk，结束时返回 io.EOF
    Close() error               // 释放资源
}

type StreamChunk struct {
    Delta    string     // 文本增量
    ToolCall *ToolCall  // 工具调用增量（渐进式构建）
    Done     bool       // 是否结束
    Usage    *TokenUsage // 最终用量（仅在 Done=true 时）
}
```

### 3.3 UserIO Port (`port/io.go`)

UserIO 是 **结构化交互协议**，而非简单的文本 IO。
一个 Kernel 通过 UserIO 无缝对接 CLI、TUI、Web、Desktop、IM 等所有界面。

```go
// UserIO 是 Kernel 与用户交互的 Port
type UserIO interface {
    // Send 向用户推送内容（单向，不等待回复）
    Send(ctx context.Context, msg OutputMessage) error

    // Ask 向用户请求输入（阻塞等待回复）
    Ask(ctx context.Context, req InputRequest) (InputResponse, error)
}
```

**输出消息**：

```go
type OutputType string

const (
    OutputText       OutputType = "text"        // 完整文本消息
    OutputStream     OutputType = "stream"      // 流式 chunk（追加到当前消息）
    OutputStreamEnd  OutputType = "stream_end"  // 流式结束
    OutputProgress   OutputType = "progress"    // 进度更新（如 "Searching files..."）
    OutputToolStart  OutputType = "tool_start"  // 工具开始执行
    OutputToolResult OutputType = "tool_result" // 工具执行结果
)

type OutputMessage struct {
    Type    OutputType     // 消息类型
    Content string         // 文本内容
    Meta    map[string]any // 附加信息（如 diff 数据、代码语言等）
}
```

**输入请求**：

```go
type InputType string

const (
    InputFreeText InputType = "free_text" // 自由文本输入
    InputConfirm  InputType = "confirm"   // y/n 确认
    InputSelect   InputType = "select"    // 从选项中选择
)

type InputRequest struct {
    Type    InputType      // 请求类型
    Prompt  string         // 问题文本
    Options []string       // 选项列表（仅 Select 类型）
    Meta    map[string]any // 附加上下文（如审批时的 diff 数据）
}

type InputResponse struct {
    Value    string // 文本值（FreeText 类型）
    Selected int    // 选中索引（Select 类型）
    Approved bool   // 是否批准（Confirm 类型）
}
```

**各界面适配示意**：

| 界面 | Send(OutputStream) | Ask(Confirm) |
|---|---|---|
| CLI | `fmt.Fprint(stdout, chunk)` | `readline("y/n")` |
| TUI | `program.Send(ChunkMsg{})` | `input component` |
| WebSocket | `conn.WriteJSON(msg)` | `WriteJSON → ReadJSON` |
| Telegram | `editMessage(buffer)` | `inline keyboard callback` |
| WhatsApp | `sendMessage(buffer)` | `等待回复消息` |

---

## 4 核心子系统详细设计

### 4.1 Tool System (`tool/`)

```go
type RiskLevel string

const (
    RiskLow    RiskLevel = "low"    // 只读操作
    RiskMedium RiskLevel = "medium" // 有限副作用
    RiskHigh   RiskLevel = "high"   // 文件写入、命令执行
)

// ToolSpec 描述一个工具的元信息
type ToolSpec struct {
    Name         string          // 唯一名称
    Description  string          // 供 LLM 理解的描述
    InputSchema  json.RawMessage // JSON Schema 定义输入格式
    Risk         RiskLevel       // 风险等级
    Capabilities []string        // 能力标签 (如 "read", "write", "execute")
}

// ToolHandler 是工具的执行函数
type ToolHandler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// Registry 管理工具的注册与查找
type Registry interface {
    Register(spec ToolSpec, handler ToolHandler) error
    Unregister(name string) error
    Get(name string) (ToolSpec, ToolHandler, bool)
    List() []ToolSpec
    ListByCapability(cap string) []ToolSpec
}
```

默认实现：基于 `sync.RWMutex` 保护的 map。

### 4.2 Session (`session/`)

Session 合并了原始设计中的 Session + ContextStore + Budget 三个概念。

```go
type SessionStatus string

const (
    StatusCreated   SessionStatus = "created"
    StatusRunning   SessionStatus = "running"
    StatusPaused    SessionStatus = "paused"
    StatusCompleted SessionStatus = "completed"
    StatusFailed    SessionStatus = "failed"
    StatusCancelled SessionStatus = "cancelled"
)

type SessionConfig struct {
    Goal         string         // 用户目标
    Mode         string         // 运行模式 (如 "interactive", "autopilot")
    TrustLevel   string         // 信任等级 (如 "trusted", "restricted")
    MaxSteps     int            // 最大步数限制
    MaxTokens    int            // 最大 token 限制
    SystemPrompt string         // 系统提示词
    Metadata     map[string]any // 扩展数据 (如 channel, group_id)
}

type Budget struct {
    MaxTokens  int
    MaxSteps   int
    UsedTokens int
    UsedSteps  int
}

func (b *Budget) Exhausted() bool {
    return (b.MaxTokens > 0 && b.UsedTokens >= b.MaxTokens) ||
           (b.MaxSteps > 0 && b.UsedSteps >= b.MaxSteps)
}

func (b *Budget) Record(tokens, steps int) {
    b.UsedTokens += tokens
    b.UsedSteps += steps
}

type Session struct {
    ID        string
    Status    SessionStatus
    Config    SessionConfig
    Messages  []port.Message     // 对话历史
    State     map[string]any     // 键值状态存储
    Budget    Budget
    CreatedAt time.Time
    EndedAt   time.Time
}

// 核心方法
func (s *Session) AppendMessage(msg port.Message)
func (s *Session) TruncateMessages(maxTokens int, counter func(port.Message) int)
func (s *Session) SetState(key string, value any)
func (s *Session) GetState(key string) (any, bool)
```

**SessionManager**：

```go
type Manager interface {
    Create(ctx context.Context, cfg SessionConfig) (*Session, error)
    Get(id string) (*Session, bool)
    List() []*Session
    Cancel(id string) error
    Notify(id string, msg port.Message) error // 跨 Session 注入消息
}
```

默认实现：基于 `sync.Mutex` 保护的 in-memory map。

**多轮会话复用**：

Session 支持跨多次 `kernel.Run()` 复用。每次调用 Run 时，Loop 会将状态从 `completed` 重置为 `running`，运行结束后再标记为 `completed`。这是 REPL、WebSocket 聊天等多轮对话场景的标准模式。

注意：Budget 在多轮间累积。长时间运行的 Session 应通过 `TruncateMessages()` 控制消息历史长度，并根据需要重置或设置足够大的 MaxSteps/MaxTokens。

### 4.3 Middleware (`middleware/`)

Middleware 是 **唯一的扩展机制**，统一替代了 Hook、Policy、EventBus 三个独立子系统。

```go
type Phase string

const (
    BeforeLLM      Phase = "before_llm"
    AfterLLM       Phase = "after_llm"
    BeforeToolCall Phase = "before_tool_call"
    AfterToolCall  Phase = "after_tool_call"
    OnSessionStart Phase = "on_session_start"
    OnSessionEnd   Phase = "on_session_end"
    OnError        Phase = "on_error"
)

// Context 携带当前执行阶段的上下文信息
type Context struct {
    Phase   Phase
    Session *session.Session
    Tool    *tool.ToolSpec     // 仅 tool 相关 phase
    Input   json.RawMessage    // 工具输入（仅 BeforeToolCall）
    Result  json.RawMessage    // 工具结果（仅 AfterToolCall）
    Error   error              // 错误信息（仅 OnError）
    IO      port.UserIO        // 用户交互接口
}

// Next 调用链中的下一个 middleware
type Next func(ctx context.Context) error

// Middleware 是统一的扩展函数签名
type Middleware func(ctx context.Context, mc *Context, next Next) error

// Chain 管理有序的 middleware 列表
type Chain struct {
    middlewares []Middleware
}

func (c *Chain) Use(mw Middleware)
func (c *Chain) Run(ctx context.Context, phase Phase, mc *Context) error
```

**执行模型**：洋葱模型（Onion Model），与 HTTP middleware 一致。

```
Request → MW1.Before → MW2.Before → MW3.Before → Handler
                                                     ↓
Response ← MW1.After  ← MW2.After  ← MW3.After  ← Result
```

### 4.4 Sandbox (`sandbox/`)

Sandbox 是安全边界的一等公民，所有文件/命令操作必须经过统一隔离层。

```go
type Sandbox interface {
    // 路径操作
    ResolvePath(path string) (string, error) // 路径逃逸保护
    ListFiles(pattern string) ([]string, error)

    // 文件操作
    ReadFile(path string) ([]byte, error)
    WriteFile(path string, content []byte) error

    // 命令执行
    Execute(ctx context.Context, cmd string, args []string) (Output, error)

    // 资源限制
    Limits() ResourceLimits
}

type Output struct {
    Stdout   string
    Stderr   string
    ExitCode int
}

type ResourceLimits struct {
    MaxFileSize    int64         // 最大文件大小
    CommandTimeout time.Duration // 命令超时
    AllowedPaths   []string     // 路径白名单
}
```

**LocalSandbox** 实现要点：
- `ResolvePath` 使用 `filepath.Abs` + 前缀检查防止路径逃逸
- `WriteFile` 自动创建父目录，使用 0644 权限
- `Execute` 设置工作目录为 sandbox root，使用 context timeout

上层应用可实现 `DockerSandbox`（Docker 容器隔离）等其他 adapter。

---

## 5 内置 Middleware

### 5.1 PolicyCheck (`middleware/builtins/policy.go`)

将权限检查实现为 BeforeToolCall middleware。

```go
type PolicyDecision string

const (
    Allow           PolicyDecision = "allow"
    Deny            PolicyDecision = "deny"
    RequireApproval PolicyDecision = "require_approval"
)

// PolicyRule 评估单个工具调用的权限
type PolicyRule func(spec tool.ToolSpec, input json.RawMessage) PolicyDecision

// PolicyCheck 构造 policy middleware，遍历 rules 取最严格决策
func PolicyCheck(rules ...PolicyRule) middleware.Middleware

// 便利规则构造器
func DenyTool(names ...string) PolicyRule
func RequireApprovalFor(names ...string) PolicyRule
func DefaultAllow() PolicyRule
```

评估逻辑：Deny > RequireApproval > Allow（最严格胜出）。

当决策为 `RequireApproval` 时，middleware 通过 `Context.IO.Ask(InputConfirm)` 请求用户确认。

### 5.2 EventEmitter (`middleware/builtins/events.go`)

将事件发布实现为 AfterToolCall / OnSessionEnd middleware。

```go
type Event struct {
    Type      string    // 事件类型 (如 "tool.completed", "session.started")
    Timestamp time.Time
    Data      any       // 事件数据
}

type EventHandler func(Event)

// EventEmitter 构造事件发射 middleware，支持 glob pattern 匹配
func EventEmitter(pattern string, handler EventHandler) middleware.Middleware
```

Pattern 支持 glob 语法：`tool.*`、`session.completed`、`*`（全部）。

### 5.3 Logger (`middleware/builtins/logger.go`)

```go
// Logger 记录每个 phase 的开始/结束/耗时
func Logger() middleware.Middleware
```

---

## 6 Agent Loop

Agent Loop 是 Kernel 的核心调度器，组合所有子系统驱动 Agent 的 think→act→observe 循环。

### 6.1 执行流程

```
                    ┌───────────────────┐
                    │   Session Ready   │
                    └────────┬──────────┘
                             │
                    ┌────────▼──────────┐
             ┌─────│  Budget Exhausted? │
             │ yes └────────┬──────────┘
             │              │ no
             │     ┌────────▼──────────┐
             │     │  BeforeLLM MW     │
             │     └────────┬──────────┘
             │              │
             │     ┌────────▼──────────┐
             │     │   LLM.Complete    │──── or Stream if StreamingLLM
             │     └────────┬──────────┘
             │              │
             │     ┌────────▼──────────┐
             │     │  AfterLLM MW      │
             │     └────────┬──────────┘
             │              │
             │     ┌────────▼──────────┐
             │     │  Has ToolCalls?   │
             │     └───┬──────────┬────┘
             │     yes │          │ no (text only)
             │         │          │
             │  ┌──────▼────┐  ┌──▼──────────────┐
             │  │ For each  │  │ UserIO.Send(Text)│
             │  │ ToolCall: │  └──┬───────────────┘
             │  │           │     │
             │  │ BeforeTool│     │ ┌─────────────┐
             │  │ MW (policy│     │ │ StopReason  │
             │  │ check)    │     │ │ =end_turn?  │
             │  │     ↓     │     │ └──┬──────┬───┘
             │  │ Execute   │     │ yes│      │no
             │  │ handler   │     │    │      │
             │  │     ↓     │     │    │   ┌──▼───┐
             │  │ AfterTool │     │    │   │ Loop │──→ back to top
             │  │ MW (event)│     │    │   └──────┘
             │  │     ↓     │     │    │
             │  │ Append    │     │    │
             │  │ ToolResult│     │    │
             │  └───────┬───┘     │    │
             │          │         │    │
             │          └──→ Loop │    │
             │                    │    │
             │     ┌──────────────▼────▼──┐
             └────→│      Session Done    │
                   └──────────────────────┘
```

### 6.2 Streaming 处理

如果注入的 LLM 实现了 `StreamingLLM` 接口，AgentLoop 自动使用流式模式：

```
Stream iterator:
  for each chunk:
    if chunk.Delta != "" → UserIO.Send(OutputStream, delta)
    if chunk.ToolCall   → 累积构建完整 ToolCall
    if chunk.Done       → UserIO.Send(OutputStreamEnd)
                        → 构建完整 CompletionResponse
```

### 6.3 配置

```go
type LoopConfig struct {
    MaxIterations int                    // 最大循环次数（默认 50）
    StopWhen      func(port.Message) bool // 自定义停止条件
}
```

---

## 7 Kernel 入口

### 7.1 核心 API

```go
// New 使用函数式选项创建 Kernel
func New(opts ...Option) *Kernel

// 常用 Option
func WithLLM(llm port.LLM) Option
func WithSandbox(sb sandbox.Sandbox) Option
func WithUserIO(io port.UserIO) Option
func Use(mw middleware.Middleware) Option

// Kernel 方法
func (k *Kernel) Boot(ctx context.Context) error
func (k *Kernel) NewSession(ctx context.Context, cfg session.SessionConfig) *session.Session
func (k *Kernel) Run(ctx context.Context, sess *session.Session) (*SessionResult, error)
func (k *Kernel) Shutdown(ctx context.Context) error

// 子系统访问
func (k *Kernel) ToolRegistry() tool.Registry
func (k *Kernel) SessionManager() session.Manager

// 便利 API（内部实现为 Middleware）
func (k *Kernel) OnEvent(pattern string, handler builtins.EventHandler)
func (k *Kernel) WithPolicy(rules ...builtins.PolicyRule)
```

### 7.2 使用示例

```go
k := kernel.New(
    kernel.WithLLM(anthropicAdapter),
    kernel.WithSandbox(sandbox.NewLocal("/workspace")),
    kernel.WithUserIO(cliIO),
)

// 注册工具
k.ToolRegistry().Register(tool.ToolSpec{
    Name:        "read_file",
    Description: "Read file contents",
    Risk:        tool.RiskLow,
}, readFileHandler)

// 设置权限策略
k.WithPolicy(
    builtins.RequireApprovalFor("write_file", "run_command"),
    builtins.DefaultAllow(),
)

// 监听事件
k.OnEvent("tool.*", func(e builtins.Event) {
    log.Printf("[%s] %v", e.Type, e.Data)
})

// 启动
k.Boot(ctx)
defer k.Shutdown(ctx)

// 运行
sess := k.NewSession(ctx, session.SessionConfig{
    Goal:       "Fix the null pointer in main.go",
    TrustLevel: "trusted",
    MaxSteps:   50,
})

result, err := k.Run(ctx, sess)
```

### 7.3 SessionResult

```go
type SessionResult struct {
    SessionID  string
    Success    bool
    Output     string
    Steps      int
    TokensUsed port.TokenUsage
    Error      string
}
```

---

## 8 目录结构

```
kernel/
├── kernel.go              # Kernel struct, New(), Boot(), Run(), Shutdown()
├── kernel_test.go         # 集成测试
├── option.go              # functional options: WithLLM, WithSandbox, Use, ...
├── setup.go               # SetupWithDefaults, SetupOption
├── kernel_boot_test.go
│
├── port/                  # Port 接口（零依赖，纯类型定义）
│   ├── types.go           # Message, Role, ToolCall, ToolResult, TokenUsage
│   ├── llm.go             # LLM, StreamingLLM, CompletionRequest/Response
│   ├── io.go              # UserIO, OutputMessage, InputRequest, InputResponse
│   ├── io_std.go          # 标准实现: NoOpIO, PrintfIO, BufferIO
│   ├── channel.go         # Channel, InboundMessage, OutboundMessage
│   └── embedder.go        # Embedder 接口
│
├── tool/                  # Tool System
│   ├── tool.go            # ToolSpec, ToolHandler, RiskLevel
│   ├── registry.go        # Registry interface + default impl
│   ├── scoped.go          # ScopedRegistry (工具白名单视图)
│   └── builtins/          # 内置核心工具 (read_file, write_file, ...)
│
├── session/               # Session Management
│   ├── session.go         # Session, SessionStatus, Budget
│   ├── manager.go         # Manager interface + memory impl
│   ├── router.go          # Router (DMScope 路由)
│   ├── store.go           # SessionStore interface
│   └── store_file.go      # 文件持久化实现
│
├── middleware/             # Middleware System
│   ├── middleware.go       # Middleware type, Phase, Context, Next
│   ├── chain.go           # Chain impl
│   └── builtins/          # 内置 middleware
│       ├── policy.go      # PolicyCheck, PolicyRule, PolicyDecision
│       ├── events.go      # EventEmitter, Event, EventHandler
│       └── logger.go      # Logger
│
├── loop/                  # Agent Loop（核心调度器）
│   ├── loop.go            # AgentLoop, LoopConfig, SessionResult
│   └── loop_test.go
│
├── sandbox/               # Sandbox（执行隔离）
│   ├── sandbox.go         # Sandbox interface, Output, ResourceLimits
│   ├── local.go           # LocalSandbox impl
│   └── local_test.go
│
├── agent/                 # Agent 委派系统
│   ├── config.go          # AgentConfig (YAML 配置)
│   ├── registry.go        # Agent Registry
│   ├── tools.go           # delegate_agent / spawn_agent 工具
│   ├── task.go            # TaskTracker (async task)
│   └── depth.go           # 委派深度限制
│
├── skill/                 # 技能系统
│   ├── skill.go           # Provider 接口, Manager
│   ├── config.go          # 配置加载 (YAML)
│   ├── mcp.go             # MCP Server 集成
│   ├── prompt.go          # SKILL.md 解析
│   └── manager.go         # SkillManager
│
├── appkit/              # 应用脚手架工具箱
│   ├── appkit.go        # ContextWithSignal, CommonFlags, Banner
│   ├── repl.go            # REPL 引擎
│   └── serve.go           # HTTP Serve 脚手架
│
├── bootstrap/             # 引导上下文
│   └── bootstrap.go       # Bootstrap Context (工作区信息注入)
│
├── gateway/               # 消息网关 [实验性]
│   ├── gateway.go         # Gateway (Channel fan-in → Router → Kernel)
│   └── channel/
│       └── cli.go         # CLI Channel 实现
│
├── knowledge/             # 知识系统 [实验性]
│   ├── memory.go          # Memory 短期/长期记忆
│   ├── store.go           # 向量存储接口
│   └── chunker.go         # 文本分块
│
├── scheduler/             # 定时任务调度器
│   ├── scheduler.go       # Scheduler
│   └── store.go           # 任务持久化
│
└── testing/               # Test helpers
    ├── mock_llm.go        # MockLLM (可编程响应)
    ├── mock_sandbox.go    # MemorySandbox (内存文件系统)
    └── mock_io.go         # RecorderIO (记录所有调用)
```

---

## 9 架构验证：应用映射预演

### 9.1 OpenClaw (多渠道 AI 助手)

| OpenClaw 子系统 | Kernel 映射 | 应用层构建 |
|---|---|---|
| Gateway (WS 控制平面) | EventEmitter MW + SessionManager | WS 服务器 + 路由器 |
| Pi Agent Runtime | AgentLoop + LLM Port | RPC 传输层 |
| Multi-channel Inbox | UserIO (各渠道 adapter) | Channel adapters |
| Session Model | Session + Budget + TruncateMessages | 会话路由 + 群组规则 |
| Tool System | ToolRegistry + Middleware | 各工具具体实现 |
| Security/Sandbox | PolicyCheck MW + Sandbox (Docker) | Docker adapter |
| Agent-to-Agent | SessionManager.Notify + Tool | sessions_* 工具实现 |
| Skills Platform | 函数注册 Tool + MW | 技能市场 + 安装流程 |

**关键映射**：

```go
// Gateway 收到消息 → 找到 Session → 运行 Loop
func (g *Gateway) HandleMessage(ctx context.Context, channelMsg ChannelMessage) {
    sess, _ := g.kernel.SessionManager().GetOrCreate(channelMsg.SessionID, ...)
    sess.AppendMessage(port.Message{Role: port.RoleUser, Content: channelMsg.Text})
    result, _ := g.kernel.Run(ctx, sess)
    // result 通过 EventEmitter → 路由回渠道
}

// Model failover 通过 LLM adapter 实现
type FailoverLLM struct { primary port.LLM; fallbacks []port.LLM }
```

### 9.2 Claude Code / Codex CLI (终端编程 Agent)

| 特性 | Kernel 映射 |
|---|---|
| 终端 REPL | UserIO CLI adapter + 持久 Session |
| 3 级权限 (ask/code/architect) | 3 个 PolicyRule 实现 |
| 文件读写/命令执行 | ToolRegistry + Sandbox |
| Sub-agent | SessionManager.Create + kernel.Run |
| Streaming 输出 | StreamingLLM + UserIO.Send(OutputStream) |
| CLAUDE.md 注入 | OnSessionStart middleware |
| /compact 命令 | Session.TruncateMessages() |

**端到端执行流**：

```
用户: "修复 main.go 中的空指针错误"
 ↓
REPL → Session.AppendMessage(user, "修复...")
 ↓
kernel.Run(ctx, session)
 ↓ Loop iteration 1:
   LLM → ToolCall{read_file, "main.go"}
   PolicyCheck → Allow (RiskLow)
   Sandbox.ReadFile("main.go") → 内容
 ↓ Loop iteration 2:
   LLM → ToolCall{edit_file, {path, old, new}}
   PolicyCheck → RequireApproval (RiskHigh)
   UserIO.Ask(Confirm, "Allow edit?") → Approved
   执行 edit → 成功
 ↓ Loop iteration 3:
   LLM → ToolCall{run_command, "go test"}
   PolicyCheck → RequireApproval
   UserIO.Ask(Confirm) → Approved
   Sandbox.Execute("go", ["test"]) → PASS
 ↓ Loop iteration 4:
   LLM → "空指针已修复，测试通过。" → StopReason=end_turn
 ↓
SessionResult{Success: true, Steps: 4}
```

### 9.3 miniroom (多人实时 Agent 游戏)

| miniroom 特性 | Kernel 映射 | 应用层构建 |
|---|---|---|
| Per-Room Agent | 独立 Kernel 实例 + Session | Room 管理、WebSocket 路由 |
| 游戏主持人 | AgentLoop + SystemPrompt 模板 | Script Registry、模板渲染 |
| 多人广播 | UserIO Port (`RoomIO` 适配器) | WebSocket 广播逻辑 |
| 虚拟角色 | 自定义 Tool (add_virtual_player, chat_as) | VirtualPlayer 数据结构 |
| 私聊 | 自定义 Tool (whisper) | 按玩家名查找连接 |
| 游戏状态 | Session.State + 自定义 Tool | GameState 枚举 |
| 多轮对话 | Session 复用 (多次 Run) | 消息队列串行化 |

**架构验证结论**：miniroom 使用了 Per-Instance Kernel 模式（每房间独立 Kernel + Session），通过自定义 `RoomIO` 实现 `port.UserIO` 将 Agent 输出广播到 WebSocket 客户端，同时跳过了 Sandbox 和内置文件工具——验证了 Kernel 的最小化组合能力。

### 9.4 多界面对接

| 界面 | UserIO Adapter | 特殊处理 |
|---|---|---|
| CLI | readline + fmt.Print | OutputStream → 不换行追加 |
| TUI (bubbletea) | program.Send | OutputStream → ChunkMsg |
| Web (WebSocket) | conn.WriteJSON/ReadJSON | OutputMessage 直接序列化 |
| Desktop (Electron) | 同 Web | 同 Web |
| Telegram | grammY sendMessage + inline keyboard | Confirm → inline button |
| WhatsApp | Baileys sendMessage | Confirm → 等待回复 |

---

## 10 设计决策记录

| 决策 | 理由 |
|---|---|
| **5 核心概念** (非 7 或 3) | 兼顾简洁与可发现性，便利 API 弥补语义缺口 |
| **Middleware 统一扩展** | 消除 Hook/Policy/EventBus 选择焦虑，1 个模式解决所有扩展需求 |
| **增强 UserIO (Send/Ask)** | 结构化协议取代原始文本 IO，无缝对接所有界面 |
| **无 Plugin 接口** | 函数注册即可，避免 Init/Shutdown 生命周期复杂性 |
| **Approval 非独立概念** | PolicyCheck MW + UserIO.Ask(Confirm) 组合实现 |
| **Task/Plan/Agent 在上层** | Kernel 只有 Session，编排逻辑不属于最小核心 |
| **Sandbox 保持独立** | 安全隔离是一等公民，显式接口便于审计 |
| **Kernel 零外部依赖** | 仅 Go stdlib，确保长期稳定演化 |
| **Session 合并 ContextStore** | Context 是 Session 的内部状态，无需独立概念 |
| **SessionManager.Notify** | 跨 Session 注入消息的最小原语，支持 Agent-to-Agent |

---

## 11 实现路线

| Phase | 内容 | 依赖 | 可并行 |
|---|---|---|---|
| **1. Port 接口** | types.go, llm.go, io.go | 无 | — |
| **2. 核心子系统** | tool/, session/, middleware/, sandbox/ | Phase 1 | 4 个子系统互相独立，可并行 |
| **3. 内置 MW** | policy.go, events.go, logger.go | middleware/ | 3 个文件可并行 |
| **4. Agent Loop** | loop.go, executor.go | Phase 1-3 | — |
| **5. Kernel 入口** | kernel.go, option.go, result.go | Phase 4 | — |
| **6. 测试** | mock adapters + 单元/集成测试 | Phase 5 | — |

### 验收标准

1. `go test ./kernel/...` 全部通过，核心路径覆盖率 > 80%
2. 集成测试：MockLLM 驱动 3+ 步循环（含 tool calls + policy + streaming）
3. `go build ./kernel/...` 零错误
4. `kernel/` 不 import `internal/`（依赖方向正确）
5. `kernel/` 仅 import Go stdlib + 自身子包（零外部依赖）
6. UserIO Send/Ask 协议验证：CLI + WS mock 均通过

---

## 12 后续演化方向

以下能力 **不在最小核心中**，可在上层应用或后续 Phase 中按需添加：

| 方向 | 实现方式 | 优先级 |
|---|---|---|
| 持久化 | Event Sourcing（EventEmitter MW → 持久化 → 重放恢复） | 中 |
| 多 Agent 编排 | 上层应用：Manager Agent 调用 SessionManager.Create 创建子 Session | 中 |
| MCP 协议 | LLM Port 的 MCP adapter 实现 | 低 |
| Session 恢复 | SessionManager 增加 Restore(id, events) 方法 | 低 |
| 分布式部署 | EventEmitter MW → 消息队列 + 分布式 SessionManager | 低 |
