package builtins

import (
	"context"
	"path/filepath"

	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

type ListFilesTool struct {
	ws *workspace.Manager
}

func NewListFilesTool(ws *workspace.Manager) *ListFilesTool {
	return &ListFilesTool{ws: ws}
}

func (t *ListFilesTool) Name() string           { return "list_files" }
func (t *ListFilesTool) Description() string    { return "Lists files in the workspace matching a pattern" }
func (t *ListFilesTool) Risk() tools.RiskLevel  { return tools.RiskLow }
func (t *ListFilesTool) Capabilities() []string { return []string{"read"} }

func (t *ListFilesTool) Execute(ctx context.Context, input tools.ToolInput) (tools.ToolOutput, error) {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		pattern = "*"
	}
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}
	fullPattern := filepath.Join(path, pattern)
	files, err := t.ws.ListFiles(fullPattern)
	if err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}
	fileList := make([]any, len(files))
	for i, f := range files {
		fileList[i] = f
	}
	return tools.ToolOutput{
		Success: true,
		Data:    map[string]any{"files": fileList, "count": len(files)},
	}, nil
}
