package product

import (
	"context"
	"errors"
	"fmt"
	"github.com/mossagents/moss/kernel"
	ckpt "github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/session"
	kws "github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/sandbox"
	"strings"
	"time"
)

type ChangeRuntime struct {
	Workspace        string
	RepoStateCapture kws.RepoStateCapture
	PatchApply       kws.PatchApply
	PatchRevert      kws.PatchRevert
	SessionStore     session.SessionStore
	SessionLookup    func(string) (*session.Session, bool)
	CreateCheckpoint func(context.Context, *session.Session, ckpt.CheckpointCreateRequest) (*ckpt.CheckpointRecord, error)
}

func ChangeRuntimeFromKernel(workspace string, k *kernel.Kernel) ChangeRuntime {
	if k == nil {
		return ChangeRuntime{Workspace: workspace}
	}
	var sessionLookup func(string) (*session.Session, bool)
	if mgr := k.SessionManager(); mgr != nil {
		sessionLookup = mgr.Get
	}
	return ChangeRuntime{
		Workspace:        workspace,
		RepoStateCapture: k.RepoStateCapture(),
		PatchApply:       k.PatchApply(),
		PatchRevert:      k.PatchRevert(),
		SessionStore:     k.SessionStore(),
		SessionLookup:    sessionLookup,
		CreateCheckpoint: k.CreateCheckpoint,
	}
}

func (rt ChangeRuntime) repoCapturePort() kws.RepoStateCapture {
	if rt.RepoStateCapture != nil {
		return rt.RepoStateCapture
	}
	return sandbox.NewGitRepoStateCapture(rt.Workspace)
}

func ApplyChange(ctx context.Context, rt ChangeRuntime, req ApplyChangeRequest) (*ChangeOperation, error) {
	patch := strings.TrimSpace(req.Patch)
	if patch == "" {
		return nil, fmt.Errorf("patch is required")
	}
	capturePort := rt.repoCapturePort()
	if capturePort == nil {
		return nil, fmt.Errorf("repository state capture is unavailable")
	}
	capture, err := capturePort.Capture(ctx)
	if err != nil {
		return nil, err
	}
	if capture == nil {
		return nil, kws.ErrRepoUnavailable
	}
	if capture.IsDirty {
		return nil, fmt.Errorf("apply requires a clean repository")
	}
	if rt.PatchApply == nil {
		return nil, fmt.Errorf("patch apply is unavailable")
	}
	store, err := OpenChangeStore()
	if err != nil {
		return nil, err
	}

	op := &ChangeOperation{
		ID:           newChangeID(capture.RepoRoot),
		RepoRoot:     canonicalRepoRoot(capture.RepoRoot),
		BaseHeadSHA:  strings.TrimSpace(capture.HeadSHA),
		SessionID:    strings.TrimSpace(req.SessionID),
		Summary:      strings.TrimSpace(req.Summary),
		Status:       ChangeStatusPreparing,
		RecoveryMode: "patch+capture",
		Capture:      cloneRepoState(capture),
		CreatedAt:    time.Now().UTC(),
	}
	if req.Source == "" {
		req.Source = kws.PatchSourceUser
	}
	if err := attachTurnMetadata(ctx, rt, op); err != nil {
		return nil, err
	}
	if err := attachCheckpointMetadata(ctx, rt, op); err != nil {
		return nil, err
	}
	if err := store.Save(ctx, op); err != nil {
		return nil, err
	}

	result, applyErr := rt.PatchApply.Apply(ctx, kws.PatchApplyRequest{
		Patch:  req.Patch,
		Source: req.Source,
	})
	if applyErr != nil {
		if result != nil && result.Applied {
			op.PatchID = strings.TrimSpace(result.PatchID)
			op.TargetFiles = append([]string(nil), result.TargetFiles...)
			op.Status = ChangeStatusApplyInconsistent
			op.RecoveryDetails = appendDetails(op.RecoveryDetails, fmt.Sprintf("apply mutated repository but durable metadata failed: %v", applyErr))
			return persistInconsistentChange(ctx, store, op, fmt.Sprintf("apply entered inconsistent state: %v", applyErr))
		}
		_ = store.Delete(ctx, op.ID)
		return nil, applyErr
	}

	op.PatchID = strings.TrimSpace(result.PatchID)
	op.TargetFiles = append([]string(nil), result.TargetFiles...)
	op.Status = ChangeStatusApplied
	if err := store.Save(ctx, op); err != nil {
		op.Status = ChangeStatusApplyInconsistent
		op.RecoveryDetails = appendDetails(op.RecoveryDetails, fmt.Sprintf("apply mutated repository but failed to finalize change record: %v", err))
		return persistInconsistentChange(ctx, store, op, fmt.Sprintf("apply mutated repository but failed to finalize durable state: %v", err))
	}
	return cloneChangeOperation(op), nil
}

