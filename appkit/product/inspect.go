package product

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appruntime "github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel/session"
)

type InspectReport struct {
	Mode      string                        `json:"mode"`
	Workspace string                        `json:"workspace"`
	Catalog   appruntime.StateCatalogHealth `json:"catalog"`
	SessionID string                        `json:"session_id,omitempty"`
	Items     []InspectStateItem            `json:"items,omitempty"`
	Run       *InspectRunReport             `json:"run,omitempty"`
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
	Data         map[string]any `json:"data,omitempty"`
}

func BuildInspectReport(ctx context.Context, workspace string, args []string) (InspectReport, error) {
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
	default:
		return InspectReport{}, fmt.Errorf("unknown inspect mode %q (supported: status, events, run)", mode)
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
				firstNonEmpty(item.SessionID, "(none)"),
				firstNonEmpty(item.Title, "(none)"),
				firstNonEmpty(item.Status, "(none)"),
				firstNonEmpty(item.Summary, "(none)"),
			)
		}
	case "run":
		if report.Run == nil {
			b.WriteString("Run: unavailable\n")
			return b.String()
		}
		run := report.Run
		fmt.Fprintf(&b, "Run session: %s\n", run.SessionID)
		fmt.Fprintf(&b, "Run id:      %s\n", firstNonEmpty(run.RunID, "(none)"))
		fmt.Fprintf(&b, "Turn id:     %s\n", firstNonEmpty(run.TurnID, "(none)"))
		if run.TurnPlan != nil {
			fmt.Fprintf(
				&b,
				"Turn plan:   iteration=%d profile=%s lane=%s lightweight=%t visible=%d hidden=%d approval=%d\n",
				run.TurnPlan.Iteration,
				firstNonEmpty(run.TurnPlan.InstructionProfile, "(default)"),
				firstNonEmpty(run.TurnPlan.ModelLane, "(default)"),
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
				firstNonEmpty(run.ModelRoute.ConfiguredModel, "(default)"),
				firstNonEmpty(run.ModelRoute.Lane, "(default)"),
				run.ModelRoute.PreferCheap,
				run.ModelRoute.MaxCostTier,
				firstNonEmpty(strings.Join(run.ModelRoute.ReasonCodes, ","), "(none)"),
				firstNonEmpty(strings.Join(run.ModelRoute.Capabilities, ","), "(none)"),
			)
		}
		if run.ToolRoute != nil {
			fmt.Fprintf(
				&b,
				"Tool route:  visible=%s hidden=%s approval=%s digest=%s\n",
				firstNonEmpty(strings.Join(run.ToolRoute.VisibleTools, ","), "(none)"),
				firstNonEmpty(strings.Join(run.ToolRoute.HiddenTools, ","), "(none)"),
				firstNonEmpty(strings.Join(run.ToolRoute.ApprovalTools, ","), "(none)"),
				firstNonEmpty(run.ToolRoute.RouteDigest, "(none)"),
			)
			if len(run.ToolRoute.Decisions) > 0 {
				b.WriteString("Tool decisions:\n")
				for _, decision := range run.ToolRoute.Decisions {
					fmt.Fprintf(
						&b,
						"- %s | status=%s | source=%s | owner=%s | risk=%s | reasons=%s\n",
						decision.Name,
						firstNonEmpty(decision.Status, "(none)"),
						firstNonEmpty(decision.Source, "(none)"),
						firstNonEmpty(decision.Owner, "(none)"),
						firstNonEmpty(decision.Risk, "(none)"),
						firstNonEmpty(strings.Join(decision.ReasonCodes, ","), "(none)"),
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
					firstNonEmpty(item.CandidateModel, "(none)"),
					item.AttemptIndex,
					item.CandidateRetry,
					firstNonEmpty(item.Outcome, "(none)"),
					firstNonEmpty(item.BreakerState, "(none)"),
					firstNonEmpty(item.FailoverTo, "(none)"),
					firstNonEmpty(item.FailureReason, "(none)"),
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
					firstNonEmpty(item.Status, "(none)"),
					firstNonEmpty(item.Title, "(none)"),
					firstNonEmpty(item.Summary, "(none)"),
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
					Iteration:          intValue(event.Data, "iteration"),
					InstructionProfile: stringData(event.Data, "instruction_profile"),
					LightweightChat:    boolValue(event.Data, "lightweight_chat"),
					ModelLane:          stringData(event.Data, "model_lane"),
					VisibleToolsCount:  intValue(event.Data, "visible_tools_count"),
					HiddenToolsCount:   intValue(event.Data, "hidden_tools_count"),
					ApprovalToolsCount: intValue(event.Data, "approval_tools_count"),
				}
			}
		case "tool.route_planned":
			if report.ToolRoute == nil {
				report.ToolRoute = &InspectToolRoute{
					VisibleTools:  stringSliceFromData(event.Data, "visible_tools"),
					HiddenTools:   stringSliceFromData(event.Data, "hidden_tools"),
					ApprovalTools: stringSliceFromData(event.Data, "approval_tools"),
					RouteDigest:   stringData(event.Data, "route_digest"),
					Decisions:     toolDecisionsFromData(event.Data["decisions"]),
				}
			}
		case "model.route_planned":
			if report.ModelRoute == nil {
				report.ModelRoute = &InspectModelRoute{
					ConfiguredModel: event.Model,
					Lane:            stringData(event.Data, "lane"),
					ReasonCodes:     stringSliceFromData(event.Data, "reason_codes"),
					Capabilities:    stringSliceFromData(event.Data, "capabilities"),
					MaxCostTier:     intValue(event.Data, "max_cost_tier"),
					PreferCheap:     boolValue(event.Data, "prefer_cheap"),
				}
			}
		case "llm_failover_attempt":
			report.Failovers = append(report.Failovers, InspectFailover{
				CandidateModel: stringData(event.Data, "candidate_model"),
				AttemptIndex:   intValue(event.Data, "attempt_index"),
				CandidateRetry: intValue(event.Data, "candidate_retry"),
				BreakerState:   stringData(event.Data, "breaker_state"),
				FailoverTo:     stringData(event.Data, "failover_to"),
				Outcome:        stringData(event.Data, "outcome"),
				FailureReason:  stringData(event.Data, "failure_reason"),
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
		Data:         cloneTraceData(meta.Data),
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
