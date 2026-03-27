package planningx

import (
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
)

const todosStateKey = "planning.todos"

// TodoItem 是 write_todos 的单条任务项。
type TodoItem = runtime.PlanningTodoItem

// WithSessionManager 设置 planning 工具使用的 SessionManager。
// Deprecated: use runtime.WithPlanningSessionManager.
func WithSessionManager(m session.Manager) kernel.Option {
	return runtime.WithPlanningSessionManager(m)
}

// RegisterTools 注册 write_todos 工具。
// Deprecated: use runtime.RegisterPlanningTools.
func RegisterTools(reg tool.Registry, manager session.Manager) error {
	return runtime.RegisterPlanningTools(reg, manager)
}
