package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mossagi/moss/internal/app"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/workspace"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		runCmd(os.Args[2:])
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
	fmt.Print(`moss - MVP Agent Runtime

Usage:
  moss run [flags]
  moss version

Flags:
  --goal        Goal for the agent to accomplish (required)
  --workspace   Workspace directory (default: ".")
  --mode        Run mode: interactive|safe|autopilot (default: interactive)
  --trust       Trust level: trusted|restricted (default: trusted)
`)
}

func runCmd(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	goal := fs.String("goal", "", "Goal for the agent to accomplish (required)")
	wsDir := fs.String("workspace", ".", "Workspace directory")
	mode := fs.String("mode", "interactive", "Run mode: interactive|safe|autopilot")
	trust := fs.String("trust", "trusted", "Trust level: trusted|restricted")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *goal == "" {
		fmt.Fprintln(os.Stderr, "error: --goal is required")
		fs.Usage()
		os.Exit(1)
	}

	var runMode domain.RunMode
	switch *mode {
	case "interactive":
		runMode = domain.RunModeInteractive
	case "safe":
		runMode = domain.RunModeSafe
	case "autopilot":
		runMode = domain.RunModeAutopilot
	default:
		fmt.Fprintf(os.Stderr, "error: invalid mode %q\n", *mode)
		os.Exit(1)
	}

	var trustLevel workspace.TrustLevel
	switch *trust {
	case "trusted":
		trustLevel = workspace.TrustLevelTrusted
	case "restricted":
		trustLevel = workspace.TrustLevelRestricted
	default:
		fmt.Fprintf(os.Stderr, "error: invalid trust level %q\n", *trust)
		os.Exit(1)
	}

	svc, err := app.NewService(*wsDir, trustLevel, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error initializing service: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cancelling run...")
		cancel()
	}()

	fmt.Printf("🌿 moss %s\n", version)
	fmt.Printf("Goal: %s\n", *goal)
	fmt.Printf("Workspace: %s\n", *wsDir)
	fmt.Printf("Mode: %s | Trust: %s\n\n", *mode, *trust)

	run, err := svc.Execute(ctx, app.RunRequest{
		Goal:      *goal,
		Mode:      runMode,
		Workspace: *wsDir,
		Trust:     trustLevel,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Run failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Run completed (ID: %s)\n", run.RunID)
	fmt.Printf("Status: %s\n", run.Status)
	if run.FinalResult != "" {
		fmt.Printf("\nResult:\n%s\n", run.FinalResult)
	}
}
