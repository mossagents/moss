package builtins

import (
	"context"

	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

type WriteFileTool struct {
	ws *workspace.Manager
}

func NewWriteFileTool(ws *workspace.Manager) *WriteFileTool {
	return &WriteFileTool{ws: ws}
}

func (t *WriteFileTool) Name() string           { return "write_file" }
func (t *WriteFileTool) Description() string    { return "Writes content to a file in the workspace" }
func (t *WriteFileTool) Risk() tools.RiskLevel  { return tools.RiskHigh }
func (t *WriteFileTool) Capabilities() []string { return []string{"write"} }

func (t *WriteFileTool) Execute(ctx context.Context, input tools.ToolInput) (tools.ToolOutput, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return tools.ToolOutput{Success: false, Error: "path is required"}, nil
	}
	content, _ := input["content"].(string)
	if err := t.ws.WriteFile(path, content); err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}
	return tools.ToolOutput{
		Success: true,
		Data:    map[string]any{"path": path, "written": true},
	}, nil
}
