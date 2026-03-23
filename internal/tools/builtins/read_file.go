package builtins

import (
	"context"

	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

type ReadFileTool struct {
	ws *workspace.Manager
}

func NewReadFileTool(ws *workspace.Manager) *ReadFileTool {
	return &ReadFileTool{ws: ws}
}

func (t *ReadFileTool) Name() string           { return "read_file" }
func (t *ReadFileTool) Description() string    { return "Reads a file from the workspace" }
func (t *ReadFileTool) Risk() tools.RiskLevel  { return tools.RiskLow }
func (t *ReadFileTool) Capabilities() []string { return []string{"read"} }

func (t *ReadFileTool) Execute(ctx context.Context, input tools.ToolInput) (tools.ToolOutput, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return tools.ToolOutput{Success: false, Error: "path is required"}, nil
	}
	content, err := t.ws.ReadFile(path)
	if err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}
	return tools.ToolOutput{
		Success: true,
		Data:    map[string]any{"content": content, "path": path, "size": len(content)},
	}, nil
}
