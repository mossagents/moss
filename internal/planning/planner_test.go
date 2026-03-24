package planning

import (
	"testing"

	"github.com/mossagi/moss/internal/domain"
)

func TestSimplePlannerCreatesResearchOnlyPlan(t *testing.T) {
	planner := NewSimplePlanner()

	plan, err := planner.Create(&domain.Task{
		TaskID: "task-1",
		Goal:   "analyze the repository structure",
	}, domain.RunModeInteractive)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].AssignedAgent != "researcher" {
		t.Fatalf("expected researcher step, got %q", plan.Steps[0].AssignedAgent)
	}
}

func TestSimplePlannerAddsCoderStepForImplementationGoals(t *testing.T) {
	planner := NewSimplePlanner()

	plan, err := planner.Create(&domain.Task{
		TaskID: "task-2",
		Goal:   "implement the requested feature",
	}, domain.RunModeInteractive)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(plan.Steps))
	}
	if plan.Steps[1].AssignedAgent != "coder" {
		t.Fatalf("expected coder step, got %q", plan.Steps[1].AssignedAgent)
	}
	if len(plan.Steps[1].DependsOn) != 1 || plan.Steps[1].DependsOn[0] != plan.Steps[0].StepID {
		t.Fatalf("expected coder step to depend on research step, got %+v", plan.Steps[1].DependsOn)
	}
}
