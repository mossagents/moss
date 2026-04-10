package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
)

type updateTaskInput struct {
	TaskID string `json:"task_id"`
	Task   string `json:"task"`
}

func registerUpdateTask(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	spec := tool.ToolSpec{
		Name:        "update_task",
		Description: "Update a running background task with follow-up instructions. Keeps the same task_id.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"task_id":{"type":"string","description":"Task ID to update"},
				"task":{"type":"string","description":"Follow-up instructions"}
			},
			"required":["task_id","task"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in updateTaskInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		taskID := strings.TrimSpace(in.TaskID)
		if taskID == "" {
			return nil, fmt.Errorf("task_id is required")
		}
		taskText := strings.TrimSpace(in.Task)
		if taskText == "" {
			return nil, fmt.Errorf("task is required")
		}
		updated, err := updateBackgroundTask(ctx, agents, tracker, delegator, taskID, taskText)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"task_id":     updated.ID,
			"agent":       updated.AgentName,
			"status":      updated.Status,
			"revision":    updated.Revision,
			"goal":        updated.Goal,
			"session_id":  updated.SessionID,
			"job_id":      updated.JobID,
			"job_item_id": updated.JobItemID,
		})
	}
	return reg.Register(spec, handler)
}

type writeAgentInput struct {
	Target      string `json:"target"`
	Message     string `json:"message"`
	Interrupt   *bool  `json:"interrupt"`
	TriggerTurn *bool  `json:"trigger_turn"`
}

func registerWriteAgentTool(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator, runtime taskrt.TaskRuntime) error {
	spec := tool.ToolSpec{
		Name:        "write_agent",
		Description: "向后台 agent 写入后续消息（当前最小实现：通过更新任务触发执行）。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"target":{"type":"string","description":"task_id 或 agent 名称"},
				"message":{"type":"string","description":"后续消息"},
				"interrupt":{"type":"boolean","description":"是否中断当前执行并立即处理（默认 true）"},
				"trigger_turn":{"type":"boolean","description":"是否触发执行（默认 true）"}
			},
			"required":["target","message"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in writeAgentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		msg := strings.TrimSpace(in.Message)
		if msg == "" {
			return nil, fmt.Errorf("message is required")
		}
		triggerTurn := true
		if in.TriggerTurn != nil {
			triggerTurn = *in.TriggerTurn
		}
		task, err := resolveTaskTarget(tracker, in.Target)
		if err != nil {
			return nil, err
		}
		if !triggerTurn {
			queued, err := enqueueTaskMessage(ctx, runtime, task.ID, msg)
			if err != nil {
				return nil, err
			}
			remaining, hasQueue, err := countQueuedMessages(ctx, runtime, task.ID)
			if err != nil {
				return nil, err
			}
			resp := map[string]any{
				"target":         task.ID,
				"task_id":        task.ID,
				"status":         "queued",
				"queued":         true,
				"queued_id":      queued.ID,
				"queued_at":      queued.CreatedAt,
				"triggered":      false,
				"consumed_count": 0,
			}
			if hasQueue {
				resp["remaining_count"] = remaining
			}
			return json.Marshal(resp)
		}
		interrupt := true
		if in.Interrupt != nil {
			interrupt = *in.Interrupt
		}
		if isActiveTask(task) && !interrupt {
			queued, err := enqueueTaskMessage(ctx, runtime, task.ID, msg)
			if err != nil {
				return nil, err
			}
			remaining, hasQueue, err := countQueuedMessages(ctx, runtime, task.ID)
			if err != nil {
				return nil, err
			}
			resp := map[string]any{
				"target":         task.ID,
				"task_id":        task.ID,
				"status":         "queued",
				"queued":         true,
				"queued_id":      queued.ID,
				"queued_at":      queued.CreatedAt,
				"triggered":      false,
				"consumed_count": 0,
			}
			if hasQueue {
				resp["remaining_count"] = remaining
			}
			return json.Marshal(resp)
		}
		updated, metrics, err := triggerAgentTurn(ctx, agents, tracker, delegator, runtime, task, msg)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"target":          updated.ID,
			"task_id":         updated.ID,
			"agent":           updated.AgentName,
			"status":          updated.Status,
			"revision":        updated.Revision,
			"session_id":      updated.SessionID,
			"job_id":          updated.JobID,
			"job_item_id":     updated.JobItemID,
			"triggered":       true,
			"consumed_count":  metrics.ConsumedCount,
			"remaining_count": metrics.RemainingCount,
		})
	}
	return reg.Register(spec, handler)
}

