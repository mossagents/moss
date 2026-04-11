package kernel

import (
	"iter"

	"github.com/mossagents/moss/kernel/session"
)

// Agent is the core interface for all agent implementations.
// An Agent processes input and yields a stream of events via a Go 1.23+ iterator.
type Agent interface {
	// Name returns the unique name of this agent.
	Name() string

	// Run executes the agent and yields events via an iterator.
	// The iterator pattern (iter.Seq2) supports both streaming and batch consumption.
	Run(ctx *InvocationContext) iter.Seq2[*session.Event, error]
}

// AgentWithDescription provides a human-readable description for the agent.
type AgentWithDescription interface {
	Agent
	Description() string
}

// AgentWithSubAgents indicates the agent manages child agents.
type AgentWithSubAgents interface {
	Agent
	SubAgents() []Agent
}

// AgentFinder supports named agent lookup within an agent tree.
type AgentFinder interface {
	FindAgent(name string) Agent
}

// FindAgentInTree searches for a named agent in an agent tree (DFS).
func FindAgentInTree(root Agent, name string) Agent {
	if root.Name() == name {
		return root
	}
	if finder, ok := root.(AgentFinder); ok {
		if found := finder.FindAgent(name); found != nil {
			return found
		}
	}
	if parent, ok := root.(AgentWithSubAgents); ok {
		for _, sub := range parent.SubAgents() {
			if found := FindAgentInTree(sub, name); found != nil {
				return found
			}
		}
	}
	return nil
}
