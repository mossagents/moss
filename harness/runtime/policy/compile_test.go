package policy

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/hooks/governance"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

func TestCompileRulesApplyCommandRules(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.Command.Rules = []CommandRule{{
		Name:   "git-push",
		Match:  "git push*",
		Access: ToolAccessRequireApproval,
	}}

	rules := CompileRules(policy)
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	result := governance.Allow
	for _, rule := range rules {
		next := rule(governance.PolicyContext{
			Tool:  tool.ToolSpec{Name: "run_command"},
			Input: input,
		})
		if next.Decision == governance.Deny {
			result = governance.Deny
			break
		}
		if next.Decision == governance.RequireApproval {
			result = governance.RequireApproval
		}
	}
	if result != governance.RequireApproval {
		t.Fatalf("decision = %s, want %s", result, governance.RequireApproval)
	}
}

func TestCompileRulesAllowRuleOverridesDefaultConfirm(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "confirm")
	policy.Command.Rules = []CommandRule{{
		Name:   "git-status",
		Match:  "git status*",
		Access: ToolAccessAllow,
	}}

	rules := CompileRules(policy)
	input, _ := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"status"},
	})
	result := governance.Allow
	for _, rule := range rules {
		next := rule(governance.PolicyContext{
			Tool:  tool.ToolSpec{Name: "run_command"},
			Input: input,
		})
		if next.Decision == governance.Deny {
			result = governance.Deny
			break
		}
		if next.Decision == governance.RequireApproval {
			result = governance.RequireApproval
		}
	}
	if result != governance.Allow {
		t.Fatalf("decision = %s, want %s", result, governance.Allow)
	}
}

// TestEvaluateDenyDangerousCommand 验证 Evaluate 对危险命令返回 Deny。
func TestEvaluateDenyDangerousCommand(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	input, _ := json.Marshal(map[string]any{"command": "rm -rf /"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "run_command"}, input)
	if decision != governance.Deny {
		t.Fatalf("expected Deny for dangerous command, got %s", decision)
	}
}

// TestEvaluateProtectedPathPrefix 验证 Evaluate 对受保护路径返回 RequireApproval。
func TestEvaluateProtectedPathPrefix(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.ProtectedPathPrefixes = []string{"/etc/"}
	input, _ := json.Marshal(map[string]any{"path": "/etc/hosts"})
	decision := Evaluate(policy, tool.ToolSpec{Name: "read_file"}, input)
	if decision != governance.RequireApproval {
		t.Fatalf("expected RequireApproval for protected path, got %s", decision)
	}
}

// TestEvaluateNormalToolAllowed 验证普通工具在全自动模式下直接放行。
func TestEvaluateNormalToolAllowed(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	decision := Evaluate(policy, tool.ToolSpec{Name: "list_files"}, nil)
	if decision != governance.Allow {
		t.Fatalf("expected Allow for normal tool in full-auto, got %s", decision)
	}
}

// TestEvaluateExplicitUserApprovalClass 验证 ApprovalClassExplicitUser 的工具需要审批。
func TestEvaluateExplicitUserApprovalClass(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	spec := tool.ToolSpec{
		Name:          "delete_account",
		ApprovalClass: tool.ApprovalClassExplicitUser,
	}
	decision := Evaluate(policy, spec, nil)
	if decision != governance.RequireApproval {
		t.Fatalf("expected RequireApproval for ApprovalClassExplicitUser, got %s", decision)
	}
}

// TestEvaluateDenyApprovalClass 验证被拒绝的审批类返回 Deny。
func TestEvaluateDenyApprovalClass(t *testing.T) {
	policy := ResolveToolPolicyForWorkspace("", appconfig.TrustTrusted, "full-auto")
	policy.DeniedClasses = []tool.ApprovalClass{tool.ApprovalClassSupervisorOnly}
	spec := tool.ToolSpec{
		Name:          "privileged_op",
		ApprovalClass: tool.ApprovalClassSupervisorOnly,
	}
	decision := Evaluate(policy, spec, nil)
	if decision != governance.Deny {
		t.Fatalf("expected Deny for denied approval class, got %s", decision)
	}
}

// ── CompileSecurityPolicy tests ──

func TestCompileSecurityPolicy_ProtectedPathsAbsolute(t *testing.T) {
	wsRoot := t.TempDir()
	tp := ToolPolicy{
		Trust:                 appconfig.TrustTrusted,
		ApprovalMode:          "confirm",
		ProtectedPathPrefixes: []string{".git", ".moss"},
	}
	sp := CompileSecurityPolicy(tp, wsRoot)
	if len(sp.ProtectedPaths) < 2 {
		t.Fatalf("expected at least 2 protected paths, got %d: %v", len(sp.ProtectedPaths), sp.ProtectedPaths)
	}
	for _, p := range sp.ProtectedPaths {
		if !filepath.IsAbs(p) {
			t.Fatalf("protected path should be absolute: %q", p)
		}
	}
}

