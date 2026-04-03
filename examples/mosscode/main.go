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
//	go run . --prompt "Fix flaky tests"         # one-shot
//	go run . exec --prompt "Fix flaky tests"    # one-shot
//	go run . resume --latest                    # 恢复最近可恢复会话
//	go run . doctor                             # 运行产品自检
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mossagents/moss/adapters"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/product"
	appruntime "github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/logging"
	"github.com/mossagents/moss/presets/deepagent"
	"github.com/mossagents/moss/sandbox"
	mosstui "github.com/mossagents/moss/userio/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

const appName = "mosscode"

type config struct {
	flags           *appkit.AppFlags
	command         string
	prompt          string
	approvalMode    string
	governance      product.GovernanceConfig
	execJSON        bool
	resumeSessionID string
	resumeLatest    bool
	configArgs      []string
	doctorJSON      bool
	debugConfigJSON bool
	reviewJSON      bool
	reviewArgs      []string
	forkArgs        []string
	completionArgs  []string
	checkpointArgs  []string
	applyArgs       []string
	rollbackArgs    []string
	changesArgs     []string
	explicitFlags   []string
	observer        port.Observer
	pricingCatalog  *product.PricingCatalog
}

type commandExitError struct {
	code int
}

func (e *commandExitError) Error() string {
	return ""
}

func main() {
	if err := appkit.InitializeApp(appName, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	_, _, debugCloser, err := logging.ConfigureDebugFileWhenEnabled(appconfig.AppDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: configure debug logging: %v\n", err)
		os.Exit(1)
	}
	if debugCloser != nil {
		defer debugCloser.Close()
	}

	cfg := parseFlags()
	auditObserver, auditCloser, err := product.OpenAuditObserver()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: initialize audit log: %v\n", err)
		os.Exit(1)
	}
	if auditCloser != nil {
		defer auditCloser.Close()
	}
	cfg.observer = auditObserver
	pricingCatalog, _, err := product.OpenPricingCatalog(cfg.flags.Workspace, cfg.governance.PricingCatalogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: load pricing catalog: %v\n", err)
		os.Exit(1)
	}
	cfg.pricingCatalog = pricingCatalog
	cfg.observer = product.NewPricingObserver(pricingCatalog, auditObserver)
	ctx, cancel := appkit.ContextWithSignal(context.Background())
	defer cancel()

	switch cfg.command {
	case "help":
		return
	case "exec":
		os.Exit(runExec(ctx, cfg))
	case "resume":
		exitOnCommandError(runResume(ctx, cfg))
		return
	case "fork":
		exitOnCommandError(runFork(ctx, cfg))
		return
	case "init":
		exitOnCommandError(runInit(cfg))
		return
	case "doctor":
		exitOnCommandError(runDoctor(ctx, cfg))
		return
	case "debug-config":
		exitOnCommandError(runDebugConfig(cfg))
		return
	case "completion":
		exitOnCommandError(runCompletion(cfg))
		return
	case "config":
		exitOnCommandError(runConfig(cfg))
		return
	case "review":
		exitOnCommandError(runReview(ctx, cfg))
		return
	case "checkpoint":
		exitOnCommandError(runCheckpoint(ctx, cfg))
		return
	case "apply":
		exitOnCommandError(runApply(ctx, cfg))
		return
	case "rollback":
		exitOnCommandError(runRollback(ctx, cfg))
		return
	case "changes":
		exitOnCommandError(runChanges(ctx, cfg))
		return
	}

	if err := launchTUI(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() *config {
	cfg := &config{
		flags:      &appkit.AppFlags{},
		command:    "help",
		governance: product.DefaultGovernanceConfig(),
	}
	root := buildRootCommand(cfg)
	if err := root.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func buildRootCommand(cfg *config) *cobra.Command {
	root := &cobra.Command{
		Use:           appName,
		Short:         "Lightweight production-ready coding assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpFlag := cmd.Flags().Lookup("help"); helpFlag != nil && helpFlag.Changed {
				cfg.command = "help"
				return nil
			}
			cfg.command = "tui"
			if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
				return err
			}
			if strings.TrimSpace(cfg.prompt) != "" {
				cfg.command = "exec"
			}
			return nil
		},
	}
	bindAppAndProductCobraFlags(root, cfg)
	root.Flags().StringVarP(&cfg.prompt, "prompt", "p", "", "Run one-shot mode with a single prompt (omit to launch TUI)")
	root.Flags().BoolVar(&cfg.execJSON, "json", false, "Emit one-shot execution output as JSON")
	root.SetHelpFunc(func(_ *cobra.Command, _ []string) {
		printUsage()
	})
	root.SetHelpCommand(&cobra.Command{
		Use:   "help",
		Short: "Show usage",
		Run: func(_ *cobra.Command, _ []string) {
			printUsage()
		},
	})

	execCmd := &cobra.Command{
		Use:   "exec",
		Short: "Run one-shot prompt mode",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.command = "exec"
			return finalizeCommonCobraFlags(cmd, cfg)
		},
	}
	bindAppAndProductCobraFlags(execCmd, cfg)
	execCmd.Flags().StringVarP(&cfg.prompt, "prompt", "p", "", "Run one-shot mode with a single prompt (omit to launch TUI)")
	execCmd.Flags().BoolVar(&cfg.execJSON, "json", false, "Emit one-shot execution output as JSON")

	resumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a recoverable thread",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.command = "resume"
			return finalizeCommonCobraFlags(cmd, cfg)
		},
	}
	bindAppAndProductCobraFlags(resumeCmd, cfg)
	resumeCmd.Flags().StringVar(&cfg.resumeSessionID, "session", "", "Resume a specific session by ID")
	resumeCmd.Flags().BoolVar(&cfg.resumeLatest, "latest", false, "Resume the latest recoverable session")

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize workspace bootstrap files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.command = "init"
			return finalizeCommonCobraFlags(cmd, cfg)
		},
	}
	bindAppAndProductCobraFlags(initCmd, cfg)
	initCmd.Flags().StringVarP(&cfg.prompt, "prompt", "p", "", "Run one-shot mode with a single prompt (omit to launch TUI)")
	initCmd.Flags().BoolVar(&cfg.execJSON, "json", false, "Emit one-shot execution output as JSON")

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.command = "doctor"
			return finalizeCommonCobraFlags(cmd, cfg)
		},
	}
	bindAppAndProductCobraFlags(doctorCmd, cfg)
	doctorCmd.Flags().BoolVar(&cfg.doctorJSON, "json", false, "Emit doctor output as JSON")

	debugConfigCmd := &cobra.Command{
		Use:   "debug-config",
		Short: "Show resolved runtime config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg.command = "debug-config"
			return finalizeCommonCobraFlags(cmd, cfg)
		},
	}
	bindAppAndProductCobraFlags(debugConfigCmd, cfg)
	debugConfigCmd.Flags().BoolVar(&cfg.debugConfigJSON, "json", false, "Emit debug-config output as JSON")

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage persisted config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.command = "config"
			if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
				return err
			}
			cfg.configArgs = append([]string(nil), args...)
			return nil
		},
	}
	bindAppAndProductCobraFlags(configCmd, cfg)

	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Inspect review state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.command = "review"
			if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
				return err
			}
			cfg.reviewArgs = append([]string(nil), args...)
			return nil
		},
	}
	bindAppAndProductCobraFlags(reviewCmd, cfg)
	reviewCmd.Flags().BoolVar(&cfg.reviewJSON, "json", false, "Emit review output as JSON")

	root.AddCommand(
		execCmd,
		resumeCmd,
		func() *cobra.Command {
			cmd := &cobra.Command{
				Use:   "fork",
				Short: "Fork from session or checkpoint",
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.command = "fork"
					if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
						return err
					}
					cfg.forkArgs = append([]string(nil), args...)
					return nil
				},
			}
			cmd.FParseErrWhitelist.UnknownFlags = true
			bindAppAndProductCobraFlags(cmd, cfg)
			return cmd
		}(),
		initCmd,
		doctorCmd,
		debugConfigCmd,
		&cobra.Command{
			Use:   "completion",
			Short: "Emit shell completion script",
			RunE: func(_ *cobra.Command, args []string) error {
				cfg.command = "completion"
				cfg.completionArgs = append([]string(nil), args...)
				return nil
			},
		},
		configCmd,
		reviewCmd,
		func() *cobra.Command {
			cmd := &cobra.Command{
				Use:   "checkpoint",
				Short: "Manage persisted checkpoints",
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.command = "checkpoint"
					if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
						return err
					}
					cfg.checkpointArgs = append([]string(nil), args...)
					return nil
				},
			}
			cmd.FParseErrWhitelist.UnknownFlags = true
			bindAppAndProductCobraFlags(cmd, cfg)
			return cmd
		}(),
		func() *cobra.Command {
			cmd := &cobra.Command{
				Use:   "apply",
				Short: "Apply explicit patch",
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.command = "apply"
					if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
						return err
					}
					cfg.applyArgs = append([]string(nil), args...)
					return nil
				},
			}
			cmd.FParseErrWhitelist.UnknownFlags = true
			bindAppAndProductCobraFlags(cmd, cfg)
			return cmd
		}(),
		func() *cobra.Command {
			cmd := &cobra.Command{
				Use:   "rollback",
				Short: "Roll back persisted change",
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.command = "rollback"
					if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
						return err
					}
					cfg.rollbackArgs = append([]string(nil), args...)
					return nil
				},
			}
			cmd.FParseErrWhitelist.UnknownFlags = true
			bindAppAndProductCobraFlags(cmd, cfg)
			return cmd
		}(),
		func() *cobra.Command {
			cmd := &cobra.Command{
				Use:   "changes",
				Short: "List or inspect persisted changes",
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.command = "changes"
					if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
						return err
					}
					cfg.changesArgs = append([]string(nil), args...)
					return nil
				},
			}
			cmd.FParseErrWhitelist.UnknownFlags = true
			bindAppAndProductCobraFlags(cmd, cfg)
			return cmd
		}(),
	)

	return root
}

