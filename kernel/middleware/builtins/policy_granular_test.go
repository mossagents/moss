package builtins

import (
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestRequireApprovalForPathPrefix(t *testing.T) {
	rule := RequireApprovalForPathPrefix(".git", ".moss")

	allow := rule(PolicyContext{Input: []byte(`{"path":"src/main.go"}`)})
	if allow.Decision != Allow {
		t.Fatalf("expected allow, got %s", allow.Decision)
	}
	need := rule(PolicyContext{Input: []byte(`{"path":".git/config"}`)})
	if need.Decision != RequireApproval {
		t.Fatalf("expected require approval, got %s", need.Decision)
	}
	if need.Reason.Code != "path.protected_prefix" {
		t.Fatalf("expected reason code path.protected_prefix, got %q", need.Reason.Code)
	}
}

func TestDenyCommandContaining(t *testing.T) {
	rule := DenyCommandContaining("rm -rf /", "format c:")

	if got := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "read_file"},
		Input: []byte(`{"command":"go test ./..."}`),
	}); got.Decision != Allow {
		t.Fatalf("expected allow, got %s", got.Decision)
	}
	if got := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "read_file"},
		Input: []byte(`{"command":"rm -rf /tmp"}`),
	}); got.Decision != Allow {
		t.Fatalf("expected non-run_command call to allow, got %s", got.Decision)
	}
	if got := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "run_command"},
		Input: []byte(`{"command":"rm -rf /tmp"}`),
	}); got.Decision != Deny {
		t.Fatalf("expected deny, got %s", got.Decision)
	}
	if got := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "run_command"},
		Input: []byte(`{"command":"rm -rf /tmp"}`),
	}); got.Reason.Code != "command.fragment_denied" {
		t.Fatalf("expected reason code command.fragment_denied, got %q", got.Reason.Code)
	}
}

func TestRequireApprovalForHTTPMethod(t *testing.T) {
	rule := RequireApprovalForHTTPMethod("GET", "POST")
	if got := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "http_request"},
		Input: []byte(`{"url":"https://example.com","method":"GET"}`),
	}); got.Decision != Allow {
		t.Fatalf("expected allow, got %s", got.Decision)
	}
	if got := rule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "http_request"},
		Input: []byte(`{"url":"https://example.com","method":"PUT"}`),
	}); got.Decision != RequireApproval {
		t.Fatalf("expected require approval, got %s", got.Decision)
	}
}

func TestRequireApprovalForURLHostAndDenyURLHost(t *testing.T) {
	requireRule := RequireApprovalForURLHost("example.com")
	if got := requireRule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "http_request"},
		Input: []byte(`{"url":"https://example.com/api"}`),
	}); got.Decision != Allow {
		t.Fatalf("expected allow, got %s", got.Decision)
	}
	if got := requireRule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "http_request"},
		Input: []byte(`{"url":"https://api.other.dev"}`),
	}); got.Decision != RequireApproval {
		t.Fatalf("expected require approval, got %s", got.Decision)
	}

	denyRule := DenyURLHost("blocked.dev")
	if got := denyRule(PolicyContext{
		Tool:  tool.ToolSpec{Name: "http_request"},
		Input: []byte(`{"url":"https://blocked.dev"}`),
	}); got.Decision != Deny {
		t.Fatalf("expected deny, got %s", got.Decision)
	}
}
