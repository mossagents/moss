# 🏭 Moss 生产就绪路线图

> 基于 8 个示例场景的真实业务分析，分阶段补齐核心能力

---

## 1 当前状态评估

### 1.1 示例场景 → 生产推演

| 示例 | 演示场景 | 真实业务推演 | 暴露的核心缺口 |
|---|---|---|---|
| **miniroom** | 多人 WebSocket 游戏 | 多租户 SaaS、多房间实时协作 | Sandbox 与 Kernel 绑定本地路径，无法多实例部署 |
| **minitrade** | 量化交易模拟 | 高频交易信号/策略引擎 | Scheduler 单实例重复执行、状态丢失 |
| **miniwork** | Manager → Worker 编排 | CI/CD 流水线、数据处理 Pipeline | 任务丢失、无跨实例可见性 |
| **miniclaw** | 个人助手 + 知识库 | 企业知识管理 + 定时任务 | Knowledge 内存驻留、重启丢失 |
| **minicode** | 单人代码助手 | 团队共享代码 Agent | 无认证授权、审计缺失 |
| **websocket** | WebSocket 聊天 | 高并发 API Gateway | 无心跳/重连、无限流量 |
| **basic/custom-tool** | 基础 REPL | 嵌入式 SDK 集成 | 错误信息不结构化、难以上层捕获 |

### 1.2 关键差距矩阵

```
                    单实例生产    Web 分布式    高可用
Kernel Core         ████████░░    ████░░░░░░    ██░░░░░░░░
Session             ████████░░    ███░░░░░░░    ██░░░░░░░░
Sandbox/Workspace   ████████░░    ██░░░░░░░░    ██░░░░░░░░
工具系统             █████████░    ████░░░░░░    ███░░░░░░░
Middleware          █████████░    ████████░░    ████████░░
可观测性             ██░░░░░░░░    █░░░░░░░░░    █░░░░░░░░░
错误处理             ███░░░░░░░    ██░░░░░░░░    ██░░░░░░░░
安全/审计            ████░░░░░░    ██░░░░░░░░    ██░░░░░░░░
```

---

## 2 分阶段路线

### 总览

```
Phase 1 (单实例加固)          → 让每个示例可以真实上线
Phase 2 (Workspace 抽象化)    → 让 miniroom/websocket 可多实例部署
Phase 3 (分布式基础设施)       → Session/Scheduler/Knowledge 分布式化
Phase 4 (安全与合规)           → 认证授权 + 审计日志 + RBAC
```

---

## 3 Phase 1：单实例生产加固

> **目标**：让任何一个 example 可以跑在单台机器上面向真实用户，不丢数据、不吞错误、可追踪问题。

### 3.1 Workspace 抽象（Sandbox 解耦本地路径）

**现状问题**：

`Sandbox` 接口直接绑定了文件系统操作（ReadFile/WriteFile/ListFiles），在以下场景中不适用：

- **miniroom 多房间**：每个房间 Kernel 共享同一本地目录，无法隔离
- **websocket 多连接**：per-connection Kernel 的 Sandbox 指向同一 workDir
- **Web 部署**：文件存储可能在 S3/OSS，而非本地

**设计方案**：引入 `Workspace` Port 接口，将"文件系统"与"命令执行"分离。

```go
// kernel/port/workspace.go

// Workspace 是 Agent 工作区的抽象层。
// 不同部署场景（本地、Docker、云存储、虚拟内存）实现此接口。
type Workspace interface {
    // ReadFile 从工作区读取文件。
    ReadFile(ctx context.Context, path string) ([]byte, error)
    // WriteFile 向工作区写入文件。
    WriteFile(ctx context.Context, path string, content []byte) error
    // ListFiles 按 glob 模式列出文件。
    ListFiles(ctx context.Context, pattern string) ([]string, error)
    // Stat 获取文件元信息（存在性、大小、修改时间）。
    Stat(ctx context.Context, path string) (FileInfo, error)
    // DeleteFile 删除文件。
    DeleteFile(ctx context.Context, path string) error
}

type FileInfo struct {
    Name    string
    Size    int64
    IsDir   bool
    ModTime time.Time
}

// Executor 是命令执行的抽象层。
// 与 Workspace（文件存储）正交：可组合不同的 Workspace + Executor。
type Executor interface {
    Execute(ctx context.Context, cmd string, args []string) (sandbox.Output, error)
    Limits() sandbox.ResourceLimits
}
```