func bindAppAndProductCobraFlags(cmd *cobra.Command, cfg *config) {
	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	cmd.Flags().AddGoFlagSet(fs)
}

func bindProductFlags(fs *flag.FlagSet, cfg *config) {
	fs.StringVar(&cfg.approvalMode, "approval", "", "Approval mode: read-only|confirm|full-auto")
	fs.StringVar(&cfg.governance.RouterConfigPath, "router-config", cfg.governance.RouterConfigPath, "Model router config path")
	fs.StringVar(&cfg.governance.PricingCatalogPath, "pricing-catalog", cfg.governance.PricingCatalogPath, "Pricing catalog YAML path")
	fs.IntVar(&cfg.governance.LLMRetries, "llm-retries", cfg.governance.LLMRetries, "LLM retry attempts (0 disables retries)")
	fs.DurationVar(&cfg.governance.LLMRetryInitial, "llm-retry-initial", cfg.governance.LLMRetryInitial, "Initial LLM retry backoff")
	fs.DurationVar(&cfg.governance.LLMRetryMaxDelay, "llm-retry-max-delay", cfg.governance.LLMRetryMaxDelay, "Maximum LLM retry backoff")
	fs.IntVar(&cfg.governance.LLMBreakerFailures, "llm-breaker-failures", cfg.governance.LLMBreakerFailures, "Consecutive LLM failures before breaker opens (0 disables)")
	fs.DurationVar(&cfg.governance.LLMBreakerReset, "llm-breaker-reset", cfg.governance.LLMBreakerReset, "How long the LLM breaker stays open before half-open retry")
	fs.BoolVar(&cfg.governance.LLMFailoverEnabled, "llm-failover", cfg.governance.LLMFailoverEnabled, "Enable router-based runtime LLM failover")
	fs.IntVar(&cfg.governance.LLMFailoverMaxCandidates, "llm-failover-max-candidates", cfg.governance.LLMFailoverMaxCandidates, "Maximum ordered router candidates to consider during failover")
	fs.IntVar(&cfg.governance.LLMFailoverPerCandidateRetries, "llm-failover-retries", cfg.governance.LLMFailoverPerCandidateRetries, "Retry attempts per candidate model before switching")
	fs.BoolVar(&cfg.governance.LLMFailoverOnBreakerOpen, "llm-failover-on-breaker-open", cfg.governance.LLMFailoverOnBreakerOpen, "Skip to the next candidate when a candidate breaker is open")
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
	if err := appkit.InitializeApp(appName, cfg.flags, "MOSSCODE", "MOSS"); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg.approvalMode = appkit.FirstNonEmpty(
		cfg.approvalMode,
		os.Getenv("MOSSCODE_APPROVAL_MODE"),
		os.Getenv("MOSS_APPROVAL_MODE"),
		product.ApprovalModeConfirm,
	)
	cfg.approvalMode = product.NormalizeApprovalMode(cfg.approvalMode)
	if err := product.ValidateApprovalMode(cfg.approvalMode); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	cfg.explicitFlags = collectExplicitFlagNames(fs)
	applyGovernanceEnv(&cfg.governance, cfg.explicitFlags)
}

