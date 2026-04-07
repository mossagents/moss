# Workspace ↔ Sandbox 边界设计

> 状态：**草稿** · 优先级：P1 · 关联待办：P1-D6 / P1-I9

---

## 1. 问题陈述

当前 Workspace 与 Sandbox 的职责边界不清晰：

| 问题 | 现状 |
|------|------|
| 概念混淆 | `port.Workspace` 提供文件读写，`sandbox.Sandbox` 提供快照/patch，但没有明确的分工文档 |
| 隔离缺失 | Workspace 直接操作宿主机文件系统，无容器级隔离 |
| 执行路由 | `port.Executor` 执行命令，但不清楚哪些命令应在 sandbox 中执行、哪些可以直接执行 |
| 状态同步 | sandbox 快照 (`git stash`-style) 与 workspace 文件状态何时同步不明确 |
| Subagent 隔离 | 多个 subagent 并发写同一 workspace 时没有冲突保护 |

---

## 2. 设计原则

```
┌─────────────────────────────────────────────────────────────┐
│                     Agent (AgentLoop)                        │
│                                                              │
│  文件操作 ──────→  Workspace（逻辑抽象层）                    │
│  命令执行 ──────→  Executor（命令执行层）                     │
│                         │                                    │
│                    路由决策                                   │
│                    ┌────┴────┐                               │
│             安全命令│         │高风险命令                      │
│                    ↓         ↓                               │
│              HostExecutor  SandboxExecutor                   │
│              （直接执行）   （容器/进程隔离）                  │
│                              │                               │
│                         Sandbox（隔离层）                     │
│                         - 快照/回滚                          │
│                         - 资源限制                           │
│                         - 网络隔离                           │
└─────────────────────────────────────────────────────────────┘
```

**核心原则**：
1. **Workspace** = 文件系统的逻辑视图（读写、路径解析、权限检查）
2. **Sandbox** = 执行环境的隔离容器（命令执行、资源限制、快照管理）
3. 两者通过 **挂载点** 关联：sandbox 挂载 workspace 目录
4. **只读操作** 可绕过 sandbox；**写操作和命令执行** 必须经过 sandbox（当启用时）

---

## 3. Workspace 职责（精化）

```go
// kernel/port/workspace.go（精化版）
type Workspace interface {
    // === 文件操作（逻辑层，不感知沙箱）===
    ReadFile(ctx context.Context, path string) ([]byte, error)
    WriteFile(ctx context.Context, path string, content []byte) error
    DeleteFile(ctx context.Context, path string) error
    ListFiles(ctx context.Context, pattern string) ([]FileInfo, error)
    Exists(ctx context.Context, path string) (bool, error)

    // === 路径管理 ===
    Root() string                              // 工作目录绝对路径
    Resolve(rel string) (string, error)        // 解析相对路径（防路径穿越）
    Relative(abs string) (string, error)       // 转换为相对路径

    // === 访问控制 ===
    // CheckAccess 检查路径是否在允许访问范围内
    CheckAccess(ctx context.Context, path string, mode AccessMode) error
}

type AccessMode int
const (
    AccessRead  AccessMode = 1 << iota
    AccessWrite
    AccessDelete
    AccessExecute
)
```

**Workspace 不负责**：命令执行、进程管理、网络访问、资源限制。

---

## 4. Sandbox 职责（精化）

```go
// sandbox/sandbox.go（精化版）
type Sandbox interface {
    // === 快照管理（现有，保留）===
    Snapshot(ctx context.Context) (SnapshotID, error)
    Restore(ctx context.Context, id SnapshotID) error
    ListSnapshots(ctx context.Context) ([]SnapshotInfo, error)
    DeleteSnapshot(ctx context.Context, id SnapshotID) error
    Diff(ctx context.Context, id SnapshotID) ([]FileDiff, error)
    Apply(ctx context.Context, patch []byte) error

    // === 生命周期（新增）===
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Status(ctx context.Context) (SandboxStatus, error)

    // === 执行（新增，接管 Executor 在高风险场景的角色）===
    Exec(ctx context.Context, cmd ExecRequest) (ExecResult, error)

    // === 工作目录关联 ===
    // WorkDir 返回 sandbox 内部的工作目录路径
    WorkDir() string
    // Mount 将宿主机目录挂载到 sandbox
    Mount(ctx context.Context, hostPath, sandboxPath string, readonly bool) error
}

type SandboxStatus string
const (
    SandboxIdle    SandboxStatus = "idle"
    SandboxRunning SandboxStatus = "running"
    SandboxStopped SandboxStatus = "stopped"
    SandboxError   SandboxStatus = "error"
)
```

