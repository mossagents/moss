package releasegate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/checkpoint"
	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/observe"
	kruntime "github.com/mossagents/moss/kernel/runtime"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/kernel/testing"
	"github.com/mossagents/moss/kernel/tool"
)

// ProbeResult records the outcome of one release-gate probe scenario.
type ProbeResult struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Passed  bool   `json:"passed"`
	Details string `json:"details,omitempty"`
}

// ProbeReport is the machine-readable output consumed by release guards.
type ProbeReport struct {
	Environment string                           `json:"environment"`
	Probes      []ProbeResult                    `json:"probes"`
	Errors      []string                         `json:"errors,omitempty"`
	Snapshot    observe.NormalizedMetricsSnapshot `json:"snapshot"`
	Metrics     map[string]float64               `json:"metrics"`
	GateStatus  observe.GateStatus               `json:"gate_status"`
}

// Passed reports whether probes and release gates both succeeded.
func (r ProbeReport) Passed() bool {
	if len(r.Errors) > 0 || !r.GateStatus.AllPassed {
		return false
	}
	for _, probe := range r.Probes {
		if !probe.Passed {
			return false
		}
	}
	return true
}

// Run executes the smoke + replay probes and validates the resulting normalized
// metrics with the environment-specific release gate profile.
func Run(ctx context.Context, env string) ProbeReport {
	report := ProbeReport{
		Environment: observe.NormalizeReleaseGateEnvironment(env),
	}
	observer := observe.NewMetricsObserver(nil)

	report.runProbe("runtime-smoke", "smoke", func() error {
		return runRuntimeSmokeProbe(ctx, observer)
	})
	report.runProbe("checkpoint-replay", "replay", func() error {
		return runCheckpointReplayProbe(ctx, observer)
	})

	report.Snapshot = observer.Snapshot()
	report.Metrics = report.Snapshot.Map()
	report.GateStatus = observe.NewReleaseGateMeterForEnvironment(report.Environment).ValidateSnapshot(report.Snapshot, report.Environment)
	return report
}

