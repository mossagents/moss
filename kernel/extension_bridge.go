package kernel

import (
	"context"
	"sort"

	"github.com/mossagi/moss/kernel/agent"
	"github.com/mossagi/moss/kernel/bootstrap"
	kerrors "github.com/mossagi/moss/kernel/errors"
	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
)

type extensionState struct {
	skills    *skill.Manager
	bootstrap *bootstrap.Context
	sched     *scheduler.Scheduler
	store     session.SessionStore
	agents    *agent.Registry
	tasks     *agent.TaskTracker

	bootHooks     []orderedBootHook
	shutdownHooks []orderedShutdownHook
	promptHooks   []orderedPromptHook

	skillHooksReady     bool
	bootstrapHooksReady bool
	schedulerHooksReady bool
	storeHooksReady     bool
	agentHooksReady     bool
}

type orderedBootHook struct {
	order int
	run   func(context.Context, *Kernel) error
}

type orderedShutdownHook struct {
	order int
	run   func(context.Context, *Kernel) error
}

type orderedPromptHook struct {
	order int
	run   func(*Kernel) string
}

func newExtensionState() *extensionState {
	return &extensionState{
		skills: skill.NewManager(),
	}
}

// ExtensionBridge 提供扩展层对非 core 运行时部件的桥接配置入口。
// 这些能力不再作为 kernel root 的一等构造选项暴露。
type ExtensionBridge struct {
	k *Kernel
}

// Extensions 返回扩展桥接配置入口。
func Extensions(k *Kernel) *ExtensionBridge {
	return &ExtensionBridge{k: k}
}

func (k *Kernel) extensionState() *extensionState {
	if k.ext == nil {
		k.ext = newExtensionState()
	}
	return k.ext
}

