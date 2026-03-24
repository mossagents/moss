package manager

import (
	"context"
	"fmt"
	"strings"

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

	if task.Plan == nil || len(task.Plan.Steps) == 0 {
		return a.runLegacyResearchFlow(ctx, task, catalog)
	}

	fmt.Printf("[manager] Plan ready with %d steps\n", len(task.Plan.Steps))

	var phaseSummaries []string
	var artifacts []string
	for _, step := range task.Plan.Steps {
		result, err := a.runPlannedStep(ctx, task, step, catalog)
		if err != nil {
			return &domain.TaskResult{
				Success: false,
				Error:   fmt.Sprintf("%s failed: %v", step.AssignedAgent, err),
			}, nil
		}
		phaseSummaries = append(phaseSummaries, fmt.Sprintf("%s: %s", step.Title, result.Summary))
		artifacts = append(artifacts, result.Artifacts...)
	}

	summary := fmt.Sprintf("Goal: %s\nPlan: %s\n%s", task.Goal, task.Plan.Summary, strings.Join(phaseSummaries, "\n"))

	return &domain.TaskResult{
		Summary:   summary,
		Artifacts: artifacts,
		Success:   true,
	}, nil
}

func (a *Agent) runLegacyResearchFlow(ctx context.Context, task *domain.Task, catalog *tools.Catalog) (*domain.TaskResult, error) {
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

func (a *Agent) runPlannedStep(ctx context.Context, parentTask *domain.Task, step domain.PlanStep, catalog *tools.Catalog) (*domain.TaskResult, error) {
	subTask := &domain.Task{
		TaskID:        step.StepID,
		RunID:         parentTask.RunID,
		AssignedAgent: step.AssignedAgent,
		Goal:          step.Goal,
		Status:        domain.TaskStatusPending,
	}

	switch step.AssignedAgent {
	case "researcher":
		return a.researcher.Run(ctx, subTask, catalog)
	case "coder":
		return a.coder.Run(ctx, subTask, catalog)
	default:
		return nil, fmt.Errorf("unsupported planned agent %q", step.AssignedAgent)
	}
}
