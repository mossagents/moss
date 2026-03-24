package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mossagi/moss/kernel"
	"github.com/mossagi/moss/kernel/middleware/builtins"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/sandbox"
	"github.com/mossagi/moss/kernel/session"
)

const version = "0.2.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
	case "tui":
		tuiCmd(os.Args[2:])
	case "version":
		fmt.Printf("moss %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`moss - Agent Runtime Kernel

Usage:
  moss run [flags]
  moss tui [flags]
  moss version

Flags:
  --goal        Goal for the agent to accomplish (required unless interactive TUI is used)
  --workspace   Workspace directory (default: ".")
  --mode        Run mode: interactive|autopilot (default: interactive)
  --trust       Trust level: trusted|restricted (default: trusted)
`)
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	goal := fs.String("goal", "", "Goal for the agent to accomplish")
	wsDir := fs.String("workspace", ".", "Workspace directory")
	mode := fs.String("mode", "interactive", "Run mode: interactive|autopilot")
	trust := fs.String("trust", "trusted", "Trust level: trusted|restricted")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *goal == "" {
		if *mode == "interactive" {
			runTUI(*wsDir, *mode, *trust, os.Stdin, os.Stdout, os.Stderr)
			return
		}
		fmt.Fprintln(os.Stderr, "error: --goal is required")
		fs.Usage()
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cancelling run...")
		cancel()
	}()

	k, err := buildKernel(*wsDir, *trust, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing kernel: %v\n", err)
		os.Exit(1)
	}
	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "error booting kernel: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	fmt.Printf("🌿 moss %s\n", version)
	fmt.Printf("Goal: %s\n", *goal)
	fmt.Printf("Workspace: %s\n", *wsDir)
	fmt.Printf("Mode: %s | Trust: %s\n\n", *mode, *trust)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:       *goal,
		Mode:       *mode,
		TrustLevel: *trust,
		MaxSteps:   50,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating session: %v\n", err)
		os.Exit(1)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: *goal})

	result, err := k.Run(ctx, sess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Run failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Session completed (ID: %s)\n", result.SessionID)
	fmt.Printf("Steps: %d | Tokens: %d\n", result.Steps, result.TokensUsed.TotalTokens)
	if result.Output != "" {
		fmt.Printf("\nResult:\n%s\n", result.Output)
	}
}

// buildKernel 构建 Kernel 实例（当前没有真实 LLM adapter，使用占位）。
func buildKernel(wsDir, trust string, _ *os.File, _ *os.File) (*kernel.Kernel, error) {
	sb, err := sandbox.NewLocal(wsDir)
	if err != nil {
		return nil, err
	}

	cliIO := &cliUserIO{writer: os.Stdout, reader: os.Stdin}

	k := kernel.New(
		kernel.WithLLM(nil), // TODO: 接入真实 LLM adapter
		kernel.WithSandbox(sb),
		kernel.WithUserIO(cliIO),
	)

	// 根据 trust level 设置策略
	if trust == "restricted" {
		k.WithPolicy(
			builtins.RequireApprovalFor("write_file", "run_command"),
			builtins.DefaultAllow(),
		)
	}

	return k, nil
}
