package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/loop"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	kws "github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/logging"
	"strings"
	"time"
)

// Delegator 是 agent 包对 Kernel 能力的抽象，避免循环依赖。
// 由 Kernel 实现。
type Delegator interface {
	NewSession(ctx context.Context, cfg session.SessionConfig) (*session.Session, error)
	RunWithTools(ctx context.Context, sess *session.Session, tools tool.Registry) (*loop.SessionResult, error)
	ToolRegistry() tool.Registry
}

// RuntimeDeps 描述委派工具可选依赖。
type RuntimeDeps struct {
	TaskRuntime taskrt.TaskRuntime
	Mailbox     taskrt.Mailbox
	Isolation   kws.WorkspaceIsolation
}

// RegisterTools 向工具注册表注册委派相关工具。
func RegisterTools(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	return RegisterToolsWithDeps(reg, agents, tracker, delegator, RuntimeDeps{})
}

// RegisterToolsWithDeps 向工具注册表注册委派与协作相关工具。
func RegisterToolsWithDeps(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator, deps RuntimeDeps) error {
	if err := registerDelegate(reg, agents, delegator); err != nil {
		return err
	}
	if err := registerSpawn(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if err := registerQuery(reg, tracker); err != nil {
		return err
	}
	if err := registerReadAgentTool(reg, tracker); err != nil {
		return err
	}
	if err := registerListAgentsTool(reg, tracker); err != nil {
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
	if err := registerWriteAgentTool(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if err := registerWaitAgentTool(reg, tracker); err != nil {
		return err
	}
	if err := registerCloseAgentTool(reg, tracker); err != nil {
		return err
	}
	if err := registerResumeAgentTool(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if err := registerTask(reg, agents, tracker, delegator); err != nil {
		return err
	}
	if deps.TaskRuntime != nil {
		if err := registerPlanTask(reg, deps.TaskRuntime); err != nil {
			return err
		}
		if err := registerClaimTask(reg, deps.TaskRuntime); err != nil {
			return err
		}
	}
	if deps.Mailbox != nil {
		if err := registerSendMail(reg, deps.Mailbox); err != nil {
			return err
		}
		if err := registerReadMailbox(reg, deps.Mailbox); err != nil {
			return err
		}
	}
	if deps.Isolation != nil {
		if err := registerAcquireWorkspace(reg, deps.Isolation, deps.TaskRuntime); err != nil {
			return err
		}
		if err := registerReleaseWorkspace(reg, deps.Isolation); err != nil {
			return err
		}
	}
	return nil
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

		result, err := runAgent(ctx, agents, nil, "", 0, delegator, in.Agent, in.Task)
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
	return reg.Register(spec, handler)
}

type listAgentsInput struct {
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
				"status":{"type":"string","description":"可选: running|completed|failed|cancelled"},
				"agent":{"type":"string","description":"可选: 按 agent 名称过滤"},
				"limit":{"type":"integer","description":"可选: 最大返回条数（默认20，最大100）"}
			}
		}`),
		Risk: tool.RiskLow,
	}
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in listAgentsInput
		if len(strings.TrimSpace(string(input))) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return nil, fmt.Errorf("parse input: %w", err)
			}
		}
		filter := TaskFilter{AgentName: strings.TrimSpace(in.Agent)}
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

func registerWriteAgentTool(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
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
			return json.Marshal(map[string]any{
				"target": task.ID,
				"status": "queued",
				"note":   "queue-only mode is not yet enabled in runtime; message accepted without immediate execution",
			})
		}
		interrupt := true
		if in.Interrupt != nil {
			interrupt = *in.Interrupt
		}
		if isActiveTask(task) && !interrupt {
			return json.Marshal(map[string]any{
				"target": task.ID,
				"status": "queued",
				"note":   "task is running and interrupt=false; message queued",
			})
		}
		updated, err := triggerAgentTurn(ctx, agents, tracker, delegator, task, msg)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"target":      updated.ID,
			"task_id":     updated.ID,
			"agent":       updated.AgentName,
			"status":      updated.Status,
			"revision":    updated.Revision,
			"session_id":  updated.SessionID,
			"job_id":      updated.JobID,
			"job_item_id": updated.JobItemID,
			"triggered":   true,
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
				"poll_millis":{"type":"integer","description":"轮询间隔毫秒，默认250，最小50"}
			},
			"required":["target"]
		}`),
		Risk: tool.RiskLow,
	}
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

func registerResumeAgentTool(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
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
		updated, err := triggerAgentTurn(ctx, agents, tracker, delegator, task, message)
		if err != nil {
			return nil, err
		}
		resp := buildTaskResponse(updated)
		resp["target"] = updated.ID
		resp["resumed"] = true
		resp["completed"] = isTerminalTaskStatus(updated.Status)
		if isRecoverableTask(task) {
			resp["note"] = "task was recovered from persisted runtime state and restarted"
		}
		return json.Marshal(resp)
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
			result, err := runAgent(ctx, agents, nil, "", 0, delegator, in.Agent, in.Task)
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
	logging.GetLogger().DebugContext(ctx, "background task requested",
		"task_id", taskID,
		"agent", agentName,
		"parent_session_id", SessionID(ctx),
	)
	if _, err := launchBackgroundTask(ctx, agents, tracker, delegator, taskID, agentName, goal, time.Time{}); err != nil {
		return "", err
	}
	return taskID, nil
}

func buildTaskResponse(task *Task) map[string]any {
	resp := map[string]any{
		"task_id":     task.ID,
		"agent":       task.AgentName,
		"status":      task.Status,
		"revision":    task.Revision,
		"active":      task.Active,
		"recoverable": isRecoverableTask(task),
	}
	if task.SessionID != "" {
		resp["session_id"] = task.SessionID
	}
	if task.JobID != "" {
		resp["job_id"] = task.JobID
	}
	if task.JobItemID != "" {
		resp["job_item_id"] = task.JobItemID
	}
	switch task.Status {
	case TaskCompleted:
		resp["result"] = task.Result
	case TaskCancelled, TaskFailed:
		resp["error"] = task.Error
	}
	return resp
}

func resolveTaskTarget(tracker *TaskTracker, target string) (*Task, error) {
	name := strings.TrimSpace(target)
	if name == "" {
		return nil, fmt.Errorf("target is required")
	}
	if task, ok := tracker.Get(name); ok {
		return task, nil
	}
	normalized := strings.TrimPrefix(name, "agent:")
	filter := TaskFilter{AgentName: normalized}
	candidates := tracker.List(filter)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("target %q not found", target)
	}
	return candidates[0], nil
}

