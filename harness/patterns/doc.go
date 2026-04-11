// Package patterns provides composable multi-agent orchestration primitives.
//
// It includes deterministic workflow agents (Sequential, Parallel, Loop)
// and dynamic orchestration agents (Supervisor) that can be freely nested
// to express complex multi-agent architectures.
//
// All agents implement kernel.Agent and can be used as sub-agents of each
// other, enabling hierarchical composition.
package patterns
