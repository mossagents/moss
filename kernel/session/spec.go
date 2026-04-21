package session

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/model"
)

// SessionSpec 持久化用户请求层的正交会话规格。
type SessionSpec struct {
	Workspace SessionWorkspace `json:"workspace,omitempty"`
	Intent    SessionIntent    `json:"intent,omitempty"`
	Runtime   SessionRuntime   `json:"runtime,omitempty"`
	Origin    SessionOrigin    `json:"origin,omitempty"`
}

type SessionWorkspace struct {
	Trust string `json:"trust,omitempty"`
}

type SessionIntent struct {
	CollaborationMode string `json:"collaboration_mode,omitempty"`
	PromptPack        string `json:"prompt_pack,omitempty"`
}

type SessionRuntime struct {
	RunMode           string `json:"run_mode,omitempty"`
	PermissionProfile string `json:"permission_profile,omitempty"`
	SessionPolicy     string `json:"session_policy,omitempty"`
	ModelProfile      string `json:"model_profile,omitempty"`
}

type SessionOrigin struct {
	Preset string `json:"preset,omitempty"`
}

// ResolvedSessionSpec 持久化解析后的运行时规格。
type ResolvedSessionSpec struct {
	Workspace ResolvedWorkspace `json:"workspace,omitempty"`
	Intent    ResolvedIntent    `json:"intent,omitempty"`
	Runtime   ResolvedRuntime   `json:"runtime,omitempty"`
	Prompt    ResolvedPrompt    `json:"prompt,omitempty"`
	Origin    ResolvedOrigin    `json:"origin,omitempty"`
}

type ResolvedWorkspace struct {
	Trust string `json:"trust,omitempty"`
}

type ResolvedIntent struct {
	CollaborationMode string        `json:"collaboration_mode,omitempty"`
	PromptPack        PromptPackRef `json:"prompt_pack,omitempty"`
	CapabilityCeiling []string      `json:"capability_ceiling,omitempty"`
}

type PromptPackRef struct {
	ID     string `json:"id,omitempty"`
	Source string `json:"source,omitempty"`
}

type ResolvedRuntime struct {
	RunMode           string            `json:"run_mode,omitempty"`
	PermissionProfile string            `json:"permission_profile,omitempty"`
	PermissionPolicy  json.RawMessage   `json:"permission_policy,omitempty"`
	SessionPolicyName string            `json:"session_policy_name,omitempty"`
	SessionPolicy     SessionPolicySpec `json:"session_policy,omitempty"`
	ModelProfile      string            `json:"model_profile,omitempty"`
	Provider          string            `json:"provider,omitempty"`
	ModelConfig       model.ModelConfig `json:"model_config,omitempty"`
	ReasoningEffort   string            `json:"reasoning_effort,omitempty"`
	Verbosity         string            `json:"verbosity,omitempty"`
	RouterLane        string            `json:"router_lane,omitempty"`
}

type SessionPolicySpec struct {
	MaxSteps             int           `json:"max_steps,omitempty"`
	MaxTokens            int           `json:"max_tokens,omitempty"`
	Timeout              time.Duration `json:"timeout,omitempty"`
	AutoCompactThreshold int           `json:"auto_compact_threshold,omitempty"`
	AsyncTaskLimit       int           `json:"async_task_limit,omitempty"`
}

type ResolvedPrompt struct {
	BasePackID                 string   `json:"base_pack_id,omitempty"`
	TrustedAugmentationIDs     []string `json:"trusted_augmentation_ids,omitempty"`
	TrustedAugmentationDigests []string `json:"trusted_augmentation_digests,omitempty"`
	RenderedPromptVersion      string   `json:"rendered_prompt_version,omitempty"`
	SnapshotRef                string   `json:"snapshot_ref,omitempty"`
}

type ResolvedOrigin struct {
	Preset string `json:"preset,omitempty"`
}

// PromptSnapshot 持久化已渲染 prompt 的可信快照。
type PromptSnapshot struct {
	Ref            string                `json:"ref,omitempty"`
	Layers         []ResolvedPromptLayer `json:"layers,omitempty"`
	RenderedPrompt string                `json:"rendered_prompt,omitempty"`
	Version        string                `json:"version,omitempty"`
}

type ResolvedPromptLayer struct {
	ID      string `json:"id,omitempty"`
	Source  string `json:"source,omitempty"`
	Content string `json:"content,omitempty"`
}

func cloneSessionSpec(spec *SessionSpec) *SessionSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	return &out
}

func cloneResolvedSessionSpec(spec *ResolvedSessionSpec) *ResolvedSessionSpec {
	if spec == nil {
		return nil
	}
	out := *spec
	out.Intent.CapabilityCeiling = append([]string(nil), spec.Intent.CapabilityCeiling...)
	out.Runtime.PermissionPolicy = append(json.RawMessage(nil), spec.Runtime.PermissionPolicy...)
	out.Prompt.TrustedAugmentationIDs = append([]string(nil), spec.Prompt.TrustedAugmentationIDs...)
	out.Prompt.TrustedAugmentationDigests = append([]string(nil), spec.Prompt.TrustedAugmentationDigests...)
	return &out
}

func clonePromptSnapshot(snapshot *PromptSnapshot) *PromptSnapshot {
	if snapshot == nil {
		return nil
	}
	out := *snapshot
	if len(snapshot.Layers) > 0 {
		out.Layers = append([]ResolvedPromptLayer(nil), snapshot.Layers...)
	}
	return &out
}

func SessionFacetValues(sess *Session) (runMode, preset, workspaceTrust, collaborationMode, promptPack, permissionProfile, sessionPolicy, modelProfile string) {
	if sess == nil {
		return "", "", "", "", "", "", "", ""
	}
	resolved := sess.Config.ResolvedSessionSpec
	requested := sess.Config.SessionSpec

	runMode = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Runtime.RunMode }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Runtime.RunMode }),
		strings.TrimSpace(sess.Config.Mode),
	)
	preset = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Origin.Preset }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Origin.Preset }),
	)
	workspaceTrust = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Workspace.Trust }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Workspace.Trust }),
		strings.TrimSpace(sess.Config.TrustLevel),
	)
	collaborationMode = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Intent.CollaborationMode }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Intent.CollaborationMode }),
	)
	promptPack = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Intent.PromptPack.ID }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Intent.PromptPack }),
	)
	permissionProfile = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Runtime.PermissionProfile }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Runtime.PermissionProfile }),
	)
	sessionPolicy = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Runtime.SessionPolicyName }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Runtime.SessionPolicy }),
	)
	modelProfile = firstNonEmptyTrimmed(
		valueIfResolved(resolved, func(spec *ResolvedSessionSpec) string { return spec.Runtime.ModelProfile }),
		valueIfRequested(requested, func(spec *SessionSpec) string { return spec.Runtime.ModelProfile }),
	)
	return runMode, preset, workspaceTrust, collaborationMode, promptPack, permissionProfile, sessionPolicy, modelProfile
}

func ResolvedApprovalMode(sess *Session) string {
	summary, ok := ToolPolicySummaryFromSession(sess)
	if !ok {
		return ""
	}
	return strings.TrimSpace(summary.ApprovalMode)
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func valueIfResolved(spec *ResolvedSessionSpec, getter func(*ResolvedSessionSpec) string) string {
	if spec == nil {
		return ""
	}
	return getter(spec)
}

func valueIfRequested(spec *SessionSpec, getter func(*SessionSpec) string) string {
	if spec == nil {
		return ""
	}
	return getter(spec)
}
