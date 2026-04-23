package swarm

import (
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestContractChildTaskContractUsesNormalizedValues(t *testing.T) {
	contract := Contract{
		Role:            RoleSupervisor,
		Goal:            "  synthesize findings  ",
		InputContext:    "  latest findings  ",
		ThreadBudget:    Budget{MaxSteps: 10, MaxTokens: 100},
		TaskBudget:      Budget{MaxSteps: 3, MaxTokens: 25, TimeoutSec: 60},
		ApprovalCeiling: tool.ApprovalClassExplicitUser,
		WritableScopes:  []string{" workspace:/repo ", "workspace:/repo", ""},
		MemoryScope:     "  memory/research  ",
		AllowedEffects:  []tool.Effect{tool.EffectWritesWorkspace, tool.EffectWritesWorkspace, tool.EffectWritesMemory},
		PublishArtifacts: []ArtifactKind{
			ArtifactSummary,
			ArtifactCitationSet,
			ArtifactSummary,
		},
	}

	got := contract.ChildTaskContract()
	if got.Goal != "synthesize findings" {
		t.Fatalf("Goal = %q", got.Goal)
	}
	if got.Budget.MaxSteps != 3 || got.Budget.MaxTokens != 25 || got.Budget.TimeoutSec != 60 {
		t.Fatalf("unexpected task budget: %+v", got.Budget)
	}
	if got.MemoryScope != "memory/research" {
		t.Fatalf("MemoryScope = %q", got.MemoryScope)
	}
	if len(got.WritableScopes) != 1 || got.WritableScopes[0] != "workspace:/repo" {
		t.Fatalf("unexpected writable scopes: %#v", got.WritableScopes)
	}
	if len(got.AllowedEffects) != 2 {
		t.Fatalf("unexpected allowed effects: %#v", got.AllowedEffects)
	}
	if len(got.ReturnArtifacts) != 2 || got.ReturnArtifacts[0] != string(ArtifactCitationSet) || got.ReturnArtifacts[1] != string(ArtifactSummary) {
		t.Fatalf("unexpected return artifacts: %#v", got.ReturnArtifacts)
	}
}

func TestContractChildTaskContractFallsBackToThreadBudget(t *testing.T) {
	contract := Contract{
		Role:         RoleWorker,
		ThreadBudget: Budget{MaxSteps: 7, MaxTokens: 70, TimeoutSec: 30},
	}

	got := contract.ChildTaskContract()
	if got.Budget.MaxSteps != 7 || got.Budget.MaxTokens != 70 || got.Budget.TimeoutSec != 30 {
		t.Fatalf("unexpected fallback budget: %+v", got.Budget)
	}
}

func TestContractValidateRejectsInvalidBudgetAndRole(t *testing.T) {
	tests := []Contract{
		{Role: "", ThreadBudget: Budget{MaxSteps: 1}},
		{Role: RolePlanner, ThreadBudget: Budget{MaxSteps: -1}},
		{Role: RolePlanner, ApprovalCeiling: tool.ApprovalClass("bogus")},
	}
	for _, tc := range tests {
		if err := tc.Validate(); err == nil {
			t.Fatalf("Validate(%+v) succeeded, want error", tc)
		}
	}
}

func TestContractFromTaskContractRoundTrip(t *testing.T) {
	orig := Contract{
		Role:            RoleWorker,
		Goal:            "collect evidence",
		InputContext:    "repo",
		TaskBudget:      Budget{MaxSteps: 2, MaxTokens: 20, TimeoutSec: 10},
		ApprovalCeiling: tool.ApprovalClassPolicyGuarded,
		WritableScopes:  []string{"workspace:/repo/docs"},
		MemoryScope:     "memory/research",
		AllowedEffects:  []tool.Effect{tool.EffectWritesMemory},
		PublishArtifacts: []ArtifactKind{
			ArtifactFinding,
			ArtifactSourceSet,
		},
	}

	roundTrip := ContractFromTaskContract(orig.ChildTaskContract())
	if roundTrip.Goal != orig.Goal || roundTrip.InputContext != orig.InputContext {
		t.Fatalf("round trip mismatch: %+v", roundTrip)
	}
	if roundTrip.TaskBudget != orig.TaskBudget {
		t.Fatalf("task budget mismatch: %+v", roundTrip.TaskBudget)
	}
	if len(roundTrip.PublishArtifacts) != 2 || roundTrip.PublishArtifacts[0] != ArtifactFinding || roundTrip.PublishArtifacts[1] != ArtifactSourceSet {
		t.Fatalf("unexpected publish artifacts: %#v", roundTrip.PublishArtifacts)
	}
}
