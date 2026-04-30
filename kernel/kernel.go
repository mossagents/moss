package kernel

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/errors"
	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

// Kernel 是 Agent Runtime 的顶层入口，组合所有子系统。
type Kernel struct {
	llm          model.LLM
	io           io.UserIO
	logger       *slog.Logger
	workspace    workspace.Workspace
	tasks        taskrt.TaskRuntime
	mailbox      taskrt.Mailbox
	isolation    workspace.WorkspaceIsolation
	repoState    workspace.RepoStateCapture
	patches      workspace.PatchApply
	reverts      workspace.PatchRevert
	snapshots    workspace.WorktreeSnapshotStore
	checkpoints  checkpoint.CheckpointStore
	store        session.SessionStore
	artifacts    artifact.Store
	tools        tool.Registry
	sessions     session.Manager
	chain        *hooks.Registry
	stages       *StageRegistry
	promptLayers *PromptLayerRegistry
	services     *ServiceRegistry
	loopCfg      loop.LoopConfig
	observer     observe.Observer

	// kernel-centric runtime：EventStore、RequestResolver、PromptCompiler
	eventStore      kruntime.EventStore
	runtimeResolver kruntime.RequestResolver
	promptCompiler  kruntime.PromptCompiler

	// blueprintPolicyApplier：harness 注册，在每次 RunAgentFromBlueprint 前应用 bp.EffectiveToolPolicy。
	blueprintPolicyApplier func(bp kruntime.SessionBlueprint)

	assemblyMu     sync.Mutex
	assemblyFrozen bool
	shutdownCh     chan struct{}
	shutdownOnce   sync.Once
	runs           *runSupervisor
}

// New 使用函数式选项创建 Kernel。
func New(opts ...Option) *Kernel {
	k := &Kernel{
		tools:        tool.NewRegistry(),
		sessions:     session.NewManager(),
		chain:        hooks.NewRegistry(),
		stages:       newStageRegistry(),
		promptLayers: newPromptLayerRegistry(),
		services:     newServiceRegistry(),
		shutdownCh:   make(chan struct{}),
		runs:         newRunSupervisor(),
	}
	k.sessions = session.WithCancelHook(k.sessions, func(id string) {
		k.runs.cancelSessionRuns(id)
	})
	for _, opt := range opts {
		opt(k)
	}
	return k
}

// Apply applies additional Options during the kernel install phase.
// Once booting, serving, or shutdown begins, further option application returns an error.
func (k *Kernel) Apply(opts ...Option) error {
	if len(opts) == 0 {
		return nil
	}
	k.assemblyMu.Lock()
	defer k.assemblyMu.Unlock()
	if k.assemblyFrozen {
		return fmt.Errorf("apply kernel options after kernel install phase closed")
	}
	for _, opt := range opts {
		opt(k)
	}
	return nil
}

// PatchLoopConfig 在没有活跃 run 时安全地增量更新 loop 配置。
// 与 Apply 不同，它允许在 Boot 之后调整运行期 loop 行为。
func (k *Kernel) PatchLoopConfig(patch func(*loop.LoopConfig)) error {
	if patch == nil {
		return nil
	}
	k.assemblyMu.Lock()
	defer k.assemblyMu.Unlock()
	if k.IsShuttingDown() {
		return errors.New(errors.ErrShutdown, "kernel is shutting down")
	}
	if k.runs.activeCount() > 0 {
		return fmt.Errorf("cannot patch loop config while runs are active")
	}
	patch(&k.loopCfg)
	return nil
}

// Boot 验证 Kernel 配置完整性。
// 检查必要组件是否已设置，并给出具体的修复建议。
// 同时初始化已接入的扩展桥接逻辑（如果已配置）。
func (k *Kernel) Boot(ctx context.Context) error {
	if k.IsShuttingDown() {
		return errors.New(errors.ErrShutdown, "kernel is shutting down")
	}
	if k.runs.hasStarted() {
		return errors.New(errors.ErrValidation, "kernel boot must complete before serving work starts")
	}
	if k.stages.BootStarted() {
		return errors.New(errors.ErrValidation, "kernel boot can only run once")
	}

	var errs []string

	if k.llm == nil {
		errs = append(errs, "LLM port is required (use kernel.WithLLM())")
	}
	if k.io == nil {
		errs = append(errs, "UserIO port is not set (use kernel.WithUserIO(), or io.NoOpIO{} / io.NewPrintfIO())")
	}

	if len(errs) > 0 {
		return errors.New(errors.ErrValidation, "kernel boot failed:\n  - "+strings.Join(errs, "\n  - "))
	}
	k.freezeAssembly()
	k.propagateObserver(k.observer)
	return k.Stages().runBoot(ctx, k)
}

