package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mossagents/moss/kernel/ids"
	kernio "github.com/mossagents/moss/kernel/io"
)

func runRunCommand(cfg *runCommandConfig) error {
	ctx := context.Background()
	if cfg == nil {
		return fmt.Errorf("run config is required")
	}
	if cfg.RunID == "" {
		cfg.RunID = "swarm-" + ids.New()
	}
	env, err := buildExecutionEnv(ctx, &cfg.AppFlags, kernio.NewConsoleIO())
	if err != nil {
		return err
	}
	defer env.Close(ctx)
	lease, err := env.Locks.Acquire(cfg.RunID)
	if err != nil {
		return err
	}
	defer lease.Release()
	result, err := startRunWorkflow(ctx, env, cfg)
	if err != nil {
		return err
	}
	if err := maybeWriteRunOutput(cfg.Output, env.Paths.Root, "run-summary.json", result); err != nil {
		return err
	}
	fmt.Printf("run_id=%s root_session_id=%s status=%s artifacts=%d tasks=%d threads=%d\n",
		result.RunID, result.RootSessionID, result.Status, result.ArtifactCount, result.TaskCount, result.ThreadCount)
	if result.FinalArtifact != "" {
		fmt.Printf("final_report=%s\n", result.FinalArtifact)
	}
	return nil
}

func maybeWriteRunOutput(dir, appRoot, name string, payload any) error {
	dir = stringsTrim(dir)
	if dir == "" {
		return nil
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(appRoot, dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}
