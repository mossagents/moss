package app

import (
	"context"
	"testing"

	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/events"
)

func TestRegisterPlanSetsActiveTaskAndPublishesDelegations(t *testing.T) {
	bus := events.NewBus()
	rm := NewRunManager(nil, nil, nil, nil, bus)

	var delegated []events.Event
	bus.Subscribe(func(e events.Event) {
		if e.Type == events.EventTaskDelegated {
			delegated = append(delegated, e)
		}
	})

	run, err := rm.StartRun(context.Background(), RunRequest{
		Goal:      "implement a feature",
		Mode:      domain.RunModeInteractive,
		Workspace: "/repo",
	})
	if err != nil {
		t.Fatalf("StartRun returned error: %v", err)
	}

	plan := &domain.Plan{
		Summary: "research then code",
		Steps: []domain.PlanStep{
			{StepID: "task-1-research", AssignedAgent: "researcher", Goal: "analyze"},
			{StepID: "task-1-code", AssignedAgent: "coder", Goal: "implement", DependsOn: []string{"task-1-research"}},
		},
	}

	if err := rm.RegisterPlan(run.RunID, plan); err != nil {
		t.Fatalf("RegisterPlan returned error: %v", err)
	}

	storedRun, ok := rm.GetRun(run.RunID)
	if !ok {
		t.Fatalf("expected run %q to exist", run.RunID)
	}
	if storedRun.ActiveTaskID != "task-1-research" {
		t.Fatalf("expected active task to be first plan step, got %q", storedRun.ActiveTaskID)
	}
	if storedRun.Plan == nil || len(storedRun.Plan.Steps) != 2 {
		t.Fatalf("expected stored plan with 2 steps, got %+v", storedRun.Plan)
	}
	if len(delegated) != 2 {
		t.Fatalf("expected 2 task delegation events, got %d", len(delegated))
	}
}

func TestRegisterPlanRejectsNilPlan(t *testing.T) {
	bus := events.NewBus()
	rm := NewRunManager(nil, nil, nil, nil, bus)

	run, err := rm.StartRun(context.Background(), RunRequest{
		Goal:      "analyze",
		Mode:      domain.RunModeInteractive,
		Workspace: "/repo",
	})
	if err != nil {
		t.Fatalf("StartRun returned error: %v", err)
	}

	if err := rm.RegisterPlan(run.RunID, nil); err == nil {
		t.Fatal("expected RegisterPlan to reject nil plans")
	}
}
