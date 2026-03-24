package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/workspace"
)

func TestServiceExecuteCreatesAndRunsPlan(t *testing.T) {
	workspaceDir := t.TempDir()

	svc, err := NewService(workspaceDir, workspace.TrustLevelTrusted, strings.NewReader("y\n"), &strings.Builder{})
	if err != nil {
		t.Fatalf("NewService returned error: %v", err)
	}

	run, err := svc.Execute(context.Background(), RunRequest{
		Goal:      "implement the requested feature",
		Mode:      domain.RunModeInteractive,
		Workspace: workspaceDir,
		Trust:     workspace.TrustLevelTrusted,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if run.Plan == nil {
		t.Fatal("expected run plan to be populated")
	}
	if len(run.Plan.Steps) != 2 {
		t.Fatalf("expected 2 planned steps, got %d", len(run.Plan.Steps))
	}
	if run.ActiveTaskID != run.Plan.Steps[0].StepID {
		t.Fatalf("expected active task ID %q, got %q", run.Plan.Steps[0].StepID, run.ActiveTaskID)
	}
	if !strings.Contains(run.FinalResult, "Plan:") {
		t.Fatalf("expected final result to include plan summary, got %q", run.FinalResult)
	}
}
