package kernel

import (
	"github.com/mossagi/moss/kernel/agent"
	"github.com/mossagi/moss/kernel/bootstrap"
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

// WithChannel 追加一个消息通道。
func WithChannel(ch port.Channel) Option {
	return func(k *Kernel) { k.channels = append(k.channels, ch) }
}

// WithRouter 设置会话路由器。
func WithRouter(r *session.Router) Option {
	return func(k *Kernel) { k.router = r }
}

// WithBootstrap 设置引导上下文。
func WithBootstrap(b *bootstrap.Context) Option {
	return func(k *Kernel) { k.bootstrap = b }
}

// WithAgentRegistry 设置 Agent 注册表。
func WithAgentRegistry(r *agent.Registry) Option {
	return func(k *Kernel) { k.agents = r }
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