**兼容性**：现有 `Sandbox` 接口保持不变（作为 Workspace + Executor 的组合实现），内置工具内部切换到使用 Workspace/Executor。

**实现计划**：

| 实现 | 场景 | 说明 |
|---|---|---|
| `LocalWorkspace` | 单机部署 | 从现有 LocalSandbox 提取，保留路径逃逸保护 |
| `MemoryWorkspace` | 测试、短生命周期 Agent | 内存 map[string][]byte，miniroom 每房间独立 |
| `ScopedWorkspace` | 多租户 | 前缀隔离 + 委托到底层 Workspace |
| `LocalExecutor` | 单机命令执行 | 从现有 LocalSandbox 提取 |
| `NoOpExecutor` | 纯对话场景 | 拒绝所有命令执行 |

### 3.2 结构化错误体系

**现状问题**：全部使用 `fmt.Errorf`，上层无法区分错误类型进行恢复。

```go
// kernel/kerr/errors.go

package kerr

// Code 是错误分类码。
type Code string

const (
    ErrBudgetExhausted Code = "BUDGET_EXHAUSTED"
    ErrToolNotFound    Code = "TOOL_NOT_FOUND"
    ErrToolExecution   Code = "TOOL_EXECUTION"
    ErrLLMCall         Code = "LLM_CALL"
    ErrLLMTimeout      Code = "LLM_TIMEOUT"
    ErrSandboxDenied   Code = "SANDBOX_DENIED"
    ErrSessionNotFound Code = "SESSION_NOT_FOUND"
    ErrPolicyDenied    Code = "POLICY_DENIED"
    ErrValidation      Code = "VALIDATION"
    ErrInternal        Code = "INTERNAL"
)

// Error 是 Moss Kernel 的结构化错误。
type Error struct {
    Code      Code           // 机器可读分类
    Message   string         // 人类可读描述
    Cause     error          // 原始错误（可选）
    Retryable bool           // 是否可重试
    Meta      map[string]any // 附加上下文（tool_name, session_id 等）
}

func (e *Error) Error() string   { ... }
func (e *Error) Unwrap() error   { return e.Cause }
func (e *Error) Is(target error) bool { ... }

// 便利构造
func New(code Code, msg string) *Error
func Wrap(code Code, msg string, cause error) *Error
func IsRetryable(err error) bool
func GetCode(err error) Code
```

### 3.3 可观测性基础

**现状问题**：只有可选的 Logger middleware，无结构化日志、无指标、无追踪。

**设计方案**：在 Kernel 内核引入轻量级 Hook 点，不引入外部依赖。

```go
// kernel/port/observer.go

// Observer 是 Kernel 运行事件的观察者接口。
// 上层应用实现此接口对接 OpenTelemetry / Prometheus / slog 等。
type Observer interface {
    // OnLLMCall 在 LLM 调用完成后触发。
    OnLLMCall(ctx context.Context, e LLMCallEvent)
    // OnToolCall 在工具调用完成后触发。
    OnToolCall(ctx context.Context, e ToolCallEvent)
    // OnSessionEvent 在 Session 生命周期事件时触发。
    OnSessionEvent(ctx context.Context, e SessionEvent)
    // OnError 在错误发生时触发。
    OnError(ctx context.Context, e ErrorEvent)
}

type LLMCallEvent struct {
    SessionID  string
    Model      string
    Duration   time.Duration
    Usage      TokenUsage
    StopReason string
    Error      error
}

type ToolCallEvent struct {
    SessionID string
    ToolName  string
    Risk      string
    Duration  time.Duration
    Error     error
}

type SessionEvent struct {
    SessionID string
    Type      string // "created", "running", "completed", "failed", "cancelled"
}

type ErrorEvent struct {
    SessionID string
    Phase     string
    Error     error
}

// NoOpObserver 是默认的空实现。
type NoOpObserver struct{}
```

