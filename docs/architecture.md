# 🏗️ Moss 架构设计

> Minimal Agent Runtime Kernel — 5 核心概念 + 2 Port 接口，零外部依赖

---

## 设计哲学

类比 Linux Kernel：**核心最小化、接口稳定、可扩展**。

Kernel 只提供 Agent 运行的不可约原语，所有业务逻辑（Agent 角色、Task 编排、Plan 生成等）在上层应用中实现。

### 第一性原理

一个 Agent 的本质行为：

```
loop {
    observe(context)  → 感知当前状态
    think(llm)        → 推理下一步行动
    act(tool)         → 执行动作
    check(policy)     → 安全检查
}
```

这个循环是 **唯一的不可约内核**。其他一切都是为这个循环服务的基础设施。

---

## 分层架构

```
┌──────────────────────────────────────────────────────────────┐
│                   Applications / Agents                       │
│  (CLI, TUI, Web 服务, 自定义 Agent, ...)                      │
├──────────────────────────────────────────────────────────────┤
│                   Middleware Chain                             │
│  (PolicyCheck, EventEmitter, Logger, 自定义 Middleware)       │
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
│  Claude / OpenAI / 兼容 API         CLI / TUI / WS / IM     │
│  LocalSandbox / DockerSandbox                                 │
└──────────────────────────────────────────────────────────────┘
```

**依赖规则**：`Adapters → Applications → Kernel → Ports`

Kernel 层**零外部依赖**（仅 Go stdlib + 自身子包）。

---

## 核心概念

| 概念 | 职责 | Linux Kernel 类比 |
|---|---|---|
| **Loop** | Agent 执行循环 (think→act→observe) | Process Scheduler |
| **Tool** | 能力注册、查找、执行 | System Calls |
| **Session** | 执行上下文 (消息+状态+预算) | Process + Memory |
| **Middleware** | 统一扩展点 (合并 Hook/Policy/Event) | Kernel Modules |
| **Sandbox** | 执行隔离 (文件+命令) | Namespaces / cgroups |

### Port 接口

| Port | 职责 |
|---|---|
| **LLM** | 模型调用 (Complete + Stream) |
| **UserIO** | 结构化交互协议 (Send + Ask) |

---

## 核心子系统

### Tool System

```go
type ToolSpec struct {
    Name         string          // 唯一名称
    Description  string          // 供 LLM 理解的描述
    InputSchema  json.RawMessage // JSON Schema
    Risk         RiskLevel       // low / medium / high
    Capabilities []string        // 能力标签
}

type ToolHandler func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

type Registry interface {
    Register(spec ToolSpec, handler ToolHandler) error
    Unregister(name string) error
    Get(name string) (ToolSpec, ToolHandler, bool)
    List() []ToolSpec
    ListByCapability(cap string) []ToolSpec
}
```

**内置 6 个核心工具**：

| 工具 | 风险 | 说明 |
|---|---|---|
| `read_file` | Low | 读取文件内容 |
| `write_file` | High | 写入文件（自动创建目录） |
| `list_files` | Low | Glob 模式列出文件 |
| `search_text` | Low | 正则搜索文件内容 |
| `run_command` | High | 执行 shell 命令 |
| `ask_user` | Medium | 向用户请求输入 |

### Session

Session 统一管理对话历史、状态存储和资源预算。

```go
type Session struct {
    ID        string
    Status    SessionStatus     // created / running / paused / completed / failed / cancelled
    Config    SessionConfig     // Goal, Mode, TrustLevel, MaxSteps, MaxTokens, SystemPrompt
    Messages  []port.Message    // 对话历史
    State     map[string]any    // 键值状态存储
    Budget    Budget            // MaxTokens, MaxSteps, UsedTokens, UsedSteps
    CreatedAt time.Time
    EndedAt   time.Time
}

type Manager interface {
    Create(ctx, cfg) (*Session, error)
    Get(id) (*Session, bool)
    List() []*Session
    Cancel(id) error
    Notify(id, msg) error       // 跨 Session 注入消息
}
```

### Middleware

Middleware 是**唯一的扩展机制**，统一替代了 Hook、Policy、EventBus。

```go
type Middleware func(ctx context.Context, mc *Context, next Next) error

// 7 个执行阶段
BeforeLLM / AfterLLM / BeforeToolCall / AfterToolCall /
OnSessionStart / OnSessionEnd / OnError
```

