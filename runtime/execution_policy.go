package runtime

import (
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/runtime/policy/policystate"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	toolctx "github.com/mossagents/moss/kernel/toolctx"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	policypack "github.com/mossagents/moss/runtime/policy"
)

type ToolAccess = policypack.ToolAccess

const (
	ToolAccessAllow           ToolAccess = policypack.ToolAccessAllow
	ToolAccessRequireApproval ToolAccess = policypack.ToolAccessRequireApproval
	ToolAccessDeny            ToolAccess = policypack.ToolAccessDeny
)

type CommandPolicy = policypack.CommandPolicy

type HTTPPolicy = policypack.HTTPPolicy

type CommandRule = policypack.CommandRule

type HTTPRule = policypack.HTTPRule

type ToolPolicy = policypack.ToolPolicy

func ResolveToolPolicyForWorkspace(workspace, trust, approvalMode string) ToolPolicy {
	return policypack.ResolveToolPolicyForWorkspace(workspace, trust, approvalMode)
}

func ValidateToolPolicy(policy ToolPolicy) error {
	return policypack.ValidateToolPolicy(policy)
}

func NormalizeToolPolicy(policy ToolPolicy) ToolPolicy {
	return policypack.NormalizeToolPolicy(policy)
}

func EncodeToolPolicyMetadata(policy ToolPolicy) (map[string]any, error) {
	return policypack.EncodeToolPolicyMetadata(policy)
}

func DecodeToolPolicyMetadata(value any) (ToolPolicy, bool) {
	return policypack.DecodeToolPolicyMetadata(value)
}

func SummarizeToolPolicy(policy ToolPolicy) session.ToolPolicySummary {
	return policypack.SummarizeToolPolicy(policy)
}

func MergeToolPolicyPermissions(policy ToolPolicy, perms io.PermissionProfile) ToolPolicy {
	return policypack.MergeToolPolicyPermissions(policy, perms)
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
	return policypack.ResolveToolPolicy(trust, approvalMode, commandPolicyDefaults(sb, "", nil))
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
	return policypack.NormalizeApprovalMode(mode)
}

// ValidateApprovalMode validates the canonical runtime approval-mode set.
func ValidateApprovalMode(mode string) error {
	return policypack.ValidateApprovalMode(mode)
}

func cloneToolPolicy(p ToolPolicy) ToolPolicy {
	p.Command.AllowedPaths = append([]string(nil), p.Command.AllowedPaths...)
	p.Command.Env = cloneStringMap(p.Command.Env)
	p.Command.Network.AllowHosts = append([]string(nil), p.Command.Network.AllowHosts...)
	p.Command.Rules = append([]CommandRule(nil), p.Command.Rules...)
	p.HTTP.AllowedMethods = append([]string(nil), p.HTTP.AllowedMethods...)
	p.HTTP.AllowedSchemes = append([]string(nil), p.HTTP.AllowedSchemes...)
	p.HTTP.AllowedHosts = append([]string(nil), p.HTTP.AllowedHosts...)
	p.HTTP.Rules = append([]HTTPRule(nil), p.HTTP.Rules...)
	p.ProtectedPathPrefixes = append([]string(nil), p.ProtectedPathPrefixes...)
	p.ApprovalRequiredClasses = append([]tool.ApprovalClass(nil), p.ApprovalRequiredClasses...)
	p.DeniedClasses = append([]tool.ApprovalClass(nil), p.DeniedClasses...)
	return p
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


