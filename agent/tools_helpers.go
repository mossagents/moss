package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/loop"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/logging"
)

// ── 公共执行逻辑 ─────────────────────────────────────

const defaultQueuedConsumeLimit = 8

func startBackgroundTask(ctx context.Context, agents *Registry, tracker *TaskTracker, delegator Delegator, agentName, goal string, contract taskrt.TaskContract) (string, error) {
	if _, ok := agents.Get(agentName); !ok {
		return "", fmt.Errorf("agent %q not found", agentName)
	}
	depth := Depth(ctx)
	if depth >= MaxDepth(ctx) {
		return "", fmt.Errorf("delegation depth limit (%d) exceeded", MaxDepth(ctx))
	}

	taskID := uuid.New().String()
	logging.GetLogger().DebugContext(ctx, "background task requested",
		"task_id", taskID,
		"agent", agentName,
		"parent_session_id", SessionID(ctx),
	)
	if _, err := launchBackgroundTask(ctx, agents, tracker, delegator, taskID, agentName, goal, contract, time.Time{}); err != nil {
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
	if task.Contract.TaskID != "" || len(task.Contract.AllowedEffects) > 0 || task.Contract.ApprovalCeiling != "" || task.Contract.Budget.MaxSteps > 0 || task.Contract.Budget.MaxTokens > 0 || task.Contract.Budget.TimeoutSec > 0 {
		resp["contract"] = task.Contract
	}
	resp["supervisor_artifact"] = buildSupervisorArtifact(task)
	switch task.Status {
	case TaskCompleted:
		resp["result"] = task.Result
	case TaskCancelled, TaskFailed:
		resp["error"] = task.Error
	}
	return resp
}

func buildSupervisorArtifact(task *Task) map[string]any {
	artifact := map[string]any{
		"task_id": task.ID,
		"agent":   task.AgentName,
		"status":  task.Status,
		"summary": summarizeSupervisorResult(task),
		"structured_result": map[string]any{
			"task_id":           task.ID,
			"agent_name":        task.AgentName,
			"status":            task.Status,
			"session_id":        task.SessionID,
			"parent_session_id": task.ParentSessionID,
			"job_id":            task.JobID,
			"job_item_id":       task.JobItemID,
			"result":            task.Result,
			"error":             task.Error,
			"contract":          task.Contract,
		},
	}
	return artifact
}

func summarizeSupervisorResult(task *Task) string {
	switch task.Status {
	case TaskCompleted:
		if strings.TrimSpace(task.Result) != "" {
			return strings.TrimSpace(task.Result)
		}
		return "task completed"
	case TaskFailed, TaskCancelled:
		if strings.TrimSpace(task.Error) != "" {
			return strings.TrimSpace(task.Error)
		}
		return "task ended without a result"
	default:
		return "task is running under supervisor control"
	}
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
	if len(candidates) > 1 {
		return nil, fmt.Errorf("target %q is ambiguous: %d tasks found for agent %q; please specify task_id", target, len(candidates), normalized)
	}
	return candidates[0], nil
}

func enqueueTaskMessage(ctx context.Context, runtime taskrt.TaskRuntime, taskID string, message string) (*taskrt.TaskMessage, error) {
	queue, ok := runtime.(taskrt.TaskMessageRuntime)
	if !ok || queue == nil {
		return nil, fmt.Errorf("queued delivery requires a task runtime with task message persistence")
	}
	queued, err := queue.EnqueueTaskMessage(ctx, taskrt.TaskMessage{
		TaskID:  taskID,
		Content: message,
	})
	if err != nil {
		return nil, err
	}
	return queued, nil
}

func consumeQueuedMessages(ctx context.Context, runtime taskrt.TaskRuntime, taskID string, message string) (string, queueMetrics, error) {
	queue, ok := runtime.(taskrt.TaskMessageRuntime)
	if !ok || queue == nil {
		return strings.TrimSpace(message), queueMetrics{}, nil
	}
	queued, err := queue.ConsumeTaskMessages(ctx, taskID, defaultQueuedConsumeLimit)
	if err != nil {
		if errors.Is(err, taskrt.ErrTaskNotFound) {
			return "", queueMetrics{}, err
		}
		return "", queueMetrics{}, fmt.Errorf("consume queued messages: %w", err)
	}
	remaining, err := queue.ListTaskMessages(ctx, taskID, 0)
	if err != nil {
		return "", queueMetrics{}, fmt.Errorf("list remaining queued messages: %w", err)
	}
	metrics := queueMetrics{ConsumedCount: len(queued), RemainingCount: len(remaining)}
	parts := make([]string, 0, len(queued)+1)
	for _, item := range queued {
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}
	if current := strings.TrimSpace(message); current != "" {
		parts = append(parts, current)
	}
	if len(parts) == 0 {
		return "", metrics, nil
	}
	if len(parts) == 1 {
		return parts[0], metrics, nil
	}
	return strings.Join(parts, "\n\n"), metrics, nil
}

func countQueuedMessages(ctx context.Context, runtime taskrt.TaskRuntime, taskID string) (int, bool, error) {
	queue, ok := runtime.(taskrt.TaskMessageRuntime)
	if !ok || queue == nil {
		return 0, false, nil
	}
	items, err := queue.ListTaskMessages(ctx, taskID, 0)
	if err != nil {
		return 0, true, err
	}
	return len(items), true, nil
}

func runAgent(ctx context.Context, agents *Registry, tracker *TaskTracker, taskID string, revision int64, delegator Delegator, agentName, task string, requestedContract taskrt.TaskContract) (*loop.SessionResult, error) {
	cfg, ok := agents.Get(agentName)
	if !ok {
		var availableNames []string
		for _, a := range agents.List() {
			availableNames = append(availableNames, a.Name)
		}
		return nil, fmt.Errorf("agent %q not found (available: %s)", agentName, strings.Join(availableNames, ", "))
	}

	depth := Depth(ctx)
	if depth >= MaxDepth(ctx) {
		return nil, fmt.Errorf("delegation depth limit (%d) exceeded", MaxDepth(ctx))
	}

	parentSessionID := SessionID(ctx)
	childCtx := WithDepth(ctx, depth+1)
	logging.GetLogger().DebugContext(ctx, "delegated agent starting",
		"agent", agentName,
		"task_id", taskID,
		"parent_session_id", parentSessionID,
		"depth", depth+1,
	)

	baseTools := tool.Scoped(delegator.ToolRegistry(), cfg.Tools)
	contract := normalizeTaskContract(requestedContract, firstNonEmpty(taskID, requestedContract.TaskID), task, baseTools, cfg)
	scopedTools := withTaskContract(baseTools, contract)

	sessionPrompt := strings.TrimSpace(cfg.SystemPrompt)
	contractPrompt := strings.TrimSpace(renderTaskContractPrompt(contract))
	if contractPrompt != "" {
		if sessionPrompt == "" {
			sessionPrompt = contractPrompt
		} else {
			sessionPrompt += "\n\n" + contractPrompt
		}
	}
	metadata := map[string]any{"child_task_contract": contract}
	maxSteps := cfg.MaxSteps
	if contract.Budget.MaxSteps > 0 {
		maxSteps = contract.Budget.MaxSteps
	}
	maxTokens := 0
	if contract.Budget.MaxTokens > 0 {
		maxTokens = contract.Budget.MaxTokens
	}
	runCtx, cancel := applyContractTimeout(childCtx, contract)
	defer cancel()

	sess, err := delegator.NewSession(runCtx, session.SessionConfig{
		Goal:         task,
		Mode:         "delegated",
		TrustLevel:   cfg.TrustLevel,
		SystemPrompt: sessionPrompt,
		MaxSteps:     maxSteps,
		MaxTokens:    maxTokens,
		Metadata:     metadata,
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

	result, err := delegator.RunWithTools(WithSessionID(runCtx, sess.ID), sess, scopedTools)
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
	contract taskrt.TaskContract,
	createdAt time.Time,
) (*Task, error) {
	cfg, ok := agents.Get(agentName)
	if !ok {
		return nil, fmt.Errorf("agent %q not found", agentName)
	}
	contract = normalizeTaskContract(contract, taskID, goal, tool.Scoped(delegator.ToolRegistry(), cfg.Tools), cfg)
	task := &Task{
		ID:              taskID,
		AgentName:       agentName,
		Goal:            goal,
		Status:          TaskRunning,
		Contract:        contract,
		ParentSessionID: SessionID(ctx),
		CreatedAt:       createdAt,
	}
	taskCtx, cancel := context.WithCancel(ctx)
	revision := tracker.Start(task, cancel)

	go func(rev int64) {
		result, err := runAgent(WithSessionID(taskCtx, task.ParentSessionID), agents, tracker, taskID, rev, delegator, agentName, goal, contract)
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
		return launchBackgroundTask(ctx, agents, tracker, delegator, taskID, task.AgentName, newGoal, task.Contract, createdAt)
	}
	tracker.CancelIf(taskID, task.Revision, "restarted with updated instructions")
	return launchBackgroundTask(ctx, agents, tracker, delegator, taskID, task.AgentName, newGoal, task.Contract, createdAt)
}

func triggerAgentTurn(
	ctx context.Context,
	agents *Registry,
	tracker *TaskTracker,
	delegator Delegator,
	runtime taskrt.TaskRuntime,
	task *Task,
	message string,
) (*Task, queueMetrics, error) {
	mergedMessage, metrics, err := consumeQueuedMessages(ctx, runtime, task.ID, message)
	if err != nil {
		return nil, queueMetrics{}, err
	}
	switch task.Status {
	case TaskRunning:
		if !isActiveTask(task) {
			createdAt := task.CreatedAt
			if createdAt.IsZero() {
				createdAt = time.Now()
			}
			updated, err := launchBackgroundTask(ctx, agents, tracker, delegator, task.ID, task.AgentName, mergeTaskGoal(task.Goal, mergedMessage), task.Contract, createdAt)
			return updated, metrics, err
		}
		updated, err := updateBackgroundTask(ctx, agents, tracker, delegator, task.ID, mergedMessage)
		return updated, metrics, err
	case TaskCompleted, TaskFailed, TaskCancelled:
		newGoal := mergeTaskGoal(task.Goal, mergedMessage)
		createdAt := task.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now()
		}
		updated, err := launchBackgroundTask(ctx, agents, tracker, delegator, task.ID, task.AgentName, newGoal, task.Contract, createdAt)
		return updated, metrics, err
	default:
		return nil, queueMetrics{}, fmt.Errorf("task %q is not in a triggerable state", task.ID)
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