func finalizeCommonCobraFlags(cmd *cobra.Command, cfg *config) error {
	if err := appkit.InitializeApp(appName, cfg.flags, "MOSSCODE", "MOSS"); err != nil {
		return err
	}
	cfg.approvalMode = appkit.FirstNonEmpty(
		cfg.approvalMode,
		os.Getenv("MOSSCODE_APPROVAL_MODE"),
		os.Getenv("MOSS_APPROVAL_MODE"),
		product.ApprovalModeConfirm,
	)
	cfg.approvalMode = product.NormalizeApprovalMode(cfg.approvalMode)
	if err := product.ValidateApprovalMode(cfg.approvalMode); err != nil {
		return err
	}
	cfg.explicitFlags = collectExplicitCobraFlagNames(cmd)
	applyGovernanceEnv(&cfg.governance, cfg.explicitFlags)
	return nil
}

func printUsage() {
	fmt.Print(`mosscode — lightweight production-ready coding assistant

Usage:
  mosscode [flags]
  mosscode exec --prompt "Fix flaky tests" [flags]
  mosscode resume [--latest | --session <id>] [flags]
  mosscode fork [--session <id> | --checkpoint <id|latest> | --latest] [flags]
  mosscode init [flags]
  mosscode doctor [--json] [flags]
  mosscode debug-config [--json] [flags]
  mosscode completion <powershell|bash|zsh>
  mosscode config [show|path|set|unset|mcp] [args] [flags]
  mosscode review [status|snapshots|snapshot <id>] [--json] [flags]
  mosscode checkpoint <list|show|create|replay> [flags]

Flags:
  --prompt, -p           One-shot prompt for 'exec' or legacy root invocation
  --provider    LLM provider: claude|openai|gemini
  --model       Model name
  --workspace   Workspace directory (default: ".")
  --profile     Profile: default|coding|research|planning|readonly
  --trust       Trust level: trusted|restricted
  --approval    Approval mode: read-only|confirm|full-auto (default: confirm)
  --router-config          Optional model router YAML path
  --pricing-catalog       Optional pricing catalog YAML path
  --llm-retries            LLM retry attempts; 0 disables retries
  --llm-retry-initial      Initial LLM retry backoff (default: 300ms)
  --llm-retry-max-delay    Maximum LLM retry backoff (default: 2s)
  --llm-breaker-failures   Consecutive LLM failures before breaker opens
  --llm-breaker-reset      Breaker reset window (default when enabled: 30s)
  --llm-failover           Enable router-based runtime failover
  --llm-failover-max-candidates  Max router candidates considered for failover (default: 2)
  --llm-failover-retries   Retry attempts per candidate before switching (default: 1)
  --llm-failover-on-breaker-open  Switch to next candidate when breaker is open (default: true)
  --api-key     API key
  --base-url    API base URL

Resume:
  --latest      Resume the latest recoverable session
  --session     Resume a specific recoverable session by ID

Fork:
  --session             Fork from a specific persisted session
  --checkpoint          Fork from a specific persisted checkpoint
  --latest              Fork from the latest persisted checkpoint
  --restore-worktree    Restore checkpoint worktree when possible
  --json                Emit machine-readable fork output

Doctor:
  --json        Emit machine-readable diagnostic output

Config:
  show          Show persisted config and effective runtime values
  path          Print config file path
  set           Set provider/name/model/base_url in global config
  unset         Clear name/model/base_url in global config
  mcp list                          List configured MCP servers across global/project config
  mcp show <name>                  Show MCP server details
  mcp enable <name> [global|project]   Enable an existing MCP entry
  mcp disable <name> [global|project]  Disable an existing MCP entry

Review:
  status        Show repo change summary (default)
  snapshots     List saved worktree snapshots
  snapshot      Show a specific snapshot by ID
  changes       List persisted change operations for the current repo
  change        Show a specific persisted change operation by ID
  --json        Emit machine-readable review output

Checkpoint:
  list [--json]                                             List persisted checkpoints
  show <id|latest> [--json]                                 Inspect a persisted checkpoint
  create --session <id> [--note <note>] [--json]            Create checkpoint from a persisted session
  replay [--checkpoint <id|latest> | --latest] [--mode resume|rerun] [--restore-worktree] [--json]
                                                                Prepare a fresh replay session from a checkpoint

Apply:
  --patch-file <path>   Apply an explicit patch file
  --summary <text>      Optional human-readable summary for the change
  --session <id>        Optional persisted session ID for best-effort checkpoint creation
  --json                Emit machine-readable apply output

Rollback:
  --change <id>         Roll back a specific persisted change by ID
  --json                Emit machine-readable rollback output

Changes:
  list [--limit N] [--json]   List persisted change operations for the current repo
  show <id> [--json]          Show a specific persisted change operation by ID

Exec:
  --json        Emit machine-readable execution output
`)
}

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
		BuildRunTraceObserver: func() (*product.RunTraceRecorder, port.Observer) {
			recorder := product.NewRunTraceRecorder()
			return recorder, product.NewPricingObserver(cfg.pricingCatalog, recorder)
		},
		BuildKernel: func(wsDir, trust, approvalMode, profile, apiType, model, apiKey, baseURL string, io port.UserIO) (*kernel.Kernel, error) {
			runtimeFlags := &appkit.AppFlags{
				Provider:  apiType,
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
					Name:            profile,
					TaskMode:        profile,
					Trust:           trust,
					ApprovalMode:    approvalMode,
					ExecutionPolicy: appruntime.ResolveExecutionPolicyForWorkspace(workspace, trust, approvalMode),
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

func runFork(ctx context.Context, cfg *config) error {
	return runCheckpointFork(ctx, cfg, cfg.forkArgs)
}

func runInit(cfg *config) error {
	out, err := product.InitWorkspaceBootstrap(cfg.flags.Workspace, appName)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

func runDoctor(ctx context.Context, cfg *config) error {
	report := product.BuildDoctorReport(ctx, appName, cfg.flags.Workspace, cfg.flags, cfg.explicitFlags, cfg.approvalMode, cfg.governance)
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

func runDebugConfig(cfg *config) error {
	resolved, err := resolveProfileForConfig(cfg)
	if err != nil {
		return err
	}
	report := product.BuildDebugConfigReport(
		appName,
		cfg.flags.Workspace,
		cfg.flags.DisplayProviderName(),
		cfg.flags.Model,
		resolved.Trust,
		resolved.ApprovalMode,
		resolved.Name,
		currentThemeName(),
		"",
		"",
		"",
	)
	if cfg.debugConfigJSON {
		return printJSON(report)
	}
	fmt.Println(product.RenderDebugConfigReport(report))
	return nil
}

func runCompletion(cfg *config) error {
	if len(cfg.completionArgs) != 1 {
		return fmt.Errorf("usage: mosscode completion <powershell|bash|zsh>")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.completionArgs[0])) {
	case "powershell":
		fmt.Print(renderPowerShellCompletion())
		return nil
	case "bash":
		fmt.Print(renderBashCompletion())
		return nil
	case "zsh":
		fmt.Print(renderZshCompletion())
		return nil
	default:
		return fmt.Errorf("unsupported shell %q (supported: powershell, bash, zsh)", cfg.completionArgs[0])
	}
}

func currentThemeName() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MOSSCODE_THEME"))) {
	case "plain":
		return "plain"
	default:
		return "default"
	}
}

func renderPowerShellCompletion() string {
	return `Register-ArgumentCompleter -CommandName mosscode -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $commands = @('exec','resume','fork','init','doctor','debug-config','completion','config','review','checkpoint','apply','rollback','changes')
    $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
        [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
    }
}
`
}

func renderBashCompletion() string {
	return `_mosscode_completions() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local commands="exec resume fork init doctor debug-config completion config review checkpoint apply rollback changes"
    COMPREPLY=( $(compgen -W "${commands}" -- "${cur}") )
}
complete -F _mosscode_completions mosscode
`
}

func renderZshCompletion() string {
	return `#compdef mosscode
local -a commands
commands=(
  'exec:run one-shot prompt'
  'resume:resume saved conversation'
  'fork:fork from session or checkpoint'
  'init:scaffold AGENTS.md and commands'
  'doctor:run diagnostics'
  'debug-config:show config and path diagnostics'
  'completion:emit shell completion script'
  'config:manage persisted config'
  'review:inspect working tree review state'
  'checkpoint:manage checkpoints'
  'apply:apply explicit patch'
  'rollback:rollback a change'
  'changes:list or inspect persisted changes'
)
_describe 'command' commands
`
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
	case "mcp":
		return runConfigMCP(cfg.flags, args[1:])
	default:
		return fmt.Errorf("unknown config command %q (supported: show, path, set, unset, mcp)", args[0])
	}
}

