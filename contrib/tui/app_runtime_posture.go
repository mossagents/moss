package tui

import (
	"context"
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
	Mode              string
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
	Mode         string
	ApprovalMode string
	Notice       string
}

func autosaveSessionBeforeSwitch(current *session.Session, store session.SessionStore, ctx context.Context) (string, error) {
	if current == nil || sessionDialogCount(current) == 0 {
		return "", nil
	}
	if store == nil {
		// EventStore 路径：所有事件已记录在 EventStore 中，无 FileStore 可写；静默跳过 autosave。
		return "", nil
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
	current := postureFromRuntime(a.k, a.collaborationMode, a.trust, a.approvalMode)
	bp := a.blueprint
	a.mu.Unlock()
	// §阶段5: posture patch-up 停止。
	// blueprint path 下，policy 由 blueprintPolicyApplier 在每次 RunAgentFromBlueprint 前应用，
	// 不再需要根据 session metadata 重建 kernel runtime 的 posture。
	if bp != nil {
		return "", nil
	}
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

func postureFromRuntime(k *kernel.Kernel, mode, trust, approval string) runtimePosture {
	posture := runtimePosture{
		Mode:              strings.TrimSpace(mode),
		EffectiveTrust:    configpkg.NormalizeTrustLevel(trust),
		EffectiveApproval: rpolicy.NormalizeApprovalMode(approval),
		TaskMode:          strings.TrimSpace(mode),
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
	if posture.Mode == "" {
		posture.Mode = "execute"
	}
	if posture.TaskMode == "" {
		posture.TaskMode = posture.Mode
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
	modeName := strings.TrimSpace(targetPosture.TaskMode)
	if modeName == "" {
		modeName = "execute"
	}
	return postureRebuildPlan{
		Rebuild:      true,
		TargetConfig: target.Config,
		Trust:        firstNonEmptyTrimmed(targetTrust, configpkg.TrustTrusted),
		Mode:         modeName,
		ApprovalMode: firstNonEmptyTrimmed(targetApproval, product.ApprovalModeConfirm),
		Notice:       fmt.Sprintf("Runtime auto-rebuilt to recorded posture for session %s (%s).", sessionID, formatPosture(targetPosture)),
	}, nil
}

func formatPosture(posture runtimePosture) string {
	parts := []string{}
	if strings.TrimSpace(posture.TaskMode) != "" {
		parts = append(parts, "mode="+strings.TrimSpace(posture.TaskMode))
	}
	if strings.TrimSpace(posture.EffectiveTrust) != "" {
		parts = append(parts, "trust="+configpkg.NormalizeTrustLevel(posture.EffectiveTrust))
	}
	if strings.TrimSpace(posture.EffectiveApproval) != "" {
		parts = append(parts, "approval="+rpolicy.NormalizeApprovalMode(posture.EffectiveApproval))
	}
	if len(parts) == 0 {
		return "mode=execute"
	}
	return strings.Join(parts, ", ")
}

func postureFromSession(sess *session.Session) runtimePosture {
	posture := runtimePosture{}
	if sess == nil {
		return posture
	}
	_, preset, workspaceTrust, collaborationMode, _, permissionProfile, _, _ := session.SessionFacetValues(sess)
	posture.EffectiveTrust = configpkg.NormalizeTrustLevel(firstNonEmptyTrimmed(sessionMetadataString(sess.Config.Metadata, session.MetadataEffectiveTrust), workspaceTrust, sess.Config.TrustLevel))
	posture.EffectiveApproval = rpolicy.NormalizeApprovalMode(sessionMetadataString(sess.Config.Metadata, session.MetadataEffectiveApproval))
	fallbackSelector := firstNonEmptyTrimmed(preset, permissionProfile)
	legacyMode := ""
	if normalized, err := normalizeTUIMode(fallbackSelector); err == nil {
		legacyMode = normalized
	}
	posture.TaskMode = firstNonEmptyTrimmed(sessionMetadataString(sess.Config.Metadata, session.MetadataTaskMode), collaborationMode, legacyMode)
	posture.Mode = firstNonEmptyTrimmed(collaborationMode, posture.TaskMode, legacyMode, "execute")
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
		posture.TaskMode = firstNonEmptyTrimmed(collaborationMode, legacyMode, "execute")
	}
	if posture.Mode == "" {
		posture.Mode = firstNonEmptyTrimmed(posture.TaskMode, legacyMode, "execute")
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
	currentMode := strings.TrimSpace(a.collaborationMode)
	oldCancel := a.cancel
	oldRunCancel := a.runCancel
	a.mu.Unlock()

	if buildKernel == nil {
		return fmt.Errorf("runtime rebuild is unavailable")
	}
	rebuildMode := strings.TrimSpace(plan.Mode)
	if rebuildMode == "" {
		rebuildMode = strings.TrimSpace(currentMode)
	}
	if rebuildMode == "" {
		rebuildMode = "execute"
	}
	k, ctx, cancel, err := buildRuntimeKernel(Config{
		Trust:             plan.Trust,
		CollaborationMode: firstNonEmptyTrimmed(sessionConfigCollaborationMode(plan.TargetConfig), rebuildMode, "execute"),
		ApprovalMode:      plan.ApprovalMode,
		APIKey:            apiKey,
		BaseURL:           baseURL,
		BuildKernel:       buildKernel,
		AfterBoot:         afterBoot,
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
	if err := a.installTokenOverrunNegotiation(k); err != nil {
		cancel()
		return fmt.Errorf("configure token overrun negotiation: %w", err)
	}
	k.InstallPlugin(a.permissionOverridePlugin())
	a.mu.Lock()
	a.k = k
	a.ctx = ctx
	a.cancel = cancel
	a.runCancel = nil
	a.trust = plan.Trust
	a.collaborationMode = firstNonEmptyTrimmed(sessionConfigCollaborationMode(plan.TargetConfig), rebuildMode, a.collaborationMode, "execute")
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
