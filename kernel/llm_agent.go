package kernel

import (
	"iter"
	"time"

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

// Run executes the LLM agent loop and yields events.
// Currently wraps the existing AgentLoop; Phase 2 will add real-time event streaming.
func (a *LLMAgent) Run(ctx *InvocationContext) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		l := &loop.AgentLoop{
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

		result, err := l.Run(ctx, ctx.Session())
		if err != nil {
			yield(nil, err)
			return
		}

		// Yield a single completion event wrapping the SessionResult.
		// Phase 2 will refactor this to yield events in real-time during the loop.
		event := &session.Event{
			ID:     generateEventID(),
			Author: a.name,
			Content: &model.Message{
				Role:         model.RoleAssistant,
				ContentParts: []model.ContentPart{model.TextPart(result.Output)},
			},
			Usage:     result.TokensUsed,
			Timestamp: time.Now().UTC(),
		}
		yield(event, nil)
	}
}
