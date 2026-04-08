package workspace

import (
	"context"
	intr "github.com/mossagents/moss/kernel/io"
	"sync"
	"time"
)

// Workspace 是 Agent 工作区的抽象层。
// 将文件系统操作从 Sandbox 中解耦，使不同部署场景
// （本地、Docker、云存储、内存虚拟文件系统）可以提供各自的实现。
type Workspace interface {
	// ReadFile 从工作区读取文件。
	ReadFile(ctx context.Context, path string) ([]byte, error)
	// WriteFile 向工作区写入文件。
	WriteFile(ctx context.Context, path string, content []byte) error
	// ListFiles 按 glob 模式列出文件。
	ListFiles(ctx context.Context, pattern string) ([]string, error)
	// Stat 获取文件元信息。找不到时返回 ErrNotExist。
	Stat(ctx context.Context, path string) (FileInfo, error)
	// DeleteFile 删除指定文件。
	DeleteFile(ctx context.Context, path string) error
}

// ExecNetworkMode 表示命令执行的网络策略。
type ExecNetworkMode string

const (
	ExecNetworkDefault  ExecNetworkMode = "default"
	ExecNetworkDisabled ExecNetworkMode = "disabled"
	ExecNetworkEnabled  ExecNetworkMode = "enabled"
)

// ExecNetworkPolicy 描述命令执行时的网络限制期望。
type ExecNetworkPolicy struct {
	Mode            ExecNetworkMode `json:"mode,omitempty"`
	AllowHosts      []string        `json:"allow_hosts,omitempty"`
	PreferHardBlock bool            `json:"prefer_hard_block,omitempty"`
	AllowSoftLimit  bool            `json:"allow_soft_limit,omitempty"`
}

// IsolationLevel 指定命令执行所需的隔离级别。
type IsolationLevel string

const (
	// IsolationAuto 由 Executor 根据命令类型和策略自动决定隔离级别。
	IsolationAuto IsolationLevel = "auto"
	// IsolationHost 强制在宿主机直接执行（仅适合只读、低风险命令）。
	IsolationHost IsolationLevel = "host"
	// IsolationProcess 在独立子进程中执行，提供基本的进程级隔离。
	IsolationProcess IsolationLevel = "process"
	// IsolationSandbox 强制在完全隔离的 sandbox 中执行（容器级）。
	IsolationSandbox IsolationLevel = "sandbox"
)

