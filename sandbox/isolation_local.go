package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mossagents/moss/kernel/port"
)

// LocalWorkspaceIsolation 基于本地目录实现任务隔离（POC 版本）。
type LocalWorkspaceIsolation struct {
	mu        sync.Mutex
	root      string
	leases    map[string]string
	workspace map[string]*LocalWorkspace
	executor  map[string]*LocalExecutor
}

func NewLocalWorkspaceIsolation(root string) (*LocalWorkspaceIsolation, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve isolation root: %w", err)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return nil, fmt.Errorf("create isolation root: %w", err)
	}
	return &LocalWorkspaceIsolation{
		root:      abs,
		leases:    make(map[string]string),
		workspace: make(map[string]*LocalWorkspace),
		executor:  make(map[string]*LocalExecutor),
	}, nil
}

func (i *LocalWorkspaceIsolation) Acquire(_ context.Context, taskID string) (*port.WorkspaceLease, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if wsID, ok := i.leases[taskID]; ok {
		return &port.WorkspaceLease{
			WorkspaceID: wsID,
			Workspace:   i.workspace[wsID],
			Executor:    i.executor[wsID],
		}, nil
	}

	wsID := sanitizeWorkspaceID(taskID)
	dir := filepath.Join(i.root, wsID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create workspace dir: %w", err)
	}
	sb, err := NewLocal(dir)
	if err != nil {
		return nil, err
	}
	ws := NewLocalWorkspace(sb)
	exec := NewLocalExecutor(sb)

	i.leases[taskID] = wsID
	i.workspace[wsID] = ws
	i.executor[wsID] = exec

	return &port.WorkspaceLease{
		WorkspaceID: wsID,
		Workspace:   ws,
		Executor:    exec,
	}, nil
}

func (i *LocalWorkspaceIsolation) Release(_ context.Context, workspaceID string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if strings.TrimSpace(workspaceID) == "" {
		return fmt.Errorf("workspace_id is required")
	}
	for taskID, wsID := range i.leases {
		if wsID == workspaceID {
			delete(i.leases, taskID)
		}
	}
	delete(i.workspace, workspaceID)
	delete(i.executor, workspaceID)
	return nil
}

func sanitizeWorkspaceID(taskID string) string {
	id := strings.TrimSpace(taskID)
	if id == "" {
		return "task-unknown"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-", " ", "-")
	id = replacer.Replace(id)
	return id
}

