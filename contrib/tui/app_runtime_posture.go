package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/mossagents/moss/harness/appkit/product"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	configpkg "github.com/mossagents/moss/harness/config"
	rpolicy "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
)

type runtimePosture struct {
	Profile           string
	EffectiveTrust    string
	EffectiveApproval string
	TaskMode          string
	ToolPolicy        rpolicy.ToolPolicy
	HasToolPolicy     bool
}

type postureRebuildPlan struct {
	Rebuild      bool
	TargetConfig session.SessionConfig
	Trust        string
	Profile      string
	ApprovalMode string
	Notice       string
}

func autosaveSessionBeforeSwitch(current *session.Session, store session.SessionStore, ctx context.Context) (string, error) {
	if current == nil || sessionDialogCount(current) == 0 {
		return "", nil
	}
	if store == nil {
		return "", errors.New("session store is unavailable, cannot auto-save current session before switching")
	}
	saveCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := store.Save(saveCtx, current); err != nil {
		return "", fmt.Errorf("save current session %q: %w", current.ID, err)
	}
	return fmt.Sprintf("Previous thread %s auto-saved. Use /resume %s or /resume to continue it later.", current.ID, current.ID), nil
}

func loadCheckpointSourceSession(ctx context.Context, store session.SessionStore, record *checkpoint.CheckpointRecord) (*session.Session, string, error) {
	if record == nil {
		return nil, "", checkpoint.ErrCheckpointNotFound
	}
	if store == nil {
		return nil, "", fmt.Errorf("session store is unavailable")
	}
	loaded, err := store.Load(ctx, record.SessionID)
	if err != nil {
		return nil, "", err
	}
	if loaded == nil {
		return nil, "", fmt.Errorf("session %q not found", record.SessionID)
	}
	warning := postureWarningForSession(loaded)
	return loaded, warning, nil
}

func (a *agentState) loadForkSourceSession(ctx context.Context, sourceKind, sourceID string, current *session.Session) (*session.Session, string, error) {
	a.mu.Lock()
	store := a.store
	k := a.k
	a.mu.Unlock()
	switch checkpoint.ForkSourceKind(sourceKind) {
	case checkpoint.ForkSourceCheckpoint:
		if k == nil || k.Checkpoints() == nil {
			return nil, "", fmt.Errorf("checkpoint store is unavailable")
		}
		record, err := runtimeenv.ResolveCheckpointRecord(ctx, k.Checkpoints(), sourceID)
		if err != nil {
			return nil, "", err
		}
		return loadCheckpointSourceSession(ctx, store, record)
	case checkpoint.ForkSourceSession:
		if current != nil && strings.TrimSpace(current.ID) == strings.TrimSpace(sourceID) {
			return current, postureWarningForSession(current), nil
		}
		if store == nil {
			return nil, "", fmt.Errorf("session store is unavailable")
		}
		loaded, err := store.Load(ctx, sourceID)
		if err != nil {
			return nil, "", err
		}
		if loaded == nil {
			return nil, "", fmt.Errorf("session %q not found", sourceID)
		}
		return loaded, postureWarningForSession(loaded), nil
	default:
		return nil, "", fmt.Errorf("unsupported fork source kind %q", sourceKind)
	}
}

func (a *agentState) ensureRuntimePosture(target *session.Session) (string, error) {
	if target == nil {
		return "", fmt.Errorf("target session is required")
	}
	a.mu.Lock()
	current := postureFromRuntime(a.k, a.profile, a.trust, a.approvalMode)
	a.mu.Unlock()
	plan, err := planPostureRebuild(target.ID, current, target)
	if err != nil {
		return "", err
	}
	if !plan.Rebuild {
		return plan.Notice, nil
	}
	if err := a.rebuildRuntime(plan); err != nil {
		return "", err
	}
	return plan.Notice, nil
}

