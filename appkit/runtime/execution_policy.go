package runtime

import (
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/sandbox"
)

const executionPolicyStateKey kernel.ExtensionStateKey = "execution-policy.state"

type ExecutionAccess string

const (
	ExecutionAccessAllow           ExecutionAccess = "allow"
	ExecutionAccessRequireApproval ExecutionAccess = "require-approval"
	ExecutionAccessDeny            ExecutionAccess = "deny"
)

type CommandExecutionPolicy struct {
	Access         ExecutionAccess        `json:"access"`
	DefaultTimeout time.Duration          `json:"default_timeout"`
	MaxTimeout     time.Duration          `json:"max_timeout"`
	AllowedPaths   []string               `json:"allowed_paths,omitempty"`
	ClearEnv       bool                   `json:"clear_env,omitempty"`
	Env            map[string]string      `json:"env,omitempty"`
	Network        port.ExecNetworkPolicy `json:"network"`
	Rules          []CommandRule          `json:"rules,omitempty"`
}

type HTTPExecutionPolicy struct {
	Access          ExecutionAccess `json:"access"`
	AllowedMethods  []string        `json:"allowed_methods,omitempty"`
	AllowedSchemes  []string        `json:"allowed_schemes,omitempty"`
	AllowedHosts    []string        `json:"allowed_hosts,omitempty"`
	DefaultTimeout  time.Duration   `json:"default_timeout"`
	MaxTimeout      time.Duration   `json:"max_timeout"`
	FollowRedirects bool            `json:"follow_redirects"`
	Rules           []HTTPRule      `json:"rules,omitempty"`
}

type CommandRule struct {
	Name   string          `json:"name,omitempty"`
	Match  string          `json:"match"`
	Access ExecutionAccess `json:"access"`
}

type HTTPRule struct {
	Name    string          `json:"name,omitempty"`
	Match   string          `json:"match"`
	Methods []string        `json:"methods,omitempty"`
	Access  ExecutionAccess `json:"access"`
}

type ExecutionPolicy struct {
	Trust        string                 `json:"trust"`
	ApprovalMode string                 `json:"approval_mode"`
	Command      CommandExecutionPolicy `json:"command"`
	HTTP         HTTPExecutionPolicy    `json:"http"`
}

type executionPolicyState struct {
	policy ExecutionPolicy
}

func WithExecutionPolicy(policy ExecutionPolicy) kernel.Option {
	return func(k *kernel.Kernel) {
		SetExecutionPolicy(k, policy)
	}
}

func SetExecutionPolicy(k *kernel.Kernel, policy ExecutionPolicy) {
	if k == nil {
		return
	}
	ensureExecutionPolicyState(k).policy = cloneExecutionPolicy(policy)
}

func ExecutionPolicyOf(k *kernel.Kernel) ExecutionPolicy {
	if k == nil {
		return ResolveExecutionPolicyForWorkspace("", appconfig.TrustRestricted, "confirm")
	}
	return cloneExecutionPolicy(ensureExecutionPolicyState(k).policy)
}

func ResolveExecutionPolicyForKernel(k *kernel.Kernel, trust, approvalMode string) ExecutionPolicy {
	var sb sandbox.Sandbox
	if k != nil {
		sb = k.Sandbox()
	}
	return resolveExecutionPolicy(trust, approvalMode, commandPolicyDefaults(sb, "", nil))
}

func ResolveExecutionPolicyForWorkspace(workspace, trust, approvalMode string) ExecutionPolicy {
	return resolveExecutionPolicy(trust, approvalMode, commandPolicyDefaults(nil, workspace, nil))
}

func ExecutionPolicyRules(policy ExecutionPolicy) []builtins.PolicyRule {
	rules := commandPolicyRules(policy.Command.Access, policy.Command.Rules)
	rules = append(rules, httpPolicyRules(policy.HTTP.Access, policy.HTTP.Rules)...)
	rules = append(rules,
		builtins.DenyCommandContaining("rm -rf /", "format c:", "del /f /q c:\\"),
	)
	return rules
}