func runConfigMCP(flags *appkit.AppFlags, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		servers, err := product.ListMCPServers(flags.Workspace, flags.Trust)
		if err != nil {
			return err
		}
		fmt.Print(product.RenderMCPServerList(servers))
		return nil
	}
	switch args[0] {
	case "show":
		if len(args) != 2 {
			return fmt.Errorf("usage: mosscode config mcp show <name>")
		}
		servers, err := product.GetMCPServer(flags.Workspace, flags.Trust, args[1])
		if err != nil {
			return err
		}
		fmt.Print(product.RenderMCPServerDetail(servers))
		return nil
	case "enable", "disable":
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("usage: mosscode config mcp %s <name> [global|project]", args[0])
		}
		enabled := args[0] == "enable"
		scope := ""
		if len(args) == 3 {
			scope = args[2]
		}
		server, err := product.SetMCPEnabled(flags.Workspace, args[1], scope, enabled)
		if err != nil {
			return err
		}
		fmt.Printf("Updated MCP %s [%s]: enabled=%t effective=%t status=%s\n", server.Name, server.Source, server.Enabled, server.Effective, server.Status)
		return nil
	default:
		return fmt.Errorf("unknown mcp config command %q (supported: list, show, enable, disable)", args[0])
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

type checkpointActionReport struct {
	Mode             string                      `json:"mode"`
	Checkpoints      []product.CheckpointSummary `json:"checkpoints,omitempty"`
	Checkpoint       *product.CheckpointSummary  `json:"checkpoint,omitempty"`
	CheckpointDetail *product.CheckpointDetail   `json:"checkpoint_detail,omitempty"`
	SessionID        string                      `json:"session_id,omitempty"`
	SourceKind       string                      `json:"source_kind,omitempty"`
	SourceID         string                      `json:"source_id,omitempty"`
	ReplayMode       string                      `json:"replay_mode,omitempty"`
	RestoredWorktree bool                        `json:"restored_worktree,omitempty"`
	Degraded         bool                        `json:"degraded,omitempty"`
	Details          string                      `json:"details,omitempty"`
	Note             string                      `json:"note,omitempty"`
}

type changeActionReport struct {
	Mode    string                   `json:"mode"`
	Change  *product.ChangeOperation `json:"change,omitempty"`
	Changes []product.ChangeSummary  `json:"changes,omitempty"`
	Details string                   `json:"details,omitempty"`
}

func runCheckpoint(ctx context.Context, cfg *config) error {
	if len(cfg.checkpointArgs) == 0 {
		return fmt.Errorf("usage: mosscode checkpoint <list|show|create|replay> [flags]")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.checkpointArgs[0])) {
	case "list":
		return runCheckpointList(ctx, cfg, cfg.checkpointArgs[1:])
	case "show":
		return runCheckpointShow(ctx, cfg, cfg.checkpointArgs[1:])
	case "create":
		return runCheckpointCreate(ctx, cfg, cfg.checkpointArgs[1:])
	case "fork":
		return fmt.Errorf("checkpoint branching moved to `mosscode fork`; use `mosscode fork [--session <id> | --checkpoint <id|latest> | --latest]`")
	case "replay":
		return runCheckpointReplay(ctx, cfg, cfg.checkpointArgs[1:])
	default:
		return fmt.Errorf("unknown checkpoint command %q (supported: list, show, create, replay)", cfg.checkpointArgs[0])
	}
}

