package builtintools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

var applyPatchSpec = tool.ToolSpec{
	Name:        "apply_patch",
	Description: "Apply a unified diff patch to the workspace using the runtime patch service.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"patch": {"type": "string", "description": "Unified diff patch to apply"},
			"three_way": {"type": "boolean", "description": "Attempt a 3-way apply when possible"},
			"cached": {"type": "boolean", "description": "Apply to the index only"},
			"source": {"type": "string", "description": "Patch source label (tool, llm, user)"}
		},
		"required": ["patch"]
	}`),
	Risk:         tool.RiskHigh,
	Capabilities: []string{"filesystem", "patch"},
}

func applyPatchHandlerPort(applier workspace.PatchApply) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if applier == nil {
			return nil, workspace.ErrPatchApplyUnavailable
		}
		var params struct {
			Patch    string `json:"patch"`
			ThreeWay bool   `json:"three_way"`
			Cached   bool   `json:"cached"`
			Source   string `json:"source"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		req := workspace.PatchApplyRequest{
			Patch:    params.Patch,
			ThreeWay: params.ThreeWay,
			Cached:   params.Cached,
			Source:   workspace.PatchSourceTool,
		}
		switch strings.TrimSpace(strings.ToLower(params.Source)) {
		case "", string(workspace.PatchSourceTool):
		case string(workspace.PatchSourceLLM):
			req.Source = workspace.PatchSourceLLM
		case string(workspace.PatchSourceUser):
			req.Source = workspace.PatchSourceUser
		default:
			return nil, fmt.Errorf("unsupported patch source %q", params.Source)
		}
		result, err := applier.Apply(ctx, req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(result)
	}
}
