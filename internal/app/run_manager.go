package app

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/mossagi/moss/internal/approval"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/events"
	"github.com/mossagi/moss/internal/policy"
	"github.com/mossagi/moss/internal/tools"
	"github.com/mossagi/moss/internal/workspace"
)

var eventCounter atomic.Uint64

func newEventID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), eventCounter.Add(1))
}

type RunManager struct {
	ws       *workspace.Manager
	policy   *policy.Engine
	approval *approval.Service
	catalog  *tools.Catalog
	bus      *events.Bus
	runs     map[string]*domain.Run
}

func NewRunManager(
	ws *workspace.Manager,
	pol *policy.Engine,
	svc *approval.Service,
	cat *tools.Catalog,
	bus *events.Bus,
) *RunManager {
	return &RunManager{
		ws:       ws,
		policy:   pol,
		approval: svc,
		catalog:  cat,
		bus:      bus,
		runs:     make(map[string]*domain.Run),
	}
}

func (rm *RunManager) StartRun(ctx context.Context, req RunRequest) (*domain.Run, error) {
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())

	run := &domain.Run{
		RunID:     runID,
		Goal:      req.Goal,
		Mode:      req.Mode,
		Workspace: req.Workspace,
		Status:    domain.RunStatusRunning,
		StartedAt: time.Now(),
		Budget: &domain.Budget{
			MaxSteps:  50,
			MaxTokens: 8192,
		},
	}

	rm.runs[runID] = run

	e := events.Event{
		EventID:   newEventID(),
		Type:      events.EventRunStarted,
		RunID:     runID,
		Timestamp: time.Now(),
		Payload:   map[string]any{"goal": req.Goal, "mode": string(req.Mode)},
	}
	rm.bus.Publish(e)

	return run, nil
}

func (rm *RunManager) CompleteRun(runID, result string) error {
	run, ok := rm.runs[runID]
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}
	now := time.Now()
	run.EndedAt = &now
	run.FinalResult = result
	run.Status = domain.RunStatusCompleted

	rm.bus.Publish(events.Event{
		EventID:   newEventID(),
		Type:      events.EventRunCompleted,
		RunID:     runID,
		Timestamp: now,
		Payload:   map[string]any{"result": result},
	})
	return nil
}

func (rm *RunManager) RegisterPlan(runID string, plan *domain.Plan) error {
	run, ok := rm.runs[runID]
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}

	run.Plan = plan
	run.ActiveTaskID = ""
	if plan == nil {
		return nil
	}
	run.ActiveTaskID = plan.FirstStepID()

	for _, step := range plan.Steps {
		rm.bus.Publish(events.Event{
			EventID:   newEventID(),
			Type:      events.EventTaskDelegated,
			RunID:     runID,
			TaskID:    step.StepID,
			Timestamp: time.Now(),
			Payload: map[string]any{
				"agent":      step.AssignedAgent,
				"title":      step.Title,
				"goal":       step.Goal,
				"depends_on": step.DependsOn,
			},
		})
	}

	return nil
}

func (rm *RunManager) FailRun(runID, errMsg string) error {
	run, ok := rm.runs[runID]
	if !ok {
		return fmt.Errorf("run %q not found", runID)
	}
	now := time.Now()
	run.EndedAt = &now
	run.Status = domain.RunStatusFailed
	run.FinalResult = errMsg

	rm.bus.Publish(events.Event{
		EventID:   newEventID(),
		Type:      events.EventRunFailed,
		RunID:     runID,
		Timestamp: now,
		Payload:   map[string]any{"error": errMsg},
	})
	return nil
}

func (rm *RunManager) GetRun(runID string) (*domain.Run, bool) {
	r, ok := rm.runs[runID]
	return r, ok
}