执行模型：**洋葱模型** (Onion Model)

```
Request → MW1.Before → MW2.Before → Handler → MW2.After → MW1.After → Response
```

**内置 Middleware**：

| Middleware | 功能 |
|---|---|
| `PolicyCheck` | 工具调用权限检查 (Allow / Deny / RequireApproval) |
| `EventEmitter` | 事件发布 (glob pattern 匹配) |
| `Logger` | Phase 耗时日志 |

### Sandbox

所有文件/命令操作经过统一隔离层。

```go
type Sandbox interface {
    ResolvePath(path string) (string, error)     // 路径逃逸保护
    ListFiles(pattern string) ([]string, error)
    ReadFile(path string) ([]byte, error)
    WriteFile(path string, content []byte) error
    Execute(ctx context.Context, cmd string, args []string) (Output, error)
    Limits() ResourceLimits
}
```

`LocalSandbox` 实现：路径逃逸检查 + 自动创建目录 + Shell 自动包装 + 资源限制。

### Agent Loop

核心调度器，组合所有子系统驱动 think→act→observe 循环：

```
Session Ready → Budget Check → BeforeLLM → LLM.Complete/Stream → AfterLLM
    → Has ToolCalls?
      Yes → For each: BeforeToolCall → PolicyCheck → Execute → AfterToolCall → Loop
      No  → Check StopReason → end_turn? → SessionResult
```

支持 Streaming：如果 LLM 实现 `StreamingLLM`，自动使用流式模式实时输出。

---

## Port 接口详情

### LLM Port

```go
type LLM interface {
    Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)
}

type StreamingLLM interface {
    Stream(ctx context.Context, req CompletionRequest) (StreamIterator, error)
}
```

### UserIO Port

结构化交互协议，无缝对接 CLI/TUI/Web/Desktop/IM 等所有界面。

```go
type UserIO interface {
    Send(ctx context.Context, msg OutputMessage) error     // 推送内容
    Ask(ctx context.Context, req InputRequest) (InputResponse, error)  // 请求输入
}
```

**OutputType**: `text` / `stream` / `stream_end` / `progress` / `tool_start` / `tool_result`
**InputType**: `free_text` / `confirm` / `select`

**标准实现** (`kernel/port/io_std.go`)：

| 实现 | 场景 | 行为 |
|---|---|---|
| `NoOpIO` | 后台任务、纯自动化 | 忽略所有输出，Ask 返回安全默认值 (Confirm=false) |
| `PrintfIO` | 非交互式 CLI、日志 | 格式化输出到 io.Writer，自动批准确认 |
| `BufferIO` | 测试 | 线程安全缓冲，支持 `AskFunc` 自定义响应 |

---

## 依赖图

```
adapters/claude, adapters/openai    (外部 SDK)
    ↓ implements
kernel/port                         (纯接口，零依赖)
    ↑ references
kernel/tool, kernel/session         (独立子系统)
    ↑ references
kernel/middleware                   (imports session, tool, port)
    ↑ references
kernel/loop                         (imports 以上所有)
    ↑ references
kernel/kernel.go                    (Kernel 入口，组合所有子系统)
    ↑ references
kernel/skill                        (Skill 管理，imports tool, middleware, sandbox, port)
kernel/tool/builtins                (内置工具，imports skill, port, sandbox, tool)
kernel/setup.go                     (SetupWithDefaults，imports skill, builtins)
    ↑ used by
cmd/moss                            (CLI/TUI 入口)
```

**关键隔离**：`kernel/port` 不 import 任何其他 kernel 子包。

---

## 设计决策记录

| 决策 | 理由 |
|---|---|
| 5 核心概念 (非 7 或 3) | 兼顾简洁与可发现性，便利 API 弥补语义缺口 |
| Middleware 统一扩展 | 消除 Hook/Policy/EventBus 选择焦虑 |
| 结构化 UserIO (Send/Ask) | 取代原始文本 IO，无缝对接所有界面 |
| Approval 非独立概念 | PolicyCheck MW + UserIO.Ask(Confirm) 组合实现 |
| Task/Plan/Agent 在上层 | Kernel 只有 Session，编排逻辑不属于最小核心 |
| Sandbox 保持独立 | 安全隔离是一等公民，显式接口便于审计 |
| Kernel 零外部依赖 | 仅 Go stdlib，确保长期稳定演化 |
