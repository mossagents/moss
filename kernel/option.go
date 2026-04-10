package kernel

import (
	"context"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/retry"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"os"
)

// Option 是 Kernel 的函数式配置选项。
type Option func(*Kernel)

// WithLLM 设置 LLM Port。
func WithLLM(llm model.LLM) Option {
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
func WithWorkspace(ws workspace.Workspace) Option {
	return func(k *Kernel) { k.workspace = ws }
}

// WithExecutor 设置 Executor Port（命令执行抽象）。
// 当同时设置了 Sandbox 时，内置工具优先使用 Executor。
func WithExecutor(exec workspace.Executor) Option {
	return func(k *Kernel) { k.executor = exec }
}

// WithTaskRuntime 设置 TaskRuntime Port。
func WithTaskRuntime(tasks taskrt.TaskRuntime) Option {
	return func(k *Kernel) { k.tasks = tasks }
}

// WithMailbox 设置 Mailbox Port。
func WithMailbox(mailbox taskrt.Mailbox) Option {
	return func(k *Kernel) { k.mailbox = mailbox }
}

// WithWorkspaceIsolation 设置 WorkspaceIsolation Port。
func WithWorkspaceIsolation(isolation workspace.WorkspaceIsolation) Option {
	return func(k *Kernel) { k.isolation = isolation }
}

// WithRepoStateCapture 设置 RepoStateCapture Port。
func WithRepoStateCapture(capture workspace.RepoStateCapture) Option {
	return func(k *Kernel) { k.repoState = capture }
}

// WithPatchApply 设置 PatchApply Port。
func WithPatchApply(apply workspace.PatchApply) Option {
	return func(k *Kernel) { k.patches = apply }
}

// WithPatchRevert 设置 PatchRevert Port。
func WithPatchRevert(revert workspace.PatchRevert) Option {
	return func(k *Kernel) { k.reverts = revert }
}

// WithWorktreeSnapshots 设置 WorktreeSnapshotStore Port。
func WithWorktreeSnapshots(store workspace.WorktreeSnapshotStore) Option {
	return func(k *Kernel) { k.snapshots = store }
}

// WithCheckpoints 设置 CheckpointStore Port。
func WithCheckpoints(store checkpoint.CheckpointStore) Option {
	return func(k *Kernel) { k.checkpoints = store }
}

// WithSessionStore 设置 SessionStore Port。
func WithSessionStore(store session.SessionStore) Option {
	return func(k *Kernel) { k.store = store }
}

// WithUserIO 设置 UserIO Port。
func WithUserIO(io io.UserIO) Option {
	return func(k *Kernel) { k.io = io }
}

// Use 追加一个 BeforeLLM hook（向后兼容便利方法）。
// Deprecated: 新代码应优先使用 OnBeforeLLM / OnBeforeToolCall / Install 等类型安全方法。
func Use(hook hooks.Hook[hooks.LLMEvent]) Option {
	return func(k *Kernel) { k.chain.BeforeLLM.On(hook) }
}

// OnBeforeLLM 注册 BeforeLLM hook。
func OnBeforeLLM(hook hooks.Hook[hooks.LLMEvent]) Option {
	return func(k *Kernel) { k.chain.BeforeLLM.On(hook) }
}

// OnAfterLLM 注册 AfterLLM hook。
func OnAfterLLM(hook hooks.Hook[hooks.LLMEvent]) Option {
	return func(k *Kernel) { k.chain.AfterLLM.On(hook) }
}

// OnBeforeToolCall 注册 BeforeToolCall hook。
func OnBeforeToolCall(hook hooks.Hook[hooks.ToolEvent]) Option {
	return func(k *Kernel) { k.chain.BeforeToolCall.On(hook) }
}

// OnAfterToolCall 注册 AfterToolCall hook。
func OnAfterToolCall(hook hooks.Hook[hooks.ToolEvent]) Option {
	return func(k *Kernel) { k.chain.AfterToolCall.On(hook) }
}

// OnSessionStart 注册 OnSessionStart hook。
func OnSessionStart(hook hooks.Hook[hooks.SessionEvent]) Option {
	return func(k *Kernel) { k.chain.OnSessionStart.On(hook) }
}

// OnSessionEnd 注册 OnSessionEnd hook。
func OnSessionEnd(hook hooks.Hook[hooks.SessionEvent]) Option {
	return func(k *Kernel) { k.chain.OnSessionEnd.On(hook) }
}

// OnError 注册 OnError hook。
func OnError(hook hooks.Hook[hooks.ErrorEvent]) Option {
	return func(k *Kernel) { k.chain.OnError.On(hook) }
}

// Install 安装跨 pipeline 的 hook 安装器（如 Logger、EventEmitter）。
func Install(installer func(*hooks.Registry)) Option {
	return func(k *Kernel) { installer(k.chain) }
}

// WithToolRegistry 替换默认的 Tool Registry。
func WithToolRegistry(r tool.Registry) Option {
	return func(k *Kernel) { k.tools = r }
}

// WithSessionManager 替换默认的 Session Manager。
func WithSessionManager(m session.Manager) Option {
	return func(k *Kernel) {
		if m == nil {
			return
		}
		k.sessions = session.WithCancelHook(m, func(id string) {
			k.runs.cancelSessionRuns(id)
		})
	}
}

// WithLoopConfig 配置 Agent Loop 参数。
func WithLoopConfig(cfg loop.LoopConfig) Option {
	return func(k *Kernel) { k.loopCfg = cfg }
}

// WithObserver 设置运行时事件观察者。
// Observer 用于收集可观测性指标（LLM 调用耗时、工具调用结果等），
// 不设置则使用 NoOpObserver（零开销）。
func WithObserver(o observe.Observer) Option {
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

// sandboxWorkspaceAdapter 将任意 Sandbox 适配为 workspace.Workspace。
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

func (a *sandboxWorkspaceAdapter) Stat(_ context.Context, path string) (workspace.FileInfo, error) {
	resolved, err := a.sb.ResolvePath(path)
	if err != nil {
		return workspace.FileInfo{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return workspace.FileInfo{}, err
	}
	return workspace.FileInfo{
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

// sandboxExecutorAdapter 将任意 Sandbox 适配为 workspace.Executor。
type sandboxExecutorAdapter struct {
	sb sandbox.Sandbox
}

func (a *sandboxExecutorAdapter) Execute(ctx context.Context, req workspace.ExecRequest) (workspace.ExecOutput, error) {
	return a.sb.Execute(ctx, req)
}
