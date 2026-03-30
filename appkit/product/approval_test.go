package product

import (
	"testing"

	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/tool"
)

func TestNormalizeApprovalMode(t *testing.T) {
	tests := map[string]string{
		"":          ApprovalModeConfirm,
		"confirm":   ApprovalModeConfirm,
		"readonly":  ApprovalModeReadOnly,
		"read-only": ApprovalModeReadOnly,
		"auto":      ApprovalModeFullAuto,
	}
	for input, want := range tests {
		if got := NormalizeApprovalMode(input); got != want {
			t.Fatalf("NormalizeApprovalMode(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestApprovalModePolicyRules(t *testing.T) {
	writeSpec := tool.ToolSpec{Name: "write_file"}
	readSpec := tool.ToolSpec{Name: "read_file"}
	httpSpec := tool.ToolSpec{Name: "http_request"}

	readOnlyRules, err := ApprovalModePolicyRules(ApprovalModeReadOnly)
	if err != nil {
		t.Fatalf("read-only rules: %v", err)
	}
	if got := EvaluatePolicy(readOnlyRules, writeSpec, nil); got != builtins.Deny {
		t.Fatalf("read-only write_file=%s, want %s", got, builtins.Deny)
	}
	if got := EvaluatePolicy(readOnlyRules, httpSpec, nil); got != builtins.Deny {
		t.Fatalf("read-only http_request=%s, want %s", got, builtins.Deny)
	}
	if got := EvaluatePolicy(readOnlyRules, readSpec, nil); got != builtins.Allow {
		t.Fatalf("read-only read_file=%s, want %s", got, builtins.Allow)
	}

	confirmRules, err := ApprovalModePolicyRules(ApprovalModeConfirm)
	if err != nil {
		t.Fatalf("confirm rules: %v", err)
	}
	if got := EvaluatePolicy(confirmRules, writeSpec, nil); got != builtins.RequireApproval {
		t.Fatalf("confirm write_file=%s, want %s", got, builtins.RequireApproval)
	}

	fullAutoRules, err := ApprovalModePolicyRules(ApprovalModeFullAuto)
	if err != nil {
		t.Fatalf("full-auto rules: %v", err)
	}
	if got := EvaluatePolicy(fullAutoRules, writeSpec, nil); got != builtins.Allow {
		t.Fatalf("full-auto write_file=%s, want %s", got, builtins.Allow)
	}
}
