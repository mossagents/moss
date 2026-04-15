package product

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mossagents/moss/internal/stringutil"
	rstate "github.com/mossagents/moss/runtime/state"
)

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
	Metadata     map[string]any `json:"data,omitempty"`
}

func resolveInspectSessionID(ctx context.Context, catalog *rstate.StateCatalog, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" || strings.EqualFold(target, "latest") {
		store, err := OpenSessionStore()
		if err == nil {
			summaries, err := store.List(ctx)
			if err == nil && len(summaries) > 0 {
				return summaries[0].ID, nil
			}
		}
		if catalog != nil && catalog.Enabled() {
			page, err := catalog.Query(rstate.StateQuery{
				Kinds: []rstate.StateKind{rstate.StateKindExecutionEvent},
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

func inspectStateItems(entries []rstate.StateEntry) []InspectStateItem {
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

func inspectChangesForSession(ctx context.Context, store *FileChangeStore, sessionID string, limit int) []InspectStateItem {
	if store == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	items, err := store.ListBySession(ctx, sessionID, limit)
	if err != nil {
		return nil
	}
	return inspectChangeItems(items)
}

func inspectChangeItems(items []ChangeOperation) []InspectStateItem {
	out := make([]InspectStateItem, 0, len(items))
	for _, item := range items {
		out = append(out, InspectStateItem{
			Kind:      "change",
			RecordID:  item.ID,
			SessionID: strings.TrimSpace(item.SessionID),
			Status:    string(item.Status),
			Title:     stringutil.FirstNonEmpty(strings.TrimSpace(item.Summary), item.ID),
			Summary:   strings.Join(compactStrings(item.TargetFiles), ", "),
			SortTime:  changeSortTime(item),
		})
	}
	return out
}

func changeCountsBySession(ctx context.Context, store *FileChangeStore) map[string]int {
	if store == nil {
		return map[string]int{}
	}
	counts, err := store.CountsBySession(ctx)
	if err != nil {
		return map[string]int{}
	}
	return counts
}

func buildInspectRun(entries []rstate.StateEntry, sessionID string) InspectRunReport {
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

func decodeInspectTraceEvent(entry rstate.StateEntry) (TraceEvent, bool) {
	if entry.Kind != rstate.StateKindExecutionEvent {
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
		Metadata:     cloneTraceMetadata(meta.Metadata),
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