func commandPolicyRules(defaultAccess ExecutionAccess, rules []CommandRule) []builtins.PolicyRule {
	if len(rules) == 0 && defaultAccess == ExecutionAccessAllow {
		return nil
	}
	converted := make([]builtins.CommandPatternRule, 0, len(rules))
	for _, rule := range rules {
		converted = append(converted, builtins.CommandPatternRule{
			Name:   rule.Name,
			Match:  rule.Match,
			Access: commandRuleDecision(rule.Access),
		})
	}
	return []builtins.PolicyRule{builtins.CommandRulesWithDefault(commandRuleDecision(defaultAccess), converted...)}
}

func httpPolicyRules(defaultAccess ExecutionAccess, rules []HTTPRule) []builtins.PolicyRule {
	if len(rules) == 0 && defaultAccess == ExecutionAccessAllow {
		return nil
	}
	converted := make([]builtins.HTTPPatternRule, 0, len(rules))
	for _, rule := range rules {
		converted = append(converted, builtins.HTTPPatternRule{
			Name:    rule.Name,
			Match:   rule.Match,
			Methods: append([]string(nil), rule.Methods...),
			Access:  commandRuleDecision(rule.Access),
		})
	}
	return []builtins.PolicyRule{builtins.HTTPRulesWithDefault(commandRuleDecision(defaultAccess), converted...)}
}

func commandRuleDecision(access ExecutionAccess) builtins.PolicyDecision {
	switch access {
	case ExecutionAccessDeny:
		return builtins.Deny
	case ExecutionAccessRequireApproval:
		return builtins.RequireApproval
	default:
		return builtins.Allow
	}
}

func ensureExecutionPolicyState(k *kernel.Kernel) *executionPolicyState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(executionPolicyStateKey, &executionPolicyState{})
	st := actual.(*executionPolicyState)
	if loaded {
		return st
	}
	st.policy = ResolveExecutionPolicyForKernel(k, appconfig.TrustRestricted, "confirm")
	return st
}

func resolveExecutionPolicy(trust, approvalMode string, defaults CommandExecutionPolicy) ExecutionPolicy {
	trust = appconfig.NormalizeTrustLevel(trust)
	mode := normalizeExecutionApprovalMode(approvalMode)
	policy := ExecutionPolicy{
		Trust:        trust,
		ApprovalMode: mode,
		Command: CommandExecutionPolicy{
			Access:         accessForApprovalMode(mode),
			DefaultTimeout: defaults.DefaultTimeout,
			MaxTimeout:     defaults.MaxTimeout,
			AllowedPaths:   append([]string(nil), defaults.AllowedPaths...),
			ClearEnv:       defaults.ClearEnv,
			Env:            cloneStringMap(defaults.Env),
			Network:        port.ExecNetworkPolicy{Mode: port.ExecNetworkEnabled},
		},
		HTTP: HTTPExecutionPolicy{
			Access:          accessForApprovalMode(mode),
			AllowedMethods:  []string{"GET", "HEAD", "POST"},
			AllowedSchemes:  []string{"http", "https"},
			DefaultTimeout:  30 * time.Second,
			MaxTimeout:      120 * time.Second,
			FollowRedirects: false,
		},
	}
	if trust == appconfig.TrustRestricted {
		if policy.Command.Access != ExecutionAccessDeny {
			policy.Command.Access = ExecutionAccessRequireApproval
		}
		if policy.HTTP.Access != ExecutionAccessDeny {
			policy.HTTP.Access = ExecutionAccessRequireApproval
		}
		policy.Command.Network = port.ExecNetworkPolicy{
			Mode:            port.ExecNetworkDisabled,
			PreferHardBlock: true,
			AllowSoftLimit:  true,
		}
	}
	if policy.Command.DefaultTimeout <= 0 {
		policy.Command.DefaultTimeout = 30 * time.Second
	}
	if policy.Command.MaxTimeout <= 0 {
		policy.Command.MaxTimeout = policy.Command.DefaultTimeout
	}
	if policy.Command.Network.Mode == "" {
		policy.Command.Network.Mode = port.ExecNetworkEnabled
	}
	return policy
}

