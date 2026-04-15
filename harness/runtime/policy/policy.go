package policy

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/harness/sandbox"
)

type ToolAccess string

const (
	ToolAccessAllow           ToolAccess = "allow"
	ToolAccessRequireApproval ToolAccess = "require-approval"
	ToolAccessDeny            ToolAccess = "deny"
)

type CommandPolicy struct {
	Access         ToolAccess                  `json:"access"`
	DefaultTimeout time.Duration               `json:"default_timeout"`
	MaxTimeout     time.Duration               `json:"max_timeout"`
	AllowedPaths   []string                    `json:"allowed_paths,omitempty"`
	ClearEnv       bool                        `json:"clear_env,omitempty"`
	Env            map[string]string           `json:"env,omitempty"`
	Network        workspace.ExecNetworkPolicy `json:"network"`
	Rules          []CommandRule               `json:"rules,omitempty"`
}

type HTTPPolicy struct {
	Access          ToolAccess    `json:"access"`
	AllowedMethods  []string      `json:"allowed_methods,omitempty"`
	AllowedSchemes  []string      `json:"allowed_schemes,omitempty"`
	AllowedHosts    []string      `json:"allowed_hosts,omitempty"`
	DefaultTimeout  time.Duration `json:"default_timeout"`
	MaxTimeout      time.Duration `json:"max_timeout"`
	FollowRedirects bool          `json:"follow_redirects"`
	Rules           []HTTPRule    `json:"rules,omitempty"`
}

type CommandRule struct {
	Name   string     `json:"name,omitempty"`
	Match  string     `json:"match"`
	Access ToolAccess `json:"access"`
}

type HTTPRule struct {
	Name    string     `json:"name,omitempty"`
	Match   string     `json:"match"`
	Methods []string   `json:"methods,omitempty"`
	Access  ToolAccess `json:"access"`
}

type ToolPolicy struct {
	Trust                   string               `json:"trust"`
	ApprovalMode            string               `json:"approval_mode"`
	Command                 CommandPolicy        `json:"command"`
	HTTP                    HTTPPolicy           `json:"http"`
	WorkspaceWriteAccess    ToolAccess           `json:"workspace_write_access"`
	MemoryWriteAccess       ToolAccess           `json:"memory_write_access"`
	GraphMutationAccess     ToolAccess           `json:"graph_mutation_access"`
	ProtectedPathPrefixes   []string             `json:"protected_path_prefixes,omitempty"`
	ApprovalRequiredClasses []tool.ApprovalClass `json:"approval_required_classes,omitempty"`
	DeniedClasses           []tool.ApprovalClass `json:"denied_classes,omitempty"`
}

type toolPolicyMetadataEnvelope struct {
	Version int            `json:"version"`
	Policy  map[string]any `json:"policy"`
}

func ResolveToolPolicyForWorkspace(workspaceRoot, trust, approvalMode string) ToolPolicy {
	return ResolveToolPolicy(trust, approvalMode, commandPolicyDefaults(workspaceRoot))
}

func ResolveToolPolicy(trust, approvalMode string, defaults CommandPolicy) ToolPolicy {
	trust = appconfig.NormalizeTrustLevel(trust)
	mode := normalizeToolApprovalMode(approvalMode)
	defaultAccess := accessForApprovalMode(mode)
	policy := ToolPolicy{
		Trust:                trust,
		ApprovalMode:         mode,
		WorkspaceWriteAccess: defaultAccess,
		MemoryWriteAccess:    defaultAccess,
		GraphMutationAccess:  defaultAccess,
		Command: CommandPolicy{
			Access:         defaultAccess,
			DefaultTimeout: defaults.DefaultTimeout,
			MaxTimeout:     defaults.MaxTimeout,
			AllowedPaths:   append([]string(nil), defaults.AllowedPaths...),
			ClearEnv:       defaults.ClearEnv,
			Env:            CloneStringMap(defaults.Env),
			Network:        defaults.Network,
		},
		HTTP: HTTPPolicy{
			Access:          defaultAccess,
			AllowedMethods:  []string{"GET", "HEAD", "POST"},
			AllowedSchemes:  []string{"http", "https"},
			DefaultTimeout:  30 * time.Second,
			MaxTimeout:      120 * time.Second,
			FollowRedirects: false,
		},
	}
	switch mode {
	case "read-only":
		policy.DeniedClasses = []tool.ApprovalClass{tool.ApprovalClassSupervisorOnly}
	case "confirm":
		policy.ProtectedPathPrefixes = []string{".git", ".moss"}
		policy.ApprovalRequiredClasses = []tool.ApprovalClass{tool.ApprovalClassExplicitUser}
		policy.DeniedClasses = []tool.ApprovalClass{tool.ApprovalClassSupervisorOnly}
	default:
		policy.DeniedClasses = []tool.ApprovalClass{tool.ApprovalClassSupervisorOnly}
	}
	if trust == appconfig.TrustRestricted {
		if policy.Command.Access != ToolAccessDeny {
			policy.Command.Access = ToolAccessRequireApproval
		}
		if policy.HTTP.Access != ToolAccessDeny {
			policy.HTTP.Access = ToolAccessRequireApproval
		}
		if policy.WorkspaceWriteAccess != ToolAccessDeny {
			policy.WorkspaceWriteAccess = ToolAccessRequireApproval
		}
		if policy.MemoryWriteAccess != ToolAccessDeny {
			policy.MemoryWriteAccess = ToolAccessRequireApproval
		}
		if policy.GraphMutationAccess != ToolAccessDeny {
			policy.GraphMutationAccess = ToolAccessRequireApproval
		}
		policy.Command.Network = workspace.ExecNetworkPolicy{
			Mode:            workspace.ExecNetworkDisabled,
			PreferHardBlock: true,
			AllowSoftLimit:  true,
		}
	}
	return normalizeResolvedToolPolicy(policy)
}

