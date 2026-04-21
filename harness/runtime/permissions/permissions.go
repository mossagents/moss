package permissions

import (
	"fmt"
	"strings"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/collaboration"
	runtimepolicy "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
)

type Profile struct {
	Name                    string
	ApprovalPolicy          string
	Command                 runtimepolicy.CommandPolicy
	HTTP                    runtimepolicy.HTTPPolicy
	WorkspaceWriteAccess    runtimepolicy.ToolAccess
	MemoryWriteAccess       runtimepolicy.ToolAccess
	GraphMutationAccess     runtimepolicy.ToolAccess
	ProtectedPathPrefixes   []string
	ApprovalRequiredClasses []tool.ApprovalClass
	DeniedClasses           []tool.ApprovalClass
	AllowAsyncTasks         bool
}

type CompiledPolicy struct {
	Name                 string
	Trust                string
	Policy               runtimepolicy.ToolPolicy
	BaselineCapabilities collaboration.CapabilitySet
	AllowAsyncTasks      bool
}

func Compile(profile Profile, workspaceTrust string) (CompiledPolicy, error) {
	trust, err := validateTrust(workspaceTrust)
	if err != nil {
		return CompiledPolicy{}, err
	}
	approval := strings.TrimSpace(profile.ApprovalPolicy)
	if approval == "" {
		return CompiledPolicy{}, fmt.Errorf("permission profile %q approval_policy is required", strings.TrimSpace(profile.Name))
	}
	policy := runtimepolicy.ResolveToolPolicyForWorkspace("", trust, approval)
	policy = overlayPolicy(policy, profile)
	policy = runtimepolicy.NormalizeToolPolicy(policy)
	if err := runtimepolicy.ValidateToolPolicy(policy); err != nil {
		return CompiledPolicy{}, err
	}
	return CompiledPolicy{
		Name:                 strings.TrimSpace(profile.Name),
		Trust:                trust,
		Policy:               policy,
		BaselineCapabilities: baselineCapabilities(policy, profile.AllowAsyncTasks),
		AllowAsyncTasks:      profile.AllowAsyncTasks,
	}, nil
}

func Resolve(registry map[string]Profile, name string, workspaceTrust string) (CompiledPolicy, error) {
	name = strings.TrimSpace(name)
	profile, ok := registry[name]
	if !ok {
		return CompiledPolicy{}, fmt.Errorf("unknown permission profile %q", name)
	}
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = name
	}
	return Compile(profile, workspaceTrust)
}

func (c CompiledPolicy) EffectiveCapabilitiesForMode(mode collaboration.Mode) collaboration.CapabilitySet {
	return collaboration.IntersectSets(c.BaselineCapabilities, collaboration.CeilingForMode(mode))
}

func DeriveRequiredCapabilities(spec tool.ToolSpec) (collaboration.CapabilitySet, error) {
	required := collaboration.NewCapabilitySet()
	for _, capability := range spec.Capabilities {
		switch strings.ToLower(strings.TrimSpace(capability)) {
		case "filesystem", "workspace":
			required[collaboration.CapabilityReadWorkspace] = struct{}{}
		case "execution":
			required[collaboration.CapabilityExecuteCommands] = struct{}{}
		case "network":
			required[collaboration.CapabilityAccessNetwork] = struct{}{}
		case "memory":
			required[collaboration.CapabilityWriteMemory] = struct{}{}
		case "delegation", "scheduling":
			required[collaboration.CapabilityCreateAsyncTasks] = struct{}{}
		case "planning", "context":
			required[collaboration.CapabilityMutateGraph] = struct{}{}
		}
	}
	for _, effect := range spec.EffectiveEffects() {
		switch effect {
		case tool.EffectWritesWorkspace:
			required[collaboration.CapabilityReadWorkspace] = struct{}{}
			required[collaboration.CapabilityMutateWorkspace] = struct{}{}
		case tool.EffectWritesMemory:
			required[collaboration.CapabilityWriteMemory] = struct{}{}
		case tool.EffectGraphMutation:
			required[collaboration.CapabilityMutateGraph] = struct{}{}
		case tool.EffectExternalSideEffect:
			required[collaboration.CapabilityAccessNetwork] = struct{}{}
		}
	}
	switch spec.EffectiveSideEffectClass() {
	case tool.SideEffectWorkspace:
		required[collaboration.CapabilityReadWorkspace] = struct{}{}
	case tool.SideEffectProcess:
		required[collaboration.CapabilityExecuteCommands] = struct{}{}
	case tool.SideEffectNetwork:
		required[collaboration.CapabilityAccessNetwork] = struct{}{}
	case tool.SideEffectMemory:
		required[collaboration.CapabilityWriteMemory] = struct{}{}
	case tool.SideEffectTaskGraph:
		required[collaboration.CapabilityMutateGraph] = struct{}{}
	}
	if len(required) == 0 && len(spec.EffectiveEffects()) > 0 && spec.EffectiveEffects()[0] != tool.EffectReadOnly {
		return nil, fmt.Errorf("tool %q capabilities cannot be derived safely", strings.TrimSpace(spec.Name))
	}
	return required, nil
}

