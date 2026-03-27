package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"

	"github.com/google/uuid"
)

// Delegator 是 agent 包对 Kernel 能力的抽象，避免循环依赖。
// 由 Kernel 实现。
type Delegator interface {
	NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error)
	RunWithTools(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error)
	ToolRegistry() tool.Registry
}

// RegisterTools 向工具注册表注册委派相关工具。
func RegisterTools(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	if err := registerDelegate(reg, agents, delegator); err != nil {
		return err
	}
	if err := registerSpawn(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if err := registerQuery(reg, tracker); err != nil {
		return err
	}
	if err := registerListTasks(reg, tracker); err != nil {
		return err
	}
	if err := registerCancelTask(reg, tracker); err != nil {
		return err
	}
	if err := registerUpdateTask(reg, agents, tracker, delegator); err != nil {
		return err
	}
	return registerTask(reg, agents, tracker, delegator)
}

// ── delegate_agent (同步) ─────────────────────────────

type delegateInput struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

func registerDelegate(reg tool.Registry, agents *Registry, delegator Delegator) error {
	spec := tool.ToolSpec{
		Name:        "delegate_agent",
		Description: "委派任务给另一个专业 Agent 并同步等待结果返回。用于需要特定专业能力的子任务。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"agent": {"type": "string", "description": "目标 Agent 名称"},
				"task":  {"type": "string", "description": "要委派的具体任务描述"}
			},
			"required": ["agent", "task"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in delegateInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}

		result, err := runAgent(ctx, agents, delegator, in.Agent, in.Task)
		if err != nil {
			return nil, err
		}

		return json.Marshal(map[string]string{
			"agent":  in.Agent,
			"status": "completed",
			"result": result.Output,
		})
	}

	return reg.Register(spec, handler)
}

// ── spawn_agent (异步) ────────────────────────────────

type spawnInput struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

func registerSpawn(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	spec := tool.ToolSpec{
		Name:        "spawn_agent",
		Description: "在后台启动一个 Agent 执行任务，立即返回任务 ID。用 query_agent 检查进度和结果。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"agent": {"type": "string", "description": "目标 Agent 名称"},
				"task":  {"type": "string", "description": "任务描述"}
			},
			"required": ["agent", "task"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in spawnInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		taskID, err := startBackgroundTask(ctx, agents, tracker, delegator, in.Agent, in.Task)
		if err != nil {
			return nil, err
		}

		return json.Marshal(map[string]string{
			"task_id": taskID,
			"status":  "running",
		})
	}

	return reg.Register(spec, handler)
}

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

	return reg.Register(spec, handler)
}

type listTasksInput struct {
	Status string `json:"status"`
	Agent  string `json:"agent"`
	Limit  int    `json:"limit"`
}

func registerListTasks(reg tool.Registry, tracker *TaskTracker) error {
	spec := tool.ToolSpec{
		Name:        "list_tasks",
		Description: "列出后台任务，支持按状态或 agent 过滤。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"status": {"type":"string","description":"可选: running|completed|failed|cancelled"},
				"agent": {"type":"string","description":"可选: 按 agent 名称过滤"},
				"limit": {"type":"integer","description":"可选: 最多返回条数（默认20，最大100）"}
			}
		}`),
		Risk: tool.RiskLow,
	}
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in listTasksInput
		if len(strings.TrimSpace(string(input))) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return nil, fmt.Errorf("parse input: %w", err)
			}
		}
		filter := TaskFilter{
			AgentName: strings.TrimSpace(in.Agent),
		}
		if in.Status != "" {
			status := TaskStatus(strings.TrimSpace(in.Status))
			switch status {
			case TaskRunning, TaskCompleted, TaskFailed, TaskCancelled:
				filter.Status = status
			default:
				return nil, fmt.Errorf("invalid status %q", in.Status)
			}
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 20
		}
		if limit > 100 {
			limit = 100
		}
		tasks := tracker.List(filter)
		if len(tasks) > limit {
			tasks = tasks[:limit]
		}
		return json.Marshal(map[string]any{
			"tasks": tasks,
			"count": len(tasks),
		})
	}
	return reg.Register(spec, handler)
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
	return reg.Register(spec, handler)
}

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
			"task_id":  updated.ID,
			"agent":    updated.AgentName,
			"status":   updated.Status,
			"revision": updated.Revision,
			"goal":     updated.Goal,
		})
	}
	return reg.Register(spec, handler)
}

// ── task（统一入口） ───────────────────────────────────

type taskInput struct {
	Mode   string `json:"mode"`
	Agent  string `json:"agent"`
	Task   string `json:"task"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Limit  int    `json:"limit"`
	Reason string `json:"reason"`
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
				"reason": {"type": "string", "description": "Optional cancel reason for mode=cancel"}
			}
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}

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
			result, err := runAgent(ctx, agents, delegator, in.Agent, in.Task)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]string{
				"mode":   "sync",
				"agent":  in.Agent,
				"status": "completed",
				"result": result.Output,
			})
		case "background":
			if strings.TrimSpace(in.Agent) == "" {
				return nil, fmt.Errorf("agent is required for mode=background")
			}
			if strings.TrimSpace(in.Task) == "" {
				return nil, fmt.Errorf("task is required for mode=background")
			}
			taskID, err := startBackgroundTask(ctx, agents, tracker, delegator, in.Agent, in.Task)
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
				status := TaskStatus(strings.TrimSpace(in.Status))
				switch status {
				case TaskRunning, TaskCompleted, TaskFailed, TaskCancelled:
					filter.Status = status
				default:
					return nil, fmt.Errorf("invalid status %q", in.Status)
				}
			}
			limit := in.Limit
			if limit <= 0 {
				limit = 20
			}
			if limit > 100 {
				limit = 100
			}
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