**与 Middleware 的关系**：Observer 是只读的被动观察，不能修改执行流；Middleware 可以拦截和修改。两者互补。

### 3.4 优雅关停

**现状问题**：`appkit` 的 SIGINT 处理直接 `os.Exit`，进行中的请求丢失。

```go
// kernel/kernel.go 新增方法

// Shutdown 优雅关停 Kernel。
// 1. 停止接受新请求
// 2. 等待进行中的 Session 完成（或超时后取消）
// 3. 持久化所有活跃 Session
// 4. 关闭 Skill（MCP 连接等）
// 5. 关闭 Scheduler
func (k *Kernel) Shutdown(ctx context.Context) error
```

### 3.5 LLM 熔断器

**现状问题**：LLM 调用只有重试，无熔断。当 LLM 服务不可用时，所有请求堆积。

```go
// kernel/retry/breaker.go

// Breaker 实现简单的熔断器模式。
// 连续 N 次失败 → 打开（拒绝请求）→ 半开（尝试一个）→ 关闭。
type Breaker struct {
    maxFailures int
    resetAfter  time.Duration
}
```

集成点：在 `loop.go` 的 LLM 调用前检查熔断器状态。

### 3.6 Session 请求超时

为每个 `k.Run()` 调用增加可配置的超时机制：

```go
type SessionConfig struct {
    // ... 现有字段 ...
    Timeout time.Duration // 单次 Run 的总超时时间（含所有 LLM + Tool 调用）
}
```

---

## 4 Phase 2：Workspace 抽象化落地 ✅

> **目标**：让 miniroom / websocket 等场景可以水平扩展到多实例部署。

### 4.1 核心：Sandbox → Workspace + Executor 重构 ✅

**已完成**：

- ✅ `port.Workspace` 和 `port.Executor` 接口 (Phase 1)
- ✅ 内置工具优先使用 `Workspace`/`Executor`，回退到 `Sandbox` (Phase 1)
- ✅ `WithWorkspace()` / `WithExecutor()` Option (Phase 1)
- ✅ `WithSandbox()` 自动适配为通用 Workspace + Executor 适配器
- ✅ `SessionConfig.Timeout` + `Kernel.Run` 超时强制执行

### 4.2 MemoryWorkspace 实现 ✅

Phase 1 已实现 `sandbox.MemoryWorkspace`，含容量限制和完整测试。

### 4.3 ScopedWorkspace 实现 ✅

Phase 1 已实现 `sandbox.ScopedWorkspace`，含路径隔离和完整测试。

### 4.4 Session Store 接口扩展 ✅

- ✅ `SessionStore.Watch()` 方法已添加
- ✅ `ErrNotSupported` 哨兵错误
- ✅ `FileStore.Watch()` 返回 `ErrNotSupported`
- ✅ miniroom 迁移到 `MemoryWorkspace`（每房间独立虚拟文件系统）

---

## 5 Phase 3：分布式基础设施 ✅

> **目标**：Scheduler 不重复执行、Session 可跨实例访问、Knowledge 不丢失。

### 5.1 分布式 Session Store

**接口层已就绪**（Phase 2 添加 Watch）。Redis 实现为外部 adapter，按需开发。

### 5.2 Scheduler 去重 ✅

- ✅ `Lock` 接口 (`TryLock` + `ErrLockHeld`)
- ✅ `LocalLock` 内存实现（含 TTL 自动过期）
- ✅ `Scheduler.WithLock()` Option + 执行前自动获取锁
- ✅ 完整测试覆盖

### 5.3 Knowledge 持久化

接口不变，增加向量数据库实现：

