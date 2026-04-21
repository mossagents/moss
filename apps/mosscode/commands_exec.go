package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	mosstui "github.com/mossagents/moss/contrib/tui"
	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/appkit/product"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
)

func launchTUI(cfg *config) error {
	invocation, err := resolveRuntimeInvocation(cfg, "interactive")
	if err != nil {
		return err
	}
	flags := cloneAppFlags(invocation.CompatFlags)
	cfg.approvalMode = invocation.ApprovalMode
	return mosstui.Run(mosstui.Config{
		Provider:         flags.Provider,
		WelcomeBanner:    welcomeBanner,
		Model:            flags.Model,
		Workspace:        flags.Workspace,
		Trust:            flags.Trust,
		Profile:          flags.Profile,
		ApprovalMode:     invocation.ApprovalMode,
		SessionStoreDir:  runtimeenv.SessionStoreDir(),
		BaseURL:          flags.BaseURL,
		APIKey:           flags.APIKey,
		BaseObserver:     cfg.observer,
		InitialSessionID: cfg.resumeSessionID,
		Extensions:       []*mosstui.Extension{newCodingExtension(flags.Workspace)},
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
			return buildKernel(context.Background(), runtimeFlags, io, approvalMode, cfg.governance, cfg.observer)
		},
		PromptConfigInstructions: buildProductPromptInstructions(flags.Workspace, flags.Trust),
		BuildSessionConfig: func(workspace, trust, approvalMode, profile, systemPrompt string) session.SessionConfig {
			runtimeFlags := runtimeFlags(workspace, flags.Provider, flags.Model, flags.APIKey, flags.BaseURL)
			runtimeFlags.Trust = trust
			runtimeFlags.Profile = profile
			base := session.SessionConfig{
				Goal:         "interactive coding assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				SystemPrompt: systemPrompt,
				MaxSteps:     200,
			}
			if invocation.Typed && matchesCompatibilitySelection(invocation, trust, approvalMode, profile) {
				return buildTypedProjectedSessionConfig(base, runtimeFlags, invocation)
			}
			return buildLegacyProjectedSessionConfig(base, runtimeFlags, trust, approvalMode, profile, "coding")
		},
	})
}

func runResume(ctx context.Context, cfg *config) error {
	summaries, snapshotCounts, err := runtimeenv.ListResumeCandidates(ctx, cfg.flags.Workspace)
	if err != nil {
		return err
	}
	selected, recoverable, err := runtimeenv.SelectResumeSummary(summaries, cfg.resumeSessionID, cfg.resumeLatest)
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
	invocation, err := resolveRuntimeInvocation(cfg, "oneshot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	cfg.approvalMode = invocation.ApprovalMode
	report, err := executeOneShot(ctx, cfg, invocation)
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

func executeOneShot(ctx context.Context, cfg *config, invocation runtimeInvocation) (product.ExecReport, error) {
	runtimeFlags := cloneAppFlags(invocation.CompatFlags)
	report := product.ExecReport{
		App:          appName,
		Goal:         cfg.prompt,
		Workspace:    runtimeFlags.Workspace,
		Provider:     runtimeFlags.DisplayProviderName(),
		Model:        runtimeFlags.Model,
		Trust:        runtimeFlags.Trust,
		ApprovalMode: invocation.ApprovalMode,
		Status:       "failed",
	}
	var recorder *product.RecordingIO
	traceRecorder := product.NewRunTraceRecorder()
	var userIO io.UserIO
	if cfg.execJSON {
		recorder = product.NewRecordingIO(invocation.ApprovalMode)
		userIO = recorder
	} else {
		userIO = io.NewConsoleIO()
	}
	traceObserver := product.NewPricingObserver(cfg.pricingCatalog, traceRecorder)
	k, err := buildKernel(ctx, runtimeFlags, userIO, invocation.ApprovalMode, cfg.governance, observe.JoinObservers(cfg.observer, traceObserver))
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
		modelName := runtimeFlags.Model
		if modelName == "" {
			modelName = "(default)"
		}
		hints := map[string]string{
			"Provider":  runtimeFlags.Provider,
			"Model":     modelName,
			"Workspace": runtimeFlags.Workspace,
			"Run":       "oneshot",
			"Trust":     runtimeFlags.Trust,
			"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
			"Prompt":    cfg.prompt,
		}
		if invocation.Typed {
			hints["Preset"] = strings.TrimSpace(invocation.ResolvedSpec.Origin.Preset)
			hints["Mode"] = strings.TrimSpace(invocation.ResolvedSpec.Intent.CollaborationMode)
			hints["Permissions"] = strings.TrimSpace(invocation.ResolvedSpec.Runtime.PermissionProfile)
		} else {
			hints["Profile"] = runtimeFlags.Profile
			hints["Approval"] = invocation.ApprovalMode
		}
		appkit.PrintBannerWithHint("mosscode — Code Assistant",
			hints,
			"Using deep harness defaults: persistent threads/memories + context offload + async task lifecycle.",
		)
	}

	baseSessionConfig := session.SessionConfig{
		Goal:       cfg.prompt,
		Mode:       "oneshot",
		TrustLevel: runtimeFlags.Trust,
		MaxSteps:   80,
	}
	sessCfg := buildLegacyProjectedSessionConfig(baseSessionConfig, runtimeFlags, runtimeFlags.Trust, invocation.ApprovalMode, runtimeFlags.Profile, "coding")
	if invocation.Typed {
		sessCfg = buildTypedProjectedSessionConfig(baseSessionConfig, runtimeFlags, invocation)
	}
	systemPrompt, metadata, err := composeProductSystemPrompt(runtimeFlags.Workspace, runtimeFlags.Trust, k, sessCfg)
	if err != nil {
		report.Error = err.Error()
		return report, fmt.Errorf("compose system prompt: %w", err)
	}
	sessCfg.SystemPrompt = systemPrompt
	sessCfg.Metadata = metadata
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

func runtimeFlags(workspace, provider, model, apiKey, baseURL string) *appkit.AppFlags {
	return &appkit.AppFlags{
		Provider:  provider,
		Model:     model,
		Workspace: workspace,
		APIKey:    apiKey,
		BaseURL:   baseURL,
	}
}

func printResumeCandidates(summaries []session.SessionSummary, snapshotCounts map[string]int) {
	if len(summaries) == 0 {
		fmt.Println("No recoverable threads found.")
		return
	}
	fmt.Println("Recoverable threads:")
	for _, summary := range summaries {
		fmt.Printf("- %s | status=%s | run=%s | collab=%s | permissions=%s | steps=%d | snapshots=%d | created=%s | goal=%s\n",
			summary.ID,
			summary.Status,
			strings.TrimSpace(summary.Mode),
			strings.TrimSpace(summary.CollaborationMode),
			strings.TrimSpace(summary.PermissionProfile),
			summary.Steps,
			snapshotCounts[summary.ID],
			summary.CreatedAt,
			summary.Goal,
		)
	}
	fmt.Println()
	fmt.Println("Use `mosscode resume --latest` or `mosscode resume --session <id>` to continue the thread.")
}