func (k *Kernel) bootExtensions(ctx context.Context) error {
	hooks := append([]orderedBootHook(nil), k.extensionState().bootHooks...)
	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	for _, hook := range hooks {
		if hook.run == nil {
			continue
		}
		if err := hook.run(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kernel) shutdownExtensions(ctx context.Context) error {
	hooks := append([]orderedShutdownHook(nil), k.extensionState().shutdownHooks...)
	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	for _, hook := range hooks {
		if hook.run == nil {
			continue
		}
		if err := hook.run(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kernel) extendSystemPrompt(base string) string {
	sysPrompt := base
	hooks := append([]orderedPromptHook(nil), k.extensionState().promptHooks...)
	sort.SliceStable(hooks, func(i, j int) bool { return hooks[i].order < hooks[j].order })
	for _, hook := range hooks {
		if hook.run == nil {
			continue
		}
		if section := hook.run(k); section != "" {
			if sysPrompt != "" {
				sysPrompt += "\n\n" + section
			} else {
				sysPrompt = section
			}
		}
	}
	return sysPrompt
}

// OnBoot 注册一个按顺序执行的扩展启动 hook。
func (b *ExtensionBridge) OnBoot(order int, hook func(context.Context, *Kernel) error) {
	b.k.extensionState().bootHooks = append(b.k.extensionState().bootHooks, orderedBootHook{
		order: order,
		run:   hook,
	})
}

// OnShutdown 注册一个按顺序执行的扩展关停 hook。
func (b *ExtensionBridge) OnShutdown(order int, hook func(context.Context, *Kernel) error) {
	b.k.extensionState().shutdownHooks = append(b.k.extensionState().shutdownHooks, orderedShutdownHook{
		order: order,
		run:   hook,
	})
}

// OnSystemPrompt 注册一个系统提示词增强 hook。
func (b *ExtensionBridge) OnSystemPrompt(order int, hook func(*Kernel) string) {
	b.k.extensionState().promptHooks = append(b.k.extensionState().promptHooks, orderedPromptHook{
		order: order,
		run:   hook,
	})
}

// SkillManager 返回 Skill 管理器。
func (b *ExtensionBridge) SkillManager() *skill.Manager {
	b.ensureSkillHooks()
	ext := b.k.extensionState()
	if ext.skills == nil {
		ext.skills = skill.NewManager()
	}
	return ext.skills
}

// SkillDeps 返回当前 Kernel 的 Skill 依赖。
func (b *ExtensionBridge) SkillDeps() skill.Deps {
	return skill.Deps{
		ToolRegistry: b.k.tools,
		Middleware:   b.k.chain,
		Sandbox:      b.k.sandbox,
		UserIO:       b.k.io,
		Workspace:    b.k.workspace,
		Executor:     b.k.executor,
	}
}

// AgentRegistry 返回 Agent 注册表。
// 若此前未显式配置，则按需创建一个默认注册表，供扩展层装配使用。
func (b *ExtensionBridge) AgentRegistry() *agent.Registry {
	b.ensureAgentHooks()
	ext := b.k.extensionState()
	if ext.agents == nil {
		ext.agents = agent.NewRegistry()
	}
	return ext.agents
}

// TaskTracker 返回异步任务跟踪器（可能为 nil）。
func (b *ExtensionBridge) TaskTracker() *agent.TaskTracker {
	return b.k.extensionState().tasks
}

// SetSkillManager 替换当前 Skill Manager。
func (b *ExtensionBridge) SetSkillManager(m *skill.Manager) {
	b.ensureSkillHooks()
	b.k.extensionState().skills = m
}

// SetSessionStore 设置 Session 持久化存储。
func (b *ExtensionBridge) SetSessionStore(s session.SessionStore) {
	b.ensureStoreHooks()
	b.k.extensionState().store = s
}

// SetScheduler 设置定时任务调度器。
func (b *ExtensionBridge) SetScheduler(s *scheduler.Scheduler) {
	b.ensureSchedulerHooks()
	b.k.extensionState().sched = s
}

// SetBootstrap 设置引导上下文。
func (b *ExtensionBridge) SetBootstrap(ctx *bootstrap.Context) {
	b.ensureBootstrapHooks()
	b.k.extensionState().bootstrap = ctx
}

// SetAgentRegistry 设置 Agent 注册表。
func (b *ExtensionBridge) SetAgentRegistry(r *agent.Registry) {
	b.ensureAgentHooks()
	b.k.extensionState().agents = r
}

func (b *ExtensionBridge) ensureSkillHooks() {
	ext := b.k.extensionState()
	if ext.skillHooksReady {
		return
	}
	ext.skillHooksReady = true
	b.OnShutdown(300, func(ctx context.Context, _ *Kernel) error {
		if ext.skills == nil {
			return nil
		}
		return ext.skills.ShutdownAll(ctx)
	})
	b.OnSystemPrompt(200, func(_ *Kernel) string {
		if ext.skills == nil {
			return ""
		}
		return ext.skills.SystemPromptAdditions()
	})
}

func (b *ExtensionBridge) ensureBootstrapHooks() {
	ext := b.k.extensionState()
	if ext.bootstrapHooksReady {
		return
	}
	ext.bootstrapHooksReady = true
	b.OnSystemPrompt(100, func(_ *Kernel) string {
		if ext.bootstrap == nil {
			return ""
		}
		return ext.bootstrap.SystemPromptSection()
	})
}

func (b *ExtensionBridge) ensureSchedulerHooks() {
	ext := b.k.extensionState()
	if ext.schedulerHooksReady {
		return
	}
	ext.schedulerHooksReady = true
	b.OnShutdown(200, func(_ context.Context, _ *Kernel) error {
		if ext.sched != nil {
			ext.sched.Stop()
		}
		return nil
	})
}

func (b *ExtensionBridge) ensureStoreHooks() {
	ext := b.k.extensionState()
	if ext.storeHooksReady {
		return
	}
	ext.storeHooksReady = true
	b.OnShutdown(100, func(ctx context.Context, _ *Kernel) error {
		if ext.store == nil {
			return nil
		}
		for _, sess := range b.k.sessions.List() {
			if sess.Status == session.StatusRunning {
				sess.Status = session.StatusPaused
			}
			ext.store.Save(ctx, sess)
		}
		return nil
	})
}

func (b *ExtensionBridge) ensureAgentHooks() {
	ext := b.k.extensionState()
	if ext.agentHooksReady {
		return
	}
	ext.agentHooksReady = true
	b.OnBoot(100, func(_ context.Context, k *Kernel) error {
		if ext.agents == nil || len(ext.agents.List()) == 0 {
			return nil
		}
		if ext.tasks == nil {
			ext.tasks = agent.NewTaskTracker()
		}
		if err := agent.RegisterTools(k.tools, ext.agents, ext.tasks, k); err != nil {
			return kerrors.Wrap(kerrors.ErrInternal, "register agent delegation tools", err)
		}
		return nil
	})
}