func runAgent(ctx context.Context, agents *Registry, tracker *TaskTracker, taskID string, revision int64, delegator Delegator, agentName, task string) (*loop.SessionResult, error) {
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

	parentSessionID := SessionID(ctx)
	childCtx := WithDepth(ctx, depth+1)
	logging.GetLogger().DebugContext(ctx, "delegated agent starting",
		"agent", agentName,
		"task_id", taskID,
		"parent_session_id", parentSessionID,
		"depth", depth+1,
	)

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
	session.SetThreadSource(sess, "delegated")
	session.SetThreadParent(sess, parentSessionID)
	session.SetThreadTaskID(sess, taskID)
	session.RefreshThreadMetadata(sess, time.Now().UTC(), "delegated")
	if tracker != nil {
		tracker.BindSession(taskID, revision, sess.ID)
	}
	logging.GetLogger().DebugContext(ctx, "delegated session created",
		"agent", agentName,
		"task_id", taskID,
		"session_id", sess.ID,
		"parent_session_id", parentSessionID,
	)

	sess.AppendMessage(mdl.Message{
		Role:         mdl.RoleUser,
		ContentParts: []mdl.ContentPart{mdl.TextPart(task)},
	})

	result, err := delegator.RunWithTools(WithSessionID(childCtx, sess.ID), sess, scopedTools)
	if err != nil {
		logging.GetLogger().DebugContext(ctx, "delegated agent failed",
			"agent", agentName,
			"task_id", taskID,
			"session_id", sess.ID,
			"error", err.Error(),
		)
		return nil, fmt.Errorf("agent %q execution failed: %w", agentName, err)
	}
	logging.GetLogger().DebugContext(ctx, "delegated agent completed",
		"agent", agentName,
		"task_id", taskID,
		"session_id", sess.ID,
		"steps", result.Steps,
		"tokens", result.TokensUsed.TotalTokens,
	)

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
		ID:              taskID,
		AgentName:       agentName,
		Goal:            goal,
		Status:          TaskRunning,
		ParentSessionID: SessionID(ctx),
		CreatedAt:       createdAt,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	revision := tracker.Start(task, cancel)

	go func(rev int64) {
		result, err := runAgent(WithSessionID(taskCtx, task.ParentSessionID), agents, tracker, taskID, rev, delegator, agentName, goal)
		if err != nil {
			logging.GetLogger().DebugContext(taskCtx, "background task failed",
				"task_id", taskID,
				"agent", agentName,
				"error", err.Error(),
			)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				tracker.CancelIf(taskID, rev, err.Error())
				return
			}
			tracker.FailIf(taskID, rev, err.Error())
			return
		}
		logging.GetLogger().DebugContext(taskCtx, "background task completed",
			"task_id", taskID,
			"agent", agentName,
			"tokens", result.TokensUsed.TotalTokens,
		)
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
	newGoal := mergeTaskGoal(task.Goal, update)
	createdAt := task.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if !isActiveTask(task) {
		return launchBackgroundTask(ctx, agents, tracker, delegator, taskID, task.AgentName, newGoal, createdAt)
	}
	tracker.CancelIf(taskID, task.Revision, "restarted with updated instructions")
	return launchBackgroundTask(ctx, agents, tracker, delegator, taskID, task.AgentName, newGoal, createdAt)
}

