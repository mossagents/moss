package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mossagents/moss/harness/appkit/product/changes"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/workspace"
)

type checkpointActionReport struct {
	Mode             string                         `json:"mode"`
	Checkpoints      []runtimeenv.CheckpointSummary `json:"checkpoints,omitempty"`
	Checkpoint       *runtimeenv.CheckpointSummary  `json:"checkpoint,omitempty"`
	CheckpointDetail *runtimeenv.CheckpointDetail   `json:"checkpoint_detail,omitempty"`
	SessionID        string                         `json:"session_id,omitempty"`
	SourceKind       string                         `json:"source_kind,omitempty"`
	SourceID         string                         `json:"source_id,omitempty"`
	ReplayMode       string                         `json:"replay_mode,omitempty"`
	RestoredWorktree bool                           `json:"restored_worktree,omitempty"`
	Degraded         bool                           `json:"degraded,omitempty"`
	Details          string                         `json:"details,omitempty"`
	Note             string                         `json:"note,omitempty"`
}

type changeActionReport struct {
	Mode    string                   `json:"mode"`
	Change  *changes.ChangeOperation `json:"change,omitempty"`
	Changes []changes.ChangeSummary  `json:"changes,omitempty"`
	Details string                   `json:"details,omitempty"`
}

func runFork(ctx context.Context, cfg *config) error {
	return runCheckpointFork(ctx, cfg)
}

func runCheckpoint(ctx context.Context, cfg *config) error {
	if strings.TrimSpace(cfg.checkpointAction) == "" {
		return fmt.Errorf("usage: mosscode checkpoint <list|show|create|replay> [flags]")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.checkpointAction)) {
	case "list":
		return runCheckpointList(ctx, cfg)
	case "show":
		return runCheckpointShow(ctx, cfg)
	case "create":
		return runCheckpointCreate(ctx, cfg)
	case "fork":
		return fmt.Errorf("checkpoint branching moved to `mosscode fork`; use `mosscode fork [--session <thread-id> | --checkpoint <id|latest> | --latest]`")
	case "replay":
		return runCheckpointReplay(ctx, cfg)
	default:
		return fmt.Errorf("unknown checkpoint command %q (supported: list, show, create, replay)", cfg.checkpointAction)
	}
}

func runCheckpointList(ctx context.Context, cfg *config) error {
	limit := cfg.checkpointLimit
	if limit <= 0 {
		limit = 20
	}
	items, err := runtimeenv.ListCheckpoints(ctx, limit)
	if err != nil {
		return err
	}
	report := checkpointActionReport{
		Mode:        "list",
		Checkpoints: items,
	}
	if cfg.checkpointJSON {
		return printJSON(report)
	}
	fmt.Println(runtimeenv.RenderCheckpointSummaries(items))
	return nil
}

func runCheckpointShow(ctx context.Context, cfg *config) error {
	checkpointID := strings.TrimSpace(cfg.checkpointID)
	if checkpointID == "" {
		return fmt.Errorf("usage: mosscode checkpoint show <id|latest> [--json]")
	}
	detail, err := runtimeenv.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return err
	}
	report := checkpointActionReport{
		Mode:             "show",
		CheckpointDetail: detail,
	}
	if cfg.checkpointJSON {
		return printJSON(report)
	}
	fmt.Println(runtimeenv.RenderCheckpointDetail(detail))
	return nil
}

func runCheckpointCreate(ctx context.Context, cfg *config) error {
	sessionID := strings.TrimSpace(cfg.checkpointCreateSessionID)
	note := strings.TrimSpace(cfg.checkpointCreateNote)
	if sessionID == "" {
		return fmt.Errorf("usage: mosscode checkpoint create --session <thread-id> [--note <note>] [--json]")
	}
	k, err := buildCheckpointKernel(ctx, cfg)
	if err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	if err := k.Boot(ctx); err != nil {
		return err
	}
	if k.SessionStore() == nil {
		return fmt.Errorf("thread store is unavailable")
	}
	sess, err := k.SessionStore().Load(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("thread %q not found", sessionID)
	}
	record, err := k.CreateCheckpoint(ctx, sess, checkpoint.CheckpointCreateRequest{Note: note})
	if err != nil {
		return err
	}
	summary := runtimeenv.SummarizeCheckpoint(*record)
	report := checkpointActionReport{
		Mode:       "create",
		Checkpoint: &summary,
		Note:       note,
	}
	if cfg.checkpointJSON {
		return printJSON(report)
	}
	fmt.Printf("Created checkpoint %s for thread %s.\n", summary.ID, summary.SessionID)
	if summary.SnapshotID != "" {
		fmt.Printf("Snapshot: %s\n", summary.SnapshotID)
	}
	fmt.Printf("Patches: %d | Lineage: %d\n", summary.PatchCount, summary.LineageDepth)
	if strings.TrimSpace(summary.Note) != "" {
		fmt.Printf("Note: %s\n", summary.Note)
	}
	return nil
}

