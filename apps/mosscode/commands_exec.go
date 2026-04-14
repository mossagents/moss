package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/product"
	mosstui "github.com/mossagents/moss/contrib/tui"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	appruntime "github.com/mossagents/moss/runtime"
)

func launchTUI(cfg *config) error {
	flags := cfg.flags
	resolved, err := resolveProfileForConfig(cfg)
	if err != nil {
		return err
	}
	flags.Trust = resolved.Trust
	flags.Profile = resolved.Name
	cfg.approvalMode = resolved.ApprovalMode
	return mosstui.Run(mosstui.Config{
		Provider:         flags.Provider,
		WelcomeBanner:    welcomeBanner,
		Model:            flags.Model,
		Workspace:        flags.Workspace,
		Trust:            resolved.Trust,
		Profile:          resolved.Name,
		ApprovalMode:     resolved.ApprovalMode,
		SessionStoreDir:  product.SessionStoreDir(),
		BaseURL:          flags.BaseURL,
		APIKey:           flags.APIKey,
		BaseObserver:     cfg.observer,
		InitialSessionID: cfg.resumeSessionID,
		BuildRunTraceObserver: func() (*product.RunTraceRecorder, observe.Observer) {
			recorder := product.NewRunTraceRecorder()
			return recorder, product.NewPricingObserver(cfg.pricingCatalog, recorder)
		},
		BuildKernel: func(wsDir, trust, approvalMode, profile, provider, model, apiKey, baseURL string, io io.UserIO) (*kernel.Kernel, error) {
			runtimeFlags := &appkit.AppFlags{
				Provider:  provider,
				Model:     model,
				Workspace: wsDir,
				Trust:     trust,
				Profile:   profile,
				APIKey:    apiKey,
				BaseURL:   baseURL,
			}
			k, _, err := buildKernel(context.Background(), runtimeFlags, io, approvalMode, cfg.governance, cfg.observer)
			return k, err
		},
		BuildSystemPrompt: buildSystemPrompt,
		BuildSessionConfig: func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig {
			resolvedProfile, err := appruntime.ResolveProfileForWorkspace(appruntime.ProfileResolveOptions{
				Workspace:        workspace,
				RequestedProfile: profile,
				Trust:            trust,
				ApprovalMode:     approvalMode,
			})
			if err != nil {
				resolvedProfile = appruntime.ResolvedProfile{
					Name:         profile,
					TaskMode:     profile,
					Trust:        trust,
					ApprovalMode: approvalMode,
					ToolPolicy:   appruntime.ResolveToolPolicyForWorkspace(workspace, trust, approvalMode),
				}
			}
			return appruntime.ApplyResolvedProfileToSessionConfig(session.SessionConfig{
				Goal:         "interactive coding assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				SystemPrompt: systemPrompt,
				MaxSteps:     200,
			}, resolvedProfile)
		},
	})
}

func runResume(ctx context.Context, cfg *config) error {
	summaries, snapshotCounts, err := product.ListResumeCandidates(ctx, cfg.flags.Workspace)
	if err != nil {
		return err
	}
	selected, recoverable, err := product.SelectResumeSummary(summaries, cfg.resumeSessionID, cfg.resumeLatest)
	if err != nil {
		return err
	}
	if selected == nil {
		printResumeCandidates(recoverable, snapshotCounts)
		return nil
	}
	cfg.resumeSessionID = selected.ID
	fmt.Printf("Resuming thread %s (status=%s steps=%d snapshots=%d)\n",
		selected.ID, selected.Status, selected.Steps, snapshotCounts[selected.ID])
	return launchTUI(cfg)
}

