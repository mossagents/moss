package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	intr "github.com/mossagents/moss/kernel/interaction"
	mdl "github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/userio/prompting"
	"github.com/mossagents/moss/userio/tui"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

const version = "0.3.0"

func main() {
	logging.EnableDebugFromArgs(os.Args[1:])
	args := stripLeadingDebugArgs(os.Args[1:])
	// 确保 ~/.moss 配置目录存在
	if err := config.EnsureAppDir(); err != nil {
		logging.GetLogger().Warn("cannot create config dir", slog.Any("error", err))
	}
	if enabled, path, closer, err := logging.ConfigureDebugFileWhenEnabled(config.AppDir()); err != nil {
		logging.GetLogger().Warn("cannot enable debug file logging", slog.Any("error", err))
	} else {
		if closer != nil {
			defer closer.Close()
		}
		if enabled {
			logging.GetLogger().Info("debug file logging enabled", slog.String("path", path))
		}
	}

	// 无参数默认进入 TUI
	if len(args) == 0 {
		launchTUI(os.Args[1:]) // empty slice
		return
	}

	switch args[0] {
	case "run":
		runCmd(args[1:])
	case "doctor":
		doctorCmd(args[1:])
	case "review":
		reviewCmd(args[1:])
	case "inspect":
		inspectCmd(args[1:])
	case "skill":
		skillCmd(args[1:])
	case "version":
		fmt.Printf("moss %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		// 不识别的命令也进入 TUI
		launchTUI(os.Args[1:])
	}
}

func stripLeadingDebugArgs(args []string) []string {
	out := append([]string(nil), args...)
	for len(out) > 0 {
		arg := out[0]
		if arg == "--debug" {
			out = out[1:]
			continue
		}
		if strings.HasPrefix(arg, "--debug=") {
			value := strings.TrimSpace(strings.TrimPrefix(arg, "--debug="))
			if _, err := strconv.ParseBool(value); err == nil {
				out = out[1:]
				continue
			}
		}
		break
	}
	return out
}

func printUsage() {
	fmt.Print(usageText())
}

func usageText() string {
	return `moss - Agent Runtime Kernel

Usage:
  moss                    Launch interactive TUI (default)
  moss run [flags]        Run with a specific goal
  moss skill <sub>        Manage skills (list|search|install|remove|info)
  moss doctor [flags]     Inspect runtime, paths, repo, and extension health
  moss review [args]      Inspect repository review, snapshot, and change state
  moss inspect [args]     Inspect state catalog events and the latest run
  moss version            Show version

	AppFlags:
  --debug       Enable trace logging to ~/.moss/debug.log
  --goal        Goal for the agent to accomplish
  --workspace   Workspace directory (default: ".")
  --trust       Trust level: trusted|restricted (default: restricted)
  --provider    LLM provider: claude|openai-completions|openai-responses|gemini (default from config or "openai-completions")
  --name        LLM provider display name, e.g. openai-completions|openai-responses|deepseek
  --model       Model name (default from config or API default)
  --base-url    LLM API base URL (override config)
  --api-key     LLM API key (override config)

Config:
  ~/.moss/config.yaml    Global configuration (provider, name, model, base_url, api_key, skills)
  ./moss.yaml            Project-level skill configuration

Environment:
  ANTHROPIC_API_KEY  Fallback when provider=claude and no api_key in config.
  OPENAI_API_KEY     Fallback when provider=openai-completions/openai-responses and no api_key in config.
  OPENAI_BASE_URL    Fallback when provider=openai-completions/openai-responses and no base_url in config.
  GEMINI_API_KEY     Fallback when provider=gemini and no api_key in config.
  GOOGLE_API_KEY     Alternate fallback when provider=gemini and no api_key in config.
`
}

// launchTUI 启动 Bubble Tea TUI 界面。
func launchTUI(args []string) {
	fs := flag.NewFlagSet("moss", flag.ExitOnError)
	_ = fs.Bool("debug", logging.DebugEnabled(), "Enable trace logging to ~/.moss/debug.log")
	f := &appkit.AppFlags{}
	appkit.BindAppFlags(fs, f)
	_ = fs.Parse(args)
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	f.ApplyDefaults()
	builder := appkit.NewRuntimeBuilder()
	resolution, err := builder.Resolve(f)
	if err != nil {
		logging.GetLogger().Error("error resolving profile", slog.Any("error", err))
		os.Exit(1)
	}

	if err := tui.Run(tui.Config{
		ProviderName:             f.DisplayProviderName(),
		Provider:                 f.Provider,
		Model:                    f.Model,
		Workspace:                f.Workspace,
		Trust:                    f.Trust,
		BaseURL:                  f.BaseURL,
		APIKey:                   f.APIKey,
		BuildKernel:              buildKernelWithIO,
		PromptConfigInstructions: resolution.ConfigInstructions,
		PromptModelInstructions:  resolution.ModelInstructions,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	goal := fs.String("goal", "", "Goal for the agent to accomplish")
	mode := fs.String("mode", "interactive", "Run mode: interactive|autopilot")
	_ = fs.Bool("debug", logging.DebugEnabled(), "Enable trace logging to ~/.moss/debug.log")
	f := &appkit.AppFlags{}
	appkit.BindAppFlags(fs, f)

	if err := fs.Parse(args); err != nil {
		logging.GetLogger().Error("error parsing flags", slog.Any("error", err))
		os.Exit(1)
	}
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	f.ApplyDefaults()
	builder := appkit.NewRuntimeBuilder()
	resolution, err := builder.Resolve(f)
	if err != nil {
		logging.GetLogger().Error("error resolving profile", slog.Any("error", err))
		os.Exit(1)
	}

	if *goal == "" {
		fmt.Fprintln(os.Stderr, "error: --goal is required for 'run' command")
		fmt.Fprintln(os.Stderr, "hint: run 'moss' without arguments to enter interactive TUI")
		fs.Usage()
		os.Exit(1)
	}

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	cliIO := &cliUserIO{writer: os.Stdout, reader: os.Stdin, workspace: f.Workspace, profile: resolution.Profile.Name}
	k, err := appkit.BuildKernel(ctx, f, cliIO)
	if err != nil {
		logging.GetLogger().Error("error initializing kernel", slog.Any("error", err))
		os.Exit(1)
	}
	if err := product.ApplyResolvedProfile(k, resolution.Profile); err != nil {
		logging.GetLogger().Error("error applying runtime profile", slog.Any("error", err))
		os.Exit(1)
	}

	if err := k.Boot(ctx); err != nil {
		logging.GetLogger().Error("error booting kernel", slog.Any("error", err))
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	fmt.Printf("🌿 moss %s\n", version)
	fmt.Printf("Goal: %s\n", *goal)
	fmt.Printf("Workspace: %s\n", f.Workspace)
	fmt.Printf("Mode: %s | Trust: %s\n", *mode, f.Trust)

	skills := runtime.SkillsManager(k).List()
	if len(skills) > 0 {
		fmt.Printf("Skills: ")
		for i, s := range skills {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(s.Name)
		}
		fmt.Println()
	}
	fmt.Println()

	meta := map[string]any{}
	meta["profile"] = resolution.Profile.Name
	meta[session.MetadataTaskMode] = resolution.Profile.TaskMode
	sysPrompt, err := buildRunSystemPrompt(f.Workspace, f.Trust, resolution.ConfigInstructions, resolution.ModelInstructions, meta, k)
	if err != nil {
		logging.GetLogger().Error("error composing system prompt", slog.Any("error", err))
		os.Exit(1)
	}

	sessCfg := runtime.ApplyResolvedProfileToSessionConfig(session.SessionConfig{
		Goal:         *goal,
		Mode:         *mode,
		TrustLevel:   f.Trust,
		Profile:      resolution.Profile.Name,
		MaxSteps:     50,
		SystemPrompt: strings.TrimSpace(sysPrompt),
		Metadata:     meta,
	}, resolution.Profile)
	sess, err := k.NewSession(ctx, sessCfg)
	if err != nil {
		logging.GetLogger().Error("error creating session", slog.Any("error", err))
		os.Exit(1)
	}
	sess.AppendMessage(mdl.Message{Role: mdl.RoleUser, ContentParts: []mdl.ContentPart{mdl.TextPart(*goal)}})

	result, err := k.Run(ctx, sess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Run failed: %v\n", err)
		logging.GetLogger().Error("run failed", slog.Any("error", err))
		os.Exit(1)
	}

	fmt.Printf("\n✅ Session completed (ID: %s)\n", result.SessionID)
	fmt.Printf("Steps: %d | Tokens: %d\n", result.Steps, result.TokensUsed.TotalTokens)
	if result.Output != "" {
		fmt.Printf("\nResult:\n%s\n", result.Output)
	}
}

func doctorCmd(args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Emit doctor output as JSON")
	f := &appkit.AppFlags{}
	appkit.BindAppFlags(fs, f)
	_ = fs.Parse(args)
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()
	report := product.BuildDoctorReport(ctx, "moss", f.Workspace, f, nil, "", product.GovernanceConfig{})
	if *jsonOut {
		if err := printJSON(report); err != nil {
			logging.GetLogger().Error("error rendering doctor json", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}
	fmt.Print(product.RenderDoctorReport(report))
}

func reviewCmd(args []string) {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Emit review output as JSON")
	f := &appkit.AppFlags{}
	appkit.BindAppFlags(fs, f)
	_ = fs.Parse(args)
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()
	report, err := product.BuildReviewReport(ctx, f.Workspace, fs.Args())
	if err != nil {
		logging.GetLogger().Error("review failed", slog.Any("error", err))
		os.Exit(1)
	}
	if *jsonOut {
		if err := printJSON(report); err != nil {
			logging.GetLogger().Error("error rendering review json", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}
	fmt.Print(product.RenderReviewReport(report))
}

func inspectCmd(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "Emit inspect output as JSON")
	f := &appkit.AppFlags{}
	appkit.BindAppFlags(fs, f)
	_ = fs.Parse(args)
	f.MergeGlobalConfig()
	f.MergeEnv("MOSS")
	f.ApplyDefaults()

	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()
	report, err := product.BuildInspectReportForTrust(ctx, f.Workspace, f.Trust, fs.Args())
	if err != nil {
		logging.GetLogger().Error("inspect failed", slog.Any("error", err))
		os.Exit(1)
	}
	if *jsonOut {
		if err := printJSON(report); err != nil {
			logging.GetLogger().Error("error rendering inspect json", slog.Any("error", err))
			os.Exit(1)
		}
		return
	}
	fmt.Print(product.RenderInspectReport(report))
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func buildRunSystemPrompt(workspace, trust, configInstructions, modelInstructions string, metadata map[string]any, k *kernel.Kernel) (string, error) {
	sessionInstructions, err := prompting.SessionInstructionsFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	profileName, taskMode, err := prompting.ProfileModeFromMetadata(metadata)
	if err != nil {
		return "", err
	}
	out, err := prompting.Compose(prompting.ComposeInput{
		Workspace:           workspace,
		Trust:               trust,
		ConfigInstructions:  strings.TrimSpace(configInstructions),
		SessionInstructions: sessionInstructions,
		ModelInstructions:   strings.TrimSpace(modelInstructions),
		ProfileName:         profileName,
		TaskMode:            taskMode,
		Kernel:              k,
	})
	if err != nil {
		return "", err
	}
	prompting.AttachComposeDebugMeta(metadata, out.DebugMeta)
	return out.Prompt, nil
}

// buildKernelWithIO 构建 Kernel 实例，供 TUI Config.BuildKernel 回调使用。
func buildKernelWithIO(wsDir, trust, approvalMode, profile, provider, model, apiKey, baseURL string, io intr.UserIO) (*kernel.Kernel, error) {
	ctx := context.Background()
	identity := config.NormalizeProviderIdentity("", provider, provider)
	resolved, err := runtime.ResolveProfileForWorkspace(runtime.ProfileResolveOptions{
		Workspace:        wsDir,
		RequestedProfile: profile,
		Trust:            trust,
		ApprovalMode:     approvalMode,
	})
	if err != nil {
		return nil, err
	}
	k, err := appkit.BuildKernel(ctx, &appkit.AppFlags{
		Provider:  identity.Provider,
		Name:      identity.Name,
		Model:     model,
		Workspace: wsDir,
		Trust:     resolved.Trust,
		Profile:   resolved.Name,
		APIKey:    apiKey,
		BaseURL:   baseURL,
	}, io)
	if err != nil {
		return nil, err
	}
	if err := product.ApplyResolvedProfile(k, resolved); err != nil {
		return nil, err
	}
	return k, nil
}
