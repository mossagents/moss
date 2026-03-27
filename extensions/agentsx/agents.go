package agentsx

import (
	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
)

// Deprecated: use runtime.WithAgentRegistry.
func WithRegistry(r *agent.Registry) kernel.Option { return runtime.WithAgentRegistry(r) }

// Deprecated: use runtime.WithTaskRuntime.
func WithTaskRuntime(rt port.TaskRuntime) kernel.Option { return runtime.WithTaskRuntime(rt) }

// Deprecated: use runtime.WithMailbox.
func WithMailbox(mb port.Mailbox) kernel.Option { return runtime.WithMailbox(mb) }

// Deprecated: use runtime.WithWorkspaceIsolation.
func WithWorkspaceIsolation(isolation port.WorkspaceIsolation) kernel.Option {
	return runtime.WithWorkspaceIsolation(isolation)
}

// Deprecated: use runtime.AgentRegistry.
func Registry(k *kernel.Kernel) *agent.Registry { return runtime.AgentRegistry(k) }

// Deprecated: use runtime.AgentTaskTracker.
func TaskTracker(k *kernel.Kernel) *agent.TaskTracker { return runtime.AgentTaskTracker(k) }