// ── 公共执行逻辑 ─────────────────────────────────────

func startBackgroundTask(ctx context.Context, agents *Registry, tracker *TaskTracker, delegator Delegator, agentName, goal string) (string, error) {
	if _, ok := agents.Get(agentName); !ok {
		return "", fmt.Errorf("agent %q not found", agentName)
	}
	depth := Depth(ctx)
	if depth >= MaxDelegationDepth {
		return "", fmt.Errorf("delegation depth limit (%d) exceeded", MaxDelegationDepth)
	}

	taskID := uuid.New().String()
	if _, err := launchBackgroundTask(ctx, agents, tracker, delegator, taskID, agentName, goal, time.Time{}); err != nil {
		return "", err
	}
	return taskID, nil
}

func buildTaskResponse(task *Task) map[string]string {
	resp := map[string]string{
		"task_id": task.ID,
		"agent":   task.AgentName,
		"status":  string(task.Status),
	}
	switch task.Status {
	case TaskCompleted:
		resp["result"] = task.Result
	case TaskCancelled, TaskFailed:
		resp["error"] = task.Error
	}
	return resp
}

func runAgent(ctx context.Context, agents *Registry, delegator Delegator, agentName, task string) (*loop.SessionResult, error) {
	cfg, ok := agents.Get(agentName)
	if !ok {
		availableNames := make([]string, 0)
		for _, a := range agents.List() {
			availableNames = append(availableNames, a.Name)
		}
		return nil, fmt.Errorf("agent %q not found (available: %s)", agentName, strings.Join(availableNames, ", "))
	}

	depth := Depth(ctx)
	if depth >= MaxDelegationDepth {
		return nil, fmt.Errorf("delegation depth limit (%d) exceeded", MaxDelegationDepth)
	}

	childCtx := WithDepth(ctx, depth+1)

	scopedTools := tool.Scoped(delegator.ToolRegistry(), cfg.Tools)

	sess, err := delegator.NewSession(childCtx, session.SessionConfig{
		Goal:         task,
		Mode:         "delegated",
		TrustLevel:   cfg.TrustLevel,
		SystemPrompt: cfg.SystemPrompt,
		MaxSteps:     cfg.MaxSteps,
	})
	if err != nil {
		return nil, fmt.Errorf("create session for agent %q: %w", agentName, err)
	}

	sess.AppendMessage(port.Message{
		Role:    port.RoleUser,
		Content: task,
	})

	result, err := delegator.RunWithTools(childCtx, sess, scopedTools)
	if err != nil {
		return nil, fmt.Errorf("agent %q execution failed: %w", agentName, err)
	}

	return result, nil
}

func launchBackgroundTask(
	ctx context.Context,
	agents *Registry,
	tracker *TaskTracker,
	delegator Delegator,
	taskID, agentName, goal string,
	createdAt time.Time,
) (*Task, error) {
	task := &Task{
		ID:        taskID,
		AgentName: agentName,
		Goal:      goal,
		Status:    TaskRunning,
		CreatedAt: createdAt,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	revision := tracker.Start(task, cancel)

	go func(rev int64) {
		result, err := runAgent(taskCtx, agents, delegator, agentName, goal)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				tracker.CancelIf(taskID, rev, err.Error())
				return
			}
			tracker.FailIf(taskID, rev, err.Error())
			return
		}
		tracker.CompleteIf(taskID, rev, result.Output, result.TokensUsed)
	}(revision)

	updated, _ := tracker.Get(taskID)
	return updated, nil
}

func updateBackgroundTask(
	ctx context.Context,
	agents *Registry,
	tracker *TaskTracker,
	delegator Delegator,
	taskID string,
	update string,
) (*Task, error) {
	task, ok := tracker.Get(taskID)
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}
	if task.Status != TaskRunning {
		return nil, fmt.Errorf("task %q is not running", taskID)
	}
	newGoal := strings.TrimSpace(task.Goal + "\n\nFollow-up update: " + strings.TrimSpace(update))
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	tracker.CancelIf(taskID, task.Revision, "restarted with updated instructions")
	return launchBackgroundTask(ctx, agents, tracker, delegator, taskID, task.AgentName, newGoal, createdAt)
}