---

## 5. Executor 路由策略

`port.Executor` 根据命令的风险等级路由到不同后端：

```go
// kernel/port/executor.go（扩展）
type ExecRequest struct {
    // ... 现有字段 ...
    
    // IsolationLevel 指定隔离需求
    // auto（默认）= 由 Executor 根据 policy 决定
    // host = 强制在宿主机执行（仅读命令）
    // sandbox = 强制在 sandbox 中执行
    IsolationLevel IsolationLevel `json:"isolation_level,omitempty"`
}

type IsolationLevel string
const (
    IsolationAuto    IsolationLevel = "auto"
    IsolationHost    IsolationLevel = "host"
    IsolationSandbox IsolationLevel = "sandbox"
)
```

**路由规则**（`auto` 模式）：

| 命令类型 | 判断依据 | 路由目标 |
|----------|----------|----------|
| 只读查询 | `ls`, `cat`, `grep`, `find` | 宿主机（直接执行）|
| 代码执行 | `python`, `node`, `go run` | Sandbox |
| 包管理 | `npm`, `pip`, `go get` | Sandbox |
| git 操作 | `git commit`, `git push` | 宿主机（需审批）|
| 系统修改 | `apt`, `brew`, `chmod` | Sandbox（或拒绝）|
| 网络请求 | `curl`, `wget` | Sandbox（网络受限）|

---

## 6. Subagent 并发保护

当多个 subagent 并发操作同一 workspace 时：

```go
// kernel/port/workspace.go（扩展）
type WorkspaceLock interface {
    // Lock 获取路径锁（支持粒度：文件/目录/全局）
    Lock(ctx context.Context, path string, agentID string) (func(), error)
    // TryLock 非阻塞尝试获取锁
    TryLock(ctx context.Context, path string, agentID string) (func(), bool)
    // CurrentHolder 查看当前持锁者
    CurrentHolder(path string) (string, bool)
}
```

**锁策略**：
- 并发**读**：不加锁
- 并发**写不同文件**：不加锁（路径不重叠）
- 并发**写同一文件**：必须持锁（FIFO 队列）
- **快照操作**：需要全局锁

---

## 7. Sandbox 实现规划

当前实现（`sandbox/git_sandbox.go`）基于 git stash，已能很好地满足快照/回滚需求。

未来实现层级：

| 级别 | 实现方案 | 隔离程度 | 适用场景 |
|------|----------|----------|----------|
| L0 | Git sandbox（现有）| 文件系统版本控制 | 开发调试 |
| L1 | 进程沙箱（`os/exec` + `seccomp`）| 系统调用过滤 | 安全要求中等 |
| L2 | 容器沙箱（Docker/Podman）| 完整 OS 隔离 | 生产环境 |
| L3 | VM 沙箱（Firecracker）| 硬件级隔离 | 高安全需求 |

P1 阶段目标：实现 L1（进程沙箱），通过 `port.Sandbox` 接口抽象使上层代码不感知底层实现。

---

## 8. 文件结构规划

```
sandbox/
├── sandbox.go          # Sandbox 接口（从 port 迁移或引用）
├── git/
│   └── git_sandbox.go  # 现有实现，重构为 Git 后端
├── process/
│   └── process_sandbox.go  # L1：进程沙箱（P1 新增）
└── docker/
    └── docker_sandbox.go   # L2：Docker 沙箱（P2 后续）

kernel/port/
├── workspace.go        # 精化 Workspace 接口 + WorkspaceLock
└── executor.go         # 扩展 IsolationLevel
```

---

## 9. 迁移路径

1. 明确现有代码中 workspace 和 sandbox 的用法（搜索 `port.Workspace`、`sandbox.New`）
2. 在接口上添加 `WorkspaceLock` 支持（不破坏现有实现，提供 `NoopLock` 默认实现）
3. 重构 `sandbox.Sandbox` 增加 `Start/Stop/Status` 生命周期方法
4. 在 `AgentLoop` 的 tool 调用链中加入 Executor 路由逻辑
5. 实现进程沙箱（L1），可选开启

---

*文档状态：草稿 · 待评审*
