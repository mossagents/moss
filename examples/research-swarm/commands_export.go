package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	kruntime "github.com/mossagents/moss/kernel/runtime"
)

func runExportCommand(cfg *exportCommandConfig) error {
	ctx := context.Background()
	if cfg == nil {
		return fmt.Errorf("export config is required")
	}
	env, err := openSnapshotEnv()
	if err != nil {
		return err
	}
	defer env.Close(ctx)
	target, err := env.Targets.ResolveForExport(ctx, cfg.SessionID, cfg.RunID, cfg.Latest)
	if err != nil {
		return err
	}
	snapshot, err := env.Recovery.Load(ctx, target)
	if err != nil {
		return err
	}
	format := strings.ToLower(stringsTrim(cfg.Format))
	if format == "" {
		format = defaultExportFmt
	}
	outDir := stringsTrim(cfg.Output)
	if outDir == "" {
		outDir = filepath.Join(env.Paths.Exports, snapshot.RunID)
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(env.Paths.Root, outDir)
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return err
	}
	switch format {
	case "bundle":
		return exportBundle(ctx, env, snapshot, outDir, cfg.IncludePayloads)
	case "json":
		return exportJSONSummary(ctx, env, snapshot, outDir)
	case "jsonl":
		return exportJSONL(ctx, env, snapshot, outDir)
	default:
		return fmt.Errorf("unsupported export format %q", format)
	}
}

func exportBundle(ctx context.Context, env *runtimeEnv, snapshot *RecoveredRunSnapshot, outDir string, includePayloads bool) error {
	if err := exportJSONSummary(ctx, env, snapshot, outDir); err != nil {
		return err
	}
	data, err := env.EventStore.Export(ctx, snapshot.RootSessionID, kruntime.ExportFormatJSONL)
	if err != nil {
		if snapshot.EventsPartial {
			_ = os.WriteFile(filepath.Join(outDir, "events.warning.txt"), []byte(snapshot.EventsLastError), 0o600)
		} else {
			return err
		}
	} else if err := os.WriteFile(filepath.Join(outDir, "events.jsonl"), data, 0o600); err != nil {
		return err
	}
	if snapshot.FinalArtifactName != "" && snapshot.FinalArtifactThread != "" {
		item, err := env.Artifacts.Load(ctx, snapshot.FinalArtifactThread, snapshot.FinalArtifactName, 0)
		if err != nil {
			return err
		}
		if item == nil {
			return fmt.Errorf("final report artifact %q not found", snapshot.FinalArtifactName)
		}
		if err := os.WriteFile(filepath.Join(outDir, "final-report.md"), item.Data, 0o600); err != nil {
			return err
		}
	}
	if includePayloads {
		payloadDir := filepath.Join(outDir, "payloads")
		if err := os.MkdirAll(payloadDir, 0o700); err != nil {
			return err
		}
		for _, ref := range snapshot.Snapshot.Artifacts {
			item, err := env.Artifacts.Load(ctx, ref.SessionID, ref.Name, ref.Version)
			if err != nil || item == nil {
				continue
			}
			name := filepath.Base(ref.Name)
			if name == "" {
				name = ref.Name
			}
			if err := os.WriteFile(filepath.Join(payloadDir, name), item.Data, 0o600); err != nil {
				return err
			}
		}
	}
	fmt.Printf("exported bundle to %s\n", outDir)
	return nil
}

func exportJSONSummary(_ context.Context, _ *runtimeEnv, snapshot *RecoveredRunSnapshot, outDir string) error {
	summary := map[string]any{
		"run_id":            snapshot.RunID,
		"root_session_id":   snapshot.RootSessionID,
		"status":            snapshot.Status,
		"recoverable":       snapshot.Recoverable,
		"degraded":          snapshot.Degraded,
		"events_partial":    snapshot.EventsPartial,
		"events_last_error": snapshot.EventsLastError,
		"lang":              snapshot.Lang,
		"thread_count":      len(snapshot.Snapshot.Threads),
		"task_count":        len(snapshot.Snapshot.Tasks),
		"artifact_count":    len(snapshot.Snapshot.Artifacts),
	}
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "summary.json"), data, 0o600); err != nil {
		return err
	}
	artifacts, err := json.MarshalIndent(snapshot.Snapshot.Artifacts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "artifacts.json"), artifacts, 0o600)
}

func exportJSONL(ctx context.Context, env *runtimeEnv, snapshot *RecoveredRunSnapshot, outDir string) error {
	if snapshot.EventsPartial {
		return fmt.Errorf("runtime events for run %q are partial; jsonl export requires a complete event stream", snapshot.RunID)
	}
	data, err := env.EventStore.Export(ctx, snapshot.RootSessionID, kruntime.ExportFormatJSONL)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "events.jsonl"), data, 0o600); err != nil {
		return err
	}
	fmt.Printf("exported events to %s\n", filepath.Join(outDir, "events.jsonl"))
	return nil
}
