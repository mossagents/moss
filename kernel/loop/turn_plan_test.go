package loop

import (
	"testing"

	"github.com/mossagents/moss/kernel/session"
)

func TestBuildTurnPlan_IncludesPromptVersionFromSessionMetadata(t *testing.T) {
	sess := &session.Session{
		ID: "s1",
		Config: session.SessionConfig{
			Metadata: map[string]any{
				session.MetadataInstructionProfile: "planning",
				session.MetadataPromptVersion:      "unified:abc123",
			},
		},
	}

	plan := buildTurnPlan(sess, "run-1", 1, nil)
	if plan.PromptVersion != "unified:abc123" {
		t.Fatalf("prompt version = %q", plan.PromptVersion)
	}
	if plan.InstructionProfile != "planning" {
		t.Fatalf("instruction profile = %q", plan.InstructionProfile)
	}
}