func RollbackChange(ctx context.Context, rt ChangeRuntime, req RollbackChangeRequest) (*ChangeOperation, error) {
	changeID := strings.TrimSpace(req.ChangeID)
	if changeID == "" {
		return nil, fmt.Errorf("change_id is required")
	}
	if rt.PatchRevert == nil {
		return nil, fmt.Errorf("patch revert is unavailable")
	}
	capturePort := rt.repoCapturePort()
	if capturePort == nil {
		return nil, fmt.Errorf("repository state capture is unavailable")
	}
	capture, err := capturePort.Capture(ctx)
	if err != nil {
		return nil, err
	}
	if capture == nil {
		return nil, kws.ErrRepoUnavailable
	}
	store, err := OpenChangeStore()
	if err != nil {
		return nil, err
	}
	op, err := store.Load(ctx, changeID)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, fmt.Errorf("change %q not found", changeID)
	}
	if canonicalRepoRoot(op.RepoRoot) != canonicalRepoRoot(capture.RepoRoot) {
		return nil, fmt.Errorf("change %q belongs to repository %q, current repository is %q", changeID, op.RepoRoot, capture.RepoRoot)
	}
	if op.Status != ChangeStatusApplied {
		return nil, fmt.Errorf("change %q is not rollback-ready (status=%s)", changeID, op.Status)
	}
	if strings.TrimSpace(op.PatchID) == "" {
		return cloneChangeOperation(op), &ChangeOperationError{
			Operation: cloneChangeOperation(op),
			Message:   manualRecoveryDetails(op, fmt.Sprintf("exact rollback is unavailable for change %q", changeID)),
		}
	}
	result, revertErr := rt.PatchRevert.Revert(ctx, kws.PatchRevertRequest{PatchID: op.PatchID})
	if revertErr != nil {
		if result != nil && result.Reverted {
			op.Status = ChangeStatusRollbackInconsistent
			op.RollbackMode = RollbackModeExact
			op.RollbackDetails = fmt.Sprintf("rollback reverted repository but durable metadata failed: %v", revertErr)
			if !result.RevertedAt.IsZero() {
				op.RolledBackAt = result.RevertedAt.UTC()
			} else {
				op.RolledBackAt = time.Now().UTC()
			}
			return persistInconsistentChange(ctx, store, op, fmt.Sprintf("rollback entered inconsistent state: %v", revertErr))
		}
		return cloneChangeOperation(op), &ChangeOperationError{
			Operation: cloneChangeOperation(op),
			Message:   manualRecoveryDetails(op, fmt.Sprintf("exact rollback failed: %v", revertErr)),
		}
	}

	op.Status = ChangeStatusRolledBack
	op.RollbackMode = RollbackModeExact
	op.RollbackDetails = ""
	if !result.RevertedAt.IsZero() {
		op.RolledBackAt = result.RevertedAt.UTC()
	} else {
		op.RolledBackAt = time.Now().UTC()
	}
	if err := store.Save(ctx, op); err != nil {
		op.Status = ChangeStatusRollbackInconsistent
		op.RollbackDetails = fmt.Sprintf("rollback reverted repository but failed to finalize change record: %v", err)
		return persistInconsistentChange(ctx, store, op, fmt.Sprintf("rollback mutated repository but failed to finalize durable state: %v", err))
	}
	return cloneChangeOperation(op), nil
}

