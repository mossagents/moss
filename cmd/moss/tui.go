package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mossagi/moss/internal/app"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/workspace"
)

type terminalUI struct {
	reader *bufio.Reader
	writer io.Writer
}

const ansiClearScreen = "\033[H\033[2J"

func newTerminalUI(reader io.Reader, writer io.Writer) *terminalUI {
	if reader == nil {
		reader = os.Stdin
	}
	if writer == nil {
		writer = os.Stdout
	}
	return &terminalUI{
		reader: bufio.NewReader(reader),
		writer: writer,
	}
}

func tuiCmd(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	wsDir := fs.String("workspace", ".", "Workspace directory")
	mode := fs.String("mode", "interactive", "Run mode: interactive|safe|autopilot")
	trust := fs.String("trust", "trusted", "Trust level: trusted|restricted")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	runMode, err := parseRunMode(*mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	trustLevel, err := parseTrustLevel(*trust)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	runTUI(*wsDir, runMode, trustLevel, os.Stdin, os.Stdout, os.Stderr)
}

func runTUI(defaultWorkspace string, defaultMode domain.RunMode, defaultTrust workspace.TrustLevel, reader io.Reader, writer io.Writer, errWriter io.Writer) {
	ui := newTerminalUI(reader, writer)
	req, err := ui.collectRunRequest(defaultWorkspace, defaultMode, defaultTrust)
	if err != nil {
		if errors.Is(err, errCancelled) {
			fmt.Fprintln(writer, "\nRun cancelled.")
			return
		}
		fmt.Fprintf(errWriter, "error collecting TUI input: %v\n", err)
		os.Exit(1)
	}

	svc, err := app.NewService(req.Workspace, req.Trust, reader, writer)
	if err != nil {
		fmt.Fprintf(errWriter, "error initializing service: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runCmdWithService(ctx, svc, req, writer, errWriter)
}

var errCancelled = errors.New("cancelled")

func (ui *terminalUI) collectRunRequest(defaultWorkspace string, defaultMode domain.RunMode, defaultTrust workspace.TrustLevel) (app.RunRequest, error) {
	ui.renderFrame(defaultWorkspace, defaultMode, defaultTrust)

	goal, err := ui.promptRequired("Goal", "")
	if err != nil {
		return app.RunRequest{}, err
	}

	workspaceValue, err := ui.promptWithDefault("Workspace", defaultWorkspace)
	if err != nil {
		return app.RunRequest{}, err
	}

	modeValue, err := ui.promptWithDefault("Mode", string(defaultMode))
	if err != nil {
		return app.RunRequest{}, err
	}
	runMode, err := parseRunMode(modeValue)
	if err != nil {
		return app.RunRequest{}, err
	}

	trustValue, err := ui.promptWithDefault("Trust", string(defaultTrust))
	if err != nil {
		return app.RunRequest{}, err
	}
	trustLevel, err := parseTrustLevel(trustValue)
	if err != nil {
		return app.RunRequest{}, err
	}

	start, err := ui.promptWithDefault("Start run? [Y/n]", "y")
	if err != nil {
		return app.RunRequest{}, err
	}
	if strings.EqualFold(start, "n") || strings.EqualFold(start, "no") {
		return app.RunRequest{}, errCancelled
	}

	return app.RunRequest{
		Goal:      goal,
		Mode:      runMode,
		Workspace: workspaceValue,
		Trust:     trustLevel,
	}, nil
}

func (ui *terminalUI) renderFrame(defaultWorkspace string, defaultMode domain.RunMode, defaultTrust workspace.TrustLevel) {
	fmt.Fprint(ui.writer, ansiClearScreen)
	fmt.Fprintf(ui.writer, "🌿 moss %s\n", version)
	fmt.Fprintln(ui.writer, "┌────────────────────────────────────────────────────────────┐")
	fmt.Fprintln(ui.writer, "│ moss interactive TUI                                      │")
	fmt.Fprintln(ui.writer, "│ Press Enter to keep defaults. Type 'q' to cancel any time.│")
	fmt.Fprintln(ui.writer, "└────────────────────────────────────────────────────────────┘")
	fmt.Fprintf(ui.writer, "Defaults → workspace=%s | mode=%s | trust=%s\n\n", defaultWorkspace, defaultMode, defaultTrust)
}

func (ui *terminalUI) promptRequired(label, fallback string) (string, error) {
	for {
		value, err := ui.promptWithDefault(label, fallback)
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
		fmt.Fprintf(ui.writer, "%s is required.\n", label)
	}
}

func (ui *terminalUI) promptWithDefault(label, fallback string) (string, error) {
	if fallback != "" {
		fmt.Fprintf(ui.writer, "%s [%s]: ", label, fallback)
	} else {
		fmt.Fprintf(ui.writer, "%s: ", label)
	}

	line, err := ui.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if strings.EqualFold(line, "q") || strings.EqualFold(line, "quit") {
		return "", errCancelled
	}
	if line == "" {
		return fallback, nil
	}
	return line, nil
}

func runCmdWithService(ctx context.Context, svc *app.Service, req app.RunRequest, writer io.Writer, errWriter io.Writer) {
	fmt.Fprintf(writer, "\nLaunching run from TUI...\n\n")
	fmt.Fprintf(writer, "Goal: %s\n", req.Goal)
	fmt.Fprintf(writer, "Workspace: %s\n", req.Workspace)
	fmt.Fprintf(writer, "Mode: %s | Trust: %s\n", req.Mode, req.Trust)

	run, err := svc.Execute(ctx, req)
	if err != nil {
		fmt.Fprintf(errWriter, "\n❌ Run failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(writer, "\n✅ Run completed (ID: %s)\n", run.RunID)
	fmt.Fprintf(writer, "Status: %s\n", run.Status)
	if run.FinalResult != "" {
		fmt.Fprintf(writer, "\nResult:\n%s\n", run.FinalResult)
	}
}
