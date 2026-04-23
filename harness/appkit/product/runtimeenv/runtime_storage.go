package runtimeenv

import (
	"context"
	"fmt"
	"strings"

	rstate "github.com/mossagents/moss/harness/runtime/state"
	"github.com/mossagents/moss/harness/sandbox"
	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/checkpoint"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/workspace"
)

// OpenEventStore opens (or creates) the SQLite-backed EventStore at the
// default path returned by EventStoreDBPath.
func OpenEventStore() (kruntime.EventStore, error) {
	store, err := kruntime.NewSQLiteEventStore(EventStoreDBPath())
	if err != nil {
		return nil, fmt.Errorf("event store: %w", err)
	}
	return store, nil
}

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

func OpenArtifactStore() (*artifact.FileStore, error) {
	store, err := artifact.NewFileStore(ArtifactStoreDir())
	if err != nil {
		return nil, fmt.Errorf("artifact store: %w", err)
	}
	return store, nil
}

func OpenStateCatalog() (*rstate.StateCatalog, error) {
	catalog, err := rstate.NewStateCatalog(StateStoreDir(), StateEventDir(), true)
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
	snapshots, err := ListSnapshots(ctx, workspace)
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

func ListSnapshots(ctx context.Context, workspaceDir string) ([]workspace.WorktreeSnapshot, error) {
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