| 实现 | 适用场景 |
|---|---|
| `InMemoryStore`（现有） | 开发/测试 |
| `SQLiteStore` | 单机生产（外部 adapter） |
| `PgVectorStore` | 分布式生产（外部 adapter） |

### 5.4 分布式 Task Tracker ✅

- ✅ `TaskStore` 接口 (`Save`/`Load`/`List` + `TaskFilter`)
- ✅ `InMemoryTaskStore` 实现（线程安全、返回副本）
- ✅ 完整测试覆盖

---

## 6 Phase 4：安全与合规 ✅

> **目标**：多租户环境下的认证授权与审计。

### 6.1 认证框架 ✅

- ✅ `port.Identity` 结构体（UserID/TenantID/Roles/Meta + HasRole 方法）
- ✅ `port.Authenticator` 接口
- ✅ `AuthMiddleware` — 在 OnSessionStart 阶段从 Metadata 取 token 认证，注入 Identity 到 Session.State

### 6.2 RBAC 工具访问控制 ✅

- ✅ `RBACRule` 结构体（Role/Tools/Action，支持 `*` 通配符）
- ✅ `RBAC()` middleware — 按角色+工具名第一匹配规则决策
- ✅ `SetIdentity`/`GetIdentity` 辅助函数
- ✅ `RiskBasedPolicy` — PolicyRule 按工具风险级别决策

### 6.3 审计日志 ✅

- ✅ `AuditLogger` 实现 Observer 接口（JSON Lines 输出）
- ✅ 支持 LLM 调用、工具调用、Session 事件、错误事件四种审计记录
- ✅ 线程安全，不侵入核心逻辑

### 6.4 速率限制 ✅

- ✅ `RateLimiter(rps, burst)` middleware — BeforeLLM 阶段按 Session 限流
- ✅ 令牌桶算法实现
- ✅ `kerr.ErrRateLimit` 错误码

---

## 7 实施优先级

```
P0 — 不做就不能上线（Phase 1）✅
 ├─ ✅ Workspace 抽象层（port 接口 + LocalWorkspace 提取）
 ├─ ✅ 结构化错误体系 (kerr 包)
 ├─ ✅ 可观测性 Observer 接口
 ├─ ✅ 优雅关停 Shutdown
 └─ ✅ LLM 熔断器

P1 — 上线后立即需要（Phase 1 + Phase 2）✅
 ├─ ✅ MemoryWorkspace（miniroom 多房间隔离）
 ├─ ✅ ScopedWorkspace（多租户路径隔离）
 ├─ ✅ Session 超时机制
 └─ ✅ 内置工具切换到 Workspace/Executor

P2 — 多实例部署需要（Phase 3）✅
 ├─ ✅ Session Store Watch 接口
 ├─ ✅ Scheduler 分布式锁
 ├─ ✅ Task Store 接口
 └─ Knowledge SQLite/PgVector Store（外部 adapter，按需开发）

P3 — 企业级需要（Phase 4）✅
 ├─ ✅ 认证框架
 ├─ ✅ RBAC
 ├─ ✅ 审计日志
 └─ ✅ 速率限制
```

---

## 8 Phase 1 详细执行计划

Phase 1 是当前需要立即执行的部分，拆解为可独立完成的 PR：

### PR-1: kerr 结构化错误包 ✅

- ✅ 新建 `kernel/kerr/errors.go`
- ✅ 定义 14 种错误码枚举 + Error 结构体 + Retryable/Meta 扩展
- ✅ 7 个单测全部通过
- 在 loop.go / session / sandbox 关键路径替换 fmt.Errorf（后续渐进迁移）

### PR-2: Observer 可观测性接口 ✅

- ✅ 新建 `kernel/port/observer.go`
- ✅ 定义 Observer 接口 + NoOpObserver + 4 种事件类型
- ✅ Kernel 新增 `WithObserver()` Option
- ✅ loop.go 中 LLM 调用、工具调用处插入 Observer 回调（含耗时追踪）
- ✅ Session 生命周期事件（running / completed）

### PR-3: Workspace + Executor 接口 ✅

