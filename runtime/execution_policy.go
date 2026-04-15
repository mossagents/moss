package runtime

import (
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/internal/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"github.com/mossagents/moss/toolpolicy"
)

type ToolAccess = toolpolicy.ToolAccess

const (
	ToolAccessAllow           ToolAccess = toolpolicy.ToolAccessAllow
	ToolAccessRequireApproval ToolAccess = toolpolicy.ToolAccessRequireApproval
	ToolAccessDeny            ToolAccess = toolpolicy.ToolAccessDeny
)

type CommandPolicy = toolpolicy.CommandPolicy

type HTTPPolicy = toolpolicy.HTTPPolicy

type CommandRule = toolpolicy.CommandRule

type HTTPRule = toolpolicy.HTTPRule

type ToolPolicy = toolpolicy.ToolPolicy

func ResolveToolPolicyForWorkspace(workspace, trust, approvalMode string) ToolPolicy {
	return toolpolicy.ResolveToolPolicyForWorkspace(workspace, trust, approvalMode)
}

func ValidateToolPolicy(policy ToolPolicy) error {
	return toolpolicy.ValidateToolPolicy(policy)
}

func NormalizeToolPolicy(policy ToolPolicy) ToolPolicy {
	return toolpolicy.NormalizeToolPolicy(policy)
}

func EncodeToolPolicyMetadata(policy ToolPolicy) (map[string]any, error) {
	return toolpolicy.EncodeToolPolicyMetadata(policy)
}

func DecodeToolPolicyMetadata(value any) (ToolPolicy, bool) {
	return toolpolicy.DecodeToolPolicyMetadata(value)
}

func SummarizeToolPolicy(policy ToolPolicy) session.ToolPolicySummary {
	return toolpolicy.SummarizeToolPolicy(policy)
}

func MergeToolPolicyPermissions(policy ToolPolicy, perms io.PermissionProfile) ToolPolicy {
	return toolpolicy.MergeToolPolicyPermissions(policy, perms)
}

func toolPolicyOf(k *kernel.Kernel) ToolPolicy {
	if policy, ok := currentToolPolicy(k); ok {
		return policy
	}
	return resolveToolPolicyForKernel(k, appconfig.TrustRestricted, "confirm")
}

func currentToolPolicy(k *kernel.Kernel) (ToolPolicy, bool) {
	st, ok := policystate.Lookup(k)
	if !ok {
		return ToolPolicy{}, false
	}
	return DecodeToolPolicyMetadata(st.Payload())
}

func toolPolicyForToolContext(ctx toolctx.ToolCallContext, k *kernel.Kernel, base ToolPolicy) ToolPolicy {
	if k == nil || strings.TrimSpace(ctx.SessionID) == "" {
		return base
	}
	sess, ok := k.SessionManager().Get(ctx.SessionID)
	if !ok {
		return base
	}
	return MergeToolPolicyPermissions(base, session.GrantedPermissionsOf(sess))
}

func resolveToolPolicyForKernel(k *kernel.Kernel, trust, approvalMode string) ToolPolicy {
	var sb sandbox.Sandbox
	if k != nil {
		sb = k.Sandbox()
	}
	return toolpolicy.ResolveToolPolicy(trust, approvalMode, commandPolicyDefaults(sb, "", nil))
}

func commandPolicyDefaults(sb sandbox.Sandbox, workspaceRoot string, _ workspace.Workspace) CommandPolicy {
	timeout := 30 * time.Second
	allowedPaths := []string{}
	network := workspace.ExecNetworkPolicy{Mode: workspace.ExecNetworkEnabled}
	if sb != nil {
		limits := sb.Limits()
		if limits.CommandTimeout > 0 {
			timeout = limits.CommandTimeout
		}
		allowedPaths = append(allowedPaths, limits.AllowedPaths...)
		if limits.NetworkPolicy.AllowOutbound || len(limits.NetworkPolicy.AllowedHosts) > 0 || len(limits.NetworkPolicy.BlockedPorts) > 0 {
			network.Mode = workspace.ExecNetworkEnabled
			network.AllowHosts = append([]string(nil), limits.NetworkPolicy.AllowedHosts...)
			if !limits.NetworkPolicy.AllowOutbound && len(limits.NetworkPolicy.AllowedHosts) == 0 {
				network.Mode = workspace.ExecNetworkDisabled
			}
		}
	} else if abs := absWorkspace(workspaceRoot); abs != "" {
		allowedPaths = append(allowedPaths, abs)
	}
	return CommandPolicy{
		DefaultTimeout: timeout,
		MaxTimeout:     timeout,
		AllowedPaths:   normalizeStringSlice(allowedPaths),
		ClearEnv:       true,
		Env:            sandbox.SafeInheritedEnvironment(),
		Network:        network,
	}
}

// NormalizeApprovalMode canonicalizes approval mode aliases onto the runtime
// policy vocabulary.
func NormalizeApprovalMode(mode string) string {
	return toolpolicy.NormalizeApprovalMode(mode)
}

// ValidateApprovalMode validates the canonical runtime approval-mode set.
func ValidateApprovalMode(mode string) error {
	return toolpolicy.ValidateApprovalMode(mode)
}

func cloneToolPolicy(policy ToolPolicy) ToolPolicy {
	policy.Command.AllowedPaths = append([]string(nil), policy.Command.AllowedPaths...)
	policy.Command.Env = cloneStringMap(policy.Command.Env)
	policy.Command.Network.AllowHosts = append([]string(nil), policy.Command.Network.AllowHosts...)
	policy.Command.Rules = append([]CommandRule(nil), policy.Command.Rules...)
	policy.HTTP.AllowedMethods = append([]string(nil), policy.HTTP.AllowedMethods...)
	policy.HTTP.AllowedSchemes = append([]string(nil), policy.HTTP.AllowedSchemes...)
	policy.HTTP.AllowedHosts = append([]string(nil), policy.HTTP.AllowedHosts...)
	policy.HTTP.Rules = append([]HTTPRule(nil), policy.HTTP.Rules...)
	policy.ProtectedPathPrefixes = append([]string(nil), policy.ProtectedPathPrefixes...)
	policy.ApprovalRequiredClasses = append([]tool.ApprovalClass(nil), policy.ApprovalRequiredClasses...)
	policy.DeniedClasses = append([]tool.ApprovalClass(nil), policy.DeniedClasses...)
	return policy
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func absWorkspace(workspaceRoot string) string {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return ""
	}
	abs, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return ""
	}
	return abs
}
