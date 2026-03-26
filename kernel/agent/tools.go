package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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

// RegisterTools 向工具注册表注册 delegate_agent、spawn_agent、query_agent 三个工具。
func RegisterTools(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	if err := registerDelegate(reg, agents, delegator); err != nil {
		return err
	}
	if err := registerSpawn(reg, agents, tracker, delegator); err != nil {
		return err
	}
	return registerQuery(reg, tracker)
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

		// 预检查：Agent 存在 + 深度限制
		if _, ok := agents.Get(in.Agent); !ok {
			return nil, fmt.Errorf("agent %q not found", in.Agent)
		}
		depth := Depth(ctx)
		if depth >= MaxDelegationDepth {
			return nil, fmt.Errorf("delegation depth limit (%d) exceeded", MaxDelegationDepth)
		}

		taskID := uuid.New().String()
		task := &Task{
			ID:        taskID,
			AgentName: in.Agent,
			Goal:      in.Task,
			Status:    TaskRunning,
		}
		tracker.Add(task)

		// 后台执行
		go func() {
			result, err := runAgent(ctx, agents, delegator, in.Agent, in.Task)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					tracker.Cancel(taskID, err.Error())
					return
				}
				tracker.Fail(taskID, err.Error())
				return
			}
			tracker.Complete(taskID, result.Output, result.TokensUsed)
		}()

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

		resp := map[string]string{
			"task_id": task.ID,
			"agent":   task.AgentName,
			"status":  string(task.Status),
		}
		switch task.Status {
		case TaskCompleted:
			resp["result"] = task.Result
		case TaskCancelled:
			resp["error"] = task.Error
		case TaskFailed:
			resp["error"] = task.Error
		}

		return json.Marshal(resp)
	}

	return reg.Register(spec, handler)
}

// ── 公共执行逻辑 ─────────────────────────────────────

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