// RenderReport renders a human-readable summary for CI logs.
func RenderReport(report ProbeReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== Release Gate Probe ===\n")
	fmt.Fprintf(&b, "Environment: %s\n\n", report.Environment)

	if len(report.Probes) > 0 {
		b.WriteString("Probe Results:\n")
		for _, probe := range report.Probes {
			status := "✓"
			if !probe.Passed {
				status = "✗"
			}
			fmt.Fprintf(&b, "  %s %s (%s)", status, probe.Name, probe.Kind)
			if strings.TrimSpace(probe.Details) != "" {
				fmt.Fprintf(&b, ": %s", probe.Details)
			}
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	if len(report.Errors) > 0 {
		b.WriteString("Probe Errors:\n")
		for _, errText := range report.Errors {
			fmt.Fprintf(&b, "  - %s\n", errText)
		}
		b.WriteByte('\n')
	}

	b.WriteString(report.GateStatus.Report())
	return b.String()
}

func (r *ProbeReport) runProbe(name, kind string, fn func() error) {
	result := ProbeResult{Name: name, Kind: kind, Passed: true}
	if err := fn(); err != nil {
		result.Passed = false
		result.Details = err.Error()
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %v", name, err))
	} else {
		result.Details = "ok"
	}
	r.Probes = append(r.Probes, result)
}

func runRuntimeSmokeProbe(ctx context.Context, observer observe.Observer) error {
	workspaceDir, err := os.MkdirTemp("", "moss-releasegate-smoke-*")
	if err != nil {
		return fmt.Errorf("create smoke workspace: %w", err)
	}
	defer os.RemoveAll(workspaceDir)

	store, err := kruntime.NewSQLiteEventStore(":memory:")
	if err != nil {
		return fmt.Errorf("open in-memory event store: %w", err)
	}
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("I will inspect the smoke fixture.")},
					ToolCalls: []model.ToolCall{{
						ID:        "smoke-read",
						Name:      "read_file",
						Arguments: json.RawMessage(`{"path":"README.md"}`),
					}},
				},
				ToolCalls: []model.ToolCall{{
					ID:        "smoke-read",
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"README.md"}`),
				}},
				StopReason: "tool_use",
				Usage:      model.TokenUsage{PromptTokens: 8, CompletionTokens: 6, TotalTokens: 14},
			},
			{
				Message: model.Message{
					Role:         model.RoleAssistant,
					ContentParts: []model.ContentPart{model.TextPart("release gate smoke passed")},
				},
				StopReason: "end_turn",
				Usage:      model.TokenUsage{PromptTokens: 4, CompletionTokens: 5, TotalTokens: 9},
			},
		},
	}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&kernio.NoOpIO{}),
		kernel.WithObserver(observer),
		kernel.WithEventStore(store),
	)
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name:        "read_file",
		Description: "Read smoke fixture",
		Risk:        tool.RiskLow,
	}, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`"smoke fixture contents"`), nil
	})); err != nil {
		return fmt.Errorf("register smoke tool: %w", err)
	}

	bp, err := k.StartRuntimeSession(ctx, kruntime.RuntimeRequest{
		// The probe intentionally uses the default coding/workspace-write path so
		// release gates fail when canonical runtime request resolution regresses.
		PermissionProfile: "workspace-write",
		PromptPack:        "coding",
		Workspace:         workspaceDir,
		ModelProfile:      "gpt-5",
	})
	if err != nil {
		return fmt.Errorf("start runtime session: %w", err)
	}
	if strings.TrimSpace(bp.Identity.SessionID) == "" {
		return fmt.Errorf("runtime session resolved an empty session id")
	}

	userMsg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("Run the smoke probe")},
	}
	result, err := kernel.CollectRunAgentFromBlueprint(ctx, k, bp, nil, k.BuildLLMAgent("releasegate-smoke"), &userMsg, &kernio.NoOpIO{})
	if err != nil {
		return fmt.Errorf("run smoke blueprint: %w", err)
	}
	if result == nil || !result.Success {
		return fmt.Errorf("smoke run did not succeed: %+v", result)
	}
	if !strings.Contains(strings.ToLower(result.Output), "smoke passed") {
		return fmt.Errorf("unexpected smoke output %q", result.Output)
	}
	return nil
}

func runCheckpointReplayProbe(ctx context.Context, observer observe.Observer) error {
	baseDir, err := os.MkdirTemp("", "moss-releasegate-replay-*")
	if err != nil {
		return fmt.Errorf("create replay probe dir: %w", err)
	}
	defer os.RemoveAll(baseDir)

	sessionStore, err := session.NewFileStore(filepath.Join(baseDir, "sessions"))
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}
	checkpointStore, err := checkpoint.NewFileCheckpointStore(filepath.Join(baseDir, "checkpoints"))
	if err != nil {
		return fmt.Errorf("open checkpoint store: %w", err)
	}
	mock := &kt.MockLLM{
		Responses: []model.CompletionResponse{{
			Message: model.Message{
				Role:         model.RoleAssistant,
				ContentParts: []model.ContentPart{model.TextPart("replay flow resumed successfully")},
			},
			StopReason: "end_turn",
			Usage:      model.TokenUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
		}},
	}
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(&kernio.NoOpIO{}),
		kernel.WithObserver(observer),
		kernel.WithSessionStore(sessionStore),
		kernel.WithCheckpoints(checkpointStore),
	)

	source := &session.Session{
		ID:     "release-gate-source",
		Status: session.StatusRunning,
		Config: session.SessionConfig{
			Goal:     "validate replay flow",
			MaxSteps: 4,
		},
		Messages: []model.Message{
			{Role: model.RoleSystem, ContentParts: []model.ContentPart{model.TextPart("system prompt")}},
			{Role: model.RoleUser, ContentParts: []model.ContentPart{model.TextPart("initial request")}},
			{Role: model.RoleAssistant, ContentParts: []model.ContentPart{model.TextPart("intermediate answer")}},
		},
	}
	record, err := k.CreateCheckpoint(ctx, source, checkpoint.CheckpointCreateRequest{
		Note: "release gate replay probe",
	})
	if err != nil {
		return fmt.Errorf("create checkpoint: %w", err)
	}

	replayed, replayResult, err := k.ReplayFromCheckpoint(ctx, checkpoint.ReplayRequest{
		CheckpointID: record.ID,
		Mode:         checkpoint.ReplayModeRerun,
		Note:         "release gate replay probe",
	})
	if err != nil {
		return fmt.Errorf("replay checkpoint: %w", err)
	}
	if replayResult == nil || replayResult.Mode != checkpoint.ReplayModeRerun {
		return fmt.Errorf("unexpected replay result %+v", replayResult)
	}

	userMsg := model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart("continue after replay")},
	}
	replayed.AppendMessage(userMsg)
	result, err := kernel.CollectRunAgentResult(ctx, k, kernel.RunAgentRequest{
		Session:     replayed,
		Agent:       k.BuildLLMAgent("releasegate-replay"),
		UserContent: &userMsg,
		IO:          &kernio.NoOpIO{},
	})
	if err != nil {
		return fmt.Errorf("run replayed session: %w", err)
	}
	if result == nil || !result.Success {
		return fmt.Errorf("replay run did not succeed: %+v", result)
	}
	if !strings.Contains(strings.ToLower(result.Output), "replay") {
		return fmt.Errorf("unexpected replay output %q", result.Output)
	}
	return nil
}
