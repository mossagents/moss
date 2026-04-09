package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	taskrt "github.com/mossagents/moss/kernel/task"
	"github.com/mossagents/moss/kernel/tool"
	kws "github.com/mossagents/moss/kernel/workspace"
)

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
	spec = agentToolSpec(spec)
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
	spec = agentToolSpec(spec)
	handler := func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in claimTaskInput
		if err := unmarshalOpt(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
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
	spec = agentToolSpec(spec)
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
	spec = agentToolSpec(spec)
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
	spec = agentToolSpec(spec)
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
	spec = agentToolSpec(spec)
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