func ValidateToolPolicy(policy ToolPolicy) error {
	if isZeroToolPolicy(policy) {
		return fmt.Errorf("tool policy must not be empty")
	}
	trust := strings.ToLower(strings.TrimSpace(policy.Trust))
	switch trust {
	case appconfig.TrustTrusted, appconfig.TrustRestricted:
	default:
		return fmt.Errorf("tool policy trust %q is invalid", strings.TrimSpace(policy.Trust))
	}
	mode := normalizeToolApprovalMode(policy.ApprovalMode)
	switch mode {
	case "read-only", "confirm", "full-auto":
	default:
		return fmt.Errorf("tool policy approval mode %q is invalid", strings.TrimSpace(policy.ApprovalMode))
	}
	for _, check := range []struct {
		name  string
		value ToolAccess
	}{
		{name: "command", value: policy.Command.Access},
		{name: "http", value: policy.HTTP.Access},
		{name: "workspace_write", value: policy.WorkspaceWriteAccess},
		{name: "memory_write", value: policy.MemoryWriteAccess},
		{name: "graph_mutation", value: policy.GraphMutationAccess},
	} {
		if check.value == "" {
			continue
		}
		if !isValidToolAccess(check.value) {
			return fmt.Errorf("tool policy %s access %q is invalid", check.name, check.value)
		}
	}
	for i, rule := range policy.Command.Rules {
		if strings.TrimSpace(rule.Match) == "" {
			return fmt.Errorf("command rule %d: match is required", i+1)
		}
		if !isValidToolAccess(rule.Access) {
			return fmt.Errorf("command rule %d: access %q is invalid", i+1, rule.Access)
		}
	}
	for i, rule := range policy.HTTP.Rules {
		if strings.TrimSpace(rule.Match) == "" {
			return fmt.Errorf("http rule %d: match is required", i+1)
		}
		if !isValidToolAccess(rule.Access) {
			return fmt.Errorf("http rule %d: access %q is invalid", i+1, rule.Access)
		}
	}
	return nil
}

