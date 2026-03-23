package policy

import (
	"testing"

	"github.com/mossagi/moss/internal/workspace"
)

func TestEngineAllowLowRisk(t *testing.T) {
	e := New()
	req := CheckRequest{
		ToolName:       "read_file",
		Capabilities:   []string{"read"},
		WorkspaceTrust: workspace.TrustLevelTrusted,
		AllowedCaps:    []string{"read"},
	}
	decision := e.Check(req)
	if decision != DecisionAllow {
		t.Errorf("expected allow, got %s", decision)
	}
}

func TestEngineRequireApprovalRunCommand(t *testing.T) {
	e := New()
	req := CheckRequest{
		ToolName:       "run_command",
		Capabilities:   []string{"execute"},
		WorkspaceTrust: workspace.TrustLevelTrusted,
		AllowedCaps:    []string{"execute"},
	}
	decision := e.Check(req)
	if decision != DecisionApprove {
		t.Errorf("expected require_approval, got %s", decision)
	}
}

func TestEngineRestrictedWorkspace(t *testing.T) {
	e := New()
	req := CheckRequest{
		ToolName:       "search_text",
		Capabilities:   []string{"execute"},
		WorkspaceTrust: workspace.TrustLevelRestricted,
		AllowedCaps:    []string{"execute"},
	}
	decision := e.Check(req)
	if decision != DecisionDeny {
		t.Errorf("expected deny for execute cap in restricted workspace, got %s", decision)
	}
}

func TestEngineDenyMissingCap(t *testing.T) {
	e := New()
	req := CheckRequest{
		ToolName:       "read_file",
		Capabilities:   []string{"read"},
		WorkspaceTrust: workspace.TrustLevelTrusted,
		AllowedCaps:    []string{"write"},
	}
	decision := e.Check(req)
	if decision != DecisionDeny {
		t.Errorf("expected deny for missing cap, got %s", decision)
	}
}
