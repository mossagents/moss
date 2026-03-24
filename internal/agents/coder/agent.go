package coder

import (
	"context"
	"fmt"

	"github.com/mossagi/moss/internal/agents"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/tools"
)

type Agent struct {
	spec    agents.AgentSpec
	catalog *tools.Catalog
}

func New(catalog *tools.Catalog) *Agent {
	return &Agent{
		spec: agents.AgentSpec{
			Name:                "coder",
			Role:                agents.RoleCoder,
			Instructions:        "You are the coder agent. Use write tools (with approval) to implement changes.",
			AllowedCapabilities: []string{"read", "write", "execute"},
			AllowedTools:        []string{"list_files", "read_file", "write_file", "run_command"},
			Limits:              &agents.AgentLimits{MaxSteps: 15, MaxTokens: 4096},
		},
		catalog: catalog,
	}
}

func (a *Agent) Spec() agents.AgentSpec {
	return a.spec
}

func (a *Agent) Run(ctx context.Context, task *domain.Task, catalog *tools.Catalog) (*domain.TaskResult, error) {
	fmt.Printf("[coder] Starting task: %s\n", task.Goal)

	var artifacts []string

	if listTool, ok := catalog.Get("list_files"); ok {
		result, err := listTool.Execute(ctx, tools.ToolInput{"path": ".", "pattern": "*.go"})
		if err == nil && result.Success {
			if files, ok := result.Data["files"]; ok {
				fmt.Printf("[coder] Found Go files: %v\n", files)
			}
		}
	}

	return &domain.TaskResult{
		Summary:   fmt.Sprintf("Coder analyzed task: %s", task.Goal),
		Artifacts: artifacts,
		Success:   true,
	}, nil
}
