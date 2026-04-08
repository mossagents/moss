package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
	"sort"
	"strings"
)

const schedulingStateKey kernel.ExtensionStateKey = "scheduling.state"

type schedulingState struct {
	scheduler *scheduler.Scheduler
}

type ScheduleItem struct {
	ID       string
	Schedule string
	Goal     string
	LastRun  string
	NextRun  string
	RunCount int
}

type ScheduleController interface {
	List() ([]ScheduleItem, error)
	ListText() (string, error)
	Cancel(id string) (string, error)
	RunNow(id string) (string, error)
}

type SchedulerAdapter struct {
	Scheduler *scheduler.Scheduler
}

func WithScheduler(s *scheduler.Scheduler) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureSchedulingState(k).scheduler = s
	}
}

func (a SchedulerAdapter) List() ([]ScheduleItem, error) {
	if a.Scheduler == nil {
		return nil, fmt.Errorf("scheduler is unavailable")
	}
	jobs := a.Scheduler.ListJobs()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].ID < jobs[j].ID })
	items := make([]ScheduleItem, 0, len(jobs))
	for _, job := range jobs {
		item := ScheduleItem{
			ID:       job.ID,
			Schedule: job.Schedule,
			Goal:     strings.TrimSpace(job.Goal),
			RunCount: job.RunCount,
		}
		if !job.NextRun.IsZero() {
			item.NextRun = job.NextRun.Format("2006-01-02 15:04:05")
		}
		if !job.LastRun.IsZero() {
			item.LastRun = job.LastRun.Format("2006-01-02 15:04:05")
		}
		items = append(items, item)
	}
	return items, nil
}

func (a SchedulerAdapter) ListText() (string, error) {
	items, err := a.List()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "No background scheduled jkobs.", nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Schedules (%d):\n", len(items)))
	for _, item := range items {
		b.WriteString(fmt.Sprintf("- %s | %s", item.ID, item.Schedule))
		if item.NextRun != "" {
			b.WriteString(" | next: " + item.NextRun)
		}
		if item.LastRun != "" {
			b.WriteString(" | last: " + item.LastRun)
		}
		if item.RunCount > 0 {
			b.WriteString(fmt.Sprintf(" | runs: %d", item.RunCount))
		}
		if item.Goal != "" {
			b.WriteString(" | " + item.Goal)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (a SchedulerAdapter) Cancel(id string) (string, error) {
	if a.Scheduler == nil {
		return "Scheduler is unavailable.", nil
	}
	id = strings.TrimSpace(id)
	if err := a.Scheduler.RemoveJob(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Schedule %s deleted.", id), nil
}

func (a SchedulerAdapter) RunNow(id string) (string, error) {
	if a.Scheduler == nil {
		return "Scheduler is not ready yet.", nil
	}
	id = strings.TrimSpace(id)
	if err := a.Scheduler.Trigger(id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Schedule %s started immediately.", id), nil
}

func RegisterSchedulerTools(k *kernel.Kernel, sched *scheduler.Scheduler) error {
	return RegisterSchedulerToolRegistry(k.ToolRegistry(), sched)
}

func RegisterSchedulerToolRegistry(reg tool.Registry, sched *scheduler.Scheduler) error {
	if sched == nil {
		return fmt.Errorf("scheduler is nil")
	}
	tools := []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{scheduleTaskSpec, scheduleTaskHandler(sched)},
		{listSchedulesSpec, listSchedulesHandler(sched)},
		{cancelScheduleSpec, cancelScheduleHandler(sched)},
	}
	for _, t := range tools {
		if _, _, exists := reg.Get(t.spec.Name); exists {
			continue
		}
		if err := reg.Register(t.spec, t.handler); err != nil {
			return err
		}
	}
	return nil
}

func ensureSchedulingState(k *kernel.Kernel) *schedulingState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(schedulingStateKey, &schedulingState{})
	st := actual.(*schedulingState)
	if loaded {
		return st
	}
	bridge.OnShutdown(200, func(_ context.Context, _ *kernel.Kernel) error {
		if st.scheduler != nil {
			st.scheduler.Stop()
		}
		return nil
	})
	return st
}

var scheduleTaskSpec = tool.ToolSpec{
	Name: "schedule_task",
	Description: `Schedule a recurring or one-shot task for later execution.
The task will be executed automatically at the specified interval.

Schedule formats:
  - "@every 30m"  — run every 30 minutes
  - "@every 6h"   — run every 6 hours
  - "@every 1h30m" — run every 1.5 hours
	- "@after 10m"  — run once after 10 minutes
  - "@once"       — run once immediately (then auto-remove)

Example: schedule_task(id="crawl-news", schedule="@every 6h", goal="Crawl news.ycombinator.com and save top stories")`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"id":       {"type": "string", "description": "Unique identifier for this scheduled task"},
			"schedule": {"type": "string", "description": "Schedule expression: '@every <duration>', '@after <duration>' or '@once'"},
			"goal":     {"type": "string", "description": "Goal description for the task — what it should accomplish"}
		},
		"required": ["id", "schedule", "goal"]
	}`),
	Risk:         tool.RiskMedium,
	Capabilities: []string{"scheduling"},
}

func scheduleTaskHandler(sched *scheduler.Scheduler) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			ID       string `json:"id"`
			Schedule string `json:"schedule"`
			Goal     string `json:"goal"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		job := scheduler.Job{
			ID:       params.ID,
			Schedule: params.Schedule,
			Goal:     params.Goal,
			Config: session.SessionConfig{
				Goal:     params.Goal,
				Mode:     "scheduled",
				MaxSteps: 30,
			},
		}
		if err := sched.AddJob(job); err != nil {
			return nil, fmt.Errorf("schedule task: %w", err)
		}
		return json.Marshal(map[string]any{
			"status":   "scheduled",
			"id":       params.ID,
			"schedule": params.Schedule,
			"goal":     params.Goal,
			"total":    sched.Count(),
		})
	}
}

var listSchedulesSpec = tool.ToolSpec{
	Name:         "list_schedules",
	Description:  "List all currently scheduled tasks with their status, schedule, and run history.",
	InputSchema:  json.RawMessage(`{"type": "object", "properties": {}}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"scheduling"},
}

func listSchedulesHandler(sched *scheduler.Scheduler) tool.ToolHandler {
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		items, err := (SchedulerAdapter{Scheduler: sched}).List()
		if err != nil {
			return nil, err
		}
		type jobInfo struct {
			ID       string `json:"id"`
			Schedule string `json:"schedule"`
			Goal     string `json:"goal"`
			RunCount int    `json:"run_count"`
			LastRun  string `json:"last_run,omitempty"`
			NextRun  string `json:"next_run,omitempty"`
		}
		infos := make([]jobInfo, len(items))
		for i, item := range items {
			infos[i] = jobInfo{
				ID:       item.ID,
				Schedule: item.Schedule,
				Goal:     item.Goal,
				RunCount: item.RunCount,
				LastRun:  item.LastRun,
				NextRun:  item.NextRun,
			}
		}
		return json.Marshal(map[string]any{"count": len(infos), "jobs": infos})
	}
}

var cancelScheduleSpec = tool.ToolSpec{
	Name:        "cancel_schedule",
	Description: "Cancel a scheduled task by its ID. The task will no longer execute.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "ID of the scheduled task to cancel"}
		},
		"required": ["id"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"scheduling"},
}

func cancelScheduleHandler(sched *scheduler.Scheduler) tool.ToolHandler {
	return func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if err := sched.RemoveJob(params.ID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status":    "cancelled",
			"id":        params.ID,
			"remaining": sched.Count(),
		})
	}
}
