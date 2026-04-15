package product

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/internal/stringutil"
	rstate "github.com/mossagents/moss/runtime/state"
)

type InspectReport struct {
	Mode         string                        `json:"mode"`
	Workspace    string                        `json:"workspace"`
	Catalog      rstate.StateCatalogHealth `json:"catalog"`
	SessionID    string                        `json:"session_id,omitempty"`
	Items        []InspectStateItem            `json:"items,omitempty"`
	Run          *InspectRunReport             `json:"run,omitempty"`
	Threads      []InspectThreadSummary        `json:"threads,omitempty"`
	Thread       *InspectThreadReport          `json:"thread,omitempty"`
	Prompt       *InspectPromptReport          `json:"prompt,omitempty"`
	Capabilities *InspectCapabilityReport      `json:"capabilities,omitempty"`
	Replay       *InspectReplayReport          `json:"replay,omitempty"`
	Compare      *InspectCompareReport         `json:"compare,omitempty"`
	Governance   *InspectGovernanceReport      `json:"governance,omitempty"`
}

type InspectStateItem struct {
	Kind      string    `json:"kind"`
	RecordID  string    `json:"record_id"`
	SessionID string    `json:"session_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	Title     string    `json:"title,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	SortTime  time.Time `json:"sort_time"`
}

func BuildInspectReport(ctx context.Context, workspace string, args []string) (InspectReport, error) {
	return BuildInspectReportForTrust(ctx, workspace, appconfig.TrustTrusted, args)
}

func BuildInspectReportForTrust(ctx context.Context, workspace, trust string, args []string) (InspectReport, error) {
	mode := "status"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}
	catalog, err := OpenStateCatalog()
	if err != nil {
		return InspectReport{}, err
	}
	changeStore, err := OpenChangeStore()
	if err != nil {
		return InspectReport{}, err
	}
	report := InspectReport{
		Mode:      mode,
		Workspace: workspace,
		Catalog:   catalog.Health(),
	}
	switch mode {
	case "status":
		page, err := catalog.Query(rstate.StateQuery{Limit: inspectLimit(args, 1, 20)})
		if err != nil {
			return InspectReport{}, err
		}
		report.Items = inspectStateItems(page.Items)
		return report, nil
	case "events":
		limit, text := inspectLimitAndText(args[1:], 20)
		page, err := catalog.Query(rstate.StateQuery{
			Kinds: []rstate.StateKind{rstate.StateKindExecutionEvent},
			Limit: limit,
			Text:  text,
		})
		if err != nil {
			return InspectReport{}, err
		}
		report.Items = inspectStateItems(page.Items)
		return report, nil
	case "run":
		target := "latest"
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			target = strings.TrimSpace(args[1])
		}
		limit := inspectLimit(args, 2, 40)
		sessionID, err := resolveInspectSessionID(ctx, catalog, target)
		if err != nil {
			return InspectReport{}, err
		}
		report.SessionID = sessionID
		page, err := catalog.Query(rstate.StateQuery{
			Kinds:     []rstate.StateKind{rstate.StateKindExecutionEvent},
			SessionID: sessionID,
			Limit:     limit,
		})
		if err != nil {
			return InspectReport{}, err
		}
		run := buildInspectRun(page.Items, sessionID)
		run.Changes = inspectChangesForSession(ctx, changeStore, sessionID, 10)
		report.Run = &run
		return report, nil
	case "threads":
		threads, err := buildInspectThreads(ctx, workspace, catalog, changeStore, inspectLimit(args, 1, 20))
		if err != nil {
			return InspectReport{}, err
		}
		report.Threads = threads
		return report, nil
	case "thread":
		target := "latest"
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			target = strings.TrimSpace(args[1])
		}
		thread, err := buildInspectThread(ctx, workspace, catalog, changeStore, target)
		if err != nil {
			return InspectReport{}, err
		}
		report.SessionID = thread.Summary.ID
		report.Thread = thread
		return report, nil
	case "prompt":
		target := "latest"
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			target = strings.TrimSpace(args[1])
		}
		prompt, err := buildInspectPrompt(ctx, target)
		if err != nil {
			return InspectReport{}, err
		}
		report.SessionID = prompt.SessionID
		report.Prompt = prompt
		return report, nil
	case "capabilities":
		capabilities, err := buildInspectCapabilities(workspace, trust)
		if err != nil {
			return InspectReport{}, err
		}
		report.Capabilities = capabilities
		return report, nil
	case "replay":
		target := "latest"
		if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
			target = strings.TrimSpace(args[1])
		}
		replay, err := buildInspectReplay(ctx, workspace, catalog, changeStore, target)
		if err != nil {
			return InspectReport{}, err
		}
		report.SessionID = replay.SessionID
		report.Replay = replay
		return report, nil
	case "compare":
		if len(args) < 3 {
			return InspectReport{}, fmt.Errorf("inspect compare requires two selectors")
		}
		compare, err := buildInspectCompare(ctx, catalog, changeStore, strings.TrimSpace(args[1]), strings.TrimSpace(args[2]))
		if err != nil {
			return InspectReport{}, err
		}
		report.SessionID = stringutil.FirstNonEmpty(compare.Left.SessionID, compare.Right.SessionID)
		report.Compare = compare
		return report, nil
	case "governance":
		governance, err := buildInspectGovernance(ctx, workspace, catalog, inspectLimit(args, 1, 200))
		if err != nil {
			return InspectReport{}, err
		}
		report.Governance = governance
		return report, nil
	default:
		return InspectReport{}, fmt.Errorf("unknown inspect mode %q (supported: status, events, run, threads, thread, prompt, capabilities, replay, compare, governance)", mode)
	}
}

