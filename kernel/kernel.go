package kernel

import (
	"context"
	"errors"

	"github.com/mossagi/moss/kernel/loop"
	"github.com/mossagi/moss/kernel/middleware"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/skill"
	"github.com/mossagi/moss/kernel/tool"
)

// Kernel 是 Agent Runtime 的顶层入口，组合所有子系统。
type Kernel struct {
	llm      port.LLM
	io       port.UserIO
	sandbox  sandbox.Sandbox
	tools    tool.Registry
	sessions session.Manager
	chain    *middleware.Chain
	loopCfg  loop.LoopConfig
	skills   *skill.Manager
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
func (k *Kernel) Boot(_ context.Context) error {
	if k.llm == nil {
		return errors.New("kernel: LLM port is required")
	}
	return nil
}

// NewSession 创建新 Session。
func (k *Kernel) NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error) {
	return k.sessions.Create(ctx, cfg)
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

// OnEvent 注册事件监听（便利 API，内部实现为 EventEmitter middleware）。
func (k *Kernel) OnEvent(pattern string, handler builtins.EventHandler) {
	k.chain.Use(builtins.EventEmitter(pattern, handler))
}

// WithPolicy 设置权限策略（便利 API，内部实现为 PolicyCheck middleware）。
func (k *Kernel) WithPolicy(rules ...builtins.PolicyRule) {
	k.chain.Use(builtins.PolicyCheck(rules...))
}
