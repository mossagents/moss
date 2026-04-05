package product

import (
	"context"
	"strings"
	"testing"
	"time"

	appruntime "github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

func TestBuildInspectReportRunSummarizesPlanningAndFailover(t *testing.T) {
	ctx := context.Background()
	configureProductTestApp(t)
	workspace := t.TempDir()
	store, err := session.NewFileStore(SessionStoreDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	now := time.Now().UTC()
	if err := store.Save(ctx, &session.Session{
		ID:        "sess-inspect",
		Status:    session.StatusRunning,
		CreatedAt: now,
		Config: session.SessionConfig{
			Goal: "inspect latest run",
		},
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	catalog, err := appruntime.NewStateCatalog(StateStoreDir(), StateEventDir(), true)
	if err != nil {
		t.Fatalf("NewStateCatalog: %v", err)
	}
	events := []port.ExecutionEvent{
		{
			EventID:      "evt-turn",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         port.ExecutionEventType("turn.plan_prepared"),
			Timestamp:    now,
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "turn_plan",
			Data: map[string]any{
				"iteration":            1,
				"instruction_profile":  "planning",
				"model_lane":           "reasoning",
				"visible_tools_count":  2,
				"hidden_tools_count":   1,
				"approval_tools_count": 1,
			},
		},
		{
			EventID:      "evt-tools",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         port.ExecutionEventType("tool.route_planned"),
			Timestamp:    now.Add(time.Millisecond),
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "tool_route",
			Data: map[string]any{
				"visible_tools":  []string{"read_file", "view"},
				"hidden_tools":   []string{"write_file"},
				"approval_tools": []string{"run_command"},
				"route_digest":   "read_file:visible,run_command:approval-required,write_file:hidden",
				"decisions": []map[string]any{
					{"name": "read_file", "status": "visible", "source": "builtin", "owner": "runtime", "risk": "low", "reason_codes": []string{"visible"}},
					{"name": "write_file", "status": "hidden", "source": "builtin", "owner": "runtime", "risk": "high", "reason_codes": []string{"planning_mode"}},
				},
			},
		},
		{
			EventID:      "evt-model",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         port.ExecutionEventType("model.route_planned"),
			Timestamp:    now.Add(2 * time.Millisecond),
			Phase:        "planning",
			Actor:        "runtime",
			PayloadKind:  "model_route",
			Model:        "gpt-5",
			Data: map[string]any{
				"lane":          "reasoning",
				"reason_codes":  []string{"planning_mode"},
				"capabilities":  []port.ModelCapability{port.CapReasoning},
				"max_cost_tier": 0,
				"prefer_cheap":  false,
			},
		},
		{
			EventID:      "evt-failover",
			EventVersion: 1,
			SessionID:    "sess-inspect",
			RunID:        "run-1",
			TurnID:       "run-1-turn-001",
			Type:         port.ExecutionEventType("llm_failover_attempt"),
			Timestamp:    now.Add(3 * time.Millisecond),
			Phase:        "llm",
			Actor:        "runtime",
			PayloadKind:  "llm_attempt",
			Model:        "gpt-5",
			Data: map[string]any{
				"candidate_model": "gpt-5",
				"attempt_index":   1,
				"candidate_retry": 0,
				"breaker_state":   "closed",
				"failover_to":     "claude-sonnet",
				"outcome":         "failed",
				"failure_reason":  "rate limited",
			},
		},
	}
	for _, event := range events {
		if err := catalog.AppendExecutionEvent(event); err != nil {
			t.Fatalf("AppendExecutionEvent(%s): %v", event.Type, err)
		}
	}
	if err := catalog.Upsert(appruntime.StateEntry{
		Kind:      appruntime.StateKindChange,
		RecordID:  "change-1",
		SessionID: "sess-inspect",
		Status:    "applied",
		Title:     "update tracked.txt",
		Summary:   "tracked.txt",
		SortTime:  now.Add(4 * time.Millisecond),
		CreatedAt: now.Add(4 * time.Millisecond),
		UpdatedAt: now.Add(4 * time.Millisecond),
	}); err != nil {
		t.Fatalf("Upsert change entry: %v", err)
	}

	report, err := BuildInspectReport(ctx, workspace, []string{"run", "latest", "10"})
	if err != nil {
		t.Fatalf("BuildInspectReport: %v", err)
	}
	if report.Run == nil {
		t.Fatal("expected run report")
	}
	if report.Run.SessionID != "sess-inspect" || report.Run.RunID != "run-1" {
		t.Fatalf("unexpected run identity: %+v", report.Run)
	}
	if report.Run.TurnPlan == nil || report.Run.TurnPlan.ModelLane != "reasoning" {
		t.Fatalf("unexpected turn plan: %+v", report.Run.TurnPlan)
	}
	if report.Run.ToolRoute == nil || len(report.Run.ToolRoute.Decisions) != 2 {
		t.Fatalf("unexpected tool route: %+v", report.Run.ToolRoute)
	}
	if report.Run.ModelRoute == nil || report.Run.ModelRoute.Lane != "reasoning" {
		t.Fatalf("unexpected model route: %+v", report.Run.ModelRoute)
	}
	if len(report.Run.Failovers) != 1 || report.Run.Failovers[0].FailoverTo != "claude-sonnet" {
		t.Fatalf("unexpected failovers: %+v", report.Run.Failovers)
	}
	if len(report.Run.Changes) != 1 || report.Run.Changes[0].RecordID != "change-1" {
		t.Fatalf("unexpected changes: %+v", report.Run.Changes)
	}

	rendered := RenderInspectReport(report)
	for _, want := range []string{
		"moss inspect (run)",
		"Run session: sess-inspect",
		"Turn plan:   iteration=1 profile=planning lane=reasoning",
		"Model route: configured=gpt-5 lane=reasoning",
		"Tool decisions:",
		"Failover:",
		"Changes:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered inspect missing %q:\n%s", want, rendered)
		}
	}
}
