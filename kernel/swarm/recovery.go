package swarm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mossagents/moss/kernel/artifact"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
)

// RecoveryQuery describes how to reconstruct one swarm run.
type RecoveryQuery struct {
	RunID               string `json:"run_id"`
	IncludeArchived     bool   `json:"include_archived,omitempty"`
	MessageLimitPerTask int    `json:"message_limit_per_task,omitempty"`
}

// Snapshot is the reconstructed view of one swarm run.
type Snapshot struct {
	RunID         string               `json:"run_id"`
	RootSessionID string               `json:"root_session_id,omitempty"`
	Threads       []session.ThreadRef  `json:"threads,omitempty"`
	Tasks         []taskrt.TaskSummary `json:"tasks,omitempty"`
	Messages      []taskrt.TaskMessage `json:"messages,omitempty"`
	Artifacts     []ArtifactRef        `json:"artifacts,omitempty"`
}

// RecoveryResolver rehydrates a swarm snapshot from existing kernel stores.
type RecoveryResolver struct {
	Sessions  session.SessionCatalog
	Tasks     taskrt.TaskGraphRuntime
	Messages  taskrt.TaskMessageRuntime
	Artifacts artifact.Store
}

// LoadRun reconstructs a snapshot of one swarm run.
func (r RecoveryResolver) LoadRun(ctx context.Context, query RecoveryQuery) (*Snapshot, error) {
	runID := strings.TrimSpace(query.RunID)
	if runID == "" {
		return nil, fmt.Errorf("run_id is required")
	}
	if r.Sessions == nil {
		return nil, fmt.Errorf("session catalog is required")
	}
	if r.Tasks == nil {
		return nil, fmt.Errorf("task graph runtime is required")
	}

	threads, err := r.Sessions.ListThreads(ctx, session.ThreadQuery{
		SwarmRunID:      runID,
		IncludeArchived: query.IncludeArchived,
	})
	if err != nil {
		return nil, fmt.Errorf("list swarm threads: %w", err)
	}
	tasks, err := r.Tasks.ListTaskSummaries(ctx, taskrt.TaskQuery{SwarmRunID: runID})
	if err != nil {
		return nil, fmt.Errorf("list swarm tasks: %w", err)
	}

	snapshot := &Snapshot{
		RunID:     runID,
		Threads:   append([]session.ThreadRef(nil), threads...),
		Tasks:     append([]taskrt.TaskSummary(nil), tasks...),
		Messages:  nil,
		Artifacts: nil,
	}
	snapshot.RootSessionID = detectRootSession(snapshot.Threads)

	if r.Messages != nil {
		snapshot.Messages, err = r.loadMessages(ctx, tasks, runID, query.MessageLimitPerTask)
		if err != nil {
			return nil, err
		}
	}
	if r.Artifacts != nil {
		snapshot.Artifacts, err = r.loadArtifacts(ctx, threads, runID)
		if err != nil {
			return nil, err
		}
	}

	sortThreads(snapshot.Threads)
	sortTaskSummaries(snapshot.Tasks)
	sortTaskMessages(snapshot.Messages)
	sortArtifactRefs(snapshot.Artifacts)
	return snapshot, nil
}

func (r RecoveryResolver) loadMessages(ctx context.Context, tasks []taskrt.TaskSummary, runID string, limit int) ([]taskrt.TaskMessage, error) {
	out := make([]taskrt.TaskMessage, 0)
	for _, item := range tasks {
		messages, err := r.Messages.ListTaskMessages(ctx, item.Handle.ID, limit)
		if err != nil {
			return nil, fmt.Errorf("list task messages for %s: %w", item.Handle.ID, err)
		}
		for _, message := range messages {
			if strings.TrimSpace(message.SwarmRunID) == "" {
				message.SwarmRunID = item.Handle.SwarmRunID
			}
			if strings.TrimSpace(message.ThreadID) == "" {
				message.ThreadID = item.Handle.ThreadID
			}
			if strings.TrimSpace(message.SwarmRunID) != runID {
				continue
			}
			out = append(out, message)
		}
	}
	return out, nil
}

func (r RecoveryResolver) loadArtifacts(ctx context.Context, threads []session.ThreadRef, runID string) ([]ArtifactRef, error) {
	sessionToThread := make(map[string]session.ThreadRef, len(threads))
	for _, thread := range threads {
		if thread.SessionID != "" {
			sessionToThread[thread.SessionID] = thread
		}
	}

	out := make([]ArtifactRef, 0)
	for sessionID, thread := range sessionToThread {
		items, err := r.Artifacts.List(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("list artifacts for session %s: %w", sessionID, err)
		}
		for _, item := range items {
			ref, err := ArtifactRefFromArtifact(sessionID, item)
			if err != nil {
				return nil, fmt.Errorf("project artifact ref for session %s: %w", sessionID, err)
			}
			if ref.RunID == "" {
				ref.RunID = thread.SwarmRunID
			}
			if ref.ThreadID == "" {
				ref.ThreadID = thread.SessionID
			}
			if ref.RunID != runID {
				continue
			}
			out = append(out, ref)
		}
	}
	return out, nil
}

func detectRootSession(threads []session.ThreadRef) string {
	for _, thread := range threads {
		if strings.TrimSpace(thread.ParentSessionID) == "" {
			return thread.SessionID
		}
	}
	if len(threads) == 0 {
		return ""
	}
	return threads[0].SessionID
}

func sortThreads(items []session.ThreadRef) {
	sort.Slice(items, func(i, j int) bool {
		left := strings.TrimSpace(items[i].UpdatedAt)
		right := strings.TrimSpace(items[j].UpdatedAt)
		if left == right {
			return items[i].SessionID < items[j].SessionID
		}
		return left < right
	})
}

func sortTaskSummaries(items []taskrt.TaskSummary) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].Handle.ID < items[j].Handle.ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
}

func sortTaskMessages(items []taskrt.TaskMessage) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
}

func sortArtifactRefs(items []ArtifactRef) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			if items[i].SessionID == items[j].SessionID {
				return items[i].Name < items[j].Name
			}
			return items[i].SessionID < items[j].SessionID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
}
