package builtins

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

type SearchTextTool struct {
	ws *workspace.Manager
}

func NewSearchTextTool(ws *workspace.Manager) *SearchTextTool {
	return &SearchTextTool{ws: ws}
}

func (t *SearchTextTool) Name() string           { return "search_text" }
func (t *SearchTextTool) Description() string    { return "Searches for text in files within the workspace" }
func (t *SearchTextTool) Risk() tools.RiskLevel  { return tools.RiskLow }
func (t *SearchTextTool) Capabilities() []string { return []string{"read"} }

func (t *SearchTextTool) Execute(ctx context.Context, input tools.ToolInput) (tools.ToolOutput, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return tools.ToolOutput{Success: false, Error: "query is required"}, nil
	}
	searchPath, _ := input["path"].(string)
	if searchPath == "" {
		searchPath = "."
	}
	resolved, err := t.ws.ResolvePath(searchPath)
	if err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}

	var matches []any
	err = filepath.Walk(resolved, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(data), query) {
			rel, _ := filepath.Rel(t.ws.Root, path)
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return tools.ToolOutput{Success: false, Error: err.Error()}, nil
	}
	return tools.ToolOutput{
		Success: true,
		Data:    map[string]any{"matches": matches, "count": len(matches), "query": query},
	}, nil
}
