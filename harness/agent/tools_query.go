package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/tool"
)

// ── query_agent (查询异步结果) ────────────────────────

type queryInput struct {
	TaskID string `json:"task_id"`
}

func registerQuery(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "query_agent",
		Description: "查询由 spawn_agent 启动的后台任务状态和结果。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"task_id": {"type": "string", "description": "spawn_agent 返回的任务 ID"}
			},
			"required": ["task_id"]
		}`),
		Risk: tool.RiskLow,
	}
	spec = agentToolSpec(spec)

	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in queryInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}

		task, ok := tracker.Get(in.TaskID)
		if !ok {
			return nil, fmt.Errorf("task %q not found", in.TaskID)
		}

		return json.Marshal(buildTaskResponse(task))
	}

	return reg.Register(tool.NewRawTool(spec, handler))
}

type readAgentInput struct {
	Target string `json:"target"`
}

func registerReadAgentTool(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "read_agent",
		Description: "读取指定 agent 后台任务状态。target 支持 task_id 或 agent 名称。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"target":{"type":"string","description":"task_id 或 agent 名称"}
			},
			"required":["target"]
		}`),
		Risk: tool.RiskLow,
	}
	spec = agentToolSpec(spec)
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in readAgentInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		task, err := resolveTaskTarget(tracker, in.Target)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"target": task.ID,
			"task":   task,
		})
	}
	return reg.Register(tool.NewRawTool(spec, handler))
}

// listInput is shared by list_agents and list_tasks (identical fields).
type listInput struct {
	Status string `json:"status"`
	Agent  string `json:"agent"`
	Limit  int    `json:"limit"`
}

func registerListAgentsTool(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "list_agents",
		Description: "列出后台 agent 任务（兼容控制平面最小能力）。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"status":{"type":"string","description":"可选: pending|running|completed|failed|cancelled"},
				"agent":{"type":"string","description":"可选: 按 agent 名称过滤"},
				"limit":{"type":"integer","description":"可选: 最大返回条数（默认20，最大100）"}
			}
		}`),
		Risk: tool.RiskLow,
	}
	spec = agentToolSpec(spec)
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in listInput
		if err := unmarshalOpt(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		filter := TaskFilter{AgentName: strings.TrimSpace(in.Agent)}
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
		agents := make([]map[string]any, 0, len(tasks))
		for _, task := range tasks {
			item := map[string]any{
				"target":     task.ID,
				"task_id":    task.ID,
				"agent":      task.AgentName,
				"status":     task.Status,
				"revision":   task.Revision,
				"goal":       task.Goal,
				"updated_at": task.UpdatedAt,
			}
			if task.SessionID != "" {
				item["session_id"] = task.SessionID
			}
			if task.JobID != "" {
				item["job_id"] = task.JobID
			}
			if task.JobItemID != "" {
				item["job_item_id"] = task.JobItemID
			}
			agents = append(agents, item)
		}
		return json.Marshal(map[string]any{
			"agents": agents,
			"count":  len(agents),
		})
	}
	return reg.Register(tool.NewRawTool(spec, handler))
}

func registerListTasks(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "list_tasks",
		Description: "列出后台任务，支持按状态或 agent 过滤。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type":"string","description":"可选: pending|running|completed|failed|cancelled"},
				"agent": {"type":"string","description":"可选: 按 agent 名称过滤"},
				"limit": {"type":"integer","description":"可选: 最多返回条数（默认20，最大100）"}
			}
		}`),
		Risk: tool.RiskLow,
	}
	spec = agentToolSpec(spec)
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in listInput
		if err := unmarshalOpt(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		filter := TaskFilter{AgentName: strings.TrimSpace(in.Agent)}
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
			"tasks": tasks,
			"count": len(tasks),
		})
	}
	return reg.Register(tool.NewRawTool(spec, handler))
}

type cancelTaskInput struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason"`
}

func registerCancelTask(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "cancel_task",
		Description: "取消后台任务（若任务仍在运行）。",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"task_id":{"type":"string","description":"要取消的任务 ID"},
				"reason":{"type":"string","description":"可选: 取消原因"}
			},
			"required":["task_id"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in cancelTaskInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		taskID := strings.TrimSpace(in.TaskID)
		if taskID == "" {
			return nil, fmt.Errorf("task_id is required")
		}
		task, ok := tracker.Get(taskID)
		if !ok {
			return nil, fmt.Errorf("task %q not found", taskID)
		}
		if task.Status != TaskRunning {
			return json.Marshal(map[string]any{
				"task_id": task.ID,
				"agent":   task.AgentName,
				"status":  task.Status,
				"error":   task.Error,
				"note":    "task is not running",
			})
		}
		reason := strings.TrimSpace(in.Reason)
		if reason == "" {
			reason = "cancelled by user"
		}
		tracker.Cancel(taskID, reason)
		updated, _ := tracker.Get(taskID)
		return json.Marshal(updated)
	}
	return reg.Register(tool.NewRawTool(spec, handler))
}
