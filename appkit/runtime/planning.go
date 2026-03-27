package runtime

import (
	"context"

	"github.com/mossagents/moss/extensions/planningx"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

type PlanningTodoItem = planningx.TodoItem

func WithPlanningSessionManager(m session.Manager) kernel.Option {
	return planningx.WithSessionManager(m)
}

func RegisterPlanningTools(reg tool.Registry, manager session.Manager) error {
	return planningx.RegisterTools(reg, manager)
}

func WithPlanningDefaults() kernel.Option {
	return planningx.WithSessionManager(nil)
}

func registerPlanningOnBoot(k *kernel.Kernel) error {
	_ = context.Background()
	return planningx.RegisterTools(k.ToolRegistry(), k.SessionManager())
}
