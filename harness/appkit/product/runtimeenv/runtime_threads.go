package runtimeenv

import (
	"context"
	"fmt"
	"sort"

	"github.com/mossagents/moss/harness/internal/stringutil"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
)

type ThreadBrowseSummary struct {
	Thread        session.ThreadRef `json:"thread"`
	SnapshotCount int               `json:"snapshot_count,omitempty"`
}

func ListThreadBrowseSummaries(ctx context.Context, workspace string, query session.ThreadQuery) ([]ThreadBrowseSummary, error) {
	catalog, err := OpenSessionCatalog()
	if err != nil {
		return nil, err
	}
	threads, err := catalog.ListThreads(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	snapshotCounts, err := SnapshotCountsBySession(ctx, workspace)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadBrowseSummary, 0, len(threads))
	for _, thread := range threads {
		out = append(out, ThreadBrowseSummary{
			Thread:        thread,
			SnapshotCount: snapshotCounts[thread.SessionID],
		})
	}
	return out, nil
}

func ListForkSources(ctx context.Context, workspace string, threadLimit, checkpointLimit int) ([]session.ForkSource, error) {
	catalog, err := OpenSessionCatalog()
	if err != nil {
		return nil, err
	}
	threads, err := ListThreadBrowseSummaries(ctx, workspace, session.ThreadQuery{
		RecoverableOnly: true,
		Limit:           threadLimit,
	})
	if err != nil {
		return nil, err
	}
	checkpoints, err := catalog.ListCheckpoints(ctx, session.CheckpointQuery{Limit: checkpointLimit})
	if err != nil {
		return nil, fmt.Errorf("list checkpoints: %w", err)
	}
	out := make([]session.ForkSource, 0, len(threads)+len(checkpoints))
	for _, thread := range threads {
		out = append(out, session.ForkSource{
			Kind:      checkpoint.ForkSourceSession,
			SourceID:  thread.Thread.SessionID,
			SessionID: thread.Thread.SessionID,
			Label:     stringutil.FirstNonEmpty(thread.Thread.Preview, thread.Thread.Goal, thread.Thread.SessionID),
			Lineage:   append([]session.LineageRef(nil), thread.Thread.Lineage...),
		})
	}
	for _, ckpt := range checkpoints {
		out = append(out, session.ForkSource{
			Kind:         checkpoint.ForkSourceCheckpoint,
			SourceID:     ckpt.ID,
			SessionID:    ckpt.SessionID,
			CheckpointID: ckpt.ID,
			Label:        stringutil.FirstNonEmpty(ckpt.Note, ckpt.ID),
			Lineage:      append([]session.LineageRef(nil), ckpt.Lineage...),
		})
	}
	return out, nil
}

func ListTaskBrowseSummaries(ctx context.Context, query taskrt.TaskQuery) ([]taskrt.TaskSummary, error) {
	rt, err := OpenTaskRuntime()
	if err != nil {
		return nil, err
	}
	summaries, err := rt.ListTaskSummaries(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].Handle.ID < summaries[j].Handle.ID
		}
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}
