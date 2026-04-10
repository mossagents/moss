package tui

import (
	"context"
	"errors"
	"fmt"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	configpkg "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	"reflect"
	"strings"
	"time"
)

type postureRebuildPlan struct {
	Rebuild  bool
	Resolved runtime.ResolvedProfile
	Notice   string
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
		record, err := product.ResolveCheckpointRecord(ctx, k.Checkpoints(), sourceID)
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

func (a *agentState) ensureRuntimePosture(sessionID string, target runtime.SessionPosture) (string, error) {
	a.mu.Lock()
	current := postureFromRuntime(a.profile, a.trust, a.approvalMode, runtime.ExecutionPolicyOf(a.k))
	a.mu.Unlock()
	plan, err := planPostureRebuild(sessionID, current, target)
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

func postureFromRuntime(profile, trust, approval string, policy runtime.ExecutionPolicy) runtime.SessionPosture {
	return runtime.SessionPosture{
		Profile:           strings.TrimSpace(profile),
		EffectiveTrust:    configpkg.NormalizeTrustLevel(trust),
		EffectiveApproval: product.NormalizeApprovalMode(approval),
		ExecutionPolicy:   policy,
		HasExecution:      true,
	}
}

func postureWarningForSession(sess *session.Session) string {
	posture := runtime.SessionPostureFromSession(sess)
	if !posture.Legacy {
		return ""
	}
	return fmt.Sprintf("Warning: session %s predates profile persistence; trust was inferred as %s and the current runtime approval/profile will be used.", sess.ID, posture.EffectiveTrust)
}

func planPostureRebuild(sessionID string, current, target runtime.SessionPosture) (postureRebuildPlan, error) {
	targetTrust := configpkg.NormalizeTrustLevel(target.EffectiveTrust)
	currentTrust := configpkg.NormalizeTrustLevel(current.EffectiveTrust)
	targetApproval := product.NormalizeApprovalMode(target.EffectiveApproval)
	currentApproval := product.NormalizeApprovalMode(current.EffectiveApproval)

	if target.Legacy {
		if currentTrust != targetTrust {
			return postureRebuildPlan{}, fmt.Errorf("session %s predates profile persistence and requires trust=%s, but current runtime trust=%s; switch to a matching trust/profile before continuing", sessionID, targetTrust, currentTrust)
		}
		return postureRebuildPlan{Notice: postureWarningForRuntime(sessionID, targetTrust)}, nil
	}

	if currentTrust == targetTrust && currentApproval == targetApproval {
		if !target.HasExecution || reflect.DeepEqual(target.ExecutionPolicy, current.ExecutionPolicy) {
			return postureRebuildPlan{}, nil
		}
	}
	resolved, err := runtime.ResolveProfileFromPosture(target.Profile, target)
	if err != nil {
		return postureRebuildPlan{}, err
	}
	return postureRebuildPlan{
		Rebuild:  true,
		Resolved: resolved,
		Notice:   fmt.Sprintf("Runtime auto-rebuilt to recorded posture for session %s (%s).", sessionID, formatPosture(target)),
	}, nil
}

func postureWarningForRuntime(sessionID, trust string) string {
	return fmt.Sprintf("Warning: session %s predates profile persistence; trust was inferred as %s and the current runtime approval/profile will be used.", sessionID, trust)
}

func formatPosture(posture runtime.SessionPosture) string {
	parts := []string{}
	if strings.TrimSpace(posture.Profile) != "" {
		parts = append(parts, "profile="+strings.TrimSpace(posture.Profile))
	}
	if strings.TrimSpace(posture.EffectiveTrust) != "" {
		parts = append(parts, "trust="+configpkg.NormalizeTrustLevel(posture.EffectiveTrust))
	}
	if strings.TrimSpace(posture.EffectiveApproval) != "" {
		parts = append(parts, "approval="+product.NormalizeApprovalMode(posture.EffectiveApproval))
	}
	if strings.TrimSpace(posture.TaskMode) != "" {
		parts = append(parts, "task="+strings.TrimSpace(posture.TaskMode))
	}
	if len(parts) == 0 {
		return "profile=legacy"
	}
	return strings.Join(parts, ", ")
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
	if currentProfile == "" {
		currentProfile = "default"
	}
	k, ctx, cancel, err := buildRuntimeKernel(Config{
		Trust:        plan.Resolved.Trust,
		Profile:      currentProfile,
		ApprovalMode: plan.Resolved.ApprovalMode,
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
	if err := product.ApplyResolvedProfile(k, plan.Resolved); err != nil {
		cancel()
		return fmt.Errorf("apply rebuilt posture: %w", err)
	}
	if oldRunCancel != nil {
		oldRunCancel()
	}
	if oldCancel != nil {
		oldCancel()
	}
	k.Hooks().BeforeToolCall.Intercept(a.permissionOverrideInterceptor())
	a.mu.Lock()
	a.k = k
	a.ctx = ctx
	a.cancel = cancel
	a.runCancel = nil
	a.trust = plan.Resolved.Trust
	a.profile = strings.TrimSpace(plan.Resolved.Name)
	a.approvalMode = plan.Resolved.ApprovalMode
	a.mu.Unlock()
	return nil
}
