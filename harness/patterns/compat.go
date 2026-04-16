// Package patterns is a compatibility shim that re-exports kernel/patterns.
//
// Deprecated: Import github.com/mossagents/moss/kernel/patterns directly.
package patterns

import "github.com/mossagents/moss/kernel/patterns"

// Type aliases for backward compatibility.
type (
	SequentialAgent        = patterns.SequentialAgent
	ParallelAgent          = patterns.ParallelAgent
	LoopAgent              = patterns.LoopAgent
	SupervisorAgent        = patterns.SupervisorAgent
	SupervisorStatus       = patterns.SupervisorStatus
	SupervisorDecision     = patterns.SupervisorDecision
	SupervisorWorkerBudget = patterns.SupervisorWorkerBudget
	SupervisorWorkerHealth = patterns.SupervisorWorkerHealth
	ResearchAgent          = patterns.ResearchAgent
	ResearchConfig         = patterns.ResearchConfig
	RoutingStrategy        = patterns.RoutingStrategy
	AggregateFunc          = patterns.AggregateFunc
	ExitFunc               = patterns.ExitFunc
)

// Function re-exports.
var (
	NewResearchAgent = patterns.NewResearchAgent
	RoundRobinRouter = patterns.RoundRobinRouter
	FirstMatchRouter = patterns.FirstMatchRouter
	ConcatAggregate  = patterns.ConcatAggregate
)
