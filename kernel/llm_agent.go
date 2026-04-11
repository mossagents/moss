package kernel

import (
	"iter"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

// LLMAgent is an agent driven by a large language model.
// It wraps the existing AgentLoop to drive the think→act→observe cycle.
type LLMAgent struct {
	name        string
	description string
	llm         model.LLM
	tools       tool.Registry
	hooks       *hooks.Registry
	config      loop.LoopConfig
	subAgents   []Agent

	lifecycleHook     session.LifecycleHook
	toolLifecycleHook session.ToolLifecycleHook
}

// LLMAgentConfig configures an LLMAgent.
type LLMAgentConfig struct {
	Name        string
	Description string
	LLM         model.LLM
	Tools       tool.Registry
	Hooks       *hooks.Registry
	Config      loop.LoopConfig
	SubAgents   []Agent

	LifecycleHook     session.LifecycleHook
	ToolLifecycleHook session.ToolLifecycleHook
}

// NewLLMAgent creates a new LLM-driven agent.
func NewLLMAgent(cfg LLMAgentConfig) *LLMAgent {
	if cfg.Tools == nil {
		cfg.Tools = tool.NewRegistry()
	}
	if cfg.Hooks == nil {
		cfg.Hooks = hooks.NewRegistry()
	}
	return &LLMAgent{
		name:              cfg.Name,
		description:       cfg.Description,
		llm:               cfg.LLM,
		tools:             cfg.Tools,
		hooks:             cfg.Hooks,
		config:            cfg.Config,
		subAgents:         cfg.SubAgents,
		lifecycleHook:     cfg.LifecycleHook,
		toolLifecycleHook: cfg.ToolLifecycleHook,
	}
}

// Name returns the agent's name.
func (a *LLMAgent) Name() string { return a.name }

// Description returns the agent's description.
func (a *LLMAgent) Description() string { return a.description }

// SubAgents returns the agent's sub-agents.
func (a *LLMAgent) SubAgents() []Agent { return a.subAgents }

// LLM returns the agent's LLM port.
func (a *LLMAgent) LLM() model.LLM { return a.llm }

// Tools returns the agent's tool registry.
func (a *LLMAgent) Tools() tool.Registry { return a.tools }

// Hooks returns the agent's hook registry.
func (a *LLMAgent) Hooks() *hooks.Registry { return a.hooks }

// Run executes the LLM agent loop and yields events in real-time.
// Events are streamed as they occur: LLM responses and tool results
// are yielded immediately rather than as a single post-hoc event.
func (a *LLMAgent) Run(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		stopped := false
		safeYield := func(event *session.Event, err error) bool {
			if stopped {
				return false
			}
			if !yield(event, err) {
				stopped = true
				return false
			}
			return true
		}

		l := &loop.AgentLoop{
			AgentName:         a.name,
			LLM:               a.llm,
			Tools:              a.tools,
			Hooks:              a.hooks,
			IO:                 ctx.IO(),
			Config:             a.config,
			Observer:           ctx.Observer(),
			RunID:              ctx.RunID(),
			LifecycleHook:      a.lifecycleHook,
			ToolLifecycleHook:  a.toolLifecycleHook,
		}

		_, err := l.RunYield(ctx, ctx.Session(), safeYield)
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}
