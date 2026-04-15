package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"

	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/appkit/product"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/harness/logging"
	rpolicy "github.com/mossagents/moss/harness/runtime/policy"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func buildRootCommand(cfg *config) *cobra.Command {
	root := &cobra.Command{
		Use:   appName,
		Short: "Lightweight production-ready coding assistant",
		Long: `mosscode — lightweight production-ready coding assistant

Launch the interactive TUI (no flags), run a one-shot prompt, or use one of
the sub-commands to manage threads, checkpoints, config, and more.

Approval modes:
  read-only   No file writes — inspection only
  confirm     Prompt before every write (default)
  full-auto   Apply all changes without prompting

LLM governance flags (--llm-*) control retries, circuit-breaking, and
model-failover. Supply a --router-config YAML to define candidate models.`,
		Example: `  # Launch interactive TUI
  mosscode

  # One-shot prompt
  mosscode --prompt "Add unit tests for auth.go"

  # One-shot with a specific model, no confirmation
  mosscode --provider openai --model gpt-4o --approval full-auto \
           --prompt "Refactor the payment module"`,
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

	execCmd := &cobra.Command{
		Use:   "exec",
		Short: "Run one-shot prompt mode",
		Long: `Run a single prompt non-interactively and exit.

Equivalent to passing --prompt from the root command, but as an explicit
sub-command that is easier to script and pipe.`,
		Example: `  mosscode exec --prompt "Write a changelog entry for the last 5 commits"
  mosscode exec --prompt "Fix lint errors" --approval full-auto --json`,
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
		Long: `Resume an interrupted or paused thread in the interactive TUI.

Use --latest to resume the most recent recoverable thread, or --session to
target a specific persisted thread ID. Omitting both flags lists available
recoverable threads.`,
		Example: `  mosscode resume --latest
  mosscode resume --session sess_abc123`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runResume(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(resumeCmd, cfg)
	resumeCmd.Flags().StringVar(&cfg.resumeSessionID, "session", "", "Resume a specific persisted thread ID")
	resumeCmd.Flags().BoolVar(&cfg.resumeLatest, "latest", false, "Resume the latest recoverable thread")

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize workspace bootstrap files",
		Long: `Write bootstrap files into the current workspace directory.

Creates any missing configuration stubs (e.g. AGENTS.md, .mosscode/) so the
agent has a well-defined starting environment. Safe to run multiple times —
existing files are not overwritten.`,
		Example: `  mosscode init
  mosscode init --workspace /path/to/project`,
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
		Long: `Check the environment and report any configuration problems.

Validates the API key, model availability, workspace permissions, and other
prerequisites. Use --json for machine-readable output.`,
		Example: `  mosscode doctor
  mosscode doctor --json`,
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
		Long: `Print every effective runtime setting after merging flag overrides, environment
variables, and the persisted config file.

Useful for debugging unexpected behaviour — shows exactly what values the agent
will use for the current invocation.`,
		Example: `  mosscode debug-config
  mosscode debug-config --json`,
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
		Long: `Read and write the global mosscode config file.

Sub-commands:
  show                              Print persisted config and effective values
  path                              Print the config file path
  set provider|name|model|base_url  Set a top-level config field
  unset name|model|base_url         Clear a config field
  mcp list                          List configured MCP servers
  mcp show <name>                   Show an MCP server entry
  mcp enable  <name> [global|project]
  mcp disable <name> [global|project]`,
		Example: `  mosscode config show
  mosscode config path
  mosscode config set provider openai
  mosscode config set model gpt-4o
  mosscode config unset model
  mosscode config mcp list
  mosscode config mcp enable my-server global`,
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
		Long: `Show the current code-review state for the workspace.

Sub-commands (passed as positional args):
  status              Repository change summary (default)
  snapshots           List saved worktree snapshots
  snapshot <id>       Show a specific snapshot
  changes             List persisted change operations
  change   <id>       Show a specific change operation`,
		Example: `  mosscode review
  mosscode review status
  mosscode review snapshots
  mosscode review snapshot snap_abc123
  mosscode review changes
  mosscode review change chg_xyz789 --json`,
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
	fs.BoolVar(&cfg.debug, "debug", logging.DebugEnabled(), "Enable trace logging to ~/.mosscode/debug.log")
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
	if cfg.debug {
		_ = os.Setenv("MOSS_DEBUG", "1")
	}
	if err := appkit.InitializeApp(appName, cfg.flags, "MOSSCODE", "MOSS"); err != nil {
		return err
	}
	cfg.approvalMode = firstNonEmpty(
		cfg.approvalMode,
		os.Getenv("MOSSCODE_APPROVAL_MODE"),
		os.Getenv("MOSS_APPROVAL_MODE"),
		product.ApprovalModeConfirm,
	)
	cfg.approvalMode = rpolicy.NormalizeApprovalMode(cfg.approvalMode)
	if err := rpolicy.ValidateApprovalMode(cfg.approvalMode); err != nil {
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
	logging.GetLogger().Debug("executing command",
		"command", cmd.CommandPath(),
		"workspace", cfg.flags.Workspace,
		"prompt_mode", strings.TrimSpace(cfg.prompt) != "",
	)
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
		ctx, cancel = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
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
		Short: "Fork from thread or checkpoint",
		Long: `Create a new thread branched from an existing thread or checkpoint.

Exactly one of --session, --checkpoint, or --latest must be supplied.
The forked thread opens in the interactive TUI unless --json is given, in
which case the new thread ID is emitted to stdout and the command exits.`,
		Example: `  # Fork from the latest checkpoint
  mosscode fork --latest

  # Fork from a specific checkpoint and restore worktree files
  mosscode fork --checkpoint ckpt_abc123 --restore-worktree

  # Fork from the latest checkpoint of a specific thread (machine-readable)
  mosscode fork --session sess_xyz789 --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runFork(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(cmd, cfg)
	cmd.Flags().StringVar(&cfg.forkSessionID, "session", "", "Fork from this persisted thread ID (prefers the latest checkpoint for that thread)")
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
		Long: `Create, inspect, and replay persisted checkpoints.

A checkpoint captures the full state of a thread (conversation history,
worktree snapshot, metadata) so it can be forked or replayed later.

Sub-commands:
  list    List all persisted checkpoints
  show    Inspect a specific checkpoint
  create  Snapshot an existing thread into a checkpoint
  replay  Prepare a fresh thread from a checkpoint`,
		Example: `  mosscode checkpoint list
  mosscode checkpoint show latest
  mosscode checkpoint show ckpt_abc123 --json
  mosscode checkpoint create --session sess_xyz789 --note "before refactor"
  mosscode checkpoint replay --latest --mode resume`,
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
			createCmd.Flags().StringVar(&cfg.checkpointCreateSessionID, "session", "", "Persisted thread ID to checkpoint")
			createCmd.Flags().StringVar(&cfg.checkpointCreateNote, "note", "", "Optional checkpoint note")
			createCmd.Flags().BoolVar(&cfg.checkpointJSON, "json", false, "Emit checkpoint create output as JSON")
			return createCmd
		}(),
		func() *cobra.Command {
			replayCmd := &cobra.Command{
				Use:   "replay",
				Short: "Prepare a fresh replay thread from a checkpoint",
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
			replayCmd.Flags().StringVar(&cfg.checkpointReplayMode, "mode", string(checkpoint.ReplayModeResume), "Replay mode: resume|rerun")
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
		Long: `Apply a unified diff patch file to the workspace.

Records the operation as a persisted change so it can be rolled back later
with ` + "`mosscode rollback`" + `. Optionally associate the change with an existing
thread by passing --session.`,
		Example: `  mosscode apply --patch-file ./changes.patch
  mosscode apply --patch-file ./changes.patch --summary "Fix null pointer in auth"
  mosscode apply --patch-file ./changes.patch --session sess_abc123 --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return executeCobraCommand(cmd, cfg, commandExecutionOptions{withSignal: true, withProductRuntime: true}, func(ctx context.Context, cfg *config) error {
				return runApply(ctx, cfg)
			})
		},
	}
	bindAppAndProductCobraFlags(cmd, cfg)
	cmd.Flags().StringVar(&cfg.applyPatchFile, "patch-file", "", "Apply an explicit patch file")
	cmd.Flags().StringVar(&cfg.applySummary, "summary", "", "Optional human-readable summary for the change")
	cmd.Flags().StringVar(&cfg.applySessionID, "session", "", "Optional persisted thread ID for checkpoint creation")
	cmd.Flags().BoolVar(&cfg.applyJSON, "json", false, "Emit apply output as JSON")
	return cmd
}

func buildRollbackCommand(cfg *config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Roll back persisted change",
		Long: `Revert a previously applied persisted change operation.

Applies the inverse patch for the change identified by --change and marks the
operation as rolled back in the change log. Use ` + "`mosscode changes list`" + `
to find the change ID.`,
		Example: `  mosscode rollback --change chg_abc123
  mosscode rollback --change chg_abc123 --json`,
		Args: cobra.NoArgs,
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
		Long: `Browse persisted change operations recorded by ` + "`mosscode apply`" + `.

Each apply operation is stored with its patch, summary, and thread reference
so it can be inspected or rolled back at any time.

Sub-commands:
  list   List recent change operations (default: last 20)
  show   Show full detail for a specific change`,
		Example: `  mosscode changes list
  mosscode changes list --limit 50 --json
  mosscode changes show chg_abc123
  mosscode changes show chg_abc123 --json`,
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
