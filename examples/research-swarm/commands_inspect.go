package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mossagents/moss/kernel/session"
	kswarm "github.com/mossagents/moss/kernel/swarm"
	taskrt "github.com/mossagents/moss/kernel/task"
)

type inspectReport struct {
	RunID           string               `json:"run_id"`
	RootSessionID   string               `json:"root_session_id"`
	Status          string               `json:"status"`
	Recoverable     bool                 `json:"recoverable"`
	Degraded        bool                 `json:"degraded"`
	EventsPartial   bool                 `json:"events_partial"`
	EventsLastError string               `json:"events_last_error,omitempty"`
	Threads         []session.ThreadRef  `json:"threads,omitempty"`
	Tasks           any                  `json:"tasks,omitempty"`
	Artifacts       []kswarm.ArtifactRef `json:"artifacts,omitempty"`
	Messages        int                  `json:"message_count,omitempty"`
	Events          []map[string]any     `json:"events,omitempty"`
}

func runInspectCommand(cfg *inspectCommandConfig) error {
	ctx := context.Background()
	if cfg == nil {
		return fmt.Errorf("inspect config is required")
	}
	env, err := openSnapshotEnv()
	if err != nil {
		return err
	}
	defer env.Close(ctx)
	target, err := env.Targets.ResolveForInspect(ctx, cfg.SessionID, cfg.RunID, cfg.Latest)
	if err != nil {
		return err
	}
	snapshot, err := env.Recovery.Load(ctx, target)
	if err != nil {
		return err
	}
	report, err := buildInspectReport(ctx, env, snapshot, cfg.View, cfg.ThreadID)
	if err != nil {
		return err
	}
	if cfg.JSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	return printInspectReport(report, cfg.View)
}

func buildInspectReport(ctx context.Context, env *runtimeEnv, snapshot *RecoveredRunSnapshot, view, threadID string) (*inspectReport, error) {
	view = stringsTrim(view)
	if view == "" {
		view = defaultView
	}
	report := &inspectReport{
		RunID:           snapshot.RunID,
		RootSessionID:   snapshot.RootSessionID,
		Status:          snapshot.Status,
		Recoverable:     snapshot.Recoverable,
		Degraded:        snapshot.Degraded,
		EventsPartial:   snapshot.EventsPartial,
		EventsLastError: snapshot.EventsLastError,
		Messages:        len(snapshot.Snapshot.Messages),
	}
	switch view {
	case "run":
		report.Threads = append([]session.ThreadRef(nil), snapshot.Snapshot.Threads...)
		report.Tasks = snapshot.Snapshot.Tasks
		report.Artifacts = append([]kswarm.ArtifactRef(nil), snapshot.Snapshot.Artifacts...)
	case "threads":
		report.Threads = append([]session.ThreadRef(nil), snapshot.Snapshot.Threads...)
	case "thread":
		threadID = stringsTrim(threadID)
		if threadID == "" {
			return nil, fmt.Errorf("--thread-id is required for --view thread")
		}
		thread, ok := snapshot.ThreadIndex[threadID]
		if !ok {
			return nil, fmt.Errorf("thread %q not found", threadID)
		}
		report.Threads = []session.ThreadRef{thread}
		var tasks []any
		for _, task := range snapshot.Snapshot.Tasks {
			if task.Handle.ThreadID == threadID {
				tasks = append(tasks, task)
			}
		}
		report.Tasks = tasks
		for _, item := range snapshot.Snapshot.Artifacts {
			if item.ThreadID == threadID || item.SessionID == threadID {
				report.Artifacts = append(report.Artifacts, item)
			}
		}
	case "events":
		events, err := env.EventStore.LoadEvents(ctx, snapshot.RootSessionID, 0)
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			report.Events = append(report.Events, map[string]any{
				"seq":       event.Seq,
				"type":      string(event.Type),
				"timestamp": event.Timestamp.UTC().Format(time.RFC3339Nano),
			})
		}
	default:
		return nil, fmt.Errorf("unsupported inspect view %q", view)
	}
	return report, nil
}

func printInspectReport(report *inspectReport, view string) error {
	if report == nil {
		return fmt.Errorf("inspect report is required")
	}
	fmt.Printf("run_id=%s root_session_id=%s status=%s recoverable=%t degraded=%t events_partial=%t\n",
		report.RunID, report.RootSessionID, report.Status, report.Recoverable, report.Degraded, report.EventsPartial)
	switch view {
	case "threads":
		for _, thread := range report.Threads {
			fmt.Printf("thread=%s role=%s parent=%s status=%s goal=%s\n", thread.SessionID, thread.ThreadRole, thread.ParentSessionID, thread.Status, thread.Goal)
		}
	case "thread":
		for _, thread := range report.Threads {
			fmt.Printf("thread=%s role=%s goal=%s\n", thread.SessionID, thread.ThreadRole, thread.Goal)
		}
		fmt.Printf("artifacts=%d\n", len(report.Artifacts))
	case "events":
		for _, event := range report.Events {
			fmt.Printf("seq=%v type=%v timestamp=%v\n", event["seq"], event["type"], event["timestamp"])
		}
	default:
		fmt.Printf("threads=%d artifacts=%d messages=%d\n", len(report.Threads), len(report.Artifacts), report.Messages)
		taskCount := 0
		if report.Tasks != nil {
			if items, ok := report.Tasks.([]taskrt.TaskSummary); ok {
				taskCount = len(items)
			}
		}
		fmt.Printf("tasks=%d\n", taskCount)
	}
	return nil
}
