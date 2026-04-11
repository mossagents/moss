package product

import (
	"github.com/mossagents/moss/internal/strutil"
	"context"
	"encoding/json"
	"fmt"
	appruntime "github.com/mossagents/moss/appkit/runtime"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
	"sort"
	"strconv"
	"strings"
	"time"
)

type InspectReport struct {
	Mode         string                        `json:"mode"`
	Workspace    string                        `json:"workspace"`
	Catalog      appruntime.StateCatalogHealth `json:"catalog"`
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

type InspectRunReport struct {
	SessionID  string             `json:"session_id"`
	RunID      string             `json:"run_id,omitempty"`
	TurnID     string             `json:"turn_id,omitempty"`
	TurnPlan   *InspectTurnPlan   `json:"turn_plan,omitempty"`
	ToolRoute  *InspectToolRoute  `json:"tool_route,omitempty"`
	ModelRoute *InspectModelRoute `json:"model_route,omitempty"`
	Failovers  []InspectFailover  `json:"failovers,omitempty"`
	Changes    []InspectStateItem `json:"changes,omitempty"`
	Events     []TraceEvent       `json:"events,omitempty"`
}

type InspectTurnPlan struct {
	Iteration          int    `json:"iteration,omitempty"`
	InstructionProfile string `json:"instruction_profile,omitempty"`
	LightweightChat    bool   `json:"lightweight_chat,omitempty"`
	ModelLane          string `json:"model_lane,omitempty"`
	VisibleToolsCount  int    `json:"visible_tools_count,omitempty"`
	HiddenToolsCount   int    `json:"hidden_tools_count,omitempty"`
	ApprovalToolsCount int    `json:"approval_tools_count,omitempty"`
}

type InspectToolRoute struct {
	VisibleTools  []string              `json:"visible_tools,omitempty"`
	HiddenTools   []string              `json:"hidden_tools,omitempty"`
	ApprovalTools []string              `json:"approval_tools,omitempty"`
	RouteDigest   string                `json:"route_digest,omitempty"`
	Decisions     []InspectToolDecision `json:"decisions,omitempty"`
}

type InspectToolDecision struct {
	Name         string   `json:"name"`
	Status       string   `json:"status,omitempty"`
	Source       string   `json:"source,omitempty"`
	Owner        string   `json:"owner,omitempty"`
	Risk         string   `json:"risk,omitempty"`
	ReasonCodes  []string `json:"reason_codes,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type InspectModelRoute struct {
	ConfiguredModel string   `json:"configured_model,omitempty"`
	Lane            string   `json:"lane,omitempty"`
	ReasonCodes     []string `json:"reason_codes,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	MaxCostTier     int      `json:"max_cost_tier,omitempty"`
	PreferCheap     bool     `json:"prefer_cheap,omitempty"`
}

type InspectFailover struct {
	CandidateModel string `json:"candidate_model,omitempty"`
	AttemptIndex   int    `json:"attempt_index,omitempty"`
	CandidateRetry int    `json:"candidate_retry,omitempty"`
	BreakerState   string `json:"breaker_state,omitempty"`
	FailoverTo     string `json:"failover_to,omitempty"`
	Outcome        string `json:"outcome,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
}

type inspectExecutionMetadata struct {
	EventID      string         `json:"event_id,omitempty"`
	EventVersion int            `json:"event_version,omitempty"`
	RunID        string         `json:"run_id,omitempty"`
	TurnID       string         `json:"turn_id,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	EventType    string         `json:"event_type,omitempty"`
	Phase        string         `json:"phase,omitempty"`
	Actor        string         `json:"actor,omitempty"`
	PayloadKind  string         `json:"payload_kind,omitempty"`
	ToolName     string         `json:"tool_name,omitempty"`
	Model        string         `json:"model,omitempty"`
	Risk         string         `json:"risk,omitempty"`
	ReasonCode   string         `json:"reason_code,omitempty"`
	Enforcement  string         `json:"enforcement,omitempty"`
	DurationMS   int64          `json:"duration_ms,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

func BuildInspectReport(ctx context.Context, workspace string, args []string) (InspectReport, error) {
	return BuildInspectReportForTrust(ctx, workspace, appconfig.TrustTrusted, args)
}

func BuildInspectReportForTrust(ctx context.Context, workspace, trust string, args []string) (InspectReport, error) {
	mode := "status"
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		mode = strings.ToLower(strings.TrimSpace(args[0]))
	}
	catalog, err := appruntime.NewStateCatalog(StateStoreDir(), StateEventDir(), StateCatalogEnabled())
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
		page, err := catalog.Query(appruntime.StateQuery{Limit: inspectLimit(args, 1, 20)})
		if err != nil {
			return InspectReport{}, err
		}
		report.Items = inspectStateItems(page.Items)
		return report, nil
	case "events":
		limit, text := inspectLimitAndText(args[1:], 20)
		page, err := catalog.Query(appruntime.StateQuery{
			Kinds: []appruntime.StateKind{appruntime.StateKindExecutionEvent},
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
		page, err := catalog.Query(appruntime.StateQuery{
			Kinds:     []appruntime.StateKind{appruntime.StateKindExecutionEvent},
			SessionID: sessionID,
			Limit:     limit,
		})
		if err != nil {
			return InspectReport{}, err
		}
		run := buildInspectRun(page.Items, sessionID)
		changePage, err := catalog.Query(appruntime.StateQuery{
			Kinds:     []appruntime.StateKind{appruntime.StateKindChange},
			SessionID: sessionID,
			Limit:     10,
		})
		if err == nil {
			run.Changes = inspectStateItems(changePage.Items)
		}
		report.Run = &run
		return report, nil
	case "threads":
		threads, err := buildInspectThreads(ctx, workspace, inspectLimit(args, 1, 20))
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
		thread, err := buildInspectThread(ctx, workspace, catalog, target)
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
		replay, err := buildInspectReplay(ctx, workspace, catalog, target)
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
		compare, err := buildInspectCompare(ctx, catalog, strings.TrimSpace(args[1]), strings.TrimSpace(args[2]))
		if err != nil {
			return InspectReport{}, err
		}
		report.SessionID = strutil.FirstNonEmpty(compare.Left.SessionID, compare.Right.SessionID)
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
				strutil.FirstNonEmpty(item.SessionID, "(none)"),
				strutil.FirstNonEmpty(item.Title, "(none)"),
				strutil.FirstNonEmpty(item.Status, "(none)"),
				strutil.FirstNonEmpty(item.Summary, "(none)"),
			)
		}
	case "run":
		if report.Run == nil {
			b.WriteString("Run: unavailable\n")
			return b.String()
		}
		run := report.Run
		fmt.Fprintf(&b, "Run session: %s\n", run.SessionID)
		fmt.Fprintf(&b, "Run id:      %s\n", strutil.FirstNonEmpty(run.RunID, "(none)"))
		fmt.Fprintf(&b, "Turn id:     %s\n", strutil.FirstNonEmpty(run.TurnID, "(none)"))
		if run.TurnPlan != nil {
			fmt.Fprintf(
				&b,
				"Turn plan:   iteration=%d profile=%s lane=%s lightweight=%t visible=%d hidden=%d approval=%d\n",
				run.TurnPlan.Iteration,
				strutil.FirstNonEmpty(run.TurnPlan.InstructionProfile, "(default)"),
				strutil.FirstNonEmpty(run.TurnPlan.ModelLane, "(default)"),
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
				strutil.FirstNonEmpty(run.ModelRoute.ConfiguredModel, "(default)"),
				strutil.FirstNonEmpty(run.ModelRoute.Lane, "(default)"),
				run.ModelRoute.PreferCheap,
				run.ModelRoute.MaxCostTier,
				strutil.FirstNonEmpty(strings.Join(run.ModelRoute.ReasonCodes, ","), "(none)"),
				strutil.FirstNonEmpty(strings.Join(run.ModelRoute.Capabilities, ","), "(none)"),
			)
		}
		if run.ToolRoute != nil {
			fmt.Fprintf(
				&b,
				"Tool route:  visible=%s hidden=%s approval=%s digest=%s\n",
				strutil.FirstNonEmpty(strings.Join(run.ToolRoute.VisibleTools, ","), "(none)"),
				strutil.FirstNonEmpty(strings.Join(run.ToolRoute.HiddenTools, ","), "(none)"),
				strutil.FirstNonEmpty(strings.Join(run.ToolRoute.ApprovalTools, ","), "(none)"),
				strutil.FirstNonEmpty(run.ToolRoute.RouteDigest, "(none)"),
			)
			if len(run.ToolRoute.Decisions) > 0 {
				b.WriteString("Tool decisions:\n")
				for _, decision := range run.ToolRoute.Decisions {
					fmt.Fprintf(
						&b,
						"- %s | status=%s | source=%s | owner=%s | risk=%s | reasons=%s\n",
						decision.Name,
						strutil.FirstNonEmpty(decision.Status, "(none)"),
						strutil.FirstNonEmpty(decision.Source, "(none)"),
						strutil.FirstNonEmpty(decision.Owner, "(none)"),
						strutil.FirstNonEmpty(decision.Risk, "(none)"),
						strutil.FirstNonEmpty(strings.Join(decision.ReasonCodes, ","), "(none)"),
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
					strutil.FirstNonEmpty(item.CandidateModel, "(none)"),
					item.AttemptIndex,
					item.CandidateRetry,
					strutil.FirstNonEmpty(item.Outcome, "(none)"),
					strutil.FirstNonEmpty(item.BreakerState, "(none)"),
					strutil.FirstNonEmpty(item.FailoverTo, "(none)"),
					strutil.FirstNonEmpty(item.FailureReason, "(none)"),
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
					strutil.FirstNonEmpty(item.Status, "(none)"),
					strutil.FirstNonEmpty(item.Title, "(none)"),
					strutil.FirstNonEmpty(item.Summary, "(none)"),
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
				strutil.FirstNonEmpty(item.Source, "(none)"),
				strutil.FirstNonEmpty(item.ParentID, "(none)"),
				strutil.FirstNonEmpty(item.TaskID, "(none)"),
				item.CheckpointCount,
				item.ChangeCount,
				item.TaskCount,
				item.Archived,
				strutil.FirstNonEmpty(item.UpdatedAt, "(none)"),
				strutil.FirstNonEmpty(item.Preview, "(none)"),
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

func resolveInspectSessionID(ctx context.Context, catalog *appruntime.StateCatalog, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "latest") {
		store, err := session.NewFileStore(SessionStoreDir())
		if err == nil {
			summaries, err := store.List(ctx)
			if err == nil && len(summaries) > 0 {
				return summaries[0].ID, nil
			}
		}
		if catalog != nil && catalog.Enabled() {
			page, err := catalog.Query(appruntime.StateQuery{
				Kinds: []appruntime.StateKind{appruntime.StateKindExecutionEvent},
				Limit: 1,
			})
			if err == nil && len(page.Items) > 0 && strings.TrimSpace(page.Items[0].SessionID) != "" {
				return strings.TrimSpace(page.Items[0].SessionID), nil
			}
		}
		return "", fmt.Errorf("no session is available for inspect run")
	}
	return target, nil
}

func inspectStateItems(entries []appruntime.StateEntry) []InspectStateItem {
	items := make([]InspectStateItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, InspectStateItem{
			Kind:      string(entry.Kind),
			RecordID:  entry.RecordID,
			SessionID: entry.SessionID,
			Status:    entry.Status,
			Title:     entry.Title,
			Summary:   entry.Summary,
			SortTime:  entry.SortTime,
		})
	}
	return items
}

func buildInspectRun(entries []appruntime.StateEntry, sessionID string) InspectRunReport {
	report := InspectRunReport{SessionID: sessionID}
	for _, entry := range entries {
		event, ok := decodeInspectTraceEvent(entry)
		if !ok {
			continue
		}
		if report.RunID == "" {
			report.RunID = strings.TrimSpace(event.RunID)
		}
		if report.TurnID == "" {
			report.TurnID = strings.TrimSpace(event.TurnID)
		}
		report.Events = append(report.Events, event)
		switch event.Type {
		case "turn.plan_prepared":
			if report.TurnPlan == nil {
				report.TurnPlan = &InspectTurnPlan{
					Iteration:          intValue(event.Metadata, "iteration"),
					InstructionProfile: stringData(event.Metadata, "instruction_profile"),
					LightweightChat:    boolValue(event.Metadata, "lightweight_chat"),
					ModelLane:          stringData(event.Metadata, "model_lane"),
					VisibleToolsCount:  intValue(event.Metadata, "visible_tools_count"),
					HiddenToolsCount:   intValue(event.Metadata, "hidden_tools_count"),
					ApprovalToolsCount: intValue(event.Metadata, "approval_tools_count"),
				}
			}
		case "tool.route_planned":
			if report.ToolRoute == nil {
				report.ToolRoute = &InspectToolRoute{
					VisibleTools:  stringSliceFromData(event.Metadata, "visible_tools"),
					HiddenTools:   stringSliceFromData(event.Metadata, "hidden_tools"),
					ApprovalTools: stringSliceFromData(event.Metadata, "approval_tools"),
					RouteDigest:   stringData(event.Metadata, "route_digest"),
					Decisions:     toolDecisionsFromData(event.Metadata["decisions"]),
				}
			}
		case "model.route_planned":
			if report.ModelRoute == nil {
				report.ModelRoute = &InspectModelRoute{
					ConfiguredModel: event.Model,
					Lane:            stringData(event.Metadata, "lane"),
					ReasonCodes:     stringSliceFromData(event.Metadata, "reason_codes"),
					Capabilities:    stringSliceFromData(event.Metadata, "capabilities"),
					MaxCostTier:     intValue(event.Metadata, "max_cost_tier"),
					PreferCheap:     boolValue(event.Metadata, "prefer_cheap"),
				}
			}
		case "llm_failover_attempt":
			report.Failovers = append(report.Failovers, InspectFailover{
				CandidateModel: stringData(event.Metadata, "candidate_model"),
				AttemptIndex:   intValue(event.Metadata, "attempt_index"),
				CandidateRetry: intValue(event.Metadata, "candidate_retry"),
				BreakerState:   stringData(event.Metadata, "breaker_state"),
				FailoverTo:     stringData(event.Metadata, "failover_to"),
				Outcome:        stringData(event.Metadata, "outcome"),
				FailureReason:  stringData(event.Metadata, "failure_reason"),
			})
		}
	}
	sort.SliceStable(report.Failovers, func(i, j int) bool {
		if report.Failovers[i].AttemptIndex == report.Failovers[j].AttemptIndex {
			return report.Failovers[i].CandidateRetry < report.Failovers[j].CandidateRetry
		}
		return report.Failovers[i].AttemptIndex < report.Failovers[j].AttemptIndex
	})
	return report
}

func decodeInspectTraceEvent(entry appruntime.StateEntry) (TraceEvent, bool) {
	if entry.Kind != appruntime.StateKindExecutionEvent {
		return TraceEvent{}, false
	}
	var meta inspectExecutionMetadata
	if len(entry.Metadata) > 0 {
		if err := json.Unmarshal(entry.Metadata, &meta); err != nil {
			return TraceEvent{}, false
		}
	}
	return TraceEvent{
		EventID:      meta.EventID,
		EventVersion: meta.EventVersion,
		RunID:        meta.RunID,
		TurnID:       meta.TurnID,
		ParentID:     meta.ParentID,
		Kind:         "execution_event",
		Timestamp:    entry.SortTime.UTC(),
		SessionID:    entry.SessionID,
		Phase:        meta.Phase,
		Actor:        meta.Actor,
		PayloadKind:  meta.PayloadKind,
		Type:         meta.EventType,
		Model:        meta.Model,
		ToolName:     meta.ToolName,
		DurationMS:   meta.DurationMS,
		Error:        entry.Status,
		Metadata:         cloneTraceMetadata(meta.Metadata),
	}, true
}

func boolValue(data map[string]any, key string) bool {
	value, _ := boolData(data, key)
	return value
}

func stringSliceFromData(data map[string]any, key string) []string {
	if len(data) == 0 {
		return nil
	}
	return stringSliceValue(data[key])
}

func stringSliceValue(value any) []string {
	switch items := value.(type) {
	case []string:
		return compactStringSlice(items)
	case []any:
		return stringifySlice(items)
	default:
		return nil
	}
}

func toolDecisionsFromData(value any) []InspectToolDecision {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	decisions := make([]InspectToolDecision, 0, len(items))
	for _, raw := range items {
		obj, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		decisions = append(decisions, InspectToolDecision{
			Name:         stringData(obj, "name"),
			Status:       stringData(obj, "status"),
			Source:       stringData(obj, "source"),
			Owner:        stringData(obj, "owner"),
			Risk:         stringData(obj, "risk"),
			ReasonCodes:  stringSliceFromData(obj, "reason_codes"),
			Capabilities: stringSliceFromData(obj, "capabilities"),
		})
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		return decisions[i].Name < decisions[j].Name
	})
	return decisions
}