func TestCompileSecurityPolicy_DefaultProtectedPaths(t *testing.T) {
	wsRoot := t.TempDir()
	tp := ToolPolicy{
		Trust:        appconfig.TrustTrusted,
		ApprovalMode: "full-auto",
	}
	sp := CompileSecurityPolicy(tp, wsRoot)
	gitFound := false
	mossFound := false
	for _, p := range sp.ProtectedPaths {
		if strings.HasSuffix(p, ".git") || strings.HasSuffix(p, ".git"+string(filepath.Separator)) {
			gitFound = true
		}
		if strings.HasSuffix(p, ".moss") || strings.HasSuffix(p, ".moss"+string(filepath.Separator)) {
			mossFound = true
		}
	}
	if !gitFound {
		t.Fatal("expected .git in default protected paths")
	}
	if !mossFound {
		t.Fatal("expected .moss in default protected paths")
	}
}

func TestCompileSecurityPolicy_ReadOnlyFromDenyWrite(t *testing.T) {
	tp := ToolPolicy{
		Trust:                appconfig.TrustTrusted,
		ApprovalMode:         "read-only",
		WorkspaceWriteAccess: ToolAccessDeny,
	}
	sp := CompileSecurityPolicy(tp, t.TempDir())
	if !sp.ReadOnly {
		t.Fatal("expected ReadOnly when WorkspaceWriteAccess is deny")
	}
}

func TestCompileSecurityPolicy_NotReadOnlyForAllow(t *testing.T) {
	tp := ToolPolicy{
		Trust:                appconfig.TrustTrusted,
		ApprovalMode:         "full-auto",
		WorkspaceWriteAccess: ToolAccessAllow,
	}
	sp := CompileSecurityPolicy(tp, t.TempDir())
	if sp.ReadOnly {
		t.Fatal("expected not ReadOnly when WorkspaceWriteAccess is allow")
	}
}

func TestCompileSecurityPolicy_NetworkMode(t *testing.T) {
	tp := ToolPolicy{
		Trust:        appconfig.TrustRestricted,
		ApprovalMode: "confirm",
		Command: CommandPolicy{
			Network: workspace.ExecNetworkPolicy{
				Mode:       workspace.ExecNetworkDisabled,
				AllowHosts: []string{"api.example.com"},
			},
		},
	}
	sp := CompileSecurityPolicy(tp, t.TempDir())
	if sp.NetworkMode != workspace.ExecNetworkDisabled {
		t.Fatalf("expected disabled network mode, got %q", sp.NetworkMode)
	}
	if len(sp.AllowedHosts) != 1 || sp.AllowedHosts[0] != "api.example.com" {
		t.Fatalf("expected AllowedHosts=[api.example.com], got %v", sp.AllowedHosts)
	}
}

func TestResolveToolPolicyForWorkspace_RestrictedUsesSoftDegradableNetworkBlock(t *testing.T) {
	tp := ResolveToolPolicyForWorkspace(t.TempDir(), appconfig.TrustRestricted, "confirm")
	if tp.Command.Network.Mode != workspace.ExecNetworkDisabled {
		t.Fatalf("network mode = %q, want %q", tp.Command.Network.Mode, workspace.ExecNetworkDisabled)
	}
	if !tp.Command.Network.PreferHardBlock {
		t.Fatal("restricted policy should prefer hard network blocking when available")
	}
	if !tp.Command.Network.AllowSoftLimit {
		t.Fatal("restricted policy should explicitly allow soft fallback on governance-only backends")
	}
}

func TestCompileSecurityPolicy_NoDuplicateDefaults(t *testing.T) {
	wsRoot := t.TempDir()
	tp := ToolPolicy{
		Trust:                 appconfig.TrustTrusted,
		ApprovalMode:          "confirm",
		ProtectedPathPrefixes: []string{".git"},
	}
	sp := CompileSecurityPolicy(tp, wsRoot)
	gitCount := 0
	for _, p := range sp.ProtectedPaths {
		if strings.HasSuffix(p, ".git") {
			gitCount++
		}
	}
	if gitCount != 1 {
		t.Fatalf("expected exactly 1 .git entry, got %d in %v", gitCount, sp.ProtectedPaths)
	}
}
