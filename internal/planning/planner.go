package planning

import (
	"fmt"
	"strings"

	"github.com/mossagi/moss/internal/domain"
)

type Planner interface {
	Create(task *domain.Task, mode domain.RunMode) (*domain.Plan, error)
}

type SimplePlanner struct{}

func NewSimplePlanner() *SimplePlanner {
	return &SimplePlanner{}
}

func (p *SimplePlanner) Create(task *domain.Task, mode domain.RunMode) (*domain.Plan, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	if task.TaskID == "" {
		return nil, fmt.Errorf("task ID is required")
	}
	if strings.TrimSpace(task.Goal) == "" {
		return nil, fmt.Errorf("task goal is required")
	}

	researchStepID := task.TaskID + "-research"
	steps := []domain.PlanStep{
		{
			StepID:        researchStepID,
			Title:         "Research workspace",
			AssignedAgent: "researcher",
			Goal:          "Analyze workspace for goal: " + task.Goal,
			Status:        domain.TaskStatusPending,
		},
	}

	if goalNeedsCoding(task.Goal) {
		steps = append(steps, domain.PlanStep{
			StepID:        task.TaskID + "-code",
			Title:         "Implement changes",
			AssignedAgent: "coder",
			Goal:          "Implement changes for goal: " + task.Goal,
			DependsOn:     []string{researchStepID},
			Status:        domain.TaskStatusPending,
		})
	}

	return &domain.Plan{
		Summary: summarizePlan(mode, steps),
		Steps:   steps,
	}, nil
}

func summarizePlan(mode domain.RunMode, steps []domain.PlanStep) string {
	parts := make([]string, 0, len(steps))
	for _, step := range steps {
		parts = append(parts, fmt.Sprintf("%s via %s", step.Title, step.AssignedAgent))
	}
	return fmt.Sprintf("mode=%s; steps=%s", mode, strings.Join(parts, " -> "))
}

func goalNeedsCoding(goal string) bool {
	normalized := strings.ToLower(goal)
	for _, keyword := range []string{"implement", "fix", "write", "create", "update", "refactor"} {
		if strings.Contains(normalized, keyword) {
			return true
		}
	}
	return false
}
