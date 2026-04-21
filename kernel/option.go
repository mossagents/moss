package kernel

import (
	"log/slog"

	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/retry"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

// Option 是 Kernel 的函数式配置选项。
type Option func(*Kernel)

// WithLLM 设置 LLM Port。
func WithLLM(llm model.LLM) Option {
	return func(k *Kernel) { k.llm = llm }
}

// WithLogger sets the kernel's logger. If not set, slog.Default() is used.
func WithLogger(l *slog.Logger) Option {
	return func(k *Kernel) { k.logger = l }
}

// WithWorkspace 设置 Workspace Port。
func WithWorkspace(ws workspace.Workspace) Option {
	return func(k *Kernel) { k.workspace = ws }
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
//
// Deprecated: 阶段 4 将删除本选项。
// 新路径请改用 kernel.WithEventStore，
// 以 SQLite EventStore 为事实层持久化并支持 projection 读取。
func WithSessionStore(store session.SessionStore) Option {
	return func(k *Kernel) { k.store = store }
}

// WithArtifactStore 设置 Artifact Store Port（可选）。
func WithArtifactStore(store artifact.Store) Option {
	return func(k *Kernel) { k.artifacts = store }
}

// WithUserIO 设置 UserIO Port。
func WithUserIO(io io.UserIO) Option {
	return func(k *Kernel) { k.io = io }
}

// WithPlugin 注册一个 Plugin，将其包含的 hook 安装到对应的 pipeline。
// Plugin 是注册生命周期 hook 的推荐方式。
// 在构造阶段（New）中使用，无效 plugin 会 panic。
func WithPlugin(p Plugin) Option {
	return func(k *Kernel) {
		if err := k.installPlugin(p); err != nil {
			panic(err)
		}
	}
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

// ── kernel-centric runtime 选项（新路径，阶段 1）────────────────────

// WithEventStore 注入 EventStore，启用 kernel-centric runtime 新路径。
// 配置后可通过 kernel.StartRuntimeSession 使用事件溯源 session。
func WithEventStore(store kruntime.EventStore) Option {
	return func(k *Kernel) { k.eventStore = store }
}

// WithRuntimeResolver 注入自定义 RequestResolver。
// 若不配置，StartRuntimeSession 将使用 DefaultRequestResolver。
func WithRuntimeResolver(resolver kruntime.RequestResolver) Option {
	return func(k *Kernel) { k.runtimeResolver = resolver }
}
