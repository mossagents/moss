package loop

import (
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestToolAllowed_EmptyToolRouteDeniesAll(t *testing.T) {
	t.Parallel()
	l := &AgentLoop{}
	l.currentTurn = TurnPlan{ToolRoute: nil}
	if l.toolAllowed("any_tool") {
		t.Fatal("expected deny when ToolRoute is empty")
	}
	l.currentTurn = TurnPlan{ToolRoute: []ToolRouteDecision{}}
	if l.toolAllowed("x") {
		t.Fatal("expected deny when ToolRoute is empty slice")
	}
}

func TestToolAllowed_RespectsNonEmptyRoute(t *testing.T) {
	t.Parallel()
	l := &AgentLoop{}
	l.currentTurn = TurnPlan{
		ToolRoute: []ToolRouteDecision{
			{Name: "visible_ok", Status: ToolRouteVisible},
			{Name: "hidden_x", Status: ToolRouteHidden},
		},
	}
	if !l.toolAllowed("visible_ok") {
		t.Fatal("visible tool should be allowed")
	}
	if l.toolAllowed("hidden_x") {
		t.Fatal("hidden tool should be denied")
	}
	if l.toolAllowed("not_in_route") {
		t.Fatal("unknown tool should be denied")
	}
	if l.toolAllowed("") {
		t.Fatal("empty name should be denied")
	}
}

func TestValidateRequiredToolArgs_StrictToolsRejectMalformedSchema(t *testing.T) {
	t.Parallel()
	spec := tool.ToolSpec{
		Name:         "run_in_terminal",
		InputSchema:  json.RawMessage(`not-json`),
		Capabilities: []string{"execution"},
	}
	if err := validateRequiredToolArgs(spec, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for invalid JSON schema on execution tool")
	}
}

func TestValidateRequiredToolArgs_LowRiskStillBestEffortOnMalformedSchema(t *testing.T) {
	t.Parallel()
	spec := tool.ToolSpec{
		Name:        "safe_lookup",
		InputSchema: json.RawMessage(`not-json`),
		Risk:        tool.RiskLow,
	}
	if err := validateRequiredToolArgs(spec, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRequiredToolArgs_StrictToolWorkspaceEffect(t *testing.T) {
	t.Parallel()
	spec := tool.ToolSpec{
		Name:        "mutate",
		InputSchema: json.RawMessage(`{garbage`),
		Effects:     []tool.Effect{tool.EffectWritesWorkspace},
	}
	if err := validateRequiredToolArgs(spec, json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error")
	}
}
