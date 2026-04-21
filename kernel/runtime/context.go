package runtime

// ─────────────────────────────────────────────
// ContextProjector 接口（§7.3）
// ─────────────────────────────────────────────

// ContextProjector 接受事件流，输出 MaterializedState（含 layer 提升逻辑）。
// 它是 ProjectionEngine 的外部接口层，允许注入自定义投影实现（如用于测试）。
type ContextProjector interface {
	// Project 从事件流全量重建 MaterializedState（等价于 Replay）。
	Project(sessionID string, events []RuntimeEvent) (*MaterializedState, error)

	// ApplyIncremental 将单个事件增量应用到已有 MaterializedState。
	ApplyIncremental(state *MaterializedState, ev RuntimeEvent) error
}

// ─────────────────────────────────────────────
// DefaultContextProjector 最小实现
// ─────────────────────────────────────────────

// DefaultContextProjector 委托 ProjectionEngine 完成实际投影（§7.3）。
// layer 提升规则在 ProjectionEngine.Apply 的 TurnCompleted 处理中执行。
type DefaultContextProjector struct {
	engine *ProjectionEngine
}

// NewDefaultContextProjector 创建 DefaultContextProjector。
func NewDefaultContextProjector() *DefaultContextProjector {
	return &DefaultContextProjector{engine: NewProjectionEngine()}
}

// Project 实现 ContextProjector 接口（全量 replay + 不变量检查）。
func (p *DefaultContextProjector) Project(sessionID string, events []RuntimeEvent) (*MaterializedState, error) {
	return p.engine.Replay(sessionID, events)
}

// ApplyIncremental 实现 ContextProjector 接口（增量 apply）。
func (p *DefaultContextProjector) ApplyIncremental(state *MaterializedState, ev RuntimeEvent) error {
	return p.engine.Apply(state, ev)
}
