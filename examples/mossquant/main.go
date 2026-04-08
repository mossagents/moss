// mossquant 是一个使用 TUI 交互的投资研究与决策参考 Agent。
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/interaction"
	"github.com/mossagents/moss/kernel/loop"
	"github.com/mossagents/moss/kernel/middleware/builtins"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/scheduler"
	mosstui "github.com/mossagents/moss/userio/tui"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed templates/trading_prompt.tmpl
var tradingPromptTemplate string

type config struct {
	flags          *appkit.AppFlags
	capital        float64
	reviewInterval string
	autoReview     bool
}

type mossquantRuntime struct {
	flags          *appkit.AppFlags
	capital        float64
	reviewInterval string
	autoReview     bool
	profile        *InvestorProfile
	market         *market
	store          session.SessionStore
	sched          *scheduler.Scheduler
}

func main() {
	if err := appkit.InitializeApp("mossquant", nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	cfg := parseFlags()
	if err := launchTUI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *config {
	cfg := &config{}
	flag.Float64Var(&cfg.capital, "capital", 100000, "Starting capital ($)")
	flag.StringVar(&cfg.reviewInterval, "review-interval", "10m", "Default advisory review interval (e.g. 10m, 1h, @every 30m)")
	flag.BoolVar(&cfg.autoReview, "auto-review", true, "Automatically create the default periodic investment review job")
	cfg.flags = appkit.ParseAppFlags()
	if !workspaceProvided() && strings.TrimSpace(cfg.flags.Workspace) == "." {
		cfg.flags.Workspace = appconfig.AppDir()
	}
	return cfg
}

func launchTUI(cfg *config) error {
	flags := cfg.flags
	var rt *mossquantRuntime

	return mosstui.Run(mosstui.Config{
		ProviderName:    flags.DisplayProviderName(),
		Provider:        flags.Provider,
		Model:           flags.Model,
		Workspace:       flags.Workspace,
		Trust:           flags.Trust,
		SessionStoreDir: filepath.Join(appconfig.AppDir(), "sessions"),
		BaseURL:         flags.BaseURL,
		APIKey:          flags.APIKey,
		SidebarTitle:    "mossquant",
		RenderSidebar: func() string {
			if rt == nil || rt.profile == nil {
				return "```text\nmossquant\nTUI investment advisor\n```"
			}
			return "```text\n" + strings.TrimSpace(rt.profile.SummaryMarkdown()) + "\n```"
		},
		BuildKernel: func(wsDir, trust, approvalMode, profile, provider, model, apiKey, baseURL string, io intr.UserIO) (*kernel.Kernel, error) {
			identity := appconfig.NormalizeProviderIdentity("", provider, flags.DisplayProviderName())
			runtimeFlags := &appkit.AppFlags{
				Provider:  identity.Provider,
				Name:      identity.Name,
				Model:     model,
				Workspace: wsDir,
				Trust:     trust,
				Profile:   profile,
				APIKey:    apiKey,
				BaseURL:   baseURL,
			}
			var err error
			rt, err = newMossquantRuntime(runtimeFlags, cfg.capital, cfg.reviewInterval, cfg.autoReview)
			if err != nil {
				return nil, err
			}
			return rt.buildKernel(context.Background(), io)
		},
		AfterBoot: func(ctx context.Context, k *kernel.Kernel, io intr.UserIO) error {
			if rt == nil {
				return nil
			}
			return rt.afterBoot(ctx, k, io)
		},
		BuildSystemPrompt: func(workspace, trust string) string {
			profile, err := loadInvestorProfile(workspace)
			if err != nil {
				profile = &InvestorProfile{}
			}
			interval := effectiveReviewInterval(profile, cfg.reviewInterval)
			return buildSystemPrompt(workspace, cfg.capital, interval, profile)
		},
		BuildSessionConfig: func(workspace, trust, approvalMode, profileName, systemPrompt string) session.SessionConfig {
			profile, err := loadInvestorProfile(workspace)
			if err != nil {
				profile = &InvestorProfile{}
			}
			return session.SessionConfig{
				Goal:         "interactive investment research and advisory assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				Profile:      profileName,
				SystemPrompt: systemPrompt,
				MaxSteps:     120,
				Metadata: map[string]any{
					"risk_tolerance": profile.DisplayRiskTolerance(),
					"tracked_assets": profile.TrackedAssets(),
				},
			}
		},
		ScheduleController: runtime.SchedulerAdapter{
			Scheduler: rt.sched,
		},
	})
}

func newMossquantRuntime(flags *appkit.AppFlags, capital float64, reviewInterval string, autoReview bool) (*mossquantRuntime, error) {
	if capital <= 0 {
		capital = 100000
	}
	profile, err := loadInvestorProfile(flags.Workspace)
	if err != nil {
		return nil, fmt.Errorf("load investor profile: %w", err)
	}
	store, err := session.NewFileStore(filepath.Join(appconfig.AppDir(), "sessions"))
	if err != nil {
		return nil, fmt.Errorf("session store: %w", err)
	}
	jobStore, err := scheduler.NewFileJobStore(filepath.Join(appconfig.AppDir(), "jkobs.json"))
	if err != nil {
		return nil, fmt.Errorf("scheduler store: %w", err)
	}
	return &mossquantRuntime{
		flags:          flags,
		capital:        capital,
		reviewInterval: reviewInterval,
		autoReview:     autoReview,
		profile:        profile,
		market:         newMarket(capital),
		store:          store,
		sched:          scheduler.New(scheduler.WithPersistence(jobStore)),
	}, nil
}

func (r *mossquantRuntime) buildKernel(ctx context.Context, io intr.UserIO) (*kernel.Kernel, error) {
	memoriesDir := filepath.Join(appconfig.AppDir(), "memories")
	profile := r.profile

	k, err := appkit.BuildKernelWithExtensions(ctx, r.flags, io,
		appkit.WithSessionStore(r.store),
		appkit.WithScheduling(r.sched),
		appkit.WithPersistentMemories(memoriesDir),
		appkit.WithLoadedBootstrapContext(r.flags.Workspace, "mossquant"),
		appkit.AfterBuild(func(_ context.Context, built *kernel.Kernel) error {
			if err := registerTradeTools(built.ToolRegistry(), r.market); err != nil {
				return fmt.Errorf("register trade tools: %w", err)
			}
			if err := registerAnalysisTools(built.ToolRegistry(), r.market); err != nil {
				return fmt.Errorf("register analysis tools: %w", err)
			}
			if err := registerProfileTools(built.ToolRegistry(), r.flags.Workspace, profile); err != nil {
				return fmt.Errorf("register profile tools: %w", err)
			}
			if err := registerCredibilityTools(built.ToolRegistry()); err != nil {
				return fmt.Errorf("register credibility tools: %w", err)
			}
			if err := registerResearchAgents(built, r.flags); err != nil {
				return fmt.Errorf("register research agents: %w", err)
			}
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}

	k.WithPolicy(
		builtins.RequireApprovalFor("place_order"),
		builtins.DefaultAllow(),
	)
	k.OnEvent("tool.completed", func(e builtins.Event) {
		if data, ok := e.Data.(map[string]any); ok {
			if name, _ := data["tool"].(string); name == "place_order" {
				sendOutput(context.Background(), io, intr.OutputProgress, fmt.Sprintf("📊 Simulated trade executed at %s", e.Timestamp.Format("15:04:05")))
			}
		}
	})
	return k, nil
}

func (r *mossquantRuntime) afterBoot(ctx context.Context, k *kernel.Kernel, io intr.UserIO) error {
	r.profile, _ = loadInvestorProfile(r.flags.Workspace)
	if r.profile == nil {
		r.profile = &InvestorProfile{}
	}

	if err := runtime.StartScheduledRunner(ctx, runtime.ScheduledRunnerConfig{
		Kernel:       k,
		Scheduler:    r.sched,
		SessionStore: r.store,
		DefaultIO:    io,
		BuildSessionConfig: func(jobCtx context.Context, job scheduler.Job) (session.SessionConfig, error) {
			currentProfile, err := loadInvestorProfile(r.flags.Workspace)
			if err != nil {
				sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] failed to load profile: %v", job.ID, err))
				currentProfile = r.profile
			}
			interval := effectiveReviewInterval(currentProfile, r.reviewInterval)
			jobPrompt := buildSystemPrompt(r.flags.Workspace, r.capital, interval, currentProfile)

			jobCfg := job.Config
			if jobCfg.Goal == "" {
				jobCfg.Goal = job.Goal
			}
			if jobCfg.Mode == "" {
				jobCfg.Mode = "scheduled"
			}
			if jobCfg.TrustLevel == "" {
				jobCfg.TrustLevel = r.flags.Trust
			}
			if jobCfg.SystemPrompt == "" {
				jobCfg.SystemPrompt = jobPrompt
			}
			if jobCfg.MaxSteps <= 0 {
				jobCfg.MaxSteps = 40
			}
			return jobCfg, nil
		},
		BeforeRun: func(jobCtx context.Context, job scheduler.Job) {
			sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] started", job.ID))
		},
		OnCreateError: func(jobCtx context.Context, job scheduler.Job, err error) {
			sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] failed to create session: %v", job.ID, err))
		},
		OnRunError: func(jobCtx context.Context, job scheduler.Job, _ *session.Session, err error, _ intr.UserIO) {
			currentProfile, loadErr := loadInvestorProfile(r.flags.Workspace)
			if loadErr != nil || currentProfile == nil {
				currentProfile = r.profile
			}
			reportPath, reportErr := saveAdvisoryReport(r.flags.Workspace, job.ID, currentProfile, fmt.Sprintf("Scheduled advisory run failed.\n\nError: %v\n\nWhen external research tools are unavailable, rerun after configuring JINA_API_KEY or reduce the scope to manual/local analysis.", err))
			if reportErr == nil {
				sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] fallback report: %s", job.ID, reportPath))
			}
			sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] failed: %v", job.ID, err))
		},
		OnSaveError: func(jobCtx context.Context, job scheduler.Job, _ *session.Session, err error) {
			sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] failed to save session: %v", job.ID, err))
		},
		OnComplete: func(jobCtx context.Context, job scheduler.Job, _ *session.Session, result *loop.SessionResult, _ intr.UserIO) {
			currentProfile, loadErr := loadInvestorProfile(r.flags.Workspace)
			if loadErr != nil || currentProfile == nil {
				currentProfile = r.profile
			}
			reportPath, err := saveAdvisoryReport(r.flags.Workspace, job.ID, currentProfile, result.Output)
			if err != nil {
				sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] failed to save report: %v", job.ID, err))
			}

			summary := strings.TrimSpace(result.Output)
			if summary == "" {
				summary = fmt.Sprintf("Advisory run completed. Report saved to %s", reportPath)
			}
			sendOutput(jobCtx, io, intr.OutputText, fmt.Sprintf("⏰ Scheduled task [%s]\n%s", job.ID, summary))
			if reportPath != "" {
				sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] report: %s", job.ID, reportPath))
			}
			sendOutput(jobCtx, io, intr.OutputProgress, fmt.Sprintf("Scheduled task [%s] done (%d steps)", job.ID, result.Steps))
		},
	}); err != nil {
		return err
	}

	if r.autoReview {
		schedule, changed, err := ensureDefaultReviewJob(r.sched, r.profile, r.reviewInterval, r.flags.Trust)
		if err != nil {
			return fmt.Errorf("default review schedule: %w", err)
		}
		if changed {
			sendOutput(ctx, io, intr.OutputProgress, fmt.Sprintf("Investment review schedule synced: %s", schedule))
		}
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.market.tick()
			}
		}
	}()

	sendOutput(ctx, io, intr.OutputProgress, fmt.Sprintf("mossquant TUI ready — tracking %d assets, risk tolerance: %s", len(r.profile.TrackedAssets()), r.profile.DisplayRiskTolerance()))
	return nil
}

func buildSystemPrompt(workspace string, capital float64, reviewInterval string, profile *InvestorProfile) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	ctx["Capital"] = capital
	ctx["ReviewInterval"] = reviewInterval
	ctx["ProfileSummary"] = profile.SummaryMarkdown()
	ctx["TrackedAssets"] = profile.TrackedAssets()
	ctx["RiskTolerance"] = profile.DisplayRiskTolerance()
	return appconfig.RenderSystemPrompt(workspace, tradingPromptTemplate, ctx)
}

func sendOutput(ctx context.Context, io intr.UserIO, outputType intr.OutputType, content string) {
	if io == nil || strings.TrimSpace(content) == "" {
		return
	}
	_ = io.Send(ctx, intr.OutputMessage{
		Type:    outputType,
		Content: content,
	})
}

func workspaceProvided() bool {
	for i, arg := range os.Args[1:] {
		if arg == "--workspace" || strings.HasPrefix(arg, "--workspace=") {
			return true
		}
		if arg == "-workspace" && i+2 <= len(os.Args[1:]) {
			return true
		}
	}
	return false
}