type waitAgentInput struct {
	Target         string `json:"target"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	PollMillis     int    `json:"poll_millis"`
}

func registerWaitAgentTool(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "wait_agent",
		Description: "等待 agent 状态变化或结束（避免高频手动轮询）。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"target":{"type":"string","description":"task_id 或 agent 名称"},
				"timeout_seconds":{"type":"integer","description":"等待超时秒数，默认30，最大300"},
				"poll_millis":{"type":"integer","description":"预留字段，当前实现基于订阅与超时等待"}
			},
			"required":["target"]
		}`),
		Risk: tool.RiskLow,
	}
	spec = agentToolSpec(spec)
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in waitAgentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		task, err := resolveTaskTarget(tracker, in.Target)
		if err != nil {
			return nil, err
		}
		timeout := in.TimeoutSeconds
		if timeout <= 0 {
			timeout = 30
		}
		if timeout > 300 {
			timeout = 300
		}
		startRevision := task.Revision
		startStatus := task.Status
		if isTerminalTaskStatus(task.Status) {
			resp := buildTaskResponse(task)
			resp["target"] = task.ID
			resp["changed"] = false
			resp["completed"] = true
			return json.Marshal(resp)
		}
		if isRecoverableTask(task) {
			resp := buildTaskResponse(task)
			resp["target"] = task.ID
			resp["changed"] = false
			resp["completed"] = false
			resp["timed_out"] = false
			resp["note"] = "task was hydrated from runtime and is not attached to a live worker; use resume_agent to restart it"
			return json.Marshal(resp)
		}
		updates, unsubscribe, err := tracker.Subscribe(task.ID)
		if err != nil {
			return nil, err
		}
		defer unsubscribe()
		if current, ok := tracker.Get(task.ID); ok {
			changed := current.Revision != startRevision || current.Status != startStatus
			if changed || isTerminalTaskStatus(current.Status) {
				resp := buildTaskResponse(current)
				resp["target"] = current.ID
				resp["changed"] = changed
				resp["completed"] = isTerminalTaskStatus(current.Status)
				resp["timed_out"] = false
				return json.Marshal(resp)
			}
		}
		timer := time.NewTimer(time.Duration(timeout) * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				current, ok := tracker.Get(task.ID)
				if !ok {
					return nil, fmt.Errorf("task %q not found", task.ID)
				}
				resp := buildTaskResponse(current)
				resp["target"] = current.ID
				resp["changed"] = current.Revision != startRevision || current.Status != startStatus
				resp["completed"] = isTerminalTaskStatus(current.Status)
				resp["timed_out"] = true
				return json.Marshal(resp)
			case update, ok := <-updates:
				if !ok {
					return nil, fmt.Errorf("task %q watcher closed", task.ID)
				}
				changed := update.Revision != startRevision || update.Status != startStatus
				if changed || isTerminalTaskStatus(update.Status) {
					resp := buildTaskResponse(&update)
					resp["target"] = update.ID
					resp["changed"] = changed
					resp["completed"] = isTerminalTaskStatus(update.Status)
					resp["timed_out"] = false
					return json.Marshal(resp)
				}
			}
		}
	}
	return reg.Register(spec, handler)
}

type closeAgentInput struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

func registerCloseAgentTool(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "close_agent",
		Description: "关闭指定 agent 任务（运行中则取消，已结束则标记关闭）。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"target":{"type":"string","description":"task_id 或 agent 名称"},
				"reason":{"type":"string","description":"可选: 关闭原因"}
			},
			"required":["target"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in closeAgentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		task, err := resolveTaskTarget(tracker, in.Target)
		if err != nil {
			return nil, err
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = "closed by user"
		}
		closed := false
		note := ""
		if isActiveTask(task) {
			tracker.Cancel(task.ID, reason)
			closed = true
		} else if isRecoverableTask(task) {
			tracker.Cancel(task.ID, reason)
			closed = true
			note = "task was recoverable only and has been marked cancelled"
		} else {
			note = "task is not running"
		}
		updated, _ := tracker.Get(task.ID)
		if updated == nil {
			updated = task
		}
		return json.Marshal(map[string]any{
			"target":    updated.ID,
			"task_id":   updated.ID,
			"agent":     updated.AgentName,
			"status":    updated.Status,
			"revision":  updated.Revision,
			"closed":    closed,
			"completed": isTerminalTaskStatus(updated.Status),
			"note":      note,
		})
	}
	return reg.Register(spec, handler)
}

