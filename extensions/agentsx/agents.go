package agentsx

import (
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/agent"
)

// WithRegistry 设置 Agent 注册表。
func WithRegistry(r *agent.Registry) kernel.Option {
	return func(k *kernel.Kernel) {
		kernel.Extensions(k).SetAgentRegistry(r)
	}
}

// Registry 返回当前 Kernel 绑定的 Agent 注册表。
func Registry(k *kernel.Kernel) *agent.Registry {
	return kernel.Extensions(k).AgentRegistry()
}

// TaskTracker 返回当前 Agent 工具链的异步任务跟踪器。
func TaskTracker(k *kernel.Kernel) *agent.TaskTracker {
	return kernel.Extensions(k).TaskTracker()
}