func postureFromRuntime(k *kernel.Kernel, profile, trust, approval string) runtimePosture {
	posture := runtimePosture{
		Profile:           strings.TrimSpace(profile),
		EffectiveTrust:    configpkg.NormalizeTrustLevel(trust),
		EffectiveApproval: rpolicy.NormalizeApprovalMode(approval),
		TaskMode:          strings.TrimSpace(profile),
	}
	if posture.EffectiveTrust == "" {
		posture.EffectiveTrust = configpkg.TrustTrusted
	}
	if posture.EffectiveApproval == "" {
		posture.EffectiveApproval = product.ApprovalModeConfirm
	}
	if policy, ok := rpolicy.Current(k); ok {
		posture.ToolPolicy = policy
		posture.HasToolPolicy = true
		if posture.EffectiveTrust == "" {
			posture.EffectiveTrust = configpkg.NormalizeTrustLevel(policy.Trust)
		}
		if posture.EffectiveApproval == "" {
			posture.EffectiveApproval = rpolicy.NormalizeApprovalMode(policy.ApprovalMode)
		}
	}
	if !posture.HasToolPolicy {
		posture.ToolPolicy = rpolicy.ResolveToolPolicyForWorkspace("", posture.EffectiveTrust, posture.EffectiveApproval)
		posture.HasToolPolicy = true
	}
	if posture.Profile == "" {
		posture.Profile = "default"
	}
	if posture.TaskMode == "" {
		posture.TaskMode = "coding"
	}
	return posture
}

func postureWarningForSession(sess *session.Session) string {
	return ""
}

func planPostureRebuild(sessionID string, current runtimePosture, target *session.Session) (postureRebuildPlan, error) {
	if target == nil {
		return postureRebuildPlan{}, fmt.Errorf("target session is required")
	}
	targetPosture := postureFromSession(target)
	targetTrust := configpkg.NormalizeTrustLevel(targetPosture.EffectiveTrust)
	currentTrust := configpkg.NormalizeTrustLevel(current.EffectiveTrust)
	targetApproval := rpolicy.NormalizeApprovalMode(targetPosture.EffectiveApproval)
	currentApproval := rpolicy.NormalizeApprovalMode(current.EffectiveApproval)

	if currentTrust == targetTrust && currentApproval == targetApproval {
		if !targetPosture.HasToolPolicy || reflect.DeepEqual(targetPosture.ToolPolicy, current.ToolPolicy) {
			return postureRebuildPlan{}, nil
		}
	}
	profileName := strings.TrimSpace(targetPosture.Profile)
	if profileName == "" {
		profileName = "default"
	}
	return postureRebuildPlan{
		Rebuild:      true,
		TargetConfig: target.Config,
		Trust:        firstNonEmptyTrimmed(targetTrust, configpkg.TrustTrusted),
		Profile:      profileName,
		ApprovalMode: firstNonEmptyTrimmed(targetApproval, product.ApprovalModeConfirm),
		Notice:       fmt.Sprintf("Runtime auto-rebuilt to recorded posture for session %s (%s).", sessionID, formatPosture(targetPosture)),
	}, nil
}

func formatPosture(posture runtimePosture) string {
	parts := []string{}
	if strings.TrimSpace(posture.Profile) != "" {
		parts = append(parts, "profile="+strings.TrimSpace(posture.Profile))
	}
	if strings.TrimSpace(posture.EffectiveTrust) != "" {
		parts = append(parts, "trust="+configpkg.NormalizeTrustLevel(posture.EffectiveTrust))
	}
	if strings.TrimSpace(posture.EffectiveApproval) != "" {
		parts = append(parts, "approval="+rpolicy.NormalizeApprovalMode(posture.EffectiveApproval))
	}
	if strings.TrimSpace(posture.TaskMode) != "" {
		parts = append(parts, "task="+strings.TrimSpace(posture.TaskMode))
	}
	if len(parts) == 0 {
		return "profile=default"
	}
	return strings.Join(parts, ", ")
}