func triggerAgentTurn(
	ctx context.Context,
	agents *Registry,
	tracker *TaskTracker,
	delegator Delegator,
	task *Task,
	message string,
) (*Task, error) {
	switch task.Status {
	case TaskRunning:
		if !isActiveTask(task) {
			createdAt := task.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now()
			}
			return launchBackgroundTask(ctx, agents, tracker, delegator, task.ID, task.AgentName, mergeTaskGoal(task.Goal, message), createdAt)
		}
		return updateBackgroundTask(ctx, agents, tracker, delegator, task.ID, message)
	case TaskCompleted, TaskFailed, TaskCancelled:
		newGoal := mergeTaskGoal(task.Goal, message)
		createdAt := task.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		return launchBackgroundTask(ctx, agents, tracker, delegator, task.ID, task.AgentName, newGoal, createdAt)
	default:
		return nil, fmt.Errorf("task %q is not in a triggerable state", task.ID)
	}
}

func isTerminalTaskStatus(status TaskStatus) bool {
	return status == TaskCompleted || status == TaskFailed || status == TaskCancelled
}

func mergeTaskGoal(goal string, update string) string {
	goal = strings.TrimSpace(goal)
	update = strings.TrimSpace(update)
	if update == "" {
		return goal
	}
	if goal == "" {
		return "Follow-up update: " + update
	}
	return goal + "\n\nFollow-up update: " + update
}

type planTaskInput struct {
	ID        string   `json:"id"`
	Agent     string   `json:"agent"`
	Goal      string   `json:"goal"`
	DependsOn []string `json:"depends_on"`
}

func registerPlanTask(reg tool.Registry, runtime taskrt.TaskRuntime) error {
	spec := tool.ToolSpec{
		Name:        "plan_task",
		Description: "Create or update a pending task node in task runtime graph, with optional dependencies.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"id":{"type":"string","description":"Task id"},
				"agent":{"type":"string","description":"Preferred agent"},
				"goal":{"type":"string","description":"Task goal"},
				"depends_on":{"type":"array","items":{"type":"string"},"description":"Dependency task ids"}
			},
			"required":["id","goal"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"planning", "delegation"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in planTaskInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		id := strings.TrimSpace(in.ID)
		if id == "" {
			return nil, fmt.Errorf("id is required")
		}
		goal := strings.TrimSpace(in.Goal)
		if goal == "" {
			return nil, fmt.Errorf("goal is required")
		}
		record := taskrt.TaskRecord{
			ID:        id,
			AgentName: strings.TrimSpace(in.Agent),
			Goal:      goal,
			Status:    taskrt.TaskPending,
			DependsOn: sanitizeDepends(in.DependsOn),
		}
		if old, err := runtime.GetTask(ctx, id); err == nil && old != nil {
			record.Status = old.Status
			record.ClaimedBy = old.ClaimedBy
			record.WorkspaceID = old.WorkspaceID
			record.CreatedAt = old.CreatedAt
		}
		if err := runtime.UpsertTask(ctx, record); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status": "planned",
			"task":   record,
		})
	}
	return reg.Register(spec, handler)
}

type claimTaskInput struct {
	Claimer string `json:"claimer"`
	Agent   string `json:"agent"`
}

func registerClaimTask(reg tool.Registry, runtime taskrt.TaskRuntime) error {
	spec := tool.ToolSpec{
		Name:        "claim_task",
		Description: "Claim next ready pending task whose dependencies are completed.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"claimer":{"type":"string","description":"claimer identity"},
				"agent":{"type":"string","description":"optional preferred agent name"}
			}
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"planning", "delegation"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in claimTaskInput
		if len(strings.TrimSpace(string(input))) > 0 {
			if err := json.Unmarshal(input, &in); err != nil {
				return nil, fmt.Errorf("parse input: %w", err)
			}
		}
		claimer := strings.TrimSpace(in.Claimer)
		if claimer == "" {
			claimer = "anonymous"
		}
		task, err := runtime.ClaimNextReady(ctx, claimer, strings.TrimSpace(in.Agent))
		if err != nil {
			if errors.Is(err, taskrt.ErrNoReadyTask) {
				return json.Marshal(map[string]any{
					"status": "empty",
					"task":   nil,
				})
			}
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status": "claimed",
			"task":   task,
		})
	}
	return reg.Register(spec, handler)
}

