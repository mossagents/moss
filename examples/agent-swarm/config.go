package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/mossagents/moss/harness/appkit"
)

const (
	appName          = "agent-swarm"
	defaultView      = "run"
	defaultExportFmt = "bundle"
	defaultDemoTopic = "Summarize the trade-offs of local-first agent swarms."
)

type commandKind string

const (
	commandRun     commandKind = "run"
	commandResume  commandKind = "resume"
	commandInspect commandKind = "inspect"
	commandExport  commandKind = "export"
)

type parsedCommand struct {
	name    commandKind
	run     *runCommandConfig
	resume  *resumeCommandConfig
	inspect *inspectCommandConfig
	export  *exportCommandConfig
}

type runCommandConfig struct {
	AppFlags appkit.AppFlags
	Topic    string
	RunID    string
	Output   string
	Demo     bool
}

type resumeCommandConfig struct {
	AppFlags             appkit.AppFlags
	RunID                string
	SessionID            string
	Output               string
	Latest               bool
	Demo                 bool
	ForceDegradedResume  bool
}

type inspectCommandConfig struct {
	RunID     string
	SessionID string
	Latest    bool
	JSON      bool
	View      string
	ThreadID  string
}

type exportCommandConfig struct {
	RunID           string
	SessionID       string
	Output          string
	Format          string
	Latest          bool
	IncludePayloads bool
}

type commandExitError struct {
	code int
	msg  string
}

func (e *commandExitError) Error() string { return e.msg }

func parseCommand(args []string) (*parsedCommand, error) {
	if len(args) == 0 {
		return nil, usageError(2, rootUsage())
	}
	switch strings.TrimSpace(args[0]) {
	case string(commandRun):
		cfg, err := parseRunCommand(args[1:])
		if err != nil {
			return nil, err
		}
		return &parsedCommand{name: commandRun, run: cfg}, nil
	case string(commandResume):
		cfg, err := parseResumeCommand(args[1:])
		if err != nil {
			return nil, err
		}
		return &parsedCommand{name: commandResume, resume: cfg}, nil
	case string(commandInspect):
		cfg, err := parseInspectCommand(args[1:])
		if err != nil {
			return nil, err
		}
		return &parsedCommand{name: commandInspect, inspect: cfg}, nil
	case string(commandExport):
		cfg, err := parseExportCommand(args[1:])
		if err != nil {
			return nil, err
		}
		return &parsedCommand{name: commandExport, export: cfg}, nil
	default:
		return nil, usageError(2, rootUsage())
	}
}

func parseRunCommand(args []string) (*runCommandConfig, error) {
	cfg := &runCommandConfig{}
	fs := newFlagSet(string(commandRun))
	appkit.BindAppFlags(fs, &cfg.AppFlags)
	fs.StringVar(&cfg.Topic, "topic", "", "Research topic")
	fs.StringVar(&cfg.RunID, "run-id", "", "Explicit swarm run ID")
	fs.StringVar(&cfg.Output, "output", "", "Output directory for user-facing files")
	fs.BoolVar(&cfg.Demo, "demo", false, "Use deterministic demo execution")
	if err := parseFlagSet(fs, args); err != nil {
		return nil, err
	}
	if err := appkit.InitializeApp(appName, &cfg.AppFlags, "AGENT_SWARM", "MOSS"); err != nil {
		return nil, err
	}
	if cfg.Demo && strings.TrimSpace(cfg.Topic) == "" {
		cfg.Topic = defaultDemoTopic
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		return nil, usageError(2, "run requires --topic (or use --demo for the built-in topic)")
	}
	if !cfg.Demo && strings.TrimSpace(cfg.AppFlags.Model) == "" {
		return nil, usageError(2, "run in real mode requires --model (or configure one via env/global config)")
	}
	return cfg, nil
}

func parseResumeCommand(args []string) (*resumeCommandConfig, error) {
	cfg := &resumeCommandConfig{}
	fs := newFlagSet(string(commandResume))
	appkit.BindAppFlags(fs, &cfg.AppFlags)
	fs.StringVar(&cfg.RunID, "run-id", "", "Swarm run ID")
	fs.StringVar(&cfg.SessionID, "session", "", "Root session ID")
	fs.StringVar(&cfg.Output, "output", "", "Output directory for user-facing files")
	fs.BoolVar(&cfg.Latest, "latest", false, "Resolve the latest candidate")
	fs.BoolVar(&cfg.Demo, "demo", false, "Assert that the resumed run is demo mode")
	fs.BoolVar(&cfg.ForceDegradedResume, "force-degraded-resume", false, "Allow resume from a degraded snapshot")
	if err := parseFlagSet(fs, args); err != nil {
		return nil, err
	}
	if err := appkit.InitializeApp(appName, &cfg.AppFlags, "AGENT_SWARM", "MOSS"); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseInspectCommand(args []string) (*inspectCommandConfig, error) {
	cfg := &inspectCommandConfig{}
	fs := newFlagSet(string(commandInspect))
	fs.StringVar(&cfg.RunID, "run-id", "", "Swarm run ID")
	fs.StringVar(&cfg.SessionID, "session", "", "Root session ID")
	fs.BoolVar(&cfg.Latest, "latest", false, "Resolve the latest run")
	fs.BoolVar(&cfg.JSON, "json", false, "Render JSON output")
	fs.StringVar(&cfg.View, "view", defaultView, "View to render: run|threads|thread|events")
	fs.StringVar(&cfg.ThreadID, "thread-id", "", "Thread/session ID for --view thread")
	if err := parseFlagSet(fs, args); err != nil {
		return nil, err
	}
	if err := appkit.InitializeApp(appName, nil, "AGENT_SWARM", "MOSS"); err != nil {
		return nil, err
	}
	return cfg, nil
}

func parseExportCommand(args []string) (*exportCommandConfig, error) {
	cfg := &exportCommandConfig{}
	fs := newFlagSet(string(commandExport))
	fs.StringVar(&cfg.RunID, "run-id", "", "Swarm run ID")
	fs.StringVar(&cfg.SessionID, "session", "", "Root session ID")
	fs.StringVar(&cfg.Output, "output", "", "Export directory")
	fs.StringVar(&cfg.Format, "format", defaultExportFmt, "Export format: bundle|json|jsonl")
	fs.BoolVar(&cfg.Latest, "latest", false, "Resolve the latest run")
	fs.BoolVar(&cfg.IncludePayloads, "include-payloads", false, "Include artifact payload files")
	if err := parseFlagSet(fs, args); err != nil {
		return nil, err
	}
	if err := appkit.InitializeApp(appName, nil, "AGENT_SWARM", "MOSS"); err != nil {
		return nil, err
	}
	return cfg, nil
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlagSet(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return usageError(2, err.Error())
	}
	if len(fs.Args()) > 0 {
		return usageError(2, fmt.Sprintf("unexpected arguments: %s", strings.Join(fs.Args(), " ")))
	}
	return nil
}

func usageError(code int, msg string) error {
	return &commandExitError{code: code, msg: strings.TrimSpace(msg)}
}

func rootUsage() string {
	return strings.TrimSpace(`usage:
  go run . run --topic "<topic>" [--demo] [provider flags...]
  go run . resume [--session <id> | --run-id <id> | --latest] [--demo] [provider flags...]
  go run . inspect [--session <id> | --run-id <id> | --latest] [--view run|threads|thread|events] [--thread-id <id>] [--json]
  go run . export [--session <id> | --run-id <id> | --latest] [--format bundle|json|jsonl] [--output <dir>] [--include-payloads]`)
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}
