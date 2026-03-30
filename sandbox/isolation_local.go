package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

type localLeaseRecord struct {
	TaskID      string    `json:"task_id"`
	WorkspaceID string    `json:"workspace_id"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

// LocalWorkspaceIsolation 基于本地目录实现任务隔离（POC 版本）。
type LocalWorkspaceIsolation struct {
	mu        sync.Mutex
	root      string
	journal   string
	leases    map[string]localLeaseRecord
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
	iso := &LocalWorkspaceIsolation{
		root:      abs,
		journal:   filepath.Join(abs, "leases.json"),
		leases:    make(map[string]localLeaseRecord),
		workspace: make(map[string]*LocalWorkspace),
		executor:  make(map[string]*LocalExecutor),
	}
	if err := iso.loadJournal(); err != nil {
		return nil, err
	}
	return iso, nil
}

func (i *LocalWorkspaceIsolation) Acquire(_ context.Context, taskID string) (*port.WorkspaceLease, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if lease, ok := i.leases[taskID]; ok {
		ws, exec, recovered, err := i.ensureWorkspaceLocked(lease.WorkspaceID)
		if err != nil {
			return nil, err
		}
		return &port.WorkspaceLease{
			WorkspaceID: lease.WorkspaceID,
			TaskID:      lease.TaskID,
			AcquiredAt:  lease.AcquiredAt,
			Recovered:   recovered,
			Workspace:   ws,
			Executor:    exec,
		}, nil
	}

	wsID := newWorkspaceID(taskID)
	lease := localLeaseRecord{
		TaskID:      taskID,
		WorkspaceID: wsID,
		AcquiredAt:  time.Now().UTC(),
	}
	ws, exec, _, err := i.ensureWorkspaceLocked(wsID)
	if err != nil {
		return nil, err
	}
	i.leases[taskID] = lease
	if err := i.persistJournal(); err != nil {
		delete(i.leases, taskID)
		delete(i.workspace, wsID)
		delete(i.executor, wsID)
		return nil, err
	}

	return &port.WorkspaceLease{
		WorkspaceID: wsID,
		TaskID:      taskID,
		AcquiredAt:  lease.AcquiredAt,
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
	for taskID, lease := range i.leases {
		if lease.WorkspaceID == workspaceID {
			delete(i.leases, taskID)
		}
	}
	delete(i.workspace, workspaceID)
	delete(i.executor, workspaceID)
	return i.persistJournal()
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

func newWorkspaceID(taskID string) string {
	return fmt.Sprintf("%s-%d", sanitizeWorkspaceID(taskID), time.Now().UnixNano())
}

func (i *LocalWorkspaceIsolation) ensureWorkspaceLocked(workspaceID string) (*LocalWorkspace, *LocalExecutor, bool, error) {
	if ws, ok := i.workspace[workspaceID]; ok {
		return ws, i.executor[workspaceID], false, nil
	}
	dir := filepath.Join(i.root, workspaceID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, nil, false, fmt.Errorf("create workspace dir: %w", err)
	}
	sb, err := NewLocal(dir)
	if err != nil {
		return nil, nil, false, err
	}
	ws := NewLocalWorkspace(sb)
	exec := NewLocalExecutor(sb)
	i.workspace[workspaceID] = ws
	i.executor[workspaceID] = exec
	return ws, exec, true, nil
}

func (i *LocalWorkspaceIsolation) loadJournal() error {
	data, err := os.ReadFile(i.journal)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read lease journal: %w", err)
	}
	var leases map[string]localLeaseRecord
	if err := json.Unmarshal(data, &leases); err != nil {
		return fmt.Errorf("unmarshal lease journal: %w", err)
	}
	if leases != nil {
		i.leases = leases
	}
	return nil
}

func (i *LocalWorkspaceIsolation) persistJournal() error {
	data, err := json.MarshalIndent(i.leases, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lease journal: %w", err)
	}
	tmp := i.journal + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write lease journal tmp: %w", err)
	}
	if err := os.Rename(tmp, i.journal); err != nil {
		return fmt.Errorf("replace lease journal: %w", err)
	}
	return nil
}