type resumeAgentInput struct {
	Target  string `json:"target"`
	Message string `json:"message"`
}

func registerResumeAgentTool(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator, runtime taskrt.TaskRuntime) error {
	spec := tool.ToolSpec{
		Name:        "resume_agent",
		Description: "恢复指定 agent 任务（已结束任务可重启，新消息可作为 follow-up）。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"target":{"type":"string","description":"task_id 或 agent 名称"},
				"message":{"type":"string","description":"可选: 恢复时追加的 follow-up"}
			},
			"required":["target"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in resumeAgentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		task, err := resolveTaskTarget(tracker, in.Target)
		if err != nil {
			return nil, err
		}
		if isActiveTask(task) {
			return json.Marshal(map[string]any{
				"target":      task.ID,
				"task_id":     task.ID,
				"agent":       task.AgentName,
				"status":      task.Status,
				"revision":    task.Revision,
				"active":      true,
				"recoverable": false,
				"resumed":     false,
				"completed":   false,
				"note":        "task is already running",
			})
		}
		message := strings.TrimSpace(in.Message)
		updated, metrics, err := triggerAgentTurn(ctx, agents, tracker, delegator, runtime, task, message)
		if err != nil {
			return nil, err
		}
		resp := buildTaskResponse(updated)
		resp["target"] = updated.ID
		resp["resumed"] = true
		resp["completed"] = isTerminalTaskStatus(updated.Status)
		resp["consumed_count"] = metrics.ConsumedCount
		resp["remaining_count"] = metrics.RemainingCount
		if isRecoverableTask(task) {
			resp["note"] = "task was recovered from persisted runtime state and restarted"
		}
		return json.Marshal(resp)
	}
	return reg.Register(spec, handler)
}

// ── task（统一入口） ───────────────────────────────────

type taskInput struct {
	Mode     string              `json:"mode"`
	Agent    string              `json:"agent"`
	Task     string              `json:"task"`
	TaskID   string              `json:"task_id"`
	Status   string              `json:"status"`
	Limit    int                 `json:"limit"`
	Reason   string              `json:"reason"`
	Contract taskrt.TaskContract `json:"contract"`
}