func postureFromSession(sess *session.Session) runtimePosture {
	posture := runtimePosture{}
	if sess == nil {
		return posture
	}
	_, preset, workspaceTrust, collaborationMode, _, permissionProfile, _, _ := session.SessionFacetValues(sess)
	posture.Profile = firstNonEmptyTrimmed(sess.Config.Profile, preset, permissionProfile)
	posture.EffectiveTrust = configpkg.NormalizeTrustLevel(firstNonEmptyTrimmed(sessionMetadataString(sess.Config.Metadata, session.MetadataEffectiveTrust), workspaceTrust, sess.Config.TrustLevel))
	posture.EffectiveApproval = rpolicy.NormalizeApprovalMode(sessionMetadataString(sess.Config.Metadata, session.MetadataEffectiveApproval))
	posture.TaskMode = firstNonEmptyTrimmed(sessionMetadataString(sess.Config.Metadata, session.MetadataTaskMode), collaborationMode, posture.Profile)
	if policy, ok, err := product.ToolPolicyForSessionConfig(sess.Config); err == nil && ok {
		posture.ToolPolicy = policy
		posture.HasToolPolicy = true
		if posture.EffectiveApproval == "" {
			posture.EffectiveApproval = rpolicy.NormalizeApprovalMode(policy.ApprovalMode)
		}
		if posture.EffectiveTrust == "" {
			posture.EffectiveTrust = configpkg.NormalizeTrustLevel(policy.Trust)
		}
	}
	if posture.EffectiveApproval == "" {
		posture.EffectiveApproval = product.ApprovalModeConfirm
	}
	if posture.EffectiveTrust == "" {
		posture.EffectiveTrust = configpkg.TrustTrusted
	}
	if posture.TaskMode == "" {
		posture.TaskMode = firstNonEmptyTrimmed(collaborationMode, "coding")
	}
	return posture
}

func sessionMetadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func (a *agentState) rebuildRuntime(plan postureRebuildPlan) error {
	a.mu.Lock()
	buildKernel := a.buildKernel
	afterBoot := a.afterBoot
	workspace := a.workspace
	bridge := a.bridge
	provider := a.provider
	model := a.model
	apiKey := a.apiKey
	baseURL := a.baseURL
	currentProfile := strings.TrimSpace(a.profile)
	oldCancel := a.cancel
	oldRunCancel := a.runCancel
	a.mu.Unlock()

	if buildKernel == nil {
		return fmt.Errorf("runtime rebuild is unavailable")
	}
	rebuildProfile := strings.TrimSpace(plan.Profile)
	if rebuildProfile == "" {
		rebuildProfile = strings.TrimSpace(currentProfile)
	}
	if rebuildProfile == "" {
		rebuildProfile = "default"
	}
	k, ctx, cancel, err := buildRuntimeKernel(Config{
		Trust:        plan.Trust,
		Profile:      rebuildProfile,
		ApprovalMode: plan.ApprovalMode,
		APIKey:       apiKey,
		BaseURL:      baseURL,
		BuildKernel:  buildKernel,
		AfterBoot:    afterBoot,
	}, WelcomeConfig{
		Provider:  provider,
		Model:     model,
		Workspace: workspace,
	}, bridge)
	if err != nil {
		return err
	}
	if err := product.ApplySessionConfig(k, plan.TargetConfig); err != nil {
		cancel()
		return fmt.Errorf("apply rebuilt posture: %w", err)
	}
	if oldRunCancel != nil {
		oldRunCancel()
	}
	if oldCancel != nil {
		oldCancel()
	}
	k.InstallPlugin(a.permissionOverridePlugin())
	a.mu.Lock()
	a.k = k
	a.ctx = ctx
	a.cancel = cancel
	a.runCancel = nil
	a.trust = plan.Trust
	a.profile = rebuildProfile
	a.approvalMode = plan.ApprovalMode
	a.mu.Unlock()
	return nil
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