// NewSession 创建新 Session。
// 自动注入 system prompt：bootstrap 上下文 + skill 补充。
func (k *Kernel) NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	sess, err := k.sessions.Create(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// 合并 cfg.SystemPrompt 静态内容与 PromptLayerRegistry 的动态层内容，组装 system prompt。
	sysPrompt := cfg.SystemPrompt
	for _, layer := range k.PromptLayers().Build(k) {
		if t := model.ContentPartsToPlainText(layer.ContentParts); t != "" {
			if sysPrompt != "" {
				sysPrompt += "\n\n" + t
			} else {
				sysPrompt = t
			}
		}
	}
	if sysPrompt != "" {
		existing := sess.CopyMessages()
		sess.ReplaceMessages(append([]model.Message{{
			Role:         model.RoleSystem,
			ContentParts: []model.ContentPart{model.TextPart(sysPrompt)},
		}}, existing...))
	}

	k.emitSessionLifecycle(ctx, session.LifecycleEvent{
		Stage:     session.LifecycleCreated,
		Session:   sess,
		Timestamp: time.Now().UTC(),
	})

	return sess, nil
}

func (k *Kernel) beginRunContext(parent context.Context, sessionID string, timeout time.Duration) (context.Context, string, context.CancelFunc, error) {
	k.freezeAssembly()
	runCtx, runID, err := k.runs.begin(parent, sessionID)
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
	k.freezeAssembly()
	k.shutdownOnce.Do(func() { close(k.shutdownCh) })
	k.runs.beginShutdown()

	// 等待活跃运行结束
	k.runs.wait(ctx)

	return k.Stages().runShutdown(ctx, k)
}

