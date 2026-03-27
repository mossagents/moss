package kernel

import (
	"context"
	"strings"
	"sync"
	"time"

	kerrors "github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/middleware"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/sandbox"
)

// Kernel 是 Agent Runtime 的顶层入口，组合所有子系统。
type Kernel struct {
	llm       port.LLM
	io        port.UserIO
	sandbox   sandbox.Sandbox
	workspace port.Workspace
	executor  port.Executor
	tools     tool.Registry
	sessions  session.Manager
	chain     *middleware.Chain
	loopCfg   loop.LoopConfig
	observer  port.Observer
	ext       *extensionState

	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	runs         *runSupervisor
}

// New 使用函数式选项创建 Kernel。
func New(opts ...Option) *Kernel {
	k := &Kernel{
		tools:      tool.NewRegistry(),
		sessions:   session.NewManager(),
		chain:      middleware.NewChain(),
		ext:        newExtensionState(),
		shutdownCh: make(chan struct{}),
		runs:       newRunSupervisor(),
	}
	k.sessions = session.WithCancelHook(k.sessions, func(id string) {
		k.runs.cancelSessionRuns(id)
	})
	for _, opt := range opts {
		opt(k)
	}
	return k
}

// Boot 验证 Kernel 配置完整性。
// 检查必要组件是否已设置，并给出具体的修复建议。
// 同时初始化已接入的扩展桥接逻辑（如果已配置）。
func (k *Kernel) Boot(ctx context.Context) error {
	var errs []string

	if k.llm == nil {
		errs = append(errs, "LLM port is required (use kernel.WithLLM())")
	}
	if k.io == nil {
		errs = append(errs, "UserIO port is not set (use kernel.WithUserIO(), or port.NoOpIO{} / port.NewPrintfIO())")
	}

	if len(errs) > 0 {
		return kerrors.New(kerrors.ErrValidation, "kernel boot failed:\n  - "+strings.Join(errs, "\n  - "))
	}
	return k.bootExtensions(ctx)
}

// NewSession 创建新 Session。
// 自动注入 system prompt：bootstrap 上下文 + skill 补充。
func (k *Kernel) NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	sess, err := k.sessions.Create(ctx, cfg)
	if err != nil {
		return nil, err
	}

	sysPrompt := k.extendSystemPrompt(cfg.SystemPrompt)
	if sysPrompt != "" {
		sess.Messages = append([]port.Message{{
			Role:    port.RoleSystem,
			Content: sysPrompt,
		}}, sess.Messages...)
	}

	return sess, nil
}

// Run 在指定 Session 上运行 Agent Loop。
func (k *Kernel) Run(ctx context.Context, sess *session.Session) (*loop.SessionResult, error) {
	return k.runSession(ctx, sess, runKindForeground, k.tools, k.io)
}

// RunWithUserIO 在指定 Session 上运行 Agent Loop，并临时覆盖本次运行的 UserIO。
func (k *Kernel) RunWithUserIO(ctx context.Context, sess *session.Session, io port.UserIO) (*loop.SessionResult, error) {
	return k.runSession(ctx, sess, runKindWithUserIO, k.tools, io)
}

// RunWithTools 使用指定的工具注册表运行 Agent Loop。
// 用于 Agent 委派场景，子 Agent 使用隔离的工具集。
func (k *Kernel) RunWithTools(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error) {
	return k.runSession(ctx, sess, runKindDelegated, tools, &port.NoOpIO{})
}

func (k *Kernel) runSession(ctx context.Context, sess *session.Session, kind runKind, tools tool.Registry, io port.UserIO) (*loop.SessionResult, error) {
	if err := k.checkShutdown(); err != nil {
		return nil, err
	}
	runCtx, runID, cancel, err := k.beginRunContext(ctx, sess.ID, sess.Config.Timeout, kind)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer k.runs.end(runID)

	l := &loop.AgentLoop{
		LLM:      k.llm,
		Tools:    tools,
		Chain:    k.chain,
		IO:       io,
		Config:   k.loopCfg,
		Observer: k.observer,
	}
	return l.Run(runCtx, sess)
}

func (k *Kernel) beginRunContext(parent context.Context, sessionID string, timeout time.Duration, kind runKind) (context.Context, string, context.CancelFunc, error) {
	runCtx, runID, err := k.runs.begin(parent, sessionID, kind)
	if err != nil {
		return nil, "", nil, err
	}
	if timeout > 0 {
		timeoutCtx, cancel := context.WithTimeout(runCtx, timeout)
		return timeoutCtx, runID, cancel, nil
	}
	return runCtx, runID, func() {}, nil
}

// Shutdown 优雅关停 Kernel。
// 1. 标记拒绝新请求
// 2. 等待进行中的 Session 完成（或 ctx 超时后取消）
// 3. 关闭扩展侧资源
func (k *Kernel) Shutdown(ctx context.Context) error {
	k.shutdownOnce.Do(func() { close(k.shutdownCh) })
	k.runs.beginShutdown()

	// 等待活跃运行结束
	k.runs.wait(ctx)

	return k.shutdownExtensions(ctx)
}

func (k *Kernel) checkShutdown() error {
	select {
	case <-k.shutdownCh:
		return kerrors.New(kerrors.ErrShutdown, "kernel is shutting down")
	default:
		return nil
	}
}

// ToolRegistry 返回工具注册表。
func (k *Kernel) ToolRegistry() tool.Registry {
	return k.tools
}

// SessionManager 返回 Session 管理器。
func (k *Kernel) SessionManager() session.Manager {
	return k.sessions
}

// Middleware 返回中间件链。
func (k *Kernel) Middleware() *middleware.Chain {
	return k.chain
}

// UserIO 返回默认交互端口（可能为 nil）。
func (k *Kernel) UserIO() port.UserIO {
	return k.io
}

// LLM 返回默认模型端口（可能为 nil）。
func (k *Kernel) LLM() port.LLM {
	return k.llm
}

// Sandbox 返回沙箱抽象（可能为 nil）。
func (k *Kernel) Sandbox() sandbox.Sandbox {
	return k.sandbox
}

// Workspace 返回工作区抽象（可能为 nil）。
func (k *Kernel) Workspace() port.Workspace {
	return k.workspace
}

// Executor 返回命令执行器（可能为 nil）。
func (k *Kernel) Executor() port.Executor {
	return k.executor
}

// OnEvent 注册事件监听（便利 API，内部实现为 EventEmitter middleware）。
func (k *Kernel) OnEvent(pattern string, handler builtins.EventHandler) {
	k.chain.Use(builtins.EventEmitter(pattern, handler))
}

// WithPolicy 设置权限策略（便利 API，内部实现为 PolicyCheck middleware）。
func (k *Kernel) WithPolicy(rules ...builtins.PolicyRule) {
	k.chain.Use(builtins.PolicyCheck(rules...))
}
