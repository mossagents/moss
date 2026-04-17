package tui

import (
	"context"
	"errors"
	"fmt"
	"github.com/mossagents/moss/harness/appkit/product/changes"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/workspace"
	rprofile "github.com/mossagents/moss/harness/runtime/profile"
	"github.com/mossagents/moss/harness/userio/prompting"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (a *agentState) listPersistedSessions(limit int) (string, error) {
	a.mu.Lock()
	store := a.store
	a.mu.Unlock()
	if store == nil {
		return "", fmt.Errorf("session store is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	summaries, err := store.List(ctx)
	if err != nil {
		return "", err
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].CreatedAt > summaries[j].CreatedAt })
	if len(summaries) > limit {
		summaries = summaries[:limit]
	}
	if len(summaries) == 0 {
		return "No saved threads found.", nil
	}
	var b strings.Builder
	b.WriteString("Saved threads:\n")
	for _, s := range summaries {
		b.WriteString(fmt.Sprintf("- %s | %s | %s", s.ID, s.Status, s.Mode))
		if strings.TrimSpace(s.Profile) != "" {
			b.WriteString(fmt.Sprintf(" | profile=%s", s.Profile))
		}
		if strings.TrimSpace(s.EffectiveTrust) != "" {
			b.WriteString(fmt.Sprintf(" | trust=%s", s.EffectiveTrust))
		}
		if strings.TrimSpace(s.EffectiveApproval) != "" {
			b.WriteString(fmt.Sprintf(" | approval=%s", s.EffectiveApproval))
		}
		b.WriteString(fmt.Sprintf(" | steps=%d | created=%s\n", s.Steps, s.CreatedAt))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (a *agentState) restoreSession(sessionID string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot restore a session while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	workspace := a.workspace
	a.mu.Unlock()
	if store == nil {
		return "", fmt.Errorf("session store is unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if strings.EqualFold(sessionID, "latest") {
		summaries, _, err := runtimeenv.ListResumeCandidates(ctx, workspace)
		if err != nil {
			return "", err
		}
		selected, _, err := runtimeenv.SelectResumeSummary(summaries, "", true)
		if err != nil {
			return "", err
		}
		if selected == nil {
			return "", fmt.Errorf("no recoverable sessions found")
		}
		sessionID = selected.ID
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	loaded, err := store.Load(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if loaded == nil {
		return "", fmt.Errorf("session %q not found", sessionID)
	}
	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}
	warning, err := a.ensureRuntimePosture(loaded.ID, rprofile.SessionPostureFromSession(loaded))
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = loaded
	posture := rprofile.SessionPostureFromSession(loaded)
	if strings.TrimSpace(posture.Profile) != "" {
		a.profile = posture.Profile
	}
	a.trust = posture.EffectiveTrust
	a.approvalMode = posture.EffectiveApproval
	a.mu.Unlock()
	a.publishProgressReplay()
	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Resumed thread %s (%s, steps=%d, messages=%d).", loaded.ID, loaded.Status, loaded.Budget.UsedSteps, len(loaded.Messages))
	if warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	return b.String(), nil
}

func (a *agentState) createInteractiveSession() (*session.Session, error) {
	a.mu.Lock()
	k := a.k
	ctx := a.ctx
	workspace := a.workspace
	trust := a.trust
	approvalMode := a.approvalMode
	profile := a.profile
	buildCfg := a.buildSessionConfig
	configInstructions := a.promptConfigInstructions
	modelInstructions := a.promptModelInstructions
	a.mu.Unlock()
	if k == nil {
		return nil, errors.New("runtime is unavailable")
	}
	sessCfg := normalizeSessionConfigDefaults(session.SessionConfig{
		Goal:       "interactive",
		Mode:       "interactive",
		TrustLevel: trust,
		Profile:    profile,
		MaxSteps:   200,
	}, trust, profile, "interactive", "interactive", 200)
	if buildCfg != nil {
		sessCfg = normalizeSessionConfigDefaults(
			buildCfg(workspace, trust, approvalMode, profile, ""),
			trust,
			profile,
			"interactive",
			"interactive",
			200,
		)
	}
	metadata := preparePromptMetadata(sessCfg, profile)
	sysPrompt, err := prompting.ComposeSystemPrompt(workspace, trust, k, configInstructions, modelInstructions, metadata)
	if err != nil {
		return nil, err
	}
	sessCfg.SystemPrompt = sysPrompt
	sessCfg.Metadata = metadata
	return k.NewSession(ctx, sessCfg)
}

func (a *agentState) createCheckpoint(note string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot create a checkpoint while a run is active")
	}
	k := a.k
	sess := a.sess
	ctx := a.ctx
	a.mu.Unlock()
	if k == nil || k.Checkpoints() == nil {
		return "", fmt.Errorf("checkpoint store is unavailable")
	}
	if sess == nil {
		return "", fmt.Errorf("active session is unavailable")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	record, err := k.CreateCheckpoint(reqCtx, sess, checkpoint.CheckpointCreateRequest{Note: strings.TrimSpace(note)})
	if err != nil {
		return "", err
	}
	summary := runtimeenv.SummarizeCheckpoint(*record)
	msg := fmt.Sprintf("Created checkpoint %s for session %s.", summary.ID, sess.ID)
	if summary.SnapshotID != "" {
		msg += fmt.Sprintf(" snapshot=%s.", summary.SnapshotID)
	}
	msg += fmt.Sprintf(" patches=%d lineage=%d.", summary.PatchCount, summary.LineageDepth)
	if strings.TrimSpace(summary.Note) != "" {
		msg += fmt.Sprintf(" note=%s.", summary.Note)
	}
	return msg, nil
}

func (a *agentState) prepareProfileSwitch(nextProfile string) (string, error) {
	a.mu.Lock()
	running := a.running
	a.mu.Unlock()
	if running {
		return "", errors.New("cannot switch profile while a run is active")
	}
	note := fmt.Sprintf("profile switch to %s", strings.TrimSpace(nextProfile))
	msg, err := a.createCheckpoint(note)
	if err != nil {
		return "", fmt.Errorf("checkpoint before profile switch: %w", err)
	}
	return msg, nil
}

func (a *agentState) applyChange(patchFile, summary string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot apply a change while a run is active")
	}
	k := a.k
	ctx := a.ctx
	wsPath := a.workspace
	a.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("runtime is unavailable")
	}
	patchFile = strings.TrimSpace(patchFile)
	if patchFile == "" {
		return "", fmt.Errorf("patch file is required")
	}
	if !filepath.IsAbs(patchFile) {
		patchFile = filepath.Join(wsPath, patchFile)
	}
	data, err := os.ReadFile(patchFile)
	if err != nil {
		return "", fmt.Errorf("read patch file: %w", err)
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	item, err := changes.ApplyChange(reqCtx, changes.ChangeRuntimeFromKernel(wsPath, k), changes.ApplyChangeRequest{
		Patch:   string(data),
		Summary: strings.TrimSpace(summary),
		Source:  workspace.PatchSourceUser,
	})
	if err != nil {
		var opErr *changes.ChangeOperationError
		if errors.As(err, &opErr) && opErr.Operation != nil {
			return "", fmt.Errorf("%s\nDetails: %s", changes.RenderChangeDetail(opErr.Operation), opErr.Error())
		}
		return "", err
	}
	return changes.RenderChangeDetail(item), nil
}

func (a *agentState) rollbackChange(changeID string) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot roll back a change while a run is active")
	}
	k := a.k
	ctx := a.ctx
	workspace := a.workspace
	a.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("runtime is unavailable")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	item, err := changes.RollbackChange(reqCtx, changes.ChangeRuntimeFromKernel(workspace, k), changes.RollbackChangeRequest{
		ChangeID: strings.TrimSpace(changeID),
	})
	if err != nil {
		var opErr *changes.ChangeOperationError
		if errors.As(err, &opErr) && opErr.Operation != nil {
			return "", fmt.Errorf("%s\nDetails: %s", changes.RenderChangeDetail(opErr.Operation), opErr.Error())
		}
		return "", err
	}
	return changes.RenderChangeDetail(item), nil
}

func (a *agentState) forkSession(sourceKind, sourceID string, restoreWorktree bool) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot fork a session while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	k := a.k
	a.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("runtime is unavailable")
	}
	if sourceKind == "" {
		sourceKind = string(checkpoint.ForkSourceSession)
	}
	if strings.TrimSpace(sourceID) == "" {
		if sourceKind == string(checkpoint.ForkSourceCheckpoint) {
			sourceID = ""
		} else if current == nil {
			return "", fmt.Errorf("source id is required")
		}
		if sourceKind == string(checkpoint.ForkSourceSession) {
			sourceID = current.ID
		}
	}
	sourceSession, warning, err := a.loadForkSourceSession(ctx, sourceKind, sourceID, current)
	if err != nil {
		return "", err
	}
	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}
	if _, err := a.ensureRuntimePosture(sourceSession.ID, rprofile.SessionPostureFromSession(sourceSession)); err != nil {
		return "", err
	}
	a.mu.Lock()
	k = a.k
	ctx = a.ctx
	a.mu.Unlock()
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if sourceKind == string(checkpoint.ForkSourceCheckpoint) {
		record, err := runtimeenv.ResolveCheckpointRecord(reqCtx, k.Checkpoints(), sourceID)
		if err != nil {
			return "", err
		}
		sourceID = record.ID
	}
	next, result, err := k.ForkSession(reqCtx, checkpoint.ForkRequest{
		SourceKind:      checkpoint.ForkSourceKind(sourceKind),
		SourceID:        strings.TrimSpace(sourceID),
		RestoreWorktree: restoreWorktree,
	})
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = next
	a.mu.Unlock()
	a.publishProgressReplay()
	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Switched to forked thread %s from %s %s.", next.ID, result.SourceKind, result.SourceID)
	if result.CheckpointID != "" {
		fmt.Fprintf(&b, " checkpoint=%s.", result.CheckpointID)
	}
	if result.RestoredWorktree {
		b.WriteString(" worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Fprintf(&b, " degraded=%s.", result.Details)
	}
	if warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	return b.String(), nil
}

