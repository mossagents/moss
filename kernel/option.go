package kernel

import (
	"context"
	"os"

	"github.com/mossagi/moss/kernel/loop"
	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/retry"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
)

// Option 是 Kernel 的函数式配置选项。
type Option func(*Kernel)

// WithLLM 设置 LLM Port。
func WithLLM(llm port.LLM) Option {
	return func(k *Kernel) { k.llm = llm }
}

// WithSandbox 设置 Sandbox。
// 同时自动适配为 Workspace 和 Executor（如果尚未单独设置）。
func WithSandbox(sb sandbox.Sandbox) Option {
	return func(k *Kernel) {
		k.sandbox = sb
		if k.workspace == nil {
			k.workspace = &sandboxWorkspaceAdapter{sb: sb}
		}
		if k.executor == nil {
			k.executor = &sandboxExecutorAdapter{sb: sb}
		}
	}
}

// WithWorkspace 设置 Workspace Port（文件系统抽象）。
// 当同时设置了 Sandbox 时，内置工具优先使用 Workspace。
func WithWorkspace(ws port.Workspace) Option {
	return func(k *Kernel) { k.workspace = ws }
}

// WithExecutor 设置 Executor Port（命令执行抽象）。
// 当同时设置了 Sandbox 时，内置工具优先使用 Executor。
func WithExecutor(exec port.Executor) Option {
	return func(k *Kernel) { k.executor = exec }
}

// WithUserIO 设置 UserIO Port。
func WithUserIO(io port.UserIO) Option {
	return func(k *Kernel) { k.io = io }
}

// Use 追加一个 Middleware。
func Use(mw middleware.Middleware) Option {
	return func(k *Kernel) { k.chain.Use(mw) }
}

// WithToolRegistry 替换默认的 Tool Registry。
func WithToolRegistry(r tool.Registry) Option {
	return func(k *Kernel) { k.tools = r }
}

// WithSessionManager 替换默认的 Session Manager。
func WithSessionManager(m session.Manager) Option {
	return func(k *Kernel) { k.sessions = m }
}

// WithLoopConfig 配置 Agent Loop 参数。
func WithLoopConfig(cfg loop.LoopConfig) Option {
	return func(k *Kernel) { k.loopCfg = cfg }
}

// WithObserver 设置运行时事件观察者。
// Observer 用于收集可观测性指标（LLM 调用耗时、工具调用结果等），
// 不设置则使用 NoOpObserver（零开销）。
func WithObserver(o port.Observer) Option {
	return func(k *Kernel) { k.observer = o }
}

// WithParallelToolCalls 启用并行工具调用。
// 当 LLM 在一次响应中返回多个 tool calls 时，它们会并发执行。
func WithParallelToolCalls() Option {
	return func(k *Kernel) { k.loopCfg.ParallelToolCall = true }
}

// WithLLMRetry 配置真实 LLM 调用的重试策略。
func WithLLMRetry(cfg loop.RetryConfig) Option {
	return func(k *Kernel) { k.loopCfg.LLMRetry = cfg }
}

// WithLLMBreaker 配置 LLM 调用熔断器。
// 当连续失败次数超过阈值时，自动拒绝后续请求，避免请求堆积。
func WithLLMBreaker(cfg retry.BreakerConfig) Option {
	return func(k *Kernel) { k.loopCfg.LLMBreaker = retry.NewBreaker(cfg) }
}

// ── Sandbox → Workspace/Executor 适配器 ─────────────

// sandboxWorkspaceAdapter 将任意 Sandbox 适配为 port.Workspace。
type sandboxWorkspaceAdapter struct {
	sb sandbox.Sandbox
}

func (a *sandboxWorkspaceAdapter) ReadFile(_ context.Context, path string) ([]byte, error) {
	return a.sb.ReadFile(path)
}

func (a *sandboxWorkspaceAdapter) WriteFile(_ context.Context, path string, content []byte) error {
	return a.sb.WriteFile(path, content)
}

func (a *sandboxWorkspaceAdapter) ListFiles(_ context.Context, pattern string) ([]string, error) {
	return a.sb.ListFiles(pattern)
}

func (a *sandboxWorkspaceAdapter) Stat(_ context.Context, path string) (port.FileInfo, error) {
	resolved, err := a.sb.ResolvePath(path)
	if err != nil {
		return port.FileInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return port.FileInfo{}, err
	}
	return port.FileInfo{
		Name:    info.Name(),
		Size:    info.Size(),
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
	}, nil
}

func (a *sandboxWorkspaceAdapter) DeleteFile(_ context.Context, path string) error {
	resolved, err := a.sb.ResolvePath(path)
	if err != nil {
		return err
	}
	return os.Remove(resolved)
}

// sandboxExecutorAdapter 将任意 Sandbox 适配为 port.Executor。
type sandboxExecutorAdapter struct {
	sb sandbox.Sandbox
}

func (a *sandboxExecutorAdapter) Execute(ctx context.Context, cmd string, args []string) (port.ExecOutput, error) {
	out, err := a.sb.Execute(ctx, cmd, args)
	return port.ExecOutput{
		Stdout:   out.Stdout,
		Stderr:   out.Stderr,
		ExitCode: out.ExitCode,
	}, err
}
