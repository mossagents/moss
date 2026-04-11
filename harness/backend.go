package harness

import (
	"github.com/mossagents/moss/kernel/workspace"
)

// Backend provides unified file-system and command-execution capabilities
// for agent operations. It combines workspace.Workspace (file I/O) with
// workspace.Executor (command execution) into a single deployment unit.
//
// Different deployment scenarios (local, Docker, remote, cloud) provide
// their own Backend implementation.
type Backend interface {
	workspace.Workspace
	workspace.Executor
}

// LocalBackend composes an existing Workspace and Executor into a Backend.
type LocalBackend struct {
	workspace.Workspace
	workspace.Executor
}

var _ Backend = (*LocalBackend)(nil)