func RenderInspectReport(report InspectReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "moss inspect (%s)\n", report.Mode)
	fmt.Fprintf(
		&b,
		"State catalog: enabled=%t ready=%t entries=%d degraded=%t\n",
		report.Catalog.Enabled,
		report.Catalog.Ready,
		report.Catalog.Entries,
		report.Catalog.Degraded,
	)
	switch report.Mode {
	case "status", "events":
		if len(report.Items) == 0 {
			b.WriteString("Items: none\n")
			return b.String()
		}
		label := "Items"
		if report.Mode == "events" {
			label = "Events"
		}
		fmt.Fprintf(&b, "%s:\n", label)
		for _, item := range report.Items {
			fmt.Fprintf(
				&b,
				"- %s | kind=%s | id=%s | session=%s | title=%s | status=%s | summary=%s\n",
				item.SortTime.UTC().Format(time.RFC3339),
				item.Kind,
				item.RecordID,
				stringutil.FirstNonEmpty(item.SessionID, "(none)"),
				stringutil.FirstNonEmpty(item.Title, "(none)"),
				stringutil.FirstNonEmpty(item.Status, "(none)"),
				stringutil.FirstNonEmpty(item.Summary, "(none)"),
			)
		}
	case "run":
		if report.Run == nil {
			b.WriteString("Run: unavailable\n")
			return b.String()
		}
		run := report.Run
		fmt.Fprintf(&b, "Run session: %s\n", run.SessionID)
		fmt.Fprintf(&b, "Run id:      %s\n", stringutil.FirstNonEmpty(run.RunID, "(none)"))
		fmt.Fprintf(&b, "Turn id:     %s\n", stringutil.FirstNonEmpty(run.TurnID, "(none)"))
		if run.TurnPlan != nil {
			fmt.Fprintf(
				&b,
				"Turn plan:   iteration=%d profile=%s lane=%s lightweight=%t visible=%d hidden=%d approval=%d\n",
				run.TurnPlan.Iteration,
				stringutil.FirstNonEmpty(run.TurnPlan.InstructionProfile, "(default)"),
				stringutil.FirstNonEmpty(run.TurnPlan.ModelLane, "(default)"),
				run.TurnPlan.LightweightChat,
				run.TurnPlan.VisibleToolsCount,
				run.TurnPlan.HiddenToolsCount,
				run.TurnPlan.ApprovalToolsCount,
			)
		}
		if run.ModelRoute != nil {
			fmt.Fprintf(
				&b,
				"Model route: configured=%s lane=%s prefer_cheap=%t max_cost_tier=%d reasons=%s capabilities=%s\n",
				stringutil.FirstNonEmpty(run.ModelRoute.ConfiguredModel, "(default)"),
				stringutil.FirstNonEmpty(run.ModelRoute.Lane, "(default)"),
				run.ModelRoute.PreferCheap,
				run.ModelRoute.MaxCostTier,
				stringutil.FirstNonEmpty(strings.Join(run.ModelRoute.ReasonCodes, ","), "(none)"),
				stringutil.FirstNonEmpty(strings.Join(run.ModelRoute.Capabilities, ","), "(none)"),
			)
		}
		if run.ToolRoute != nil {
			fmt.Fprintf(
				&b,
				"Tool route:  visible=%s hidden=%s approval=%s digest=%s\n",
				stringutil.FirstNonEmpty(strings.Join(run.ToolRoute.VisibleTools, ","), "(none)"),
				stringutil.FirstNonEmpty(strings.Join(run.ToolRoute.HiddenTools, ","), "(none)"),
				stringutil.FirstNonEmpty(strings.Join(run.ToolRoute.ApprovalTools, ","), "(none)"),
				stringutil.FirstNonEmpty(run.ToolRoute.RouteDigest, "(none)"),
			)
			if len(run.ToolRoute.Decisions) > 0 {
				b.WriteString("Tool decisions:\n")
				for _, decision := range run.ToolRoute.Decisions {
					fmt.Fprintf(
						&b,
						"- %s | status=%s | source=%s | owner=%s | risk=%s | reasons=%s\n",
						decision.Name,
						stringutil.FirstNonEmpty(decision.Status, "(none)"),
						stringutil.FirstNonEmpty(decision.Source, "(none)"),
						stringutil.FirstNonEmpty(decision.Owner, "(none)"),
						stringutil.FirstNonEmpty(decision.Risk, "(none)"),
						stringutil.FirstNonEmpty(strings.Join(decision.ReasonCodes, ","), "(none)"),
					)
				}
			}
		}
		if len(run.Failovers) == 0 {
			b.WriteString("Failover:    none\n")
		} else {
			b.WriteString("Failover:\n")
			for _, item := range run.Failovers {
				fmt.Fprintf(
					&b,
					"- model=%s attempt=%d retry=%d outcome=%s breaker=%s next=%s reason=%s\n",
					stringutil.FirstNonEmpty(item.CandidateModel, "(none)"),
					item.AttemptIndex,
					item.CandidateRetry,
					stringutil.FirstNonEmpty(item.Outcome, "(none)"),
					stringutil.FirstNonEmpty(item.BreakerState, "(none)"),
					stringutil.FirstNonEmpty(item.FailoverTo, "(none)"),
					stringutil.FirstNonEmpty(item.FailureReason, "(none)"),
				)
			}
		}
		if len(run.Changes) == 0 {
			b.WriteString("Changes:     none\n")
		} else {
			b.WriteString("Changes:\n")
			for _, item := range run.Changes {
				fmt.Fprintf(
					&b,
					"- %s | id=%s | status=%s | title=%s | summary=%s\n",
					item.SortTime.UTC().Format(time.RFC3339),
					item.RecordID,
					stringutil.FirstNonEmpty(item.Status, "(none)"),
					stringutil.FirstNonEmpty(item.Title, "(none)"),
					stringutil.FirstNonEmpty(item.Summary, "(none)"),
				)
			}
		}
		if len(run.Events) == 0 {
			b.WriteString("Events:      none\n")
		} else {
			b.WriteString("Recent events:\n")
			for idx, event := range run.Events {
				fmt.Fprintf(&b, "%02d. %s\n", idx+1, formatTraceEventDetail(event))
			}
		}
	case "threads":
		if len(report.Threads) == 0 {
			b.WriteString("Threads: none\n")
			return b.String()
		}
		b.WriteString("Threads:\n")
		for _, item := range report.Threads {
			fmt.Fprintf(&b, "- %s | status=%s | recoverable=%t | source=%s | parent=%s | task=%s | checkpoints=%d | changes=%d | tasks=%d | archived=%t | updated=%s | preview=%s\n",
				item.ID,
				item.Status,
				item.Recoverable,
				stringutil.FirstNonEmpty(item.Source, "(none)"),
				stringutil.FirstNonEmpty(item.ParentID, "(none)"),
				stringutil.FirstNonEmpty(item.TaskID, "(none)"),
				item.CheckpointCount,
				item.ChangeCount,
				item.TaskCount,
				item.Archived,
				stringutil.FirstNonEmpty(item.UpdatedAt, "(none)"),
				stringutil.FirstNonEmpty(item.Preview, "(none)"),
			)
		}
	case "thread":
		if report.Thread == nil {
			b.WriteString("Thread: unavailable\n")
			return b.String()
		}
		renderInspectThreadReport(&b, *report.Thread)
	case "prompt":
		if report.Prompt == nil {
			b.WriteString("Prompt: unavailable\n")
			return b.String()
		}
		renderInspectPromptReport(&b, *report.Prompt)
	case "capabilities":
		if report.Capabilities == nil {
			b.WriteString("Capabilities: unavailable\n")
			return b.String()
		}
		renderInspectCapabilityReport(&b, *report.Capabilities)
	case "replay":
		if report.Replay == nil {
			b.WriteString("Replay: unavailable\n")
			return b.String()
		}
		renderInspectReplayReport(&b, *report.Replay)
	case "compare":
		if report.Compare == nil {
			b.WriteString("Compare: unavailable\n")
			return b.String()
		}
		renderInspectCompareReport(&b, *report.Compare)
	case "governance":
		if report.Governance == nil {
			b.WriteString("Governance: unavailable\n")
			return b.String()
		}
		renderInspectGovernanceReport(&b, *report.Governance)
	}
	return b.String()
}

func inspectLimit(args []string, index, fallback int) int {
	if len(args) <= index {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(args[index]))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func inspectLimitAndText(args []string, fallback int) (int, string) {
	if len(args) == 0 {
		return fallback, ""
	}
	if v, err := strconv.Atoi(strings.TrimSpace(args[0])); err == nil && v > 0 {
		return v, strings.TrimSpace(strings.Join(args[1:], " "))
	}
	return fallback, strings.TrimSpace(strings.Join(args, " "))
}
