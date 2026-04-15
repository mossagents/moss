package agent

import (
	"context"
	"encoding/json"
	"fmt"

	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
)

// ── delegate_agent (同步) ─────────────────────────────

type delegateInput struct {
	Agent    string              `json:"agent"`
	Task     string              `json:"task"`
	Contract taskrt.TaskContract `json:"contract"`
}

func registerDelegate(reg tool.Registry, agents *Registry, delegator Delegator) error {
	spec := tool.ToolSpec{
		Name:        "delegate_agent",
		Description: "委派任务给另一个专业 Agent 并同步等待结果返回。用于需要特定专业能力的子任务。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"agent": {"type": "string", "description": "目标 Agent 名称"},
				"task":  {"type": "string", "description": "要委派的具体任务描述"},
				"contract": {"type":"object","description":"可选: 子任务资源合同","properties":{
					"input_context":{"type":"string"},
					"budget":{"type":"object","properties":{"max_steps":{"type":"integer"},"max_tokens":{"type":"integer"},"timeout_sec":{"type":"integer"}}},
					"approval_ceiling":{"type":"string","enum":["none","policy_guarded","explicit_user_approval","supervisor_only"]},
					"writable_scopes":{"type":"array","items":{"type":"string"}},
					"memory_scope":{"type":"string"},
					"allowed_effects":{"type":"array","items":{"type":"string","enum":["read_only","writes_workspace","writes_memory","external_side_effect","graph_mutation"]}},
					"return_artifacts":{"type":"array","items":{"type":"string"}}
				}}
			},
			"required": ["agent", "task"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in delegateInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}

		result, err := runAgent(ctx, agents, nil, "", 0, delegator, in.Agent, in.Task, in.Contract)
		if err != nil {
			return nil, err
		}

		return json.Marshal(map[string]any{
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
	}

	return reg.Register(tool.NewRawTool(spec, handler))
}

// ── spawn_agent (异步) ────────────────────────────────

type spawnInput struct {
	Agent    string              `json:"agent"`
	Task     string              `json:"task"`
	Contract taskrt.TaskContract `json:"contract"`
}

func registerSpawn(reg tool.Registry, agents *Registry, tracker *TaskTracker, delegator Delegator) error {
	spec := tool.ToolSpec{
		Name:        "spawn_agent",
		Description: "在后台启动一个 Agent 执行任务，立即返回任务 ID。用 query_agent 检查进度和结果。",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"agent": {"type": "string", "description": "目标 Agent 名称"},
				"task":  {"type": "string", "description": "任务描述"},
				"contract": {"type":"object","description":"可选: 子任务资源合同","properties":{
					"input_context":{"type":"string"},
					"budget":{"type":"object","properties":{"max_steps":{"type":"integer"},"max_tokens":{"type":"integer"},"timeout_sec":{"type":"integer"}}},
					"approval_ceiling":{"type":"string","enum":["none","policy_guarded","explicit_user_approval","supervisor_only"]},
					"writable_scopes":{"type":"array","items":{"type":"string"}},
					"memory_scope":{"type":"string"},
					"allowed_effects":{"type":"array","items":{"type":"string","enum":["read_only","writes_workspace","writes_memory","external_side_effect","graph_mutation"]}},
					"return_artifacts":{"type":"array","items":{"type":"string"}}
				}}
			},
			"required": ["agent", "task"]
		}`),
		Risk:         tool.RiskMedium,
		Capabilities: []string{"delegation"},
	}
	spec = agentToolSpec(spec)

	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in spawnInput
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		taskID, err := startBackgroundTask(ctx, agents, tracker, delegator, in.Agent, in.Task, in.Contract)
		if err != nil {
			return nil, err
		}

		return json.Marshal(map[string]string{
			"task_id": taskID,
			"status":  "running",
		})
	}

	return reg.Register(tool.NewRawTool(spec, handler))
}