func (k *Kernel) checkShutdown() error {
	select {
	case <-k.shutdownCh:
		return errors.New(errors.ErrShutdown, "kernel is shutting down")
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

// Stages returns the kernel stage registry.
func (k *Kernel) Stages() *StageRegistry {
	return k.stages
}

// PromptLayers 返回 prompt layer 注册表。
// 各 harness 组件在 kernel 初始化阶段调用 PromptLayers().Add() 注册 layer builder；
// 调用 RunAgentFromBlueprint 前通过 PromptLayers().Build(k) 收集所有 layers。
func (k *Kernel) PromptLayers() *PromptLayerRegistry {
	return k.promptLayers
}

// Services returns the kernel substrate registry used by typed owner packages.
// Public composition should prefer features/options instead of treating this as
// an extension surface.
func (k *Kernel) Services() *ServiceRegistry {
	return k.services
}

// ArtifactStore returns the optional artifact store. Returns nil if not configured.
func (k *Kernel) ArtifactStore() artifact.Store {
	return k.artifacts
}

// UserIO 返回默认交互端口（可能为 nil）。
func (k *Kernel) UserIO() io.UserIO {
	return k.io
}

// LLM 返回默认模型端口（可能为 nil）。
func (k *Kernel) LLM() model.LLM {
	return k.llm
}

// Logger returns the kernel's configured logger, falling back to slog.Default().
func (k *Kernel) Logger() *slog.Logger {
	if k.logger != nil {
		return k.logger
	}
	return slog.Default()
}

// SetLLM 在 Boot 之前更新默认模型端口。
// 在 assembly 冻结后调用会 panic，请在 Boot 之前调用。
func (k *Kernel) SetLLM(llm model.LLM) {
	k.assemblyMu.Lock()
	defer k.assemblyMu.Unlock()
	if k.assemblyFrozen {
		panic(fmt.Errorf("SetLLM called after kernel assembly phase closed"))
	}
	k.llm = llm
}

// SetObserver 更新运行时事件观察者。
// 可在运行时调用以切换可观测性后端。
func (k *Kernel) SetObserver(observer observe.Observer) {
	k.assemblyMu.Lock()
	defer k.assemblyMu.Unlock()
	k.observer = observer
	k.propagateObserver(observer)
}

// Observer returns the configured runtime observer, falling back to NoOpObserver.
func (k *Kernel) Observer() observe.Observer {
	return k.observerOrNoOp()
}

func (k *Kernel) propagateObserver(observer observe.Observer) {
	if observer == nil {
		observer = observe.NoOpObserver{}
	}
	if aware, ok := k.snapshots.(interface {
		SetObserver(observe.ExecutionObserver)
	}); ok {
		aware.SetObserver(observer)
	}
	if aware, ok := k.checkpoints.(interface {
		SetObserver(observe.ExecutionObserver)
	}); ok {
		aware.SetObserver(observer)
	}
}

func (k *Kernel) observerOrNoOp() observe.Observer {
	if k.observer != nil {
		return k.observer
	}
	return observe.NoOpObserver{}
}

func (k *Kernel) freezeAssembly() {
	k.assemblyMu.Lock()
	defer k.assemblyMu.Unlock()
	if k.assemblyFrozen {
		return
	}
	k.assemblyFrozen = true
	k.stages.freeze()
	k.promptLayers.freeze()
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// runModeTriggerSource 将 RuntimeRequest.RunMode 映射为 session_created 的 trigger_source（§14.5）。
// interactive / 空值 → "interactive"；scheduled → "scheduled"；api → "api"；resume → "resume"；其他 → "api"。
func runModeTriggerSource(runMode string) string {
	switch runMode {
	case "scheduled":
		return "scheduled"
	case "resume":
		return "resume"
	case "interactive", "oneshot", "":
		return "interactive"
	default:
		return "api"
	}
}

func panicAsError(prefix string, value any) error {
	if err, ok := value.(error); ok {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %v", prefix, value)
}

// Workspace 返回工作区抽象（可能为 nil）。
func (k *Kernel) Workspace() workspace.Workspace {
	return k.workspace
}

// TaskRuntime 返回任务运行时端口（可能为 nil）。
func (k *Kernel) TaskRuntime() taskrt.TaskRuntime {
	return k.tasks
}

// Mailbox 返回代理邮箱端口（可能为 nil）。
func (k *Kernel) Mailbox() taskrt.Mailbox {
	return k.mailbox
}

// WorkspaceIsolation 返回工作区隔离端口（可能为 nil）。
func (k *Kernel) WorkspaceIsolation() workspace.WorkspaceIsolation {
	return k.isolation
}

// RepoStateCapture 返回仓库状态捕获端口（可能为 nil）。
func (k *Kernel) RepoStateCapture() workspace.RepoStateCapture {
	return k.repoState
}

// PatchApply 返回结构化补丁应用端口（可能为 nil）。
func (k *Kernel) PatchApply() workspace.PatchApply {
	return k.patches
}

// PatchRevert 返回结构化补丁回滚端口（可能为 nil）。
func (k *Kernel) PatchRevert() workspace.PatchRevert {
	return k.reverts
}

// WorktreeSnapshots 返回 worktree/ghost-state 快照端口（可能为 nil）。
func (k *Kernel) WorktreeSnapshots() workspace.WorktreeSnapshotStore {
	return k.snapshots
}

// Checkpoints 返回 checkpoint 存储端口（可能为 nil）。
func (k *Kernel) Checkpoints() checkpoint.CheckpointStore {
	return k.checkpoints
}

// SessionStore 返回会话持久化存储（可能为 nil）。
func (k *Kernel) SessionStore() session.SessionStore {
	return k.store
}

// ActiveRunCount 返回当前正在执行的 Run 数量。
func (k *Kernel) ActiveRunCount() int {
	return k.runs.activeCount()
}

// IsShuttingDown 返回 Kernel 是否已进入关停流程。
func (k *Kernel) IsShuttingDown() bool {
	select {
	case <-k.shutdownCh:
		return true
	default:
		return false
	}
}

// SetToolPolicyGate 设置不可绕过的工具权限门控函数。
// 门控在 OnToolLifecycle pipeline 之前执行，拦截器无法绕过。
// 可在 Kernel 构建后调用。
func (k *Kernel) SetToolPolicyGate(fn func(context.Context, *hooks.ToolEvent) error) {
	k.chain.SetToolPolicyGate(fn)
}

// InstallPlugin 注册一个 Plugin，将其包含的 hook 安装到对应的 pipeline。
// 可在 Kernel 构建后调用，用于运行时动态安装插件。
func (k *Kernel) InstallPlugin(p Plugin) error {
	return k.installPlugin(p)
}

func (k *Kernel) installPlugin(p Plugin) error {
	if k.runs.hasStarted() {
		return fmt.Errorf("install plugin %q after kernel started serving work", p.Name())
	}
	return installPlugin(k.chain, p)
}

// OnEvent 注册事件监听（便利 API，内部通过 hooks 安装 EventEmitter）。
func (k *Kernel) OnEvent(pattern string, handler builtins.EventHandler) error {
	return k.InstallPlugin(builtins.EventEmitterPlugin(pattern, handler))
}

// ── Agent API ───────────────────────────────────────────────────

// BuildLLMAgent creates an LLMAgent configured with the Kernel's resources.
// This is the bridge between the Kernel's resource injection model and the new Agent interface.
func (k *Kernel) BuildLLMAgent(name string) *LLMAgent {
	return NewLLMAgent(LLMAgentConfig{
		Name:         name,
		LLM:          k.llm,
		Tools:        k.tools,
		hookRegistry: k.chain,
		Config:       k.loopCfg,
		Logger:       k.Logger(),
	})
}

// RunAgent runs an Agent on the given request and yields events.
// This is the canonical execution API used by LLM agents and by RunAgentFromBlueprint.
func (k *Kernel) RunAgent(ctx context.Context, req RunAgentRequest) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		req, err := k.normalizeRunAgentRequest(req)
		if err != nil {
			yield(nil, err)
			return
		}
		if err := k.checkShutdown(); err != nil {
			yield(nil, err)
			return
		}
		runCtx, runID, cancel, err := k.beginRunContext(ctx, req.Session.ID, req.Session.Config.Timeout)
		if err != nil {
			yield(nil, err)
			return
		}
		defer cancel()
		defer k.runs.end(runID)

		invObs := k.observerOrNoOp()
		if req.Observer != nil {
			invObs = req.Observer
		}
		invCtx := NewInvocationContext(runCtx, InvocationContextParams{
			RunID:        runID,
			Branch:       req.Agent.Name(),
			Agent:        req.Agent,
			Session:      req.Session,
			UserContent:  req.UserContent,
			IO:           req.IO,
			Observer:     invObs,
			resultWriter: req.OnResult,
		})
		streamAgentEvents(req.Agent, invCtx, yield)
	}
}

// ── kernel-centric runtime（新路径，阶段 1）────────────────────────

// EventStore 返回已注入的 EventStore（可能为 nil）。
func (k *Kernel) EventStore() kruntime.EventStore {
	return k.eventStore
}

// RuntimeResolver 返回已注入的 RequestResolver（可能为 nil）。
// 若未通过 WithRuntimeResolver 配置，返回 nil。
func (k *Kernel) RuntimeResolver() kruntime.RequestResolver {
	return k.runtimeResolver
}

// SetBlueprintPolicyApplier 注册一个 blueprint policy applier hook。
// 该 hook 在每次 RunAgentFromBlueprint 执行前被调用，供 harness 将
// bp.EffectiveToolPolicy 应用到 kernel 的 policystate（§阶段4）。
//
// 这是"删除 approval mode runtime application"的实现桥接点：
// blueprint path 中的 policy 来源从 runtime approval mode 字符串改为
// 经 PolicyCompiler 编译后的 EffectiveToolPolicy。
func (k *Kernel) SetBlueprintPolicyApplier(fn func(bp kruntime.SessionBlueprint)) {
	if k == nil {
		return
	}
	k.blueprintPolicyApplier = fn
}

// StartRuntimeSession 通过 RuntimeRequest 解析 SessionBlueprint，并向 EventStore 追加 session_created。
//
// 步骤：1) RequestResolver.Resolve → SessionBlueprint；2) AppendEvents(session_created)；3) 返回 blueprint。
// 要求 WithEventStore 已配置；若未配置 WithRuntimeResolver，则使用 DefaultRequestResolver + DefaultPolicyCompiler。
// 典型后续流程：CollectRunAgentFromBlueprint / RunAgentFromBlueprint。
func (k *Kernel) StartRuntimeSession(ctx context.Context, req kruntime.RuntimeRequest) (kruntime.SessionBlueprint, error) {
	if k.eventStore == nil {
		return kruntime.SessionBlueprint{}, errors.New(errors.ErrValidation, "StartRuntimeSession requires an EventStore (use kernel.WithEventStore())")
	}
	resolver := k.runtimeResolver
	if resolver == nil {
		resolver = kruntime.NewDefaultRequestResolver(kruntime.NewDefaultPolicyCompiler())
	}

	bp, err := resolver.Resolve(req)
	if err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("resolve runtime request: %w", err)
	}

	sessionID := bp.Identity.SessionID
	ev := kruntime.RuntimeEvent{
		Type:             kruntime.EventTypeSessionCreated,
		SessionID:        sessionID,
		Seq:              0,
		BlueprintVersion: bp.Provenance.Hash,
		Payload: kruntime.SessionCreatedPayload{
			BlueprintPayload: &bp,
			TriggerSource:    runModeTriggerSource(req.RunMode),
		},
	}
	if err := k.eventStore.AppendEvents(ctx, sessionID, 0, "", []kruntime.RuntimeEvent{ev}); err != nil {
		return kruntime.SessionBlueprint{}, fmt.Errorf("append session_created event: %w", err)
	}
	return bp, nil
}