func runCheckpointFork(ctx context.Context, cfg *config) error {
	sessionID := strings.TrimSpace(cfg.forkSessionID)
	checkpointID := strings.TrimSpace(cfg.forkCheckpointID)
	sourceKind := checkpoint.ForkSourceSession
	sourceID := sessionID
	if cfg.forkLatest {
		if sessionID != "" {
			return fmt.Errorf("use either --session or --latest, not both")
		}
		if checkpointID != "" && !strings.EqualFold(checkpointID, "latest") {
			return fmt.Errorf("use either --checkpoint <id> or --latest, not both")
		}
		sourceKind = checkpoint.ForkSourceCheckpoint
		sourceID = ""
	} else if checkpointID != "" {
		if sourceID != "" {
			return fmt.Errorf("use either --session or --checkpoint, not both")
		}
		sourceKind = checkpoint.ForkSourceCheckpoint
		sourceID = checkpointID
	}
	if sourceKind == checkpoint.ForkSourceSession && sourceID == "" {
		return fmt.Errorf("usage: mosscode fork [--session <thread-id> | --checkpoint <id|latest> | --latest] [--restore-worktree] [--json]")
	}
	k, err := buildCheckpointKernel(ctx, cfg)
	if err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	if err := k.Boot(ctx); err != nil {
		return err
	}
	if sourceKind == checkpoint.ForkSourceCheckpoint {
		record, err := runtimeenv.ResolveCheckpointRecord(ctx, k.Checkpoints(), sourceID)
		if err != nil {
			return err
		}
		sourceID = record.ID
	}
	sess, result, err := k.ForkSession(ctx, checkpoint.ForkRequest{
		SourceKind:      sourceKind,
		SourceID:        sourceID,
		RestoreWorktree: cfg.forkRestoreWorktree,
	})
	if err != nil {
		return err
	}
	report := checkpointActionReport{
		Mode:             "fork",
		SessionID:        sess.ID,
		SourceKind:       string(result.SourceKind),
		SourceID:         result.SourceID,
		RestoredWorktree: result.RestoredWorktree,
		Degraded:         result.Degraded,
		Details:          result.Details,
	}
	if cfg.forkJSON {
		return printJSON(report)
	}
	fmt.Printf("Prepared forked thread %s from %s %s.\n", sess.ID, result.SourceKind, result.SourceID)
	if result.RestoredWorktree {
		fmt.Println("Worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Printf("Degraded: %s\n", result.Details)
	}
	fmt.Printf("Use `mosscode resume --session %s` to continue the thread.\n", sess.ID)
	return nil
}

func runCheckpointReplay(ctx context.Context, cfg *config) error {
	checkpointID := strings.TrimSpace(cfg.checkpointID)
	mode := strings.ToLower(strings.TrimSpace(cfg.checkpointReplayMode))
	if mode == "" {
		mode = string(checkpoint.ReplayModeResume)
	}
	if cfg.checkpointLatest {
		if checkpointID != "" && !strings.EqualFold(checkpointID, "latest") {
			return fmt.Errorf("use either --checkpoint <id> or --latest, not both")
		}
		checkpointID = ""
	}
	if checkpointID == "" && !cfg.checkpointLatest {
		return fmt.Errorf("usage: mosscode checkpoint replay [--checkpoint <id|latest> | --latest] [--replay-mode resume|rerun] [--restore-worktree] [--json]")
	}
	if mode != string(checkpoint.ReplayModeResume) && mode != string(checkpoint.ReplayModeRerun) {
		return fmt.Errorf("replay mode must be resume or rerun")
	}
	k, err := buildCheckpointKernel(ctx, cfg)
	if err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	if err := k.Boot(ctx); err != nil {
		return err
	}
	record, err := runtimeenv.ResolveCheckpointRecord(ctx, k.Checkpoints(), checkpointID)
	if err != nil {
		return err
	}
	sess, result, err := k.ReplayFromCheckpoint(ctx, checkpoint.ReplayRequest{
		CheckpointID:    record.ID,
		Mode:            checkpoint.ReplayMode(mode),
		RestoreWorktree: cfg.checkpointRestoreWorktree,
	})
	if err != nil {
		return err
	}
	report := checkpointActionReport{
		Mode:             "replay",
		SessionID:        sess.ID,
		ReplayMode:       string(result.Mode),
		RestoredWorktree: result.RestoredWorktree,
		Degraded:         result.Degraded,
		Details:          result.Details,
	}
	if cfg.checkpointJSON {
		return printJSON(report)
	}
	fmt.Printf("Prepared replay thread %s from checkpoint %s (%s).\n", sess.ID, result.CheckpointID, result.Mode)
	if result.RestoredWorktree {
		fmt.Println("Worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Printf("Degraded: %s\n", result.Details)
	}
	fmt.Printf("Use `mosscode resume --session %s` to continue the thread.\n", sess.ID)
	return nil
}

func runApply(ctx context.Context, cfg *config) error {
	patchFile := strings.TrimSpace(cfg.applyPatchFile)
	summary := strings.TrimSpace(cfg.applySummary)
	sessionID := strings.TrimSpace(cfg.applySessionID)
	if patchFile == "" {
		return fmt.Errorf("usage: mosscode apply --patch-file <path> [--summary <text>] [--session <thread-id>] [--json]")
	}
	data, err := os.ReadFile(patchFile)
	if err != nil {
		return fmt.Errorf("read patch file: %w", err)
	}
	rt, cleanup, err := buildChangeRuntime(ctx, cfg, sessionID)
	if err != nil {
		return err
	}
	defer cleanup()
	item, err := changes.ApplyChange(ctx, rt, changes.ApplyChangeRequest{
		Patch:     string(data),
		Summary:   summary,
		SessionID: sessionID,
		Source:    workspace.PatchSourceUser,
	})
	report := changeActionReport{
		Mode:   "apply",
		Change: item,
	}
	if err != nil {
		if opErr := (*changes.ChangeOperationError)(nil); errors.As(err, &opErr) {
			report.Change = opErr.Operation
			report.Details = opErr.Error()
			return emitChangeReport(report, cfg.applyJSON, true)
		}
		return err
	}
	return emitChangeReport(report, cfg.applyJSON, false)
}

func runRollback(ctx context.Context, cfg *config) error {
	changeID := strings.TrimSpace(cfg.rollbackChangeID)
	if changeID == "" {
		return fmt.Errorf("usage: mosscode rollback --change <id> [--json]")
	}
	rt, cleanup, err := buildChangeRuntime(ctx, cfg, "")
	if err != nil {
		return err
	}
	defer cleanup()
	item, err := changes.RollbackChange(ctx, rt, changes.RollbackChangeRequest{ChangeID: changeID})
	report := changeActionReport{
		Mode:   "rollback",
		Change: item,
	}
	if err != nil {
		if opErr := (*changes.ChangeOperationError)(nil); errors.As(err, &opErr) {
			report.Change = opErr.Operation
			report.Details = opErr.Error()
			return emitChangeReport(report, cfg.rollbackJSON, true)
		}
		return err
	}
	return emitChangeReport(report, cfg.rollbackJSON, false)
}

func runChanges(ctx context.Context, cfg *config) error {
	if strings.TrimSpace(cfg.changesAction) == "" {
		return fmt.Errorf("usage: mosscode changes <list|show> [flags]")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.changesAction)) {
	case "list":
		return runChangesList(ctx, cfg)
	case "show":
		return runChangesShow(ctx, cfg)
	default:
		return fmt.Errorf("unknown changes command %q (supported: list, show)", cfg.changesAction)
	}
}

func runChangesList(ctx context.Context, cfg *config) error {
	limit := cfg.changesLimit
	if limit <= 0 {
		limit = 20
	}
	items, err := changes.ListChangeOperations(ctx, cfg.flags.Workspace, limit)
	if err != nil {
		return err
	}
	report := changeActionReport{
		Mode:    "list",
		Changes: items,
	}
	return emitChangeReport(report, cfg.changesJSON, false)
}

func runChangesShow(ctx context.Context, cfg *config) error {
	changeID := strings.TrimSpace(cfg.changesShowID)
	if changeID == "" {
		return fmt.Errorf("usage: mosscode changes show <id> [--json]")
	}
	item, err := changes.LoadChangeOperation(ctx, cfg.flags.Workspace, changeID)
	if err != nil {
		return err
	}
	report := changeActionReport{
		Mode:   "show",
		Change: item,
	}
	return emitChangeReport(report, cfg.changesJSON, false)
}

func emitChangeReport(report changeActionReport, jsonOut, fail bool) error {
	if jsonOut {
		if err := printJSON(report); err != nil {
			return err
		}
		if fail {
			return &commandExitError{code: 1}
		}
		return nil
	}
	switch report.Mode {
	case "list":
		fmt.Println(changes.RenderChangeSummaries(report.Changes))
	case "show", "apply", "rollback":
		fmt.Println(changes.RenderChangeDetail(report.Change))
	}
	if strings.TrimSpace(report.Details) != "" {
		fmt.Printf("Details: %s\n", report.Details)
	}
	if fail {
		return &commandExitError{code: 1}
	}
	return nil
}
