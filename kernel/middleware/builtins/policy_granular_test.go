package builtins

import (
	"encoding/json"
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestRequireApprovalForPathPrefix(t *testing.T) {
	rule := RequireApprovalForPathPrefix(".git", ".moss")
	spec := tool.ToolSpec{Name: "write_file"}

	allow := rule(spec, json.RawMessage(`{"path":"src/main.go"}`))
	if allow != Allow {
		t.Fatalf("expected allow, got %s", allow)
	}
	need := rule(spec, json.RawMessage(`{"path":".git/config"}`))
	if need != RequireApproval {
		t.Fatalf("expected require approval, got %s", need)
	}
}

func TestDenyCommandContaining(t *testing.T) {
	rule := DenyCommandContaining("rm -rf /", "format c:")

	spec := tool.ToolSpec{Name: "run_command"}
	if got := rule(spec, json.RawMessage(`{"command":"go test ./..."}`)); got != Allow {
		t.Fatalf("expected allow, got %s", got)
	}
	if got := rule(spec, json.RawMessage(`{"command":"rm -rf /tmp"}`)); got != Deny {
		t.Fatalf("expected deny, got %s", got)
	}
}