// ExecRequest 是一次结构化命令执行请求。
type ExecRequest struct {
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	WorkingDir   string            `json:"working_dir,omitempty"`
	Timeout      time.Duration     `json:"timeout,omitempty"`
	AllowedPaths []string          `json:"allowed_paths,omitempty"`
	ClearEnv     bool              `json:"clear_env,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Network      ExecNetworkPolicy `json:"network,omitempty"`
	// IsolationLevel 指定执行隔离需求，默认 IsolationAuto。
	IsolationLevel IsolationLevel `json:"isolation_level,omitempty"`
}

// FileInfo 描述文件元信息。
type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	IsDir   bool      `json:"is_dir"`
	ModTime time.Time `json:"mod_time"`
}

// Executor 是命令执行的抽象层。
// 与 Workspace 正交：可以组合不同的 Workspace + Executor 实现。
type Executor interface {
	// Execute 在隔离环境中执行命令。
	Execute(ctx context.Context, req ExecRequest) (ExecOutput, error)
}

// ExecOutput 是命令执行的结果。
type ExecOutput struct {
	Stdout      string               `json:"stdout"`
	Stderr      string               `json:"stderr"`
	ExitCode    int                  `json:"exit_code"`
	Enforcement intr.EnforcementMode `json:"enforcement,omitempty"`
	Degraded    bool                 `json:"degraded,omitempty"`
	Details     string               `json:"details,omitempty"`
}

// NoOpExecutor 拒绝所有命令执行，用于纯对话场景。
type NoOpExecutor struct{}

func (NoOpExecutor) Execute(_ context.Context, req ExecRequest) (ExecOutput, error) {
	return ExecOutput{}, &executorDisabledError{cmd: req.Command}
}

type executorDisabledError struct {
	cmd string
}

func (e *executorDisabledError) Error() string {
	return "command execution is disabled: " + e.cmd
}

// ---- WorkspaceLock 并发保护 -----------------------------------------------

// WorkspaceLock 为并发 subagent 场景提供文件级互斥锁。
//
// 锁策略：
//   - 并发读：不加锁
//   - 并发写不同文件：不加锁（路径不重叠）
//   - 并发写同一文件：必须持锁（FIFO 队列）
//   - 快照操作：需要全局锁（path=""）
type WorkspaceLock interface {
	// Lock 获取路径锁（阻塞直到获取成功或 ctx 取消）。
	// 返回释放函数，调用方必须在使用完成后调用。
	Lock(ctx context.Context, path string, agentID string) (unlock func(), err error)
	// TryLock 非阻塞尝试获取锁。ok=false 表示锁已被持有。
	TryLock(ctx context.Context, path string, agentID string) (unlock func(), ok bool)
	// CurrentHolder 返回当前持锁的 agentID（如有）。
	CurrentHolder(path string) (agentID string, held bool)
}

// InProcessWorkspaceLock 是基于 sync.Map 的单进程 WorkspaceLock 实现。
// 适用于单节点部署；分布式场景请使用 distributed.DistributedLock 封装。
type InProcessWorkspaceLock struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

type lockEntry struct {
	holder string
	ch     chan struct{} // closed when released
}

// NewInProcessWorkspaceLock 创建单进程 WorkspaceLock。
func NewInProcessWorkspaceLock() *InProcessWorkspaceLock {
	return &InProcessWorkspaceLock{locks: make(map[string]*lockEntry)}
}

// Lock 阻塞等待直到获取路径锁。
func (l *InProcessWorkspaceLock) Lock(ctx context.Context, path string, agentID string) (func(), error) {
	for {
		if unlock, ok := l.TryLock(ctx, path, agentID); ok {
			return unlock, nil
		}
		// 等待锁释放或 ctx 取消
		l.mu.Lock()
		entry, exists := l.locks[path]
		l.mu.Unlock()
		if !exists {
			continue
		}
		select {
		case <-entry.ch:
			// 锁已释放，重试
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// TryLock 非阻塞尝试获取路径锁。
func (l *InProcessWorkspaceLock) TryLock(_ context.Context, path string, agentID string) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry, exists := l.locks[path]; exists {
		select {
		case <-entry.ch:
			// 已释放，可以重新获取
		default:
			return nil, false // 锁还在被持有
		}
	}

	ch := make(chan struct{})
	l.locks[path] = &lockEntry{holder: agentID, ch: ch}
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if entry, ok := l.locks[path]; ok && entry.holder == agentID {
			close(ch)
			delete(l.locks, path)
		}
	}, true
}

// CurrentHolder 返回当前持锁的 agentID（如有）。
func (l *InProcessWorkspaceLock) CurrentHolder(path string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, exists := l.locks[path]
	if !exists {
		return "", false
	}
	select {
	case <-entry.ch:
		return "", false // 已释放
	default:
		return entry.holder, true
	}
}

// NoOpWorkspaceLock 是无操作的 WorkspaceLock，始终成功获取锁。
// 用于单 agent 场景，避免不必要的锁开销。
type NoOpWorkspaceLock struct{}

func (NoOpWorkspaceLock) Lock(_ context.Context, _ string, _ string) (func(), error) {
	return func() {}, nil
}

func (NoOpWorkspaceLock) TryLock(_ context.Context, _ string, _ string) (func(), bool) {
	return func() {}, true
}

func (NoOpWorkspaceLock) CurrentHolder(_ string) (string, bool) {
	return "", false
}
