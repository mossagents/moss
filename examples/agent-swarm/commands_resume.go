package main

import (
	"context"
	"fmt"
)

func runResumeCommand(cfg *resumeCommandConfig) error {
	ctx := context.Background()
	if cfg == nil {
		return fmt.Errorf("resume config is required")
	}
	env, err := buildExecutionEnv(ctx, &cfg.AppFlags)
	if err != nil {
		return err
	}
	defer env.Close(ctx)
	target, err := env.Targets.ResolveForResume(ctx, cfg.SessionID, cfg.RunID, cfg.Latest)
	if err != nil {
		return err
	}
	lease, err := env.Locks.Acquire(target.SwarmRunID)
	if err != nil {
		return err
	}
	defer lease.Release()
	snapshot, err := env.Recovery.Load(ctx, target)
	if err != nil {
		return err
	}
	result, err := resumeRunWorkflow(ctx, env, target, snapshot, cfg)
	if err != nil {
		return err
	}
	if err := maybeWriteRunOutput(cfg.Output, env.Paths.Root, "resume-summary.json", result); err != nil {
		return err
	}
	fmt.Printf("run_id=%s root_session_id=%s status=%s source=%s artifacts=%d tasks=%d threads=%d\n",
		result.RunID, result.RootSessionID, result.Status, result.ResolutionSource, result.ArtifactCount, result.TaskCount, result.ThreadCount)
	if result.FinalArtifact != "" {
		fmt.Printf("final_report=%s\n", result.FinalArtifact)
	}
	return nil
}