func NormalizeToolPolicy(policy ToolPolicy) ToolPolicy {
	policy = CloneToolPolicy(policy)
	policy.Trust = appconfig.NormalizeTrustLevel(policy.Trust)
	policy.ApprovalMode = normalizeToolApprovalMode(policy.ApprovalMode)
	defaultAccess := accessForApprovalMode(policy.ApprovalMode)
	if policy.Command.Access == "" {
		policy.Command.Access = defaultAccess
	}
	if policy.HTTP.Access == "" {
		policy.HTTP.Access = defaultAccess
	}
	if policy.WorkspaceWriteAccess == "" {
		policy.WorkspaceWriteAccess = defaultAccess
	}
	if policy.MemoryWriteAccess == "" {
		policy.MemoryWriteAccess = defaultAccess
	}
	if policy.GraphMutationAccess == "" {
		policy.GraphMutationAccess = defaultAccess
	}
	if policy.Command.DefaultTimeout <= 0 {
		policy.Command.DefaultTimeout = 30 * time.Second
	}
	if policy.Command.MaxTimeout <= 0 {
		policy.Command.MaxTimeout = policy.Command.DefaultTimeout
	}
	if policy.HTTP.DefaultTimeout <= 0 {
		policy.HTTP.DefaultTimeout = 30 * time.Second
	}
	if policy.HTTP.MaxTimeout <= 0 {
		policy.HTTP.MaxTimeout = 120 * time.Second
	}
	if len(policy.HTTP.AllowedMethods) == 0 {
		policy.HTTP.AllowedMethods = []string{"GET", "HEAD", "POST"}
	}
	if len(policy.HTTP.AllowedSchemes) == 0 {
		policy.HTTP.AllowedSchemes = []string{"http", "https"}
	}
	if policy.Command.Network.Mode == "" {
		policy.Command.Network.Mode = workspace.ExecNetworkEnabled
	}
	policy.Command.AllowedPaths = normalizeStringSlice(policy.Command.AllowedPaths)
	policy.HTTP.AllowedMethods = normalizeUpperStringSlice(policy.HTTP.AllowedMethods)
	policy.HTTP.AllowedSchemes = normalizeLowerStringSlice(policy.HTTP.AllowedSchemes)
	policy.HTTP.AllowedHosts = normalizeLowerStringSlice(policy.HTTP.AllowedHosts)
	policy.Command.Network.AllowHosts = normalizeLowerStringSlice(policy.Command.Network.AllowHosts)
	policy.ProtectedPathPrefixes = normalizeStringSlice(policy.ProtectedPathPrefixes)
	policy.ApprovalRequiredClasses = normalizeApprovalClasses(policy.ApprovalRequiredClasses)
	policy.DeniedClasses = normalizeApprovalClasses(policy.DeniedClasses)
	policy.Command.Rules = normalizeCommandRules(policy.Command.Rules)
	policy.HTTP.Rules = normalizeHTTPRules(policy.HTTP.Rules)
	return policy
}

func EncodeToolPolicyMetadata(policy ToolPolicy) (map[string]any, error) {
	policy = NormalizeToolPolicy(policy)
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return map[string]any{
		"version": session.ToolPolicyMetadataVersion,
		"policy":  payload,
	}, nil
}

func DecodeToolPolicyMetadata(value any) (ToolPolicy, bool) {
	if value == nil {
		return ToolPolicy{}, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ToolPolicy{}, false
	}
	var envelope toolPolicyMetadataEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return ToolPolicy{}, false
	}
	if envelope.Version != session.ToolPolicyMetadataVersion || len(envelope.Policy) == 0 {
		return ToolPolicy{}, false
	}
	policyData, err := json.Marshal(envelope.Policy)
	if err != nil {
		return ToolPolicy{}, false
	}
	var policy ToolPolicy
	if err := json.Unmarshal(policyData, &policy); err != nil {
		return ToolPolicy{}, false
	}
	return NormalizeToolPolicy(policy), true
}

func SummarizeToolPolicy(policy ToolPolicy) session.ToolPolicySummary {
	policy = NormalizeToolPolicy(policy)
	return session.ToolPolicySummary{
		Version:                 session.ToolPolicyMetadataVersion,
		Trust:                   policy.Trust,
		ApprovalMode:            policy.ApprovalMode,
		CommandAccess:           string(policy.Command.Access),
		HTTPAccess:              string(policy.HTTP.Access),
		WorkspaceWriteAccess:    string(policy.WorkspaceWriteAccess),
		MemoryWriteAccess:       string(policy.MemoryWriteAccess),
		GraphMutationAccess:     string(policy.GraphMutationAccess),
		ProtectedPathPrefixes:   append([]string(nil), policy.ProtectedPathPrefixes...),
		ApprovalRequiredClasses: approvalClassStrings(policy.ApprovalRequiredClasses),
		DeniedClasses:           approvalClassStrings(policy.DeniedClasses),
	}
}

func MergeToolPolicyPermissions(policy ToolPolicy, perms io.PermissionProfile) ToolPolicy {
	policy = NormalizeToolPolicy(policy)
	policy.Command.AllowedPaths = normalizeStringSlice(append(policy.Command.AllowedPaths, perms.CommandPaths...))
	policy.HTTP.AllowedHosts = normalizeLowerStringSlice(append(policy.HTTP.AllowedHosts, perms.HTTPHosts...))
	if perms.CommandNetwork != nil {
		if perms.CommandNetwork.Enabled {
			policy.Command.Network.Mode = workspace.ExecNetworkEnabled
		}
		policy.Command.Network.AllowHosts = normalizeLowerStringSlice(append(policy.Command.Network.AllowHosts, perms.CommandNetwork.AllowHosts...))
	}
	return policy
}

func NormalizeApprovalMode(mode string) string {
	return normalizeToolApprovalMode(mode)
}

func ValidateApprovalMode(mode string) error {
	switch NormalizeApprovalMode(mode) {
	case "read-only", "confirm", "full-auto":
		return nil
	default:
		return fmt.Errorf("unknown approval mode %q (supported: read-only, confirm, full-auto)", strings.TrimSpace(mode))
	}
}