func registerTask(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	spec := tool.ToolSpec{
		Name:        "task",
		Description: "Unified delegation tool. mode=sync/background/query/list/cancel/update for async lifecycle.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"mode": {"type": "string", "description": "One of: sync, background, query, list, cancel, update (default: sync)"},
				"agent": {"type": "string", "description": "Target agent name (required for sync/background)"},
				"task": {"type": "string", "description": "Task description (required for sync/background/update)"},
				"task_id": {"type": "string", "description": "Task ID returned by background mode (required for query/cancel/update)"},
				"status": {"type": "string", "description": "Optional status filter for mode=list"},
				"limit": {"type": "integer", "description": "Optional max results for mode=list"},
				"reason": {"type": "string", "description": "Optional cancel reason for mode=cancel"},
				"contract": {"type":"object","description":"Optional child task contract for sync/background","properties":{
					"input_context":{"type":"string"},
					"budget":{"type":"object","properties":{"max_steps":{"type":"integer"},"max_tokens":{"type":"integer"},"timeout_sec":{"type":"integer"}}},
					"approval_ceiling":{"type":"string","enum":["none","policy_guarded","explicit_user_approval","supervisor_only"]},
					"writable_scopes":{"type":"array","items":{"type":"string"}},
					"memory_scope":{"type":"string"},
					"allowed_effects":{"type":"array","items":{"type":"string","enum":["read_only","writes_workspace","writes_memory","external_side_effect","graph_mutation"]}},
					"return_artifacts":{"type":"array","items":{"type":"string"}}
				}}
			}
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in taskInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		mode := strings.TrimSpace(in.Mode)
		if mode == "" {
			mode = "sync"
		}

		switch mode {
		case "sync":
			if strings.TrimSpace(in.Agent) == "" {
				return nil, fmt.Errorf("agent is required for mode=sync")
			}
			if strings.TrimSpace(in.Task) == "" {
				return nil, fmt.Errorf("task is required for mode=sync")
			}
			result, err := runAgent(ctx, agents, nil, "", 0, delegator, in.Agent, in.Task, in.Contract)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{
				"mode":   "sync",
				"agent":  in.Agent,
				"status": "completed",
				"result": result.Output,
				"supervisor_artifact": buildSupervisorArtifact(&Task{
					AgentName: in.Agent,
					Status:    TaskCompleted,
					Result:    result.Output,
					Contract:  in.Contract,
				}),
			})
		case "background":
			if strings.TrimSpace(in.Agent) == "" {
				return nil, fmt.Errorf("agent is required for mode=background")
			}
			if strings.TrimSpace(in.Task) == "" {
				return nil, fmt.Errorf("task is required for mode=background")
			}
			taskID, err := startBackgroundTask(ctx, agents, tracker, delegator, in.Agent, in.Task, in.Contract)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]string{
				"mode":    "background",
				"task_id": taskID,
				"agent":   in.Agent,
				"status":  "running",
			})
		case "query":
			if strings.TrimSpace(in.TaskID) == "" {
				return nil, fmt.Errorf("task_id is required for mode=query")
			}
			task, ok := tracker.Get(in.TaskID)
			if !ok {
				return nil, fmt.Errorf("task %q not found", in.TaskID)
			}
			resp := buildTaskResponse(task)
			resp["mode"] = "query"
			return json.Marshal(resp)
		case "list":
			filter := TaskFilter{
				AgentName: strings.TrimSpace(in.Agent),
			}
			if in.Status != "" {
				status, err := parseStatusFilter(in.Status)
				if err != nil {
					return nil, err
				}
				filter.Status = status
			}
			limit := clampLimit(in.Limit)
			tasks := tracker.List(filter)
			if len(tasks) > limit {
				tasks = tasks[:limit]
			}
			return json.Marshal(map[string]any{
				"mode":  "list",
				"tasks": tasks,
				"count": len(tasks),
			})
		case "cancel":
			taskID := strings.TrimSpace(in.TaskID)
			if taskID == "" {
				return nil, fmt.Errorf("task_id is required for mode=cancel")
			}
			task, ok := tracker.Get(taskID)
			if !ok {
				return nil, fmt.Errorf("task %q not found", taskID)
			}
			reason := strings.TrimSpace(in.Reason)
			if reason == "" {
				reason = "cancelled by user"
			}
			if task.Status == TaskRunning {
				tracker.Cancel(taskID, reason)
				task, _ = tracker.Get(taskID)
			}
			return json.Marshal(map[string]any{
				"mode":   "cancel",
				"task":   task,
				"status": task.Status,
			})
		case "update":
			taskID := strings.TrimSpace(in.TaskID)
			if taskID == "" {
				return nil, fmt.Errorf("task_id is required for mode=update")
			}
			taskText := strings.TrimSpace(in.Task)
			if taskText == "" {
				return nil, fmt.Errorf("task is required for mode=update")
			}
			updated, err := updateBackgroundTask(ctx, agents, tracker, delegator, taskID, taskText)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{
				"mode":     "update",
				"task_id":  updated.ID,
				"agent":    updated.AgentName,
				"status":   updated.Status,
				"revision": updated.Revision,
			})
		default:
			return nil, fmt.Errorf("unsupported task mode %q (expected sync, background, query, list, cancel, update)", mode)
		}
	}

	return reg.Register(spec, handler)
}

// ── 工具公共 helpers ─────────────────────────────────

// clampLimit normalises a limit value: default 20, max 100.
func clampLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

// parseStatusFilter validates and returns a TaskStatus for filter use.
func parseStatusFilter(raw string) (TaskStatus, error) {
	if raw == "" {
		return "", nil
	}
	status := TaskStatus(strings.TrimSpace(raw))
	switch status {
	case TaskPending, TaskRunning, TaskCompleted, TaskFailed, TaskCancelled:
		return status, nil
	default:
		return "", fmt.Errorf("invalid status %q", raw)
	}
}

// unmarshalOpt unmarshals input JSON only when the payload is non-empty.
func unmarshalOpt(input json.RawMessage, v any) error {
	if len(strings.TrimSpace(string(input))) == 0 {
		return nil
	}
	return json.Unmarshal(input, v)
}
