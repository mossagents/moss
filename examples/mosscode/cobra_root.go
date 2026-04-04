package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/kernel/port"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func buildRootCommand(cfg *config) *cobra.Command {
	root := &cobra.Command{
		Use:           appName,
		Short:         "Lightweight production-ready coding assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpFlag := cmd.Flags().Lookup("help"); helpFlag != nil && helpFlag.Changed {
				return nil
			}
			if strings.TrimSpace(cfg.prompt) != "" {
				return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
					return runExecCommand(ctx, cfg)
				})
			}
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withProductRuntime: true}, func(_ context.Context, cfg *config) error {
				return launchTUI(cfg)
			})
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
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runExecCommand(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(execCmd, cfg)
	execCmd.Flags().StringVarP(&cfg.prompt, "prompt", "p", "", "Run one-shot mode with a single prompt (omit to launch TUI)")
	execCmd.Flags().BoolVar(&cfg.execJSON, "json", false, "Emit one-shot execution output as JSON")

	resumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a recoverable thread",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runResume(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(resumeCmd, cfg)
	resumeCmd.Flags().StringVar(&cfg.resumeSessionID, "session", "", "Resume a specific session by ID")
	resumeCmd.Flags().BoolVar(&cfg.resumeLatest, "latest", false, "Resume the latest recoverable session")

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize workspace bootstrap files",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{}, func(_ context.Context, cfg *config) error {
				return runInit(cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(initCmd, cfg)
	initCmd.Flags().StringVarP(&cfg.prompt, "prompt", "p", "", "Run one-shot mode with a single prompt (omit to launch TUI)")
	initCmd.Flags().BoolVar(&cfg.execJSON, "json", false, "Emit one-shot execution output as JSON")

	doctorCmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
				return runDoctor(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(doctorCmd, cfg)
	doctorCmd.Flags().BoolVar(&cfg.doctorJSON, "json", false, "Emit doctor output as JSON")

	debugConfigCmd := &cobra.Command{
		Use:   "debug-config",
		Short: "Show resolved runtime config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{}, func(_ context.Context, cfg *config) error {
				return runDebugConfig(cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(debugConfigCmd, cfg)
	debugConfigCmd.Flags().BoolVar(&cfg.debugConfigJSON, "json", false, "Emit debug-config output as JSON")

	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage persisted config",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.configArgs = append([]string(nil), args...)
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{}, func(_ context.Context, cfg *config) error {
				return runConfig(cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(configCmd, cfg)

	reviewCmd := &cobra.Command{
		Use:   "review",
		Short: "Inspect review state",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.reviewArgs = append([]string(nil), args...)
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
				return runReview(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(reviewCmd, cfg)
	reviewCmd.Flags().BoolVar(&cfg.reviewJSON, "json", false, "Emit review output as JSON")

	root.AddCommand(
		execCmd,
		resumeCmd,
		buildForkCommand(cfg),
		initCmd,
		doctorCmd,
		debugConfigCmd,
		&cobra.Command{
			Use:   "completion",
			Short: "Emit shell completion script",
			RunE: func(_ *cobra.Command, args []string) error {
				cfg.completionArgs = append([]string(nil), args...)
				return runCompletion(cfg)
			},
		},
		configCmd,
		reviewCmd,
		buildCheckpointCommand(cfg),
		buildApplyCommand(cfg),
		buildRollbackCommand(cfg),
		buildChangesCommand(cfg),
	)

	return root
}

func bindAppAndProductCobraFlags(cmd *cobra.Command, cfg *config) {
	appkit.BindAppPFlags(cmd.Flags(), cfg.flags)
	bindProductFlags(cmd.Flags(), cfg)
}

func bindProductFlags(fs *pflag.FlagSet, cfg *config) {
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

type commandExecutionOptions struct {
	withSignal         bool
	withProductRuntime bool
}

func executeCobraCommand(cmd *cobra.Command, cfg *config, opts commandExecutionOptions, run func(context.Context, *config) error) error {
	if err := finalizeCommonCobraFlags(cmd, cfg); err != nil {
		return err
	}
	return executePreparedCommand(cfg, opts, run)
}

func executePreparedCommand(cfg *config, opts commandExecutionOptions, run func(context.Context, *config) error) error {
	cleanup := func() {}
	if opts.withProductRuntime {
		runtimeCleanup, err := initializeCommandRuntime(cfg)
		if err != nil {
			return err
		}
		cleanup = runtimeCleanup
	}
	defer cleanup()

	ctx := context.Background()
	cancel := func() {}
	if opts.withSignal {
		ctx, cancel = appkit.ContextWithSignal(context.Background())
	}
	defer cancel()
	return run(ctx, cfg)
}

func runExecCommand(ctx context.Context, cfg *config) error {
	if code := runExec(ctx, cfg); code != 0 {
		return &commandExitError{code: code}
	}
	return nil
}

func buildForkCommand(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fork",
		Short: "Fork from session or checkpoint",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runFork(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(cmd, cfg)
	cmd.Flags().StringVar(&cfg.forkSessionID, "session", "", "Fork from this session ID (prefers latest checkpoint for that session)")
	cmd.Flags().StringVar(&cfg.forkCheckpointID, "checkpoint", "", "Fork directly from this checkpoint ID")
	cmd.Flags().BoolVar(&cfg.forkLatest, "latest", false, "Fork from the latest persisted checkpoint")
	cmd.Flags().BoolVar(&cfg.forkRestoreWorktree, "restore-worktree", false, "Attempt worktree restore when forking from checkpoint state")
	cmd.Flags().BoolVar(&cfg.forkJSON, "json", false, "Emit fork output as JSON")
	return cmd
}

func buildCheckpointCommand(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoint",
		Short: "Manage persisted checkpoints",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("usage: mosscode checkpoint <list|show|create|replay> [flags]")
		},
	}
	cmd.AddCommand(
		func() *cobra.Command {
			listCmd := &cobra.Command{
				Use:   "list",
				Short: "List persisted checkpoints",
				Args:  cobra.NoArgs,
				RunE: func(cmd *cobra.Command, _ []string) error {
					cfg.checkpointAction = "list"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
						return runCheckpoint(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(listCmd, cfg)
			listCmd.Flags().BoolVar(&cfg.checkpointJSON, "json", false, "Emit checkpoint list as JSON")
			listCmd.Flags().IntVar(&cfg.checkpointLimit, "limit", 20, "Maximum checkpoints to list")
			return listCmd
		}(),
		func() *cobra.Command {
			showCmd := &cobra.Command{
				Use:   "show <id|latest>",
				Short: "Inspect a persisted checkpoint",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.checkpointID = strings.TrimSpace(args[0])
					cfg.checkpointAction = "show"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
						return runCheckpoint(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(showCmd, cfg)
			showCmd.Flags().BoolVar(&cfg.checkpointJSON, "json", false, "Emit checkpoint detail as JSON")
			return showCmd
		}(),
		func() *cobra.Command {
			createCmd := &cobra.Command{
				Use:   "create",
				Short: "Create a persisted checkpoint",
				Args:  cobra.NoArgs,
				RunE: func(cmd *cobra.Command, _ []string) error {
					cfg.checkpointAction = "create"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
						return runCheckpoint(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(createCmd, cfg)
			createCmd.Flags().StringVar(&cfg.checkpointCreateSessionID, "session", "", "Persisted session ID to checkpoint")
			createCmd.Flags().StringVar(&cfg.checkpointCreateNote, "note", "", "Optional checkpoint note")
			createCmd.Flags().BoolVar(&cfg.checkpointJSON, "json", false, "Emit checkpoint create output as JSON")
			return createCmd
		}(),
		func() *cobra.Command {
			replayCmd := &cobra.Command{
				Use:   "replay",
				Short: "Prepare a fresh replay session from a checkpoint",
				Args:  cobra.NoArgs,
				RunE: func(cmd *cobra.Command, _ []string) error {
					cfg.checkpointAction = "replay"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
						return runCheckpoint(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(replayCmd, cfg)
			replayCmd.Flags().StringVar(&cfg.checkpointID, "checkpoint", "", "Checkpoint ID to replay")
			replayCmd.Flags().BoolVar(&cfg.checkpointLatest, "latest", false, "Replay the latest persisted checkpoint")
			replayCmd.Flags().StringVar(&cfg.checkpointReplayMode, "mode", string(port.ReplayModeResume), "Replay mode: resume|rerun")
			replayCmd.Flags().BoolVar(&cfg.checkpointRestoreWorktree, "restore-worktree", false, "Attempt worktree restore before replay")
			replayCmd.Flags().BoolVar(&cfg.checkpointJSON, "json", false, "Emit checkpoint replay output as JSON")
			return replayCmd
		}(),
		func() *cobra.Command {
			forkCmd := &cobra.Command{
				Use:   "fork",
				Short: "Deprecated alias that redirects to the top-level fork command",
				Args:  cobra.NoArgs,
				RunE: func(cmd *cobra.Command, _ []string) error {
					cfg.checkpointAction = "fork"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
						return runCheckpoint(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(forkCmd, cfg)
			return forkCmd
		}(),
	)
	return cmd
}

func buildApplyCommand(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply explicit patch",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runApply(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(cmd, cfg)
	cmd.Flags().StringVar(&cfg.applyPatchFile, "patch-file", "", "Apply an explicit patch file")
	cmd.Flags().StringVar(&cfg.applySummary, "summary", "", "Optional human-readable summary for the change")
	cmd.Flags().StringVar(&cfg.applySessionID, "session", "", "Optional persisted session ID for checkpoint creation")
	cmd.Flags().BoolVar(&cfg.applyJSON, "json", false, "Emit apply output as JSON")
	return cmd
}

func buildRollbackCommand(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Roll back persisted change",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runRollback(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(cmd, cfg)
	cmd.Flags().StringVar(&cfg.rollbackChangeID, "change", "", "Roll back a specific persisted change by ID")
	cmd.Flags().BoolVar(&cfg.rollbackJSON, "json", false, "Emit rollback output as JSON")
	return cmd
}

func buildChangesCommand(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "changes",
		Short: "List or inspect persisted changes",
		RunE: func(_ *cobra.Command, _ []string) error {
			return fmt.Errorf("usage: mosscode changes <list|show> [flags]")
		},
	}
	cmd.AddCommand(
		func() *cobra.Command {
			listCmd := &cobra.Command{
				Use:   "list",
				Short: "List persisted change operations",
				Args:  cobra.NoArgs,
				RunE: func(cmd *cobra.Command, _ []string) error {
					cfg.changesAction = "list"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
						return runChanges(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(listCmd, cfg)
			listCmd.Flags().BoolVar(&cfg.changesJSON, "json", false, "Emit changes list as JSON")
			listCmd.Flags().IntVar(&cfg.changesLimit, "limit", 20, "Maximum change operations to list")
			return listCmd
		}(),
		func() *cobra.Command {
			showCmd := &cobra.Command{
				Use:   "show <id>",
				Short: "Show a specific persisted change operation",
				Args:  cobra.ExactArgs(1),
				RunE: func(cmd *cobra.Command, args []string) error {
					cfg.changesShowID = strings.TrimSpace(args[0])
					cfg.changesAction = "show"
					return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true}, func(ctx context.Context, cfg *config) error {
						return runChanges(ctx, cfg)
					})
				},
			}
			bindAppAndProductCobraFlags(showCmd, cfg)
			showCmd.Flags().BoolVar(&cfg.changesJSON, "json", false, "Emit change detail as JSON")
			return showCmd
		}(),
	)
	return cmd
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

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
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
