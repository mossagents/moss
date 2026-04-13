package product

import (
	"context"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/workspace"
	appruntime "github.com/mossagents/moss/runtime"
	"github.com/mossagents/moss/sandbox"
)

func OpenSessionStore() (session.SessionStore, error) {
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	return store, nil
}

func OpenCheckpointStore() (checkpoint.CheckpointStore, error) {
	store, err := checkpoint.NewFileCheckpointStore(CheckpointStoreDir())
	if err != nil {
		return nil, fmt.Errorf("checkpoint store: %w", err)
	}
	return store, nil
}

func OpenTaskRuntime() (*taskrt.FileTaskRuntime, error) {
	rt, err := taskrt.NewFileTaskRuntime(TaskRuntimeDir())
	if err != nil {
		return nil, fmt.Errorf("task runtime: %w", err)
	}
	return rt, nil
}

func OpenStateCatalog() (*appruntime.StateCatalog, error) {
	catalog, err := appruntime.NewStateCatalog(StateStoreDir(), StateEventDir(), true)
	if err != nil {
		return nil, err
	}
	return catalog, nil
}

func OpenWorkspaceIsolation() (workspace.WorkspaceIsolation, error) {
	isolation, err := sandbox.NewLocalWorkspaceIsolation(WorkspaceIsolationDir())
	if err != nil {
		return nil, err
	}
	return isolation, nil
}

func OpenSessionCatalog() (*session.Catalog, error) {
	store, err := OpenSessionStore()
	if err != nil {
		return nil, err
	}
	checkpoints, err := OpenCheckpointStore()
	if err != nil {
		return nil, err
	}
	return &session.Catalog{Store: store, Checkpoints: checkpoints}, nil
}

func ListResumeCandidates(ctx context.Context, workspace string) ([]session.SessionSummary, map[string]int, error) {
	store, err := OpenSessionStore()
	if err != nil {
		return nil, nil, err
	}
	summaries, err := store.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list sessions: %w", err)
	}
	counts, err := SnapshotCountsBySession(ctx, workspace)
	if err != nil {
		return nil, nil, err
	}
	return summaries, counts, nil
}

func SelectResumeSummary(summaries []session.SessionSummary, sessionID string, latest bool) (*session.SessionSummary, []session.SessionSummary, error) {
	recoverable := make([]session.SessionSummary, 0, len(summaries))
	for _, summary := range summaries {
		if summary.Recoverable {
			recoverable = append(recoverable, summary)
		}
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		for i := range summaries {
			if summaries[i].ID != sessionID {
				continue
			}
			if !summaries[i].Recoverable {
				return nil, recoverable, fmt.Errorf("session %q is not recoverable (status=%s)", sessionID, summaries[i].Status)
			}
			return &summaries[i], recoverable, nil
		}
		return nil, recoverable, fmt.Errorf("session %q not found", sessionID)
	}
	if latest {
		if len(recoverable) == 0 {
			return nil, nil, fmt.Errorf("no recoverable sessions found")
		}
		return &recoverable[0], recoverable, nil
	}
	return nil, recoverable, nil
}

func SnapshotCountsBySession(ctx context.Context, workspace string) (map[string]int, error) {
	snapshots, err := listSnapshots(ctx, workspace)
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot.SessionID == "" {
			continue
		}
		counts[snapshot.SessionID]++
	}
	return counts, nil
}

func listSnapshots(ctx context.Context, workspaceDir string) ([]workspace.WorktreeSnapshot, error) {
	store := sandbox.NewGitWorktreeSnapshotStore(workspaceDir)
	snapshots, err := store.List(ctx)
	if err != nil {
		if err == workspace.ErrWorktreeSnapshotUnavailable {
			return nil, nil
		}
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	return snapshots, nil
}