func overlayPolicy(base runtimepolicy.ToolPolicy, profile Profile) runtimepolicy.ToolPolicy {
	if profile.Command.Access != "" {
		base.Command.Access = profile.Command.Access
	}
	if profile.Command.DefaultTimeout > 0 {
		base.Command.DefaultTimeout = profile.Command.DefaultTimeout
	}
	if profile.Command.MaxTimeout > 0 {
		base.Command.MaxTimeout = profile.Command.MaxTimeout
	}
	if len(profile.Command.AllowedPaths) > 0 {
		base.Command.AllowedPaths = append([]string(nil), profile.Command.AllowedPaths...)
	}
	if profile.Command.ClearEnv {
		base.Command.ClearEnv = true
	}
	if len(profile.Command.Env) > 0 {
		base.Command.Env = cloneStringMap(profile.Command.Env)
	}
	if profile.Command.Network.Mode != "" {
		base.Command.Network = profile.Command.Network
	}
	if len(profile.Command.Rules) > 0 {
		base.Command.Rules = append([]runtimepolicy.CommandRule(nil), profile.Command.Rules...)
	}
	if profile.HTTP.Access != "" {
		base.HTTP.Access = profile.HTTP.Access
	}
	if len(profile.HTTP.AllowedMethods) > 0 {
		base.HTTP.AllowedMethods = append([]string(nil), profile.HTTP.AllowedMethods...)
	}
	if len(profile.HTTP.AllowedSchemes) > 0 {
		base.HTTP.AllowedSchemes = append([]string(nil), profile.HTTP.AllowedSchemes...)
	}
	if len(profile.HTTP.AllowedHosts) > 0 {
		base.HTTP.AllowedHosts = append([]string(nil), profile.HTTP.AllowedHosts...)
	}
	if profile.HTTP.DefaultTimeout > 0 {
		base.HTTP.DefaultTimeout = profile.HTTP.DefaultTimeout
	}
	if profile.HTTP.MaxTimeout > 0 {
		base.HTTP.MaxTimeout = profile.HTTP.MaxTimeout
	}
	if profile.HTTP.FollowRedirects {
		base.HTTP.FollowRedirects = true
	}
	if len(profile.HTTP.Rules) > 0 {
		base.HTTP.Rules = append([]runtimepolicy.HTTPRule(nil), profile.HTTP.Rules...)
	}
	if profile.WorkspaceWriteAccess != "" {
		base.WorkspaceWriteAccess = profile.WorkspaceWriteAccess
	}
	if profile.MemoryWriteAccess != "" {
		base.MemoryWriteAccess = profile.MemoryWriteAccess
	}
	if profile.GraphMutationAccess != "" {
		base.GraphMutationAccess = profile.GraphMutationAccess
	}
	if len(profile.ProtectedPathPrefixes) > 0 {
		base.ProtectedPathPrefixes = append([]string(nil), profile.ProtectedPathPrefixes...)
	}
	if len(profile.ApprovalRequiredClasses) > 0 {
		base.ApprovalRequiredClasses = append([]tool.ApprovalClass(nil), profile.ApprovalRequiredClasses...)
	}
	if len(profile.DeniedClasses) > 0 {
		base.DeniedClasses = append([]tool.ApprovalClass(nil), profile.DeniedClasses...)
	}
	return base
}

func baselineCapabilities(policy runtimepolicy.ToolPolicy, allowAsyncTasks bool) collaboration.CapabilitySet {
	capabilities := collaboration.NewCapabilitySet(collaboration.CapabilityReadWorkspace)
	if appconfig.ProjectAssetsAllowed(policy.Trust) {
		capabilities[collaboration.CapabilityLoadTrustedWorkspaceAssets] = struct{}{}
	}
	if policy.Command.Access != runtimepolicy.ToolAccessDeny {
		capabilities[collaboration.CapabilityExecuteCommands] = struct{}{}
	}
	if policy.HTTP.Access != runtimepolicy.ToolAccessDeny || policy.Command.Network.Mode != workspace.ExecNetworkDisabled {
		capabilities[collaboration.CapabilityAccessNetwork] = struct{}{}
	}
	if policy.WorkspaceWriteAccess != runtimepolicy.ToolAccessDeny {
		capabilities[collaboration.CapabilityMutateWorkspace] = struct{}{}
	}
	if policy.MemoryWriteAccess != runtimepolicy.ToolAccessDeny {
		capabilities[collaboration.CapabilityWriteMemory] = struct{}{}
	}
	if policy.GraphMutationAccess != runtimepolicy.ToolAccessDeny {
		capabilities[collaboration.CapabilityMutateGraph] = struct{}{}
	}
	if allowAsyncTasks {
		capabilities[collaboration.CapabilityCreateAsyncTasks] = struct{}{}
	}
	return capabilities
}

func validateTrust(trust string) (string, error) {
	trust = strings.ToLower(strings.TrimSpace(trust))
	switch trust {
	case appconfig.TrustTrusted, appconfig.TrustRestricted:
		return trust, nil
	default:
		return "", fmt.Errorf("unknown workspace trust %q", strings.TrimSpace(trust))
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
