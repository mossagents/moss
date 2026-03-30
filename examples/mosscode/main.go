// mosscode 是一个轻量但生产可用的代码助手示例。
//
// 目标：
//   - 保持单文件入口与低理解成本
//   - 使用增强后的 deep harness（session store / memories / offload / async task lifecycle）
//   - 同时支持交互式 TUI、one-shot CLI run 与产品化诊断入口
//
// 用法:
//
//	go run .                                    # 默认 TUI
//	go run . --goal "Fix flaky tests"           # legacy one-shot
//	go run . exec --goal "Fix flaky tests"      # one-shot
//	go run . resume --latest                    # 恢复最近可恢复会话
//	go run . doctor                             # 运行产品自检
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/product"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/presets/deepagent"
	mosstui "github.com/mossagents/moss/userio/tui"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

const appName = "mosscode"

type config struct {
	flags           *appkit.AppFlags
	command         string
	goal            string
	resumeSessionID string
	resumeLatest    bool
	configArgs      []string
	doctorJSON      bool
	reviewJSON      bool
	reviewArgs      []string
	explicitFlags   []string
}

func main() {
	appconfig.SetAppName(appName)
	_ = appconfig.EnsureAppDir()

	cfg := parseFlags()
	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	switch cfg.command {
	case "exec":
		if err := runOneShot(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	case "resume":
		if err := runResume(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	case "doctor":
		if err := runDoctor(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	case "config":
		if err := runConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	case "review":
		if err := runReview(ctx, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := launchTUI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *config {
	cfg := &config{flags: &appkit.AppFlags{}}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "exec":
			cfg.command = "exec"
			parseExecFlags(cfg, os.Args[2:])
			return cfg
		case "resume":
			cfg.command = "resume"
			parseResumeFlags(cfg, os.Args[2:])
			return cfg
		case "doctor":
			cfg.command = "doctor"
			parseDoctorFlags(cfg, os.Args[2:])
			return cfg
		case "config":
			cfg.command = "config"
			parseConfigFlags(cfg, os.Args[2:])
			return cfg
		case "review":
			cfg.command = "review"
			parseReviewFlags(cfg, os.Args[2:])
			return cfg
		case "help", "--help", "-h":
			printUsage()
			os.Exit(0)
		}
	}
	cfg.command = "tui"
	parseLegacyFlags(cfg, os.Args[1:])
	if strings.TrimSpace(cfg.goal) != "" {
		cfg.command = "exec"
	}
	return cfg
}

func parseExecFlags(cfg *config, args []string) {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	fs.StringVar(&cfg.goal, "goal", "", "Run one-shot mode with a single goal (omit to launch TUI)")
	parseCommonFlags(fs, cfg, args)
}

func parseResumeFlags(cfg *config, args []string) {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	fs.StringVar(&cfg.resumeSessionID, "session", "", "Resume a specific session by ID")
	fs.BoolVar(&cfg.resumeLatest, "latest", false, "Resume the latest recoverable session")
	parseCommonFlags(fs, cfg, args)
}

func parseDoctorFlags(cfg *config, args []string) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	fs.BoolVar(&cfg.doctorJSON, "json", false, "Emit doctor output as JSON")
	parseCommonFlags(fs, cfg, args)
}

func parseConfigFlags(cfg *config, args []string) {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	parseCommonFlags(fs, cfg, args)
	cfg.configArgs = fs.Args()
}

func parseReviewFlags(cfg *config, args []string) {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	fs.BoolVar(&cfg.reviewJSON, "json", false, "Emit review output as JSON")
	parseCommonFlags(fs, cfg, args)
	cfg.reviewArgs = fs.Args()
}

func parseLegacyFlags(cfg *config, args []string) {
	fs := flag.NewFlagSet(appName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	fs.StringVar(&cfg.goal, "goal", "", "Run one-shot mode with a single goal (omit to launch TUI)")
	parseCommonFlags(fs, cfg, args)
}

func parseCommonFlags(fs *flag.FlagSet, cfg *config, args []string) {
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printUsage()
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg.flags.MergeGlobalConfig()
	cfg.flags.MergeEnv("MOSSCODE", "MOSS")
	cfg.flags.ApplyDefaults()
	cfg.explicitFlags = collectExplicitFlagNames(fs)
}

func printUsage() {
	fmt.Print(`mosscode — lightweight production-ready coding assistant

Usage:
  mosscode [flags]
  mosscode exec --goal "Fix flaky tests" [flags]
  mosscode resume [--latest | --session <id>] [flags]
  mosscode doctor [--json] [flags]
  mosscode config [show|path|set|unset] [args] [flags]
  mosscode review [status|snapshots|snapshot <id>] [--json] [flags]

Flags:
  --goal        Legacy one-shot mode alias for 'exec --goal'
  --provider    LLM provider: claude|openai
  --model       Model name
  --workspace   Workspace directory (default: ".")
  --trust       Trust level: trusted|restricted
  --api-key     API key
  --base-url    API base URL

Resume:
  --latest      Resume the latest recoverable session
  --session     Resume a specific recoverable session by ID

Doctor:
  --json        Emit machine-readable diagnostic output

Config:
  show          Show persisted config and effective runtime values
  path          Print config file path
  set           Set provider/name/model/base_url in global config
  unset         Clear name/model/base_url in global config

Review:
  status        Show repo change summary (default)
  snapshots     List saved worktree snapshots
  snapshot      Show a specific snapshot by ID
  --json        Emit machine-readable review output
`)
}

func launchTUI(cfg *config) error {
	flags := cfg.flags
	return mosstui.Run(mosstui.Config{
		Provider:         flags.Provider,
		Model:            flags.Model,
		Workspace:        flags.Workspace,
		Trust:            flags.Trust,
		SessionStoreDir:  product.SessionStoreDir(),
		BaseURL:          flags.BaseURL,
		APIKey:           flags.APIKey,
		InitialSessionID: cfg.resumeSessionID,
		BuildKernel: func(wsDir, trust, provider, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
			runtimeFlags := &appkit.AppFlags{
				Provider:  provider,
				Model:     model,
				Workspace: wsDir,
				Trust:     trust,
				APIKey:    apiKey,
				BaseURL:   baseURL,
			}
			return buildKernel(context.Background(), runtimeFlags, io)
		},
		BuildSystemPrompt: buildSystemPrompt,
		BuildSessionConfig: func(workspace, trust, systemPrompt string) session.SessionConfig {
			return session.SessionConfig{
				Goal:         "interactive coding assistant",
				Mode:         "interactive",
				TrustLevel:   trust,
				SystemPrompt: systemPrompt,
				MaxSteps:     200,
			}
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
	fmt.Printf("Resuming session %s (status=%s steps=%d snapshots=%d)\n",
		selected.ID, selected.Status, selected.Steps, snapshotCounts[selected.ID])
	return launchTUI(cfg)
}

func runDoctor(ctx context.Context, cfg *config) error {
	report := product.BuildDoctorReport(ctx, appName, cfg.flags.Workspace, cfg.flags, cfg.explicitFlags)
	if cfg.doctorJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal doctor report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Print(product.RenderDoctorReport(report))
	return nil
}

func runConfig(cfg *config) error {
	args := cfg.configArgs
	if len(args) == 0 || args[0] == "show" {
		return showConfig(cfg.flags)
	}
	switch args[0] {
	case "path":
		cfgPath, err := product.ConfigPath()
		if err != nil {
			return err
		}
		fmt.Println(cfgPath)
		return nil
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("usage: mosscode config set <provider|name|model|base_url> <value>")
		}
		cfgPath, err := product.ConfigPath()
		if err != nil {
			return err
		}
		if _, err := product.SetConfig(args[1], strings.Join(args[2:], " "), false); err != nil {
			return err
		}
		fmt.Printf("Updated %s in %s\n", strings.ToLower(strings.TrimSpace(args[1])), cfgPath)
		return showConfig(effectiveFlags())
	case "unset":
		if len(args) != 2 {
			return fmt.Errorf("usage: mosscode config unset <name|model|base_url>")
		}
		cfgPath, err := product.ConfigPath()
		if err != nil {
			return err
		}
		if err := product.UnsetConfig(args[1], false); err != nil {
			return err
		}
		fmt.Printf("Cleared %s in %s\n", strings.ToLower(strings.TrimSpace(args[1])), cfgPath)
		return showConfig(effectiveFlags())
	default:
		return fmt.Errorf("unknown config command %q (supported: show, path, set, unset)", args[0])
	}
}

func runReview(ctx context.Context, cfg *config) error {
	report, err := product.BuildReviewReport(ctx, cfg.flags.Workspace, cfg.reviewArgs)
	if err != nil {
		return err
	}
	if cfg.reviewJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal review report: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Print(product.RenderReviewReport(report))
	return nil
}

func runOneShot(ctx context.Context, cfg *config) error {
	userIO := port.NewConsoleIO()
	k, err := buildKernel(ctx, cfg.flags, userIO)
	if err != nil {
		return err
	}
	if err := k.Boot(ctx); err != nil {
		return err
	}
	defer k.Shutdown(ctx)

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
			"Trust":     cfg.flags.Trust,
			"Tools":     fmt.Sprintf("%d loaded", len(k.ToolRegistry().List())),
			"Goal":      cfg.goal,
		},
		"Using deep harness defaults: persistent sessions/memories + context offload + async task lifecycle.",
	)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:         cfg.goal,
		Mode:         "oneshot",
		TrustLevel:   cfg.flags.Trust,
		SystemPrompt: buildSystemPrompt(cfg.flags.Workspace),
		MaxSteps:     80,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: cfg.goal})

	result, err := k.Run(ctx, sess)
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Done (session: %s, steps: %d, tokens: %d)\n", result.SessionID, result.Steps, result.TokensUsed.TotalTokens)
	if strings.TrimSpace(result.Output) != "" {
		fmt.Printf("\n%s\n", result.Output)
	}
	return nil
}

func buildKernel(ctx context.Context, flags *appkit.AppFlags, io port.UserIO) (*kernel.Kernel, error) {
	return deepagent.BuildKernel(ctx, flags, io, &deepagent.Config{
		AppName: appName,
	})
}

func buildSystemPrompt(workspace string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	return appconfig.RenderSystemPrompt(workspace, defaultSystemPromptTemplate, ctx)
}

func effectiveFlags() *appkit.AppFlags {
	f := &appkit.AppFlags{}
	f.MergeGlobalConfig()
	f.MergeEnv("MOSSCODE", "MOSS")
	f.ApplyDefaults()
	return f
}

func printResumeCandidates(summaries []session.SessionSummary, snapshotCounts map[string]int) {
	if len(summaries) == 0 {
		fmt.Println("No recoverable sessions found.")
		return
	}
	fmt.Println("Recoverable sessions:")
	for _, summary := range summaries {
		fmt.Printf("- %s | status=%s | steps=%d | snapshots=%d | goal=%s\n",
			summary.ID, summary.Status, summary.Steps, snapshotCounts[summary.ID], summary.Goal)
	}
	fmt.Println()
	fmt.Println("Use `mosscode resume --latest` or `mosscode resume --session <id>` to continue.")
}

func collectExplicitFlagNames(fs *flag.FlagSet) []string {
	names := []string{}
	fs.Visit(func(f *flag.Flag) {
		names = append(names, f.Name)
	})
	sort.Strings(names)
	return names
}

func showConfig(flags *appkit.AppFlags) error {
	out, err := product.ShowConfig(flags, false)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