func runExec(ctx context.Context, cfg *config) int {
	resolved, err := resolveProfileForConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	cfg.flags.Trust = resolved.Trust
	cfg.flags.Profile = resolved.Name
	cfg.approvalMode = resolved.ApprovalMode
	report, err := executeOneShot(ctx, cfg)
	if cfg.execJSON {
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			fmt.Fprintf(os.Stderr, "error: marshal exec report: %v\n", marshalErr)
			return 1
		}
		fmt.Println(string(data))
		if err != nil {
			return 1
		}
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func executeOneShot(ctx context.Context, cfg *config) (product.ExecReport, error) {
	report := product.ExecReport{
		App:          appName,
		Goal:         cfg.prompt,
		Workspace:    cfg.flags.Workspace,
		Provider:     cfg.flags.DisplayProviderName(),
		Model:        cfg.flags.Model,
		Trust:        cfg.flags.Trust,
		ApprovalMode: cfg.approvalMode,
		Status:       "failed",
	}
	var recorder *product.RecordingIO
	traceRecorder := product.NewRunTraceRecorder()
	var userIO io.UserIO
	if cfg.execJSON {
		recorder = product.NewRecordingIO(cfg.approvalMode)
		userIO = recorder
	} else {
		userIO = io.NewConsoleIO()
	}
	traceObserver := product.NewPricingObserver(cfg.pricingCatalog, traceRecorder)
	k, resolved, err := buildKernel(ctx, cfg.flags, userIO, cfg.approvalMode, cfg.governance, observe.JoinObservers(cfg.observer, traceObserver))
	if err != nil {
		report.Error = err.Error()
		return report, err
	}
	if err := k.Boot(ctx); err != nil {
		report.Error = err.Error()
		return report, err
	}
	defer k.Shutdown(ctx)

	if !cfg.execJSON {
		modelName := cfg.flags.Model
		if modelName == "" {
			modelName = "(default)"
		}
		appkit.PrintBannerWithHint("mosscode — Code Assistant",
			map[string]string{
				"Provider":  cfg.flags.Provider,
				"Model":     modelName,
				"Workspace": cfg.flags.Workspace,
				"Mode":      "one-shot",
				"Profile":   resolved.Name,
				"Trust":     resolved.Trust,
				"Approval":  resolved.ApprovalMode,
				"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
				"Prompt":    cfg.prompt,
			},
			"Using deep harness defaults: persistent threads/memories + context offload + async task lifecycle.",
		)
	}

	sessCfg := appruntime.ApplyResolvedProfileToSessionConfig(session.SessionConfig{
		Goal:         cfg.prompt,
		Mode:         "oneshot",
		TrustLevel:   resolved.Trust,
		SystemPrompt: buildSystemPrompt(cfg.flags.Workspace, resolved.Trust),
		MaxSteps:     80,
	}, resolved)
	sess, err := k.NewSession(ctx, sessCfg)
	if err != nil {
		report.Error = err.Error()
		return report, fmt.Errorf("create session: %w", err)
	}
	report.SessionID = sess.ID
	userMsg := model.Message{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart(cfg.prompt)}}
	sess.AppendMessage(userMsg)

	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     sess,
		Agent:       k.BuildLLMAgent("mosscode"),
		UserContent: &userMsg,
	})
	if recorder != nil {
		report.Events = recorder.Events()
	}
	trace := traceRecorder.Snapshot()
	report.PromptTokens = trace.PromptTokens
	report.CompletionTokens = trace.CompletionTokens
	report.Tokens = trace.TotalTokens
	report.EstimatedCostUSD = trace.EstimatedCostUSD
	report.Trace = trace.Timeline
	if err != nil {
		report.Error = err.Error()
		return report, fmt.Errorf("run: %w", err)
	}
	report.Status = "completed"
	report.SessionID = sess.ID
	report.Steps = result.Steps
	if report.Tokens == 0 {
		report.Tokens = result.TokensUsed.TotalTokens
	}
	report.Output = result.Output

	if !cfg.execJSON {
		fmt.Println()
		fmt.Printf("✅ Done (thread: %s, steps: %d, tokens: %d", sess.ID, result.Steps, report.Tokens)
		if report.EstimatedCostUSD > 0 {
			fmt.Printf(", cost: $%.6f", report.EstimatedCostUSD)
		}
		fmt.Printf(")\n")
		if strings.TrimSpace(result.Output) != "" {
			fmt.Printf("\n%s\n", result.Output)
		}
	}
	return report, nil
}

func printResumeCandidates(summaries []session.SessionSummary, snapshotCounts map[string]int) {
	if len(summaries) == 0 {
		fmt.Println("No recoverable threads found.")
		return
	}
	fmt.Println("Recoverable threads:")
	for _, summary := range summaries {
		fmt.Printf("- %s | status=%s | steps=%d | snapshots=%d | created=%s | goal=%s\n",
			summary.ID, summary.Status, summary.Steps, snapshotCounts[summary.ID], summary.CreatedAt, summary.Goal)
	}
	fmt.Println()
	fmt.Println("Use `mosscode resume --latest` or `mosscode resume --session <id>` to continue the thread.")
}