func (a *agentState) replayCheckpoint(checkpointID, mode string, restoreWorktree bool) (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot replay a checkpoint while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	k := a.k
	a.mu.Unlock()
	if k == nil || k.Checkpoints() == nil {
		return "", fmt.Errorf("checkpoint store is unavailable")
	}
	replayMode := checkpoint.ReplayMode(strings.ToLower(strings.TrimSpace(mode)))
	if replayMode == "" {
		replayMode = checkpoint.ReplayModeResume
	}
	if replayMode != checkpoint.ReplayModeResume && replayMode != checkpoint.ReplayModeRerun {
		return "", fmt.Errorf("replay mode must be resume or rerun")
	}
	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	record, err := runtimeenv.ResolveCheckpointRecord(reqCtx, k.Checkpoints(), checkpointID)
	if err != nil {
		return "", err
	}
	sourceSession, warning, err := loadCheckpointSourceSession(reqCtx, store, record)
	if err != nil {
		return "", err
	}
	cancel()
	if _, err := a.ensureRuntimePosture(sourceSession.ID, rprofile.SessionPostureFromSession(sourceSession)); err != nil {
		return "", err
	}
	a.mu.Lock()
	k = a.k
	ctx = a.ctx
	a.mu.Unlock()
	reqCtx, cancel = context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	next, result, err := k.ReplayFromCheckpoint(reqCtx, checkpoint.ReplayRequest{
		CheckpointID:    record.ID,
		Mode:            replayMode,
		RestoreWorktree: restoreWorktree,
	})
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = next
	a.mu.Unlock()
	a.publishProgressReplay()
	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Switched to replay thread %s from checkpoint %s (%s).", next.ID, result.CheckpointID, result.Mode)
	if result.RestoredWorktree {
		b.WriteString(" worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Fprintf(&b, " degraded=%s.", result.Details)
	}
	if warning != "" {
		b.WriteString("\n")
		b.WriteString(warning)
	}
	return b.String(), nil
}

func (a *agentState) newSession() (string, error) {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return "", errors.New("cannot create a new thread while a run is active")
	}
	current := a.sess
	store := a.store
	ctx := a.ctx
	a.mu.Unlock()

	notice, err := autosaveSessionBeforeSwitch(current, store, ctx)
	if err != nil {
		return "", err
	}

	next, err := a.createInteractiveSession()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sess = next
	a.mu.Unlock()
	a.publishProgressReplay()

	if notice != "" {
		return fmt.Sprintf("%s\nSwitched to new thread %s.", notice, next.ID), nil
	}
	return fmt.Sprintf("Started new thread %s.", next.ID), nil
}
