package tui

// agentSessionOps groups session-lifecycle operations injected into chatModel.
// A nil pointer means the capability is unavailable (no agent connected).
type agentSessionOps struct {
	info    func() string
	list    func(limit int) (string, error)
	restore func(sessionID string) (string, error)
	newSess func() (string, error)
	offload func(keepRecent int, note string) (string, error)
}

// agentCheckpointOps groups checkpoint management operations injected into chatModel.
type agentCheckpointOps struct {
	list   func(limit int) (string, error)
	show   func(checkpointID string) (string, error)
	create func(note string) (string, error)
	fork   func(sourceKind, sourceID string, restoreWorktree bool) (string, error)
	replay func(checkpointID, mode string, restoreWorktree bool) (string, error)
}

// agentTaskOps groups background-task management operations injected into chatModel.
type agentTaskOps struct {
	list   func(status string, limit int) (string, error)
	query  func(taskID string) (string, error)
	cancel func(taskID, reason string) (string, error)
}

// agentInspectOps groups debug and permission inspection operations injected into chatModel.
type agentInspectOps struct {
	permSummary func() string
	setPerm     func(toolName, mode string) (string, error)
	refreshSP   func() error
	debugPrompt func() string
}