func runCheckpointList(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("checkpoint list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	jsonOut := false
	limit := 20
	fs.BoolVar(&jsonOut, "json", false, "Emit checkpoint list as JSON")
	fs.IntVar(&limit, "limit", limit, "Maximum checkpoints to list")
	parseCommonFlags(fs, cfg, args)
	items, err := product.ListCheckpoints(ctx, limit)
	if err != nil {
		return err
	}
	report := checkpointActionReport{
		Mode:        "list",
		Checkpoints: items,
	}
	if jsonOut {
		return printJSON(report)
	}
	fmt.Println(product.RenderCheckpointSummaries(items))
	return nil
}

func runCheckpointShow(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("checkpoint show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	jsonOut := false
	fs.BoolVar(&jsonOut, "json", false, "Emit checkpoint detail as JSON")
	checkpointID := ""
	parseArgs := args
	if len(args) > 0 {
		first := strings.TrimSpace(args[0])
		if first != "" && !strings.HasPrefix(first, "-") {
			checkpointID = first
			parseArgs = args[1:]
		}
	}
	parseCommonFlags(fs, cfg, parseArgs)
	if checkpointID == "" && fs.NArg() == 1 {
		checkpointID = strings.TrimSpace(fs.Arg(0))
	}
	if checkpointID == "" || fs.NArg() > 1 {
		return fmt.Errorf("usage: mosscode checkpoint show <id|latest> [--json]")
	}
	detail, err := product.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return err
	}
	report := checkpointActionReport{
		Mode:             "show",
		CheckpointDetail: detail,
	}
	if jsonOut {
		return printJSON(report)
	}
	fmt.Println(product.RenderCheckpointDetail(detail))
	return nil
}

func runCheckpointCreate(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("checkpoint create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	sessionID := ""
	note := ""
	jsonOut := false
	fs.StringVar(&sessionID, "session", "", "Persisted session ID to checkpoint")
	fs.StringVar(&note, "note", "", "Optional checkpoint note")
	fs.BoolVar(&jsonOut, "json", false, "Emit checkpoint create output as JSON")
	parseCommonFlags(fs, cfg, args)
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("usage: mosscode checkpoint create --session <id> [--note <note>] [--json]")
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
		return fmt.Errorf("session store is unavailable")
	}
	sess, err := k.SessionStore().Load(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return fmt.Errorf("session %q not found", sessionID)
	}
	record, err := k.CreateCheckpoint(ctx, sess, port.CheckpointCreateRequest{Note: strings.TrimSpace(note)})
	if err != nil {
		return err
	}
	summary := product.SummarizeCheckpoint(*record)
	report := checkpointActionReport{
		Mode:       "create",
		Checkpoint: &summary,
		Note:       note,
	}
	if jsonOut {
		return printJSON(report)
	}
	fmt.Printf("Created checkpoint %s for session %s.\n", summary.ID, summary.SessionID)
	if summary.SnapshotID != "" {
		fmt.Printf("Snapshot: %s\n", summary.SnapshotID)
	}
	fmt.Printf("Patches: %d | Lineage: %d\n", summary.PatchCount, summary.LineageDepth)
	if strings.TrimSpace(summary.Note) != "" {
		fmt.Printf("Note: %s\n", summary.Note)
	}
	return nil
}

func runCheckpointFork(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("fork", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	sessionID := ""
	checkpointID := ""
	latest := false
	restoreWorktree := false
	jsonOut := false
	fs.StringVar(&sessionID, "session", "", "Fork from this session ID (prefers latest checkpoint for that session)")
	fs.StringVar(&checkpointID, "checkpoint", "", "Fork directly from this checkpoint ID")
	fs.BoolVar(&latest, "latest", false, "Fork from the latest persisted checkpoint")
	fs.BoolVar(&restoreWorktree, "restore-worktree", false, "Attempt worktree restore when forking from checkpoint state")
	fs.BoolVar(&jsonOut, "json", false, "Emit fork output as JSON")
	parseCommonFlags(fs, cfg, args)
	sessionID = strings.TrimSpace(sessionID)
	checkpointID = strings.TrimSpace(checkpointID)
	sourceKind := port.ForkSourceSession
	sourceID := sessionID
	if latest {
		if sessionID != "" {
			return fmt.Errorf("use either --session or --latest, not both")
		}
		if checkpointID != "" && !strings.EqualFold(checkpointID, "latest") {
			return fmt.Errorf("use either --checkpoint <id> or --latest, not both")
		}
		sourceKind = port.ForkSourceCheckpoint
		sourceID = ""
	} else if checkpointID != "" {
		if sourceID != "" {
			return fmt.Errorf("use either --session or --checkpoint, not both")
		}
		sourceKind = port.ForkSourceCheckpoint
		sourceID = checkpointID
	}
	if sourceKind == port.ForkSourceSession && sourceID == "" {
		return fmt.Errorf("usage: mosscode fork [--session <id> | --checkpoint <id|latest> | --latest] [--restore-worktree] [--json]")
	}
	k, err := buildCheckpointKernel(ctx, cfg)
	if err != nil {
		return err
	}
	defer k.Shutdown(ctx)
	if err := k.Boot(ctx); err != nil {
		return err
	}
	if sourceKind == port.ForkSourceCheckpoint {
		record, err := product.ResolveCheckpointRecord(ctx, k.Checkpoints(), sourceID)
		if err != nil {
			return err
		}
		sourceID = record.ID
	}
	sess, result, err := k.ForkSession(ctx, port.ForkRequest{
		SourceKind:      sourceKind,
		SourceID:        sourceID,
		RestoreWorktree: restoreWorktree,
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
	if jsonOut {
		return printJSON(report)
	}
	fmt.Printf("Prepared forked thread %s from %s %s.\n", sess.ID, result.SourceKind, result.SourceID)
	if result.RestoredWorktree {
		fmt.Println("Worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Printf("Degraded: %s\n", result.Details)
	}
	fmt.Printf("Use `mosscode resume --session %s` to continue.\n", sess.ID)
	return nil
}

func runCheckpointReplay(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("checkpoint replay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	checkpointID := ""
	latest := false
	mode := string(port.ReplayModeResume)
	restoreWorktree := false
	jsonOut := false
	fs.StringVar(&checkpointID, "checkpoint", "", "Checkpoint ID to replay")
	fs.BoolVar(&latest, "latest", false, "Replay the latest persisted checkpoint")
	fs.StringVar(&mode, "mode", mode, "Replay mode: resume|rerun")
	fs.BoolVar(&restoreWorktree, "restore-worktree", false, "Attempt worktree restore before replay")
	fs.BoolVar(&jsonOut, "json", false, "Emit checkpoint replay output as JSON")
	parseCommonFlags(fs, cfg, args)
	checkpointID = strings.TrimSpace(checkpointID)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if latest {
		if checkpointID != "" && !strings.EqualFold(checkpointID, "latest") {
			return fmt.Errorf("use either --checkpoint <id> or --latest, not both")
		}
		checkpointID = ""
	}
	if checkpointID == "" && !latest {
		return fmt.Errorf("usage: mosscode checkpoint replay [--checkpoint <id|latest> | --latest] [--mode resume|rerun] [--restore-worktree] [--json]")
	}
	if mode != string(port.ReplayModeResume) && mode != string(port.ReplayModeRerun) {
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
	record, err := product.ResolveCheckpointRecord(ctx, k.Checkpoints(), checkpointID)
	if err != nil {
		return err
	}
	sess, result, err := k.ReplayFromCheckpoint(ctx, port.ReplayRequest{
		CheckpointID:    record.ID,
		Mode:            port.ReplayMode(mode),
		RestoreWorktree: restoreWorktree,
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
	if jsonOut {
		return printJSON(report)
	}
	fmt.Printf("Prepared replay session %s from checkpoint %s (%s).\n", sess.ID, result.CheckpointID, result.Mode)
	if result.RestoredWorktree {
		fmt.Println("Worktree restored.")
	}
	if result.Degraded && strings.TrimSpace(result.Details) != "" {
		fmt.Printf("Degraded: %s\n", result.Details)
	}
	fmt.Printf("Use `mosscode resume --session %s` to continue.\n", sess.ID)
	return nil
}

func buildCheckpointKernel(ctx context.Context, cfg *config) (*kernel.Kernel, error) {
	k, _, err := buildKernel(ctx, cfg.flags, &port.NoOpIO{}, cfg.approvalMode, cfg.governance, cfg.observer)
	return k, err
}

func buildChangeRuntime(ctx context.Context, cfg *config, sessionID string) (product.ChangeRuntime, func(), error) {
	rt := product.ChangeRuntime{
		Workspace:        cfg.flags.Workspace,
		RepoStateCapture: sandbox.NewGitRepoStateCapture(cfg.flags.Workspace),
		PatchApply:       sandbox.NewGitPatchApply(cfg.flags.Workspace),
		PatchRevert:      sandbox.NewGitPatchRevert(cfg.flags.Workspace),
	}
	if strings.TrimSpace(sessionID) == "" {
		return rt, func() {}, nil
	}
	k, err := buildCheckpointKernel(ctx, cfg)
	if err != nil {
		return product.ChangeRuntime{}, nil, err
	}
	if err := k.Boot(ctx); err != nil {
		return product.ChangeRuntime{}, nil, err
	}
	return product.ChangeRuntimeFromKernel(cfg.flags.Workspace, k), func() {
		_ = k.Shutdown(ctx)
	}, nil
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func exitOnCommandError(err error) {
	if err == nil {
		return
	}
	var exitErr *commandExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.code)
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
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

func runApply(ctx context.Context, cfg *config) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	patchFile := ""
	summary := ""
	sessionID := ""
	jsonOut := false
	fs.StringVar(&patchFile, "patch-file", "", "Apply an explicit patch file")
	fs.StringVar(&summary, "summary", "", "Optional human-readable summary for the change")
	fs.StringVar(&sessionID, "session", "", "Optional persisted session ID for checkpoint creation")
	fs.BoolVar(&jsonOut, "json", false, "Emit apply output as JSON")
	parseCommonFlags(fs, cfg, cfg.applyArgs)
	if strings.TrimSpace(patchFile) == "" {
		return fmt.Errorf("usage: mosscode apply --patch-file <path> [--summary <text>] [--session <id>] [--json]")
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
	item, err := product.ApplyChange(ctx, rt, product.ApplyChangeRequest{
		Patch:     string(data),
		Summary:   strings.TrimSpace(summary),
		SessionID: strings.TrimSpace(sessionID),
		Source:    port.PatchSourceUser,
	})
	report := changeActionReport{
		Mode:   "apply",
		Change: item,
	}
	if err != nil {
		if opErr := (*product.ChangeOperationError)(nil); errors.As(err, &opErr) {
			report.Change = opErr.Operation
			report.Details = opErr.Error()
			return emitChangeReport(report, jsonOut, true)
		}
		return err
	}
	return emitChangeReport(report, jsonOut, false)
}

func runRollback(ctx context.Context, cfg *config) error {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	changeID := ""
	jsonOut := false
	fs.StringVar(&changeID, "change", "", "Roll back a specific persisted change by ID")
	fs.BoolVar(&jsonOut, "json", false, "Emit rollback output as JSON")
	parseCommonFlags(fs, cfg, cfg.rollbackArgs)
	if strings.TrimSpace(changeID) == "" {
		return fmt.Errorf("usage: mosscode rollback --change <id> [--json]")
	}
	rt, cleanup, err := buildChangeRuntime(ctx, cfg, "")
	if err != nil {
		return err
	}
	defer cleanup()
	item, err := product.RollbackChange(ctx, rt, product.RollbackChangeRequest{ChangeID: strings.TrimSpace(changeID)})
	report := changeActionReport{
		Mode:   "rollback",
		Change: item,
	}
	if err != nil {
		if opErr := (*product.ChangeOperationError)(nil); errors.As(err, &opErr) {
			report.Change = opErr.Operation
			report.Details = opErr.Error()
			return emitChangeReport(report, jsonOut, true)
		}
		return err
	}
	return emitChangeReport(report, jsonOut, false)
}

func runChanges(ctx context.Context, cfg *config) error {
	if len(cfg.changesArgs) == 0 {
		return fmt.Errorf("usage: mosscode changes <list|show> [flags]")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.changesArgs[0])) {
	case "list":
		return runChangesList(ctx, cfg, cfg.changesArgs[1:])
	case "show":
		return runChangesShow(ctx, cfg, cfg.changesArgs[1:])
	default:
		return fmt.Errorf("unknown changes command %q (supported: list, show)", cfg.changesArgs[0])
	}
}

func runChangesList(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("changes list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	jsonOut := false
	limit := 20
	fs.BoolVar(&jsonOut, "json", false, "Emit changes list as JSON")
	fs.IntVar(&limit, "limit", limit, "Maximum change operations to list")
	parseCommonFlags(fs, cfg, args)
	items, err := product.ListChangeOperations(ctx, cfg.flags.Workspace, limit)
	if err != nil {
		return err
	}
	report := changeActionReport{
		Mode:    "list",
		Changes: items,
	}
	return emitChangeReport(report, jsonOut, false)
}

func runChangesShow(ctx context.Context, cfg *config, args []string) error {
	fs := flag.NewFlagSet("changes show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	appkit.BindAppFlags(fs, cfg.flags)
	bindProductFlags(fs, cfg)
	jsonOut := false
	fs.BoolVar(&jsonOut, "json", false, "Emit change detail as JSON")
	parseCommonFlags(fs, cfg, args)
	if len(fs.Args()) != 1 || strings.TrimSpace(fs.Args()[0]) == "" {
		return fmt.Errorf("usage: mosscode changes show <id> [--json]")
	}
	item, err := product.LoadChangeOperation(ctx, cfg.flags.Workspace, strings.TrimSpace(fs.Args()[0]))
	if err != nil {
		return err
	}
	report := changeActionReport{
		Mode:   "show",
		Change: item,
	}
	return emitChangeReport(report, jsonOut, false)
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
		fmt.Println(product.RenderChangeSummaries(report.Changes))
	case "show", "apply", "rollback":
		fmt.Println(product.RenderChangeDetail(report.Change))
	}
	if strings.TrimSpace(report.Details) != "" {
		fmt.Printf("Details: %s\n", report.Details)
	}
	if fail {
		return &commandExitError{code: 1}
	}
	return nil
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
	var userIO port.UserIO
	if cfg.execJSON {
		recorder = product.NewRecordingIO(cfg.approvalMode)
		userIO = recorder
	} else {
		userIO = port.NewConsoleIO()
	}
	traceObserver := product.NewPricingObserver(cfg.pricingCatalog, traceRecorder)
	k, resolved, err := buildKernel(ctx, cfg.flags, userIO, cfg.approvalMode, cfg.governance, port.JoinObservers(cfg.observer, traceObserver))
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
			"Using deep harness defaults: persistent sessions/memories + context offload + async task lifecycle.",
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
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: cfg.prompt})

	result, err := k.Run(ctx, sess)
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
	report.SessionID = result.SessionID
	report.Steps = result.Steps
	if report.Tokens == 0 {
		report.Tokens = result.TokensUsed.TotalTokens
	}
	report.Output = result.Output

	if !cfg.execJSON {
		fmt.Println()
		fmt.Printf("✅ Done (session: %s, steps: %d, tokens: %d", result.SessionID, result.Steps, report.Tokens)
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

func buildKernel(ctx context.Context, flags *appkit.AppFlags, io port.UserIO, approvalMode string, governance product.GovernanceConfig, observer port.Observer) (*kernel.Kernel, appruntime.ResolvedProfile, error) {
	resolved, err := resolveProfileForFlags(flags, approvalMode)
	if err != nil {
		return nil, appruntime.ResolvedProfile{}, err
	}
	flags.Trust = resolved.Trust
	flags.Profile = resolved.Name
	disableDefaultPolicy := false
	router, _, err := product.OpenModelRouter(flags.Workspace, governance.RouterConfigPath)
	if err != nil {
		return nil, appruntime.ResolvedProfile{}, fmt.Errorf("load model router: %w", err)
	}
	failoverCfg, failoverEnabled := governance.FailoverConfig()
	useFailover := failoverEnabled && router != nil
	retryCfg, retryEnabled := governance.RetryConfig()
	breakerCfg := governance.BreakerConfig()
	if useFailover {
		disabled := false
		retryEnabled = &disabled
		retryCfg = nil
		breakerCfg = nil
	}
	k, err := deepagent.BuildKernel(ctx, flags, io, &deepagent.Config{
		AppName:                       appName,
		EnableDefaultRestrictedPolicy: &disableDefaultPolicy,
		EnableDefaultLLMRetry:         retryEnabled,
		LLMRetryConfig:                retryCfg,
		LLMBreakerConfig:              breakerCfg,
		AdditionalAppExtensions:       []appkit.Extension{},
	})
	if err != nil {
		return nil, appruntime.ResolvedProfile{}, err
	}
	if router != nil {
		var llm port.LLM = router
		if useFailover {
			failoverLLM, err := adapters.NewFailoverLLM(router, failoverCfg)
			if err != nil {
				return nil, appruntime.ResolvedProfile{}, fmt.Errorf("build failover llm: %w", err)
			}
			llm = failoverLLM
		}
		k.SetLLM(llm)
	}
	k.SetObserver(product.ComposeStateObserver(k, observer))
	if err := product.ApplyResolvedProfile(k, resolved); err != nil {
		return nil, appruntime.ResolvedProfile{}, err
	}
	return k, resolved, nil
}

func resolveProfileForFlags(flags *appkit.AppFlags, approvalMode string) (appruntime.ResolvedProfile, error) {
	return appruntime.ResolveProfileForWorkspace(appruntime.ProfileResolveOptions{
		Workspace:        flags.Workspace,
		RequestedProfile: flags.Profile,
		Trust:            flags.Trust,
		ApprovalMode:     approvalMode,
	})
}

func resolveProfileForConfig(cfg *config) (appruntime.ResolvedProfile, error) {
	trust := ""
	if hasExplicitFlag(cfg.explicitFlags, "trust") || envConfigured("MOSSCODE_TRUST", "MOSS_TRUST") {
		trust = cfg.flags.Trust
	}
	approval := ""
	if hasExplicitFlag(cfg.explicitFlags, "approval") || envConfigured("MOSSCODE_APPROVAL_MODE", "MOSS_APPROVAL_MODE") {
		approval = cfg.approvalMode
	}
	return appruntime.ResolveProfileForWorkspace(appruntime.ProfileResolveOptions{
		Workspace:        cfg.flags.Workspace,
		RequestedProfile: cfg.flags.Profile,
		Trust:            trust,
		ApprovalMode:     approval,
	})
}

func hasExplicitFlag(explicit []string, name string) bool {
	for _, item := range explicit {
		if item == name {
			return true
		}
	}
	return false
}

func envConfigured(keys ...string) bool {
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

func buildSystemPrompt(workspace, trust string) string {
	ctx := appconfig.DefaultTemplateContext(workspace)
	if prefs, err := product.LoadTUIConfig(); err == nil {
		ctx["Personality"] = product.NormalizePersonality(prefs.Personality)
		ctx["FastMode"] = prefs.FastMode != nil && *prefs.FastMode
	}
	return appconfig.RenderSystemPromptForTrust(workspace, trust, defaultSystemPromptTemplate, ctx)
}

func effectiveFlags() *appkit.AppFlags {
	f := &appkit.AppFlags{}
	_ = appkit.InitializeApp(appName, f, "MOSSCODE", "MOSS")
	return f
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
	fmt.Println("Use `mosscode resume --latest` or `mosscode resume --session <id>` to continue a thread.")
}

func collectExplicitFlagNames(fs *flag.FlagSet) []string {
	names := []string{}
	fs.Visit(func(f *flag.Flag) {
		names = append(names, f.Name)
	})
	sort.Strings(names)
	return names
}

func collectExplicitCobraFlagNames(cmd *cobra.Command) []string {
	seen := map[string]struct{}{}
	names := []string{}
	collect := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.Visit(func(f *pflag.Flag) {
			if _, ok := seen[f.Name]; ok {
				return
			}
			seen[f.Name] = struct{}{}
			names = append(names, f.Name)
		})
	}
	collect(cmd.Flags())
	collect(cmd.InheritedFlags())
	sort.Strings(names)
	return names
}

func applyGovernanceEnv(cfg *product.GovernanceConfig, explicitFlags []string) {
	explicit := make(map[string]struct{}, len(explicitFlags))
	for _, name := range explicitFlags {
		explicit[name] = struct{}{}
	}
	if _, ok := explicit["router-config"]; !ok {
		cfg.RouterConfigPath = firstEnv(cfg.RouterConfigPath, "MOSSCODE_ROUTER_CONFIG", "MOSS_ROUTER_CONFIG")
	}
	if _, ok := explicit["llm-retries"]; !ok {
		cfg.LLMRetries = firstEnvInt(cfg.LLMRetries, "MOSSCODE_LLM_RETRIES", "MOSS_LLM_RETRIES")
	}
	if _, ok := explicit["llm-retry-initial"]; !ok {
		cfg.LLMRetryInitial = firstEnvDuration(cfg.LLMRetryInitial, "MOSSCODE_LLM_RETRY_INITIAL", "MOSS_LLM_RETRY_INITIAL")
	}
	if _, ok := explicit["llm-retry-max-delay"]; !ok {
		cfg.LLMRetryMaxDelay = firstEnvDuration(cfg.LLMRetryMaxDelay, "MOSSCODE_LLM_RETRY_MAX_DELAY", "MOSS_LLM_RETRY_MAX_DELAY")
	}
	if _, ok := explicit["llm-breaker-failures"]; !ok {
		cfg.LLMBreakerFailures = firstEnvInt(cfg.LLMBreakerFailures, "MOSSCODE_LLM_BREAKER_FAILURES", "MOSS_LLM_BREAKER_FAILURES")
	}
	if _, ok := explicit["llm-breaker-reset"]; !ok {
		cfg.LLMBreakerReset = firstEnvDuration(cfg.LLMBreakerReset, "MOSSCODE_LLM_BREAKER_RESET", "MOSS_LLM_BREAKER_RESET")
	}
	if _, ok := explicit["llm-failover"]; !ok {
		cfg.LLMFailoverEnabled = firstEnvBool(cfg.LLMFailoverEnabled, "MOSSCODE_LLM_FAILOVER", "MOSS_LLM_FAILOVER")
	}
	if _, ok := explicit["llm-failover-max-candidates"]; !ok {
		cfg.LLMFailoverMaxCandidates = firstEnvInt(cfg.LLMFailoverMaxCandidates, "MOSSCODE_LLM_FAILOVER_MAX_CANDIDATES", "MOSS_LLM_FAILOVER_MAX_CANDIDATES")
	}
	if _, ok := explicit["llm-failover-retries"]; !ok {
		cfg.LLMFailoverPerCandidateRetries = firstEnvInt(cfg.LLMFailoverPerCandidateRetries, "MOSSCODE_LLM_FAILOVER_RETRIES", "MOSS_LLM_FAILOVER_RETRIES")
	}
	if _, ok := explicit["llm-failover-on-breaker-open"]; !ok {
		cfg.LLMFailoverOnBreakerOpen = firstEnvBool(cfg.LLMFailoverOnBreakerOpen, "MOSSCODE_LLM_FAILOVER_ON_BREAKER_OPEN", "MOSS_LLM_FAILOVER_ON_BREAKER_OPEN")
	}
}

func firstEnv(def string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return def
}

func firstEnvInt(def int, keys ...string) int {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(os.Stderr, "warning: ignore invalid %s=%q\n", key, value)
	}
	return def
}

func firstEnvDuration(def time.Duration, keys ...string) time.Duration {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(os.Stderr, "warning: ignore invalid %s=%q\n", key, value)
	}
	return def
}

func firstEnvBool(def bool, keys ...string) bool {
	for _, key := range keys {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
		fmt.Fprintf(os.Stderr, "warning: ignore invalid %s=%q\n", key, value)
	}
	return def
}

func showConfig(flags *appkit.AppFlags) error {
	out, err := product.ShowConfig(flags, false)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
