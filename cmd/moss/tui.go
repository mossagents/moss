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

	"github.com/mossagi/moss/adapters/claude"
	"github.com/mossagi/moss/kernel/port"
	"github.com/mossagi/moss/kernel/session"
)

// cliUserIO 是基于终端的 UserIO 实现。
type cliUserIO struct {
	writer io.Writer
	reader *os.File
}

func (c *cliUserIO) Send(_ context.Context, msg port.OutputMessage) error {
	switch msg.Type {
	case port.OutputText:
		fmt.Fprintln(c.writer, msg.Content)
	case port.OutputStream:
		fmt.Fprint(c.writer, msg.Content)
	case port.OutputStreamEnd:
		fmt.Fprintln(c.writer)
	case port.OutputProgress:
		fmt.Fprintf(c.writer, "⏳ %s\n", msg.Content)
	case port.OutputToolStart:
		fmt.Fprintf(c.writer, "🔧 Running %s...\n", msg.Content)
	case port.OutputToolResult:
		isErr, _ := msg.Meta["is_error"].(bool)
		if isErr {
			fmt.Fprintf(c.writer, "❌ %s\n", msg.Content)
		} else {
			fmt.Fprintf(c.writer, "✅ %s\n", truncate(msg.Content, 200))
		}
	}
	return nil
}

func (c *cliUserIO) Ask(_ context.Context, req port.InputRequest) (port.InputResponse, error) {
	reader := bufio.NewReader(c.reader)
	switch req.Type {
	case port.InputConfirm:
		fmt.Fprintf(c.writer, "%s [y/N]: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		line = strings.TrimSpace(strings.ToLower(line))
		return port.InputResponse{Approved: line == "y" || line == "yes"}, nil

	case port.InputSelect:
		for i, opt := range req.Options {
			fmt.Fprintf(c.writer, "  %d) %s\n", i+1, opt)
		}
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		var sel int
		fmt.Fscan(c.reader, &sel)
		return port.InputResponse{Selected: sel - 1}, nil

	default: // FreeText
		fmt.Fprintf(c.writer, "%s: ", req.Prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			return port.InputResponse{}, err
		}
		return port.InputResponse{Value: strings.TrimSpace(line)}, nil
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

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

// RunRequest 收集 TUI 输入后的运行请求。
type RunRequest struct {
	Goal      string
	Mode      string
	Workspace string
	Trust     string
}

func tuiCmd(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	wsDir := fs.String("workspace", ".", "Workspace directory")
	mode := fs.String("mode", "interactive", "Run mode: interactive|autopilot")
	trust := fs.String("trust", "trusted", "Trust level: trusted|restricted")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing flags: %v\n", err)
		os.Exit(1)
	}

	runTUI(*wsDir, *mode, *trust, os.Stdin, os.Stdout, os.Stderr)
}

func runTUI(defaultWorkspace, defaultMode, defaultTrust string, reader io.Reader, writer io.Writer, errWriter io.Writer) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	k, err := buildKernel(req.Workspace, req.Trust, claude.DefaultModel)
	if err != nil {
		fmt.Fprintf(errWriter, "error initializing kernel: %v\n", err)
		os.Exit(1)
	}
	if err := k.Boot(ctx); err != nil {
		fmt.Fprintf(errWriter, "error booting kernel: %v\n", err)
		os.Exit(1)
	}
	defer k.Shutdown(ctx)

	fmt.Fprintf(writer, "\nLaunching session...\n\n")
	fmt.Fprintf(writer, "Goal: %s\n", req.Goal)
	fmt.Fprintf(writer, "Workspace: %s\n", req.Workspace)
	fmt.Fprintf(writer, "Mode: %s | Trust: %s\n", req.Mode, req.Trust)

	sess, err := k.NewSession(ctx, session.SessionConfig{
		Goal:       req.Goal,
		Mode:       req.Mode,
		TrustLevel: req.Trust,
		MaxSteps:   50,
	})
	if err != nil {
		fmt.Fprintf(errWriter, "error creating session: %v\n", err)
		os.Exit(1)
	}
	sess.AppendMessage(port.Message{Role: port.RoleUser, Content: req.Goal})

	result, err := k.Run(ctx, sess)
	if err != nil {
		fmt.Fprintf(errWriter, "\n❌ Run failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(writer, "\n✅ Session completed (ID: %s)\n", result.SessionID)
	fmt.Fprintf(writer, "Steps: %d | Tokens: %d\n", result.Steps, result.TokensUsed.TotalTokens)
	if result.Output != "" {
		fmt.Fprintf(writer, "\nResult:\n%s\n", result.Output)
	}
}

var errCancelled = errors.New("cancelled")

func (ui *terminalUI) collectRunRequest(defaultWorkspace, defaultMode, defaultTrust string) (RunRequest, error) {
	ui.renderFrame(defaultWorkspace, defaultMode, defaultTrust)

	goal, err := ui.promptRequired("Goal", "")
	if err != nil {
		return RunRequest{}, err
	}

	workspaceValue, err := ui.promptWithDefault("Workspace", defaultWorkspace)
	if err != nil {
		return RunRequest{}, err
	}

	modeValue, err := ui.promptWithDefault("Mode", defaultMode)
	if err != nil {
		return RunRequest{}, err
	}

	trustValue, err := ui.promptWithDefault("Trust", defaultTrust)
	if err != nil {
		return RunRequest{}, err
	}

	start, err := ui.promptWithDefault("Start run? [Y/n]", "y")
	if err != nil {
		return RunRequest{}, err
	}
	if strings.EqualFold(start, "n") || strings.EqualFold(start, "no") {
		return RunRequest{}, errCancelled
	}

	return RunRequest{
		Goal:      goal,
		Mode:      modeValue,
		Workspace: workspaceValue,
		Trust:     trustValue,
	}, nil
}

func (ui *terminalUI) renderFrame(defaultWorkspace, defaultMode, defaultTrust string) {
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