- ✅ 新建 `kernel/port/workspace.go`
- ✅ 定义 Workspace 接口 + Executor 接口 + FileInfo + ExecOutput + NoOpExecutor
- ✅ `sandbox/local.go` 提供 LocalWorkspace / LocalExecutor 适配器
- ✅ Kernel 新增 WithWorkspace / WithExecutor Option

### PR-4: 内置工具使用 Workspace/Executor ✅

- ✅ `tool/builtins/` 中 read_file / write_file / list_files / search_text 支持 Workspace
- ✅ run_command 支持 Executor
- ✅ 当 Workspace/Executor 未设置时，回退到 Sandbox（向后兼容）
- ✅ 新增 7 个 Workspace/Executor 测试 + 原有测试全部通过

### PR-5: MemoryWorkspace + ScopedWorkspace ✅

- ✅ `sandbox/memory.go`：内存文件系统实现（含容量限制、路径归一化）
- ✅ `sandbox/scoped.go`：前缀隔离包装器（含路径穿越保护）
- ✅ 12 个单测全部通过

### PR-6: 优雅关停 + 熔断器 ✅

- ✅ Kernel.Shutdown() 实现（标记拒绝新请求 → 等待活跃运行 → 持久化 Session → 关闭组件）
- ✅ retry/breaker.go 熔断器（Closed→Open→HalfOpen→Closed 状态机）
- ✅ loop.go 集成熔断器
- ✅ 5 个熔断器单测全部通过

### PR-7: miniroom 示例迁移

- 每房间使用 MemoryWorkspace 替代共享 LocalSandbox
- 验证多房间隔离
- 作为 Workspace 抽象的集成验证用例

---

## 9 架构影响分析

### 9.1 改动不影响现有 API

| 改动 | 影响 | 兼容策略 |
|---|---|---|
| 新增 kerr 包 | 纯新增 | 现有 error 返回值仍可用 errors.As 解析 |
| 新增 Observer | 纯新增 | 不设置则使用 NoOpObserver |
| Workspace 接口 | 纯新增 | WithSandbox 仍有效 |
| Executor 接口 | 纯新增 | WithSandbox 仍有效 |
| Shutdown 方法 | 纯新增 | 不调用则行为不变 |
| 熔断器 | 内部增强 | 默认关闭，WithBreaker 启用 |

### 9.2 破坏性变更（None）

Phase 1 全部为新增接口和可选功能，**零破坏性变更**。

### 9.3 依赖规则检查

新增包的依赖关系：

```
kernel/kerr         → 零依赖（纯 Go）
kernel/port/observer → kernel/port（现有）
kernel/port/workspace → kernel/sandbox（Output, ResourceLimits 类型）
kernel/sandbox/memory → kernel/port/workspace
kernel/sandbox/scoped → kernel/port/workspace
kernel/retry/breaker → 零依赖（纯 Go）
```

全部满足 Kernel 零外部依赖原则。

---

## 10 成功指标

| 里程碑 | 验证方式 | 状态 |
|---|---|---|
| Phase 1 完成 | 所有 example 正常运行 + 新增单测通过 | ✅ 全部 20 个内核包测试通过 |
| Workspace 抽象验证 | 内置工具支持 Workspace/Executor + 回退 Sandbox | ✅ 7 个新 WS/Exec 测试通过 |
| 可观测性验证 | Observer 接口接入 loop.go LLM/Tool/Session 事件 | ✅ |
| 错误体系验证 | 上层可通过 kerr.GetCode() 区分错误类型 | ✅ 7 个 kerr 测试通过 |
| 优雅关停验证 | Shutdown 拒绝新请求 + 等待活跃 + 持久化 | ✅ |
| 熔断器验证 | LLM 不可用时快速失败而非堆积 | ✅ 5 个熔断器测试通过 |
| MemoryWorkspace | 内存虚拟文件系统 + 容量限制 + 路径归一化 | ✅ 8 个测试通过 |
| ScopedWorkspace | 前缀隔离 + 路径穿越防护 | ✅ 4 个测试通过 |
