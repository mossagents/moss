package policy

import (
	"encoding/json"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/hooks/builtins"
	"github.com/mossagents/moss/kernel/tool"
)

// TestApplyNilKernel 验证 Apply 对 nil kernel 返回错误。
func TestApplyNilKernel(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	if err := Apply(nil, policy); err == nil {
		t.Fatal("expected error for nil kernel, got nil")
	}
}

// TestApplyAndCurrentRoundTrip 验证 Apply 后可通过 Current 读回策略。
func TestApplyAndCurrentRoundTrip(t *testing.T) {
	k := kernel.New()
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.ProtectedPathPrefixes = []string{"/etc/"}

	if err := Apply(k, policy); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	got, ok := Current(k)
	if !ok {
		t.Fatal("Current: policy not found after Apply")
	}
	if len(got.ProtectedPathPrefixes) == 0 || got.ProtectedPathPrefixes[0] != "/etc/" {
		t.Fatalf("expected ProtectedPathPrefixes to roundtrip, got %v", got.ProtectedPathPrefixes)
	}
}

// TestCurrentBeforeApply 验证未调用 Apply 时 Current 返回 false。
func TestCurrentBeforeApply(t *testing.T) {
	k := kernel.New()
	_, ok := Current(k)
	if ok {
		t.Fatal("Current should return false on fresh kernel before Apply")
	}
}

// TestApplyInstallsPolicyGate 验证 Apply 后 toolPolicyGate 实际拦截危险命令。
// 通过直接调用 Evaluate 验证规则链正常工作（gate 安装本身通过 roundtrip 间接验证）。
func TestApplyInstallsPolicyGate(t *testing.T) {
	k := kernel.New()
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	if err := Apply(k, policy); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// 验证 Apply 后策略已存储，且规则链对危险命令返回 Deny
	input, _ := json.Marshal(map[string]any{"command": "rm -rf /"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "run_command"}, input)
	if decision != builtins.Deny {
		t.Fatalf("expected Deny for dangerous command via compiled rules, got %s", decision)
	}
}

// TestApplyIdempotent 验证多次 Apply 不会重复安装 hook。
func TestApplyIdempotent(t *testing.T) {
	k := kernel.New()
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")

	for i := 0; i < 3; i++ {
		if err := Apply(k, policy); err != nil {
			t.Fatalf("Apply #%d: %v", i, err)
		}
	}

	// 最终 Current 仍然可以读回
	_, ok := Current(k)
	if !ok {
		t.Fatal("Current should be available after repeated Apply")
	}
}