func ListChangeOperations(ctx context.Context, workspace string, limit int) ([]ChangeSummary, error) {
	repoRoot, err := resolveRepoRoot(ctx, workspace)
	if err != nil {
		return nil, err
	}
	store, err := OpenChangeStore()
	if err != nil {
		return nil, err
	}
	items, err := store.ListByRepoRoot(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return SummarizeChanges(items), nil
}

func LoadChangeOperation(ctx context.Context, workspace, id string) (*ChangeOperation, error) {
	repoRoot, err := resolveRepoRoot(ctx, workspace)
	if err != nil {
		return nil, err
	}
	return LoadChangeOperationByRepoRoot(ctx, repoRoot, id)
}

func LoadChangeOperationByRepoRoot(ctx context.Context, repoRoot, id string) (*ChangeOperation, error) {
	store, err := OpenChangeStore()
	if err != nil {
		return nil, err
	}
	item, err := store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("change %q not found", id)
	}
	if canonicalRepoRoot(item.RepoRoot) != canonicalRepoRoot(repoRoot) {
		return nil, fmt.Errorf("change %q belongs to repository %q, current repository is %q", id, item.RepoRoot, repoRoot)
	}
	return item, nil
}

func attachCheckpointMetadata(ctx context.Context, rt ChangeRuntime, op *ChangeOperation) error {
	if op == nil || strings.TrimSpace(op.SessionID) == "" {
		return nil
	}
	if rt.SessionStore == nil || rt.CreateCheckpoint == nil {
		op.RecoveryDetails = appendDetails(op.RecoveryDetails, "checkpoint creation unavailable in current runtime")
		return nil
	}
	sess, err := loadRuntimeSession(ctx, rt, op.SessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session %q not found", op.SessionID)
	}
	record, err := rt.CreateCheckpoint(ctx, sess, ckpt.CheckpointCreateRequest{
		Note: strings.TrimSpace(op.Summary),
	})
	if err != nil {
		op.RecoveryDetails = appendDetails(op.RecoveryDetails, fmt.Sprintf("checkpoint creation failed: %v", err))
		return nil
	}
	if record != nil {
		op.CheckpointID = record.ID
		op.RecoveryMode = "patch+capture+checkpoint"
	}
	return nil
}

func attachTurnMetadata(ctx context.Context, rt ChangeRuntime, op *ChangeOperation) error {
	if op == nil || strings.TrimSpace(op.SessionID) == "" || rt.SessionStore == nil {
		return nil
	}
	sess, err := loadRuntimeSession(ctx, rt, op.SessionID)
	if err != nil {
		return err
	}
	if sess == nil || len(sess.Config.Metadata) == 0 {
		return nil
	}
	op.RunID = session.MetadataValueString(sess.Config.Metadata, session.MetadataRunID)
	op.TurnID = session.MetadataValueString(sess.Config.Metadata, session.MetadataTurnID)
	op.InstructionProfile = session.MetadataValueString(sess.Config.Metadata, session.MetadataInstructionProfile)
	op.ModelLane = session.MetadataValueString(sess.Config.Metadata, session.MetadataModelLane)
	op.VisibleTools = append([]string(nil), session.MetadataValuesStrings(sess.Config.Metadata, session.MetadataVisibleTools)...)
	op.HiddenTools = append([]string(nil), session.MetadataValuesStrings(sess.Config.Metadata, session.MetadataHiddenTools)...)
	return nil
}

func loadRuntimeSession(ctx context.Context, rt ChangeRuntime, id string) (*session.Session, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	if rt.SessionLookup != nil {
		if sess, ok := rt.SessionLookup(id); ok && sess != nil {
			return sess, nil
		}
	}
	if rt.SessionStore == nil {
		return nil, nil
	}
	return rt.SessionStore.Load(ctx, id)
}

func resolveRepoRoot(ctx context.Context, workspace string) (string, error) {
	capture, err := sandbox.NewGitRepoStateCapture(workspace).Capture(ctx)
	if err != nil {
		return "", err
	}
	if capture == nil {
		return "", kws.ErrRepoUnavailable
	}
	return canonicalRepoRoot(capture.RepoRoot), nil
}

func persistInconsistentChange(ctx context.Context, store *FileChangeStore, op *ChangeOperation, msg string) (*ChangeOperation, error) {
	if op == nil {
		return nil, errors.New(msg)
	}
	if store != nil {
		if err := store.Save(ctx, op); err != nil {
			msg = fmt.Sprintf("%s; additionally failed to persist inconsistent state: %v", msg, err)
		}
	}
	return cloneChangeOperation(op), &ChangeOperationError{
		Operation: cloneChangeOperation(op),
		Message:   msg,
	}
}

func appendDetails(base, extra string) string {
	parts := compactStrings([]string{base, extra})
	return strings.Join(parts, "; ")
}
