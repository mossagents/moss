package kernel

import (
	"iter"
	"log/slog"

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
	logger      *slog.Logger
	subAgents   []Agent
}

// LLMAgentConfig configures an LLMAgent.
type LLMAgentConfig struct {
	Name        string
	Description string
	LLM         model.LLM
	Tools       tool.Registry
	Plugins     []Plugin
	Config      loop.LoopConfig
	Logger      *slog.Logger
	SubAgents   []Agent

	hookRegistry *hooks.Registry
}

// NewLLMAgent creates a new LLM-driven agent.
func NewLLMAgent(cfg LLMAgentConfig) *LLMAgent {
	if cfg.Tools == nil {
		cfg.Tools = tool.NewRegistry()
	}
	registry := cfg.hookRegistry
	if registry == nil {
		registry = hooks.NewRegistry()
	}
	for _, plugin := range cfg.Plugins {
		installPlugin(registry, plugin)
	}
	return &LLMAgent{
		name:        cfg.Name,
		description: cfg.Description,
		llm:         cfg.LLM,
		tools:       cfg.Tools,
		hooks:       registry,
		config:      cfg.Config,
		logger:      cfg.Logger,
		subAgents:   cfg.SubAgents,
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

// WithTools returns a shallow copy of the agent with a request-scoped tool registry.
// Nil keeps the existing tool registry unchanged.
func (a *LLMAgent) WithTools(tools tool.Registry) *LLMAgent {
	if a == nil {
		return nil
	}
	if tools == nil {
		return a
	}
	return a.withTools(tools)
}

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
			AgentName: a.name,
			LLM:       a.llm,
			Tools:     a.tools,
			Hooks:     a.hooks,
			IO:        ctx.IO(),
			Config:    a.config,
			Observer:  ctx.Observer(),
			Logger:    a.logger,
			RunID:     ctx.RunID(),
}

		result, err := l.RunYield(ctx, ctx.Session(), safeYield)
		ctx.setLifecycleResult(lifecycleResultFromLoop(result))
		if err != nil && !stopped {
			yield(nil, err)
		}
	}
}

func (a *LLMAgent) withTools(tools tool.Registry) *LLMAgent {
	cp := *a
	cp.tools = tools
	return &cp
}