func normalizeResolvedToolPolicy(policy ToolPolicy) ToolPolicy {
	policy = NormalizeToolPolicy(policy)
	if policy.Command.Network.Mode == "" {
		policy.Command.Network.Mode = workspace.ExecNetworkEnabled
	}
	return policy
}

func commandPolicyDefaults(workspaceRoot string) CommandPolicy {
	timeout := 30 * time.Second
	allowedPaths := []string{}
	if abs := absWorkspace(workspaceRoot); abs != "" {
		allowedPaths = append(allowedPaths, abs)
	}
	return CommandPolicy{
		DefaultTimeout: timeout,
		MaxTimeout:     timeout,
		AllowedPaths:   normalizeStringSlice(allowedPaths),
		ClearEnv:       true,
		Env:            sandbox.SafeInheritedEnvironment(),
		Network:        workspace.ExecNetworkPolicy{Mode: workspace.ExecNetworkEnabled},
	}
}

func normalizeToolApprovalMode(mode string) string {
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

func accessForApprovalMode(mode string) ToolAccess {
	switch normalizeToolApprovalMode(mode) {
	case "read-only":
		return ToolAccessDeny
	case "confirm":
		return ToolAccessRequireApproval
	default:
		return ToolAccessAllow
	}
}

// CloneToolPolicy returns a deep copy of the tool policy.
func CloneToolPolicy(policy ToolPolicy) ToolPolicy {
	policy.Command.AllowedPaths = append([]string(nil), policy.Command.AllowedPaths...)
	policy.Command.Env = CloneStringMap(policy.Command.Env)
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

func normalizeCommandRules(rules []CommandRule) []CommandRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]CommandRule, 0, len(rules))
	for _, rule := range rules {
		match := strings.TrimSpace(rule.Match)
		if match == "" {
			continue
		}
		access := normalizeToolAccess(rule.Access)
		if access == "" {
			continue
		}
		out = append(out, CommandRule{
			Name:   strings.TrimSpace(rule.Name),
			Match:  match,
			Access: access,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeHTTPRules(rules []HTTPRule) []HTTPRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]HTTPRule, 0, len(rules))
	for _, rule := range rules {
		match := strings.TrimSpace(rule.Match)
		if match == "" {
			continue
		}
		access := normalizeToolAccess(rule.Access)
		if access == "" {
			continue
		}
		out = append(out, HTTPRule{
			Name:    strings.TrimSpace(rule.Name),
			Match:   match,
			Methods: normalizeUpperStringSlice(rule.Methods),
			Access:  access,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeApprovalClasses(classes []tool.ApprovalClass) []tool.ApprovalClass {
	if len(classes) == 0 {
		return nil
	}
	out := make([]tool.ApprovalClass, 0, len(classes))
	seen := make(map[string]struct{}, len(classes))
	for _, class := range classes {
		value := strings.TrimSpace(string(class))
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tool.ApprovalClass(value))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func approvalClassStrings(classes []tool.ApprovalClass) []string {
	if len(classes) == 0 {
		return nil
	}
	out := make([]string, 0, len(classes))
	for _, class := range classes {
		value := strings.TrimSpace(string(class))
		if value != "" {
			out = append(out, value)
		}
	}
	return normalizeStringSlice(out)
}

func normalizeToolAccess(access ToolAccess) ToolAccess {
	switch strings.ToLower(strings.TrimSpace(string(access))) {
	case string(ToolAccessAllow):
		return ToolAccessAllow
	case string(ToolAccessRequireApproval):
		return ToolAccessRequireApproval
	case string(ToolAccessDeny):
		return ToolAccessDeny
	default:
		return ""
	}
}

func isValidToolAccess(access ToolAccess) bool {
	return normalizeToolAccess(access) != ""
}

func isZeroToolPolicy(policy ToolPolicy) bool {
	return strings.TrimSpace(policy.Trust) == "" &&
		strings.TrimSpace(policy.ApprovalMode) == "" &&
		policy.Command.Access == "" &&
		policy.HTTP.Access == "" &&
		policy.WorkspaceWriteAccess == "" &&
		policy.MemoryWriteAccess == "" &&
		policy.GraphMutationAccess == "" &&
		len(policy.Command.Rules) == 0 &&
		len(policy.HTTP.Rules) == 0 &&
		len(policy.ProtectedPathPrefixes) == 0 &&
		len(policy.ApprovalRequiredClasses) == 0 &&
		len(policy.DeniedClasses) == 0
}

// CloneStringMap returns a shallow copy of the given string map.
func CloneStringMap(in map[string]string) map[string]string {
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

func normalizeLowerStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeUpperStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
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
