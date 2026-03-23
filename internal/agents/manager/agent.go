package manager

import (
	"context"
	"fmt"

	"github.com/mossagi/moss/internal/agents"
	"github.com/mossagi/moss/internal/agents/coder"
	"github.com/mossagi/moss/internal/agents/researcher"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/tools"
)

type Agent struct {
	spec       agents.AgentSpec
	catalog    *tools.Catalog
	researcher *researcher.Agent
	coder      *coder.Agent
}

func New(catalog *tools.Catalog) *Agent {
	return &Agent{
		spec: agents.AgentSpec{
			Name:                "manager",
			Role:                agents.RoleManager,
			Instructions:        "You are the manager agent. Coordinate researcher and coder agents to accomplish the goal.",
			AllowedCapabilities: []string{"read", "write", "execute", "interact"},
			AllowedTools:        []string{"list_files", "read_file", "search_text", "run_command", "write_file", "ask_user"},
			Limits:              &agents.AgentLimits{MaxSteps: 20, MaxTokens: 4096},
		},
		catalog:    catalog,
		researcher: researcher.New(catalog),
		coder:      coder.New(catalog),
	}
}

func (a *Agent) Spec() agents.AgentSpec {
	return a.spec
}

func (a *Agent) Run(ctx context.Context, task *domain.Task, catalog *tools.Catalog) (*domain.TaskResult, error) {
	fmt.Printf("[manager] Starting task: %s\n", task.Goal)

	// First delegate research phase
	researchTask := &domain.Task{
		TaskID:        task.TaskID + "-research",
		RunID:         task.RunID,
		AssignedAgent: "researcher",
		Goal:          "Analyze workspace: " + task.Goal,
		Status:        domain.TaskStatusPending,
	}
	researchResult, err := a.researcher.Run(ctx, researchTask, catalog)
	if err != nil {
		return &domain.TaskResult{
			Success: false,
			Error:   fmt.Sprintf("research failed: %v", err),
		}, nil
	}

	fmt.Printf("[manager] Research complete: %s\n", researchResult.Summary)

	summary := fmt.Sprintf("Goal: %s\nResearch: %s", task.Goal, researchResult.Summary)

	return &domain.TaskResult{
		Summary:   summary,
		Artifacts: researchResult.Artifacts,
		Success:   true,
	}, nil
}
