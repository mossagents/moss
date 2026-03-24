package kernel

import (
	"github.com/mossagi/moss/kernel/loop"
	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
)

// Option 是 Kernel 的函数式配置选项。
type Option func(*Kernel)

// WithLLM 设置 LLM Port。
func WithLLM(llm port.LLM) Option {
	return func(k *Kernel) { k.llm = llm }
}

// WithSandbox 设置 Sandbox。
func WithSandbox(sb sandbox.Sandbox) Option {
	return func(k *Kernel) { k.sandbox = sb }
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

// WithSkillManager 替换默认的 Skill Manager。
func WithSkillManager(m *skill.Manager) Option {
	return func(k *Kernel) { k.skills = m }
}

// WithSessionStore 设置 Session 持久化存储。
func WithSessionStore(s session.SessionStore) Option {
	return func(k *Kernel) { k.store = s }
}

// WithScheduler 设置定时任务调度器。
func WithScheduler(s *scheduler.Scheduler) Option {
	return func(k *Kernel) { k.sched = s }
}

// WithEmbedder 设置文本嵌入模型。
func WithEmbedder(e port.Embedder) Option {
	return func(k *Kernel) { k.embedder = e }
}
