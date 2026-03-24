package builtins

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mossagi/moss/kernel/scheduler"
	"github.com/mossagi/moss/kernel/session"
	"github.com/mossagi/moss/kernel/tool"
)

// RegisterScheduleTools 注册调度相关的内置工具。
// 需要在 Kernel 设置好 Scheduler 后调用。
func RegisterScheduleTools(reg tool.Registry, sched *scheduler.Scheduler) error {
	tools := []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{scheduleTaskSpec, scheduleTaskHandler(sched)},
		{listSchedulesSpec, listSchedulesHandler(sched)},
		{cancelScheduleSpec, cancelScheduleHandler(sched)},
	}
	for _, t := range tools {
		if err := reg.Register(t.spec, t.handler); err != nil {
			return err
		}
	}
	return nil
}

// ─── schedule_task ──────────────────────────────────

var scheduleTaskSpec = tool.ToolSpec{
	Name: "schedule_task",
	Description: `Schedule a recurring or one-shot task for later execution.
The task will be executed automatically at the specified interval.

Schedule formats:
  - "@every 30m"  — run every 30 minutes
  - "@every 6h"   — run every 6 hours
  - "@every 1h30m" — run every 1.5 hours
  - "@once"       — run once immediately (then auto-remove)

Example: schedule_task(id="crawl-news", schedule="@every 6h", goal="Crawl news.ycombinator.com and save top stories")`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"id":       {"type": "string", "description": "Unique identifier for this scheduled task"},
			"schedule": {"type": "string", "description": "Schedule expression: '@every <duration>' or '@once'"},
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

// ─── list_schedules ─────────────────────────────────

var listSchedulesSpec = tool.ToolSpec{
	Name:         "list_schedules",
	Description:  "List all currently scheduled tasks with their status, schedule, and run history.",
	InputSchema:  json.RawMessage(`{"type": "object", "properties": {}}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"scheduling"},
}

func listSchedulesHandler(sched *scheduler.Scheduler) tool.ToolHandler {
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		jobs := sched.ListJobs()

		type jobInfo struct {
			ID       string `json:"id"`
			Schedule string `json:"schedule"`
			Goal     string `json:"goal"`
			RunCount int    `json:"run_count"`
			LastRun  string `json:"last_run,omitempty"`
			NextRun  string `json:"next_run,omitempty"`
		}

		infos := make([]jobInfo, len(jobs))
		for i, j := range jobs {
			infos[i] = jobInfo{
				ID:       j.ID,
				Schedule: j.Schedule,
				Goal:     j.Goal,
				RunCount: j.RunCount,
			}
			if !j.LastRun.IsZero() {
				infos[i].LastRun = j.LastRun.Format("2006-01-02 15:04:05")
			}
			if !j.NextRun.IsZero() {
				infos[i].NextRun = j.NextRun.Format("2006-01-02 15:04:05")
			}
		}

		return json.Marshal(map[string]any{
			"count": len(infos),
			"jobs":  infos,
		})
	}
}

// ─── cancel_schedule ────────────────────────────────

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