type sendMailInput struct {
	To        string         `json:"to"`
	Subject   string         `json:"subject"`
	Content   string         `json:"content"`
	RequestID string         `json:"request_id"`
	InReplyTo string         `json:"in_reply_to"`
	From      string         `json:"from"`
	Metadata  map[string]any `json:"metadata"`
}

func registerSendMail(reg tool.Registry, mailbox taskrt.Mailbox) error {
	spec := tool.ToolSpec{
		Name:        "send_mail",
		Description: "Send an async mailbox message to another agent.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"from":{"type":"string","description":"sender identity"},
				"to":{"type":"string","description":"receiver mailbox owner"},
				"subject":{"type":"string"},
				"content":{"type":"string"},
				"request_id":{"type":"string"},
				"in_reply_to":{"type":"string"},
				"metadata":{"type":"object"}
			},
			"required":["to","content"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"communication", "delegation"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in sendMailInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		to := strings.TrimSpace(in.To)
		content := strings.TrimSpace(in.Content)
		if to == "" || content == "" {
			return nil, fmt.Errorf("to and content are required")
		}
		msgID, err := mailbox.Send(ctx, taskrt.MailMessage{
			From:      strings.TrimSpace(in.From),
			To:        to,
			Subject:   strings.TrimSpace(in.Subject),
			Content:   content,
			RequestID: strings.TrimSpace(in.RequestID),
			InReplyTo: strings.TrimSpace(in.InReplyTo),
			Metadata:  in.Metadata,
		})
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status": "sent",
			"id":     msgID,
			"to":     to,
		})
	}
	return reg.Register(spec, handler)
}

type readMailboxInput struct {
	Owner string `json:"owner"`
	Limit int    `json:"limit"`
}

func registerReadMailbox(reg tool.Registry, mailbox taskrt.Mailbox) error {
	spec := tool.ToolSpec{
		Name:        "read_mailbox",
		Description: "Read pending mailbox messages for an owner (dequeue).",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"owner":{"type":"string","description":"mailbox owner"},
				"limit":{"type":"integer","description":"max messages to read"}
			},
			"required":["owner"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"communication", "delegation"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in readMailboxInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		owner := strings.TrimSpace(in.Owner)
		if owner == "" {
			return nil, fmt.Errorf("owner is required")
		}
		msgs, err := mailbox.Read(ctx, owner, in.Limit)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"owner": owner,
			"count": len(msgs),
			"mail":  msgs,
		})
	}
	return reg.Register(spec, handler)
}

type acquireWorkspaceInput struct {
	TaskID string `json:"task_id"`
}

func registerAcquireWorkspace(reg tool.Registry, isolation kws.WorkspaceIsolation, runtime taskrt.TaskRuntime) error {
	spec := tool.ToolSpec{
		Name:        "acquire_workspace",
		Description: "Acquire an isolated workspace for a task and return workspace id.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"task_id":{"type":"string","description":"task id"}
			},
			"required":["task_id"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"workspace", "delegation"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in acquireWorkspaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		taskID := strings.TrimSpace(in.TaskID)
		if taskID == "" {
			return nil, fmt.Errorf("task_id is required")
		}
		lease, err := isolation.Acquire(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if runtime != nil {
			record, gerr := runtime.GetTask(ctx, taskID)
			if gerr == nil && record != nil {
				record.WorkspaceID = lease.WorkspaceID
				_ = runtime.UpsertTask(ctx, *record)
			}
		}
		return json.Marshal(map[string]any{
			"task_id":      taskID,
			"workspace_id": lease.WorkspaceID,
			"status":       "acquired",
		})
	}
	return reg.Register(spec, handler)
}

type releaseWorkspaceInput struct {
	WorkspaceID string `json:"workspace_id"`
}

func registerReleaseWorkspace(reg tool.Registry, isolation kws.WorkspaceIsolation) error {
	spec := tool.ToolSpec{
		Name:        "release_workspace",
		Description: "Release an isolated workspace lease.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"workspace_id":{"type":"string","description":"workspace id"}
			},
			"required":["workspace_id"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"workspace", "delegation"},
	}
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in releaseWorkspaceInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		workspaceID := strings.TrimSpace(in.WorkspaceID)
		if workspaceID == "" {
			return nil, fmt.Errorf("workspace_id is required")
		}
		if err := isolation.Release(ctx, workspaceID); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"workspace_id": workspaceID,
			"status":       "released",
		})
	}
	return reg.Register(spec, handler)
}

func sanitizeDepends(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, dep := range in {
		id := strings.TrimSpace(dep)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
