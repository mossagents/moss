package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mossagents/moss/harness/appkit"
)

const (
	appName          = "research-swarm"
	defaultView      = "run"
	defaultExportFmt = "bundle"
)

type reportDetail string

const (
	detailBrief         reportDetail = "brief"
	detailStandard      reportDetail = "standard"
	detailComprehensive reportDetail = "comprehensive"
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
	AllowAll bool
	Workers  int
	Lang     string
	Detail   reportDetail
	AsOf     time.Time
}

type resumeCommandConfig struct {
	AppFlags            appkit.AppFlags
	RunID               string
	SessionID           string
	Output              string
	Latest              bool
	AllowAll            bool
	ForceDegradedResume bool
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
	detail := string(detailComprehensive)
	var asOf string
	fs.StringVar(&cfg.Topic, "topic", "", "Research topic")
	fs.StringVar(&cfg.RunID, "run-id", "", "Explicit swarm run ID")
	fs.StringVar(&cfg.Output, "output", "", "Output directory for user-facing files")
	fs.BoolVar(&cfg.AllowAll, "allow-all", false, "Allow all operations; use AI guardian for permission checks instead of interactive approval")
	fs.IntVar(&cfg.Workers, "workers", 3, "Number of parallel worker agents (default 3, min 1)")
	fs.StringVar(&cfg.Lang, "lang", "", "Output language for generated reports (e.g. zh, en, ja; default: model default)")
	fs.StringVar(&detail, "detail", detail, "Report detail: brief|standard|comprehensive")
	fs.StringVar(&asOf, "as-of", "", "Reference time for current-data research (RFC3339, default: now)")
	if err := parseFlagSet(fs, args); err != nil {
		return nil, err
	}
	if err := appkit.InitializeApp(appName, &cfg.AppFlags, "RESEARCH_SWARM", "MOSS"); err != nil {
		return nil, err
	}
	parsedDetail, err := parseReportDetail(detail)
	if err != nil {
		return nil, err
	}
	cfg.Detail = parsedDetail
	cfg.AsOf, err = parseAsOfTime(asOf)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Topic) == "" {
		return nil, usageError(2, "run requires --topic")
	}
	if strings.TrimSpace(cfg.AppFlags.Model) == "" {
		return nil, usageError(2, "run requires --model (or configure one via env/global config)")
	}
	if cfg.Workers < 1 {
		return nil, usageError(2, "--workers must be at least 1")
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
	fs.BoolVar(&cfg.AllowAll, "allow-all", false, "Allow all operations; use AI guardian for permission checks instead of interactive approval")
	fs.BoolVar(&cfg.ForceDegradedResume, "force-degraded-resume", false, "Allow resume from a degraded snapshot")
	if err := parseFlagSet(fs, args); err != nil {
		return nil, err
	}
	if err := appkit.InitializeApp(appName, &cfg.AppFlags, "RESEARCH_SWARM", "MOSS"); err != nil {
		return nil, err
	}
	if cfg.AllowAll && strings.TrimSpace(cfg.AppFlags.Model) == "" {
		return nil, usageError(2, "--allow-all requires --model (AI permission guard needs a model)")
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
	if err := appkit.InitializeApp(appName, nil, "RESEARCH_SWARM", "MOSS"); err != nil {
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
	if err := appkit.InitializeApp(appName, nil, "RESEARCH_SWARM", "MOSS"); err != nil {
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
  go run . run --topic "<topic>" [--allow-all] [--workers N] [--lang <lang>] [--detail brief|standard|comprehensive] [--as-of RFC3339] [provider flags...]
  go run . resume [--session <id> | --run-id <id> | --latest] [--allow-all] [provider flags...]
  go run . inspect [--session <id> | --run-id <id> | --latest] [--view run|threads|thread|events] [--thread-id <id>] [--json]
  go run . export [--session <id> | --run-id <id> | --latest] [--format bundle|json|jsonl] [--output <dir>] [--include-payloads]

flags:
  --allow-all   Allow all operations; route tool approvals through AI guardian instead of interactive prompts (requires --model)
  --workers N   Number of parallel worker agents to spawn (default 3, min 1)
  --lang        Output language for generated reports (e.g. zh, en, ja; default: model default)`)
}

func stringsTrim(value string) string {
	return strings.TrimSpace(value)
}

func parseReportDetail(value string) (reportDetail, error) {
	switch reportDetail(strings.ToLower(strings.TrimSpace(value))) {
	case detailBrief:
		return detailBrief, nil
	case "", detailStandard:
		return detailStandard, nil
	case detailComprehensive:
		return detailComprehensive, nil
	default:
		return "", usageError(2, fmt.Sprintf("unsupported --detail %q (expected brief|standard|comprehensive)", value))
	}
}

func parseAsOfTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC(), nil
	}
	asOf, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, usageError(2, fmt.Sprintf("invalid --as-of %q (expected RFC3339)", raw))
	}
	return asOf.UTC(), nil
}
