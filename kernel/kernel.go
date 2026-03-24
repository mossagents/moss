package kernel

import (
	"context"
	"errors"
	"strings"

	"github.com/mossagi/moss/kernel/agent"
	"github.com/mossagi/moss/kernel/bootstrap"
	"github.com/mossagi/moss/kernel/loop"
	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
)

// Kernel 是 Agent Runtime 的顶层入口，组合所有子系统。
type Kernel struct {
	llm       port.LLM
	io        port.UserIO
	sandbox   sandbox.Sandbox
	tools     tool.Registry
	sessions  session.Manager
	store     session.SessionStore
	sched     *scheduler.Scheduler
	embedder  port.Embedder
	chain     *middleware.Chain
	loopCfg   loop.LoopConfig
	skills    *skill.Manager
	channels  []port.Channel
	router    *session.Router
	bootstrap *bootstrap.Context
	agents    *agent.Registry
	tasks     *agent.TaskTracker
}

// New 使用函数式选项创建 Kernel。
func New(opts ...Option) *Kernel {
	k := &Kernel{
		tools:    tool.NewRegistry(),
		sessions: session.NewManager(),
		chain:    middleware.NewChain(),
		skills:   skill.NewManager(),
	}
	for _, opt := range opts {
		opt(k)
	}
	return k
}

// Boot 验证 Kernel 配置完整性。
// 检查必要组件是否已设置，并给出具体的修复建议。
// 同时初始化 Agent 委派系统（如果已配置 AgentRegistry）。
func (k *Kernel) Boot(_ context.Context) error {
	var errs []string

	if k.llm == nil {
		errs = append(errs, "LLM port is required (use kernel.WithLLM())")
	}
	if k.io == nil {
		errs = append(errs, "UserIO port is not set (use kernel.WithUserIO(), or port.NoOpIO{} / port.NewPrintfIO())")
	}

	if len(errs) > 0 {
		return errors.New("kernel boot failed:\n  - " + strings.Join(errs, "\n  - "))
	}

	// 初始化 Agent 委派工具
	if k.agents != nil && len(k.agents.List()) > 0 {
		if k.tasks == nil {
			k.tasks = agent.NewTaskTracker()
		}
		if err := agent.RegisterTools(k.tools, k.agents, k.tasks, k); err != nil {
			return err
		}
	}

	return nil
}

// NewSession 创建新 Session。
// 自动注入 system prompt：bootstrap 上下文 + skill 补充。
func (k *Kernel) NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	sess, err := k.sessions.Create(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// 构建 system prompt：cfg > bootstrap > skills
	sysPrompt := cfg.SystemPrompt

	if k.bootstrap != nil {
		if section := k.bootstrap.SystemPromptSection(); section != "" {
			if sysPrompt != "" {
				sysPrompt += "\n\n" + section
			} else {
				sysPrompt = section
			}
		}
	}

	if additions := k.skills.SystemPromptAdditions(); additions != "" {
		if sysPrompt != "" {
			sysPrompt += "\n\n" + additions
		} else {
			sysPrompt = additions
		}
	}

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
	l := &loop.AgentLoop{
		LLM:    k.llm,
		Tools:  k.tools,
		Chain:  k.chain,
		IO:     k.io,
		Config: k.loopCfg,
	}
	return l.Run(ctx, sess)
}

// RunWithTools 使用指定的工具注册表运行 Agent Loop。
// 用于 Agent 委派场景，子 Agent 使用隔离的工具集。
func (k *Kernel) RunWithTools(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error) {
	l := &loop.AgentLoop{
		LLM:    k.llm,
		Tools:  tools,
		Chain:  k.chain,
		IO:     &port.NoOpIO{},
		Config: k.loopCfg,
	}
	return l.Run(ctx, sess)
}

// Shutdown 关闭 Kernel，释放资源。
func (k *Kernel) Shutdown(ctx context.Context) error {
	return k.skills.ShutdownAll(ctx)
}

// ToolRegistry 返回工具注册表。
func (k *Kernel) ToolRegistry() tool.Registry {
	return k.tools
}

// SkillManager 返回 Skill 管理器。
func (k *Kernel) SkillManager() *skill.Manager {
	return k.skills
}

// SkillDeps 返回当前 Kernel 的 Skill 依赖。
func (k *Kernel) SkillDeps() skill.Deps {
	return skill.Deps{
		ToolRegistry: k.tools,
		Middleware:   k.chain,
		Sandbox:      k.sandbox,
		UserIO:       k.io,
	}
}

// SessionManager 返回 Session 管理器。
func (k *Kernel) SessionManager() session.Manager {
	return k.sessions
}

// SessionStore 返回 Session 持久化存储（可能为 nil）。
func (k *Kernel) SessionStore() session.SessionStore {
	return k.store
}

// Scheduler 返回调度器（可能为 nil）。
func (k *Kernel) Scheduler() *scheduler.Scheduler {
	return k.sched
}

// Embedder 返回嵌入模型（可能为 nil）。
func (k *Kernel) Embedder() port.Embedder {
	return k.embedder
}

// OnEvent 注册事件监听（便利 API，内部实现为 EventEmitter middleware）。
func (k *Kernel) OnEvent(pattern string, handler builtins.EventHandler) {
	k.chain.Use(builtins.EventEmitter(pattern, handler))
}

// WithPolicy 设置权限策略（便利 API，内部实现为 PolicyCheck middleware）。
func (k *Kernel) WithPolicy(rules ...builtins.PolicyRule) {
	k.chain.Use(builtins.PolicyCheck(rules...))
}

// Channels 返回已注册的消息通道列表。
func (k *Kernel) Channels() []port.Channel {
	return k.channels
}

// Router 返回会话路由器（可能为 nil）。
func (k *Kernel) Router() *session.Router {
	return k.router
}

// Bootstrap 返回引导上下文（可能为 nil）。
func (k *Kernel) Bootstrap() *bootstrap.Context {
	return k.bootstrap
}

// AgentRegistry 返回 Agent 注册表（可能为 nil）。
func (k *Kernel) AgentRegistry() *agent.Registry {
	return k.agents
}

// TaskTracker 返回异步任务跟踪器（可能为 nil）。
func (k *Kernel) TaskTracker() *agent.TaskTracker {
	return k.tasks
}