func commandPolicyDefaults(sb sandbox.Sandbox, workspace string, _ port.Workspace) CommandExecutionPolicy {
	timeout := 30 * time.Second
	allowedPaths := []string{}
	if sb != nil {
		limits := sb.Limits()
		if limits.CommandTimeout > 0 {
			timeout = limits.CommandTimeout
		}
		allowedPaths = append(allowedPaths, limits.AllowedPaths...)
	} else if abs := absWorkspace(workspace); abs != "" {
		allowedPaths = append(allowedPaths, abs)
	}
	return CommandExecutionPolicy{
		DefaultTimeout: timeout,
		MaxTimeout:     timeout,
		AllowedPaths:   normalizeStringSlice(allowedPaths),
		ClearEnv:       true,
		Env:            sandbox.SafeInheritedEnvironment(),
	}
}

func normalizeExecutionApprovalMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "confirm", "ask", "safe":
		return "confirm"
	case "read-only", "readonly", "ro":
		return "read-only"
	case "full-auto", "full", "auto":
		return "full-auto"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

func accessForApprovalMode(mode string) ExecutionAccess {
	switch normalizeExecutionApprovalMode(mode) {
	case "read-only":
		return ExecutionAccessDeny
	case "confirm":
		return ExecutionAccessRequireApproval
	default:
		return ExecutionAccessAllow
	}
}

func cloneExecutionPolicy(policy ExecutionPolicy) ExecutionPolicy {
	policy.Command.AllowedPaths = append([]string(nil), policy.Command.AllowedPaths...)
	policy.Command.Env = cloneStringMap(policy.Command.Env)
	policy.Command.Network.AllowHosts = append([]string(nil), policy.Command.Network.AllowHosts...)
	policy.Command.Rules = append([]CommandRule(nil), policy.Command.Rules...)
	policy.HTTP.AllowedMethods = append([]string(nil), policy.HTTP.AllowedMethods...)
	policy.HTTP.AllowedSchemes = append([]string(nil), policy.HTTP.AllowedSchemes...)
	policy.HTTP.AllowedHosts = append([]string(nil), policy.HTTP.AllowedHosts...)
	policy.HTTP.Rules = append([]HTTPRule(nil), policy.HTTP.Rules...)
	return policy
}

func MergeExecutionPolicyPermissions(policy ExecutionPolicy, perms port.PermissionProfile) ExecutionPolicy {
	policy = cloneExecutionPolicy(policy)
	policy.Command.AllowedPaths = normalizeStringSlice(append(policy.Command.AllowedPaths, perms.CommandPaths...))
	policy.HTTP.AllowedHosts = normalizeStringSlice(append(policy.HTTP.AllowedHosts, perms.HTTPHosts...))
	if perms.CommandNetwork != nil {
		if perms.CommandNetwork.Enabled {
			policy.Command.Network.Mode = port.ExecNetworkEnabled
		}
		policy.Command.Network.AllowHosts = normalizeStringSlice(append(policy.Command.Network.AllowHosts, perms.CommandNetwork.AllowHosts...))
	}
	return policy
}

func ExecutionPolicyForToolContext(ctx port.ToolCallContext, k *kernel.Kernel, base ExecutionPolicy) ExecutionPolicy {
	if k == nil || strings.TrimSpace(ctx.SessionID) == "" {
		return base
	}
	sess, ok := k.SessionManager().Get(ctx.SessionID)
	if !ok {
		return base
	}
	return MergeExecutionPolicyPermissions(base, session.GrantedPermissionsOf(sess))
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
	return out
}

func absWorkspace(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return ""
	}
	return abs
}
