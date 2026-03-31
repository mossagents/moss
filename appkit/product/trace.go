package product

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

type TraceEvent struct {
	Kind             string         `json:"kind"`
	Timestamp        time.Time      `json:"timestamp"`
	SessionID        string         `json:"session_id,omitempty"`
	Type             string         `json:"type,omitempty"`
	Model            string         `json:"model,omitempty"`
	ToolName         string         `json:"tool_name,omitempty"`
	DurationMS       int64          `json:"duration_ms,omitempty"`
	PromptTokens     int            `json:"prompt_tokens,omitempty"`
	CompletionTokens int            `json:"completion_tokens,omitempty"`
	TotalTokens      int            `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64        `json:"estimated_cost_usd,omitempty"`
	Error            string         `json:"error,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
}

type RunTrace struct {
	PromptTokens     int          `json:"prompt_tokens,omitempty"`
	CompletionTokens int          `json:"completion_tokens,omitempty"`
	TotalTokens      int          `json:"total_tokens,omitempty"`
	EstimatedCostUSD float64      `json:"estimated_cost_usd,omitempty"`
	LLMCalls         int          `json:"llm_calls,omitempty"`
	ToolCalls        int          `json:"tool_calls,omitempty"`
	Timeline         []TraceEvent `json:"timeline,omitempty"`
}

type RunTraceRecorder struct {
	mu    sync.Mutex
	trace RunTrace
}

type RunTraceSummary struct {
	Status        string
	Steps         int
	Trace         RunTrace
	Error         string
	CostAvailable bool
}

const defaultTraceDetailLimit = 20

func NewRunTraceRecorder() *RunTraceRecorder {
	return &RunTraceRecorder{}
}

func (r *RunTraceRecorder) Snapshot() RunTrace {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.trace
	out.Timeline = append([]TraceEvent(nil), r.trace.Timeline...)
	return out
}

func (r *RunTraceRecorder) OnLLMCall(_ context.Context, e port.LLMCallEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.LLMCalls++
	r.trace.PromptTokens += e.Usage.PromptTokens
	r.trace.CompletionTokens += e.Usage.CompletionTokens
	r.trace.TotalTokens += e.Usage.TotalTokens
	r.trace.EstimatedCostUSD += e.EstimatedCostUSD
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:             "llm_call",
		Timestamp:        time.Now().UTC(),
		SessionID:        e.SessionID,
		Model:            e.Model,
		DurationMS:       e.Duration.Milliseconds(),
		PromptTokens:     e.Usage.PromptTokens,
		CompletionTokens: e.Usage.CompletionTokens,
		TotalTokens:      e.Usage.TotalTokens,
		EstimatedCostUSD: e.EstimatedCostUSD,
		Type:             e.StopReason,
		Error:            errorString(e.Error),
	})
}

func (r *RunTraceRecorder) OnToolCall(_ context.Context, e port.ToolCallEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.ToolCalls++
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:       "tool_call",
		Timestamp:  time.Now().UTC(),
		SessionID:  e.SessionID,
		ToolName:   e.ToolName,
		DurationMS: e.Duration.Milliseconds(),
		Error:      errorString(e.Error),
	})
}

func (r *RunTraceRecorder) OnExecutionEvent(_ context.Context, e port.ExecutionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:       "execution_event",
		Timestamp:  e.Timestamp.UTC(),
		SessionID:  e.SessionID,
		Type:       string(e.Type),
		Model:      e.Model,
		ToolName:   e.ToolName,
		DurationMS: e.Duration.Milliseconds(),
		Error:      e.Error,
		Data:       cloneTraceData(e.Data),
	})
}

func (r *RunTraceRecorder) OnApproval(_ context.Context, e port.ApprovalEvent) {
	data := map[string]any{
		"id":          e.Request.ID,
		"reason":      e.Request.Reason,
		"reason_code": e.Request.ReasonCode,
	}
	if e.Decision != nil {
		data["approved"] = e.Decision.Approved
		data["source"] = e.Decision.Source
		if strings.TrimSpace(e.Decision.Reason) != "" {
			data["decision_reason"] = e.Decision.Reason
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:      "approval",
		Timestamp: time.Now().UTC(),
		SessionID: e.SessionID,
		Type:      e.Type,
		ToolName:  e.Request.ToolName,
		Data:      data,
	})
}

func (r *RunTraceRecorder) OnSessionEvent(_ context.Context, e port.SessionEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:      "session",
		Timestamp: time.Now().UTC(),
		SessionID: e.SessionID,
		Type:      e.Type,
	})
}

func (r *RunTraceRecorder) OnError(_ context.Context, e port.ErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trace.Timeline = append(r.trace.Timeline, TraceEvent{
		Kind:      "error",
		Timestamp: time.Now().UTC(),
		SessionID: e.SessionID,
		Type:      e.Phase,
		Error:     e.Message,
	})
}

type pricingObserver struct {
	catalog *PricingCatalog
	next    port.Observer
}

func NewPricingObserver(catalog *PricingCatalog, next port.Observer) port.Observer {
	if next == nil {
		return nil
	}
	if catalog == nil {
		return next
	}
	return pricingObserver{catalog: catalog, next: next}
}

func (o pricingObserver) OnLLMCall(ctx context.Context, e port.LLMCallEvent) {
	if cost, ok := o.catalog.Estimate(e.Usage, e.Model); ok {
		e.EstimatedCostUSD = cost
	}
	o.next.OnLLMCall(ctx, e)
}

func (o pricingObserver) OnToolCall(ctx context.Context, e port.ToolCallEvent) {
	o.next.OnToolCall(ctx, e)
}

func (o pricingObserver) OnExecutionEvent(ctx context.Context, e port.ExecutionEvent) {
	o.next.OnExecutionEvent(ctx, e)
}

func (o pricingObserver) OnApproval(ctx context.Context, e port.ApprovalEvent) {
	o.next.OnApproval(ctx, e)
}

func (o pricingObserver) OnSessionEvent(ctx context.Context, e port.SessionEvent) {
	o.next.OnSessionEvent(ctx, e)
}

func (o pricingObserver) OnError(ctx context.Context, e port.ErrorEvent) {
	o.next.OnError(ctx, e)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func cloneTraceData(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func RenderRunTraceSummary(summary RunTraceSummary) string {
	lines := renderRunTraceOverview("Run summary:", summary)
	if events := SelectKeyTraceEvents(summary.Trace, 5); len(events) > 0 {
		lines = append(lines, "  key events:")
		for _, event := range events {
			lines = append(lines, "    - "+event)
		}
	}
	return strings.Join(lines, "\n")
}

func RenderRunTraceDetail(summary RunTraceSummary, limit int) string {
	lines := renderRunTraceOverview("Last run trace:", summary)
	totalEvents := len(summary.Trace.Timeline)
	if totalEvents == 0 {
		lines = append(lines, "  timeline: no trace events recorded")
		return strings.Join(lines, "\n")
	}
	if limit <= 0 {
		limit = defaultTraceDetailLimit
	}
	events := summary.Trace.Timeline
	start := 0
	if limit < len(events) {
		start = len(events) - limit
		events = events[start:]
	}
	lines = append(lines, fmt.Sprintf("  timeline: showing %d of %d events", len(events), totalEvents))
	for i, event := range events {
		lines = append(lines, fmt.Sprintf("    %02d. %s", start+i+1, formatTraceEventDetail(event)))
	}
	return strings.Join(lines, "\n")
}

func renderRunTraceOverview(title string, summary RunTraceSummary) []string {
	status := normalizeRunTraceStatus(summary.Status, summary.Error)
	lines := []string{
		title,
		fmt.Sprintf("  status: %s", status),
		fmt.Sprintf("  steps: %d", summary.Steps),
		fmt.Sprintf("  llm calls: %d", summary.Trace.LLMCalls),
		fmt.Sprintf("  tool calls: %d", summary.Trace.ToolCalls),
		fmt.Sprintf(
			"  tokens: prompt=%d completion=%d total=%d",
			summary.Trace.PromptTokens,
			summary.Trace.CompletionTokens,
			summary.Trace.TotalTokens,
		),
		renderTraceCostLine(summary),
	}
	if msg := strings.TrimSpace(summary.Error); msg != "" {
		lines = append(lines, fmt.Sprintf("  error: %s", msg))
	}
	return lines
}

func renderTraceCostLine(summary RunTraceSummary) string {
	if summary.CostAvailable && summary.Trace.EstimatedCostUSD > 0 {
		return fmt.Sprintf("  cost: $%.6f", summary.Trace.EstimatedCostUSD)
	}
	return "  cost: unavailable"
}

func SelectKeyTraceEvents(trace RunTrace, limit int) []string {
	if limit <= 0 {
		return nil
	}
	candidates := make([]traceEventSummary, 0, len(trace.Timeline)+1)
	for idx, event := range trace.Timeline {
		if summary, ok := summarizeTraceEvent(event, idx); ok {
			candidates = append(candidates, summary)
		}
	}
	if slowest, ok := summarizeSlowestTraceEvent(trace); ok {
		candidates = append(candidates, slowest)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		return candidates[i].index < candidates[j].index
	})
	out := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if _, ok := seen[candidate.message]; ok {
			continue
		}
		seen[candidate.message] = struct{}{}
		out = append(out, candidate.message)
		if len(out) == limit {
			break
		}
	}
	return out
}

type traceEventSummary struct {
	priority int
	index    int
	message  string
}

func summarizeTraceEvent(event TraceEvent, index int) (traceEventSummary, bool) {
	switch event.Kind {
	case "error":
		if strings.TrimSpace(event.Error) == "" {
			return traceEventSummary{}, false
		}
		msg := "Error: " + event.Error
		if phase := strings.TrimSpace(event.Type); phase != "" {
			msg = fmt.Sprintf("Error during %s: %s", phase, event.Error)
		}
		return traceEventSummary{priority: 0, index: index, message: msg}, true
	case "approval":
		msg := summarizeApprovalEvent(event)
		if msg == "" {
			return traceEventSummary{}, false
		}
		priority := 20
		if approved, ok := boolData(event.Data, "approved"); ok && !approved {
			priority = 2
		}
		return traceEventSummary{priority: priority, index: index, message: msg}, true
	case "execution_event":
		switch event.Type {
		case "llm_failover_exhausted":
			return traceEventSummary{priority: 1, index: index, message: "LLM failover exhausted all available candidates"}, true
		case "llm_failover_switch":
			from := valueOrUnknown(stringData(event.Data, "candidate_model"), event.Model)
			to := valueOrUnknown(stringData(event.Data, "failover_to"), "next candidate")
			return traceEventSummary{priority: 3, index: index, message: fmt.Sprintf("Failover switched from %s to %s", from, to)}, true
		case "tool.completed":
			if degraded, ok := boolData(event.Data, "degraded"); ok && degraded {
				toolName := valueOrUnknown(event.ToolName, "tool")
				enforcement := valueOrUnknown(stringData(event.Data, "enforcement"), "degraded")
				return traceEventSummary{priority: 6, index: index, message: fmt.Sprintf("Tool %s completed with %s enforcement", toolName, enforcement)}, true
			}
		}
	case "llm_call":
		if strings.TrimSpace(event.Error) == "" {
			return traceEventSummary{}, false
		}
		model := valueOrUnknown(event.Model, "configured model")
		return traceEventSummary{
			priority: 4,
			index:    index,
			message:  fmt.Sprintf("LLM call on %s failed%s", model, formatDurationSuffix(event.DurationMS)),
		}, true
	case "tool_call":
		if strings.TrimSpace(event.Error) == "" {
			return traceEventSummary{}, false
		}
		toolName := valueOrUnknown(event.ToolName, "tool")
		return traceEventSummary{
			priority: 5,
			index:    index,
			message:  fmt.Sprintf("Tool %s failed%s", toolName, formatDurationSuffix(event.DurationMS)),
		}, true
	}
	return traceEventSummary{}, false
}

func summarizeApprovalEvent(event TraceEvent) string {
	toolName := valueOrUnknown(event.ToolName, "tool")
	reasonCode := strings.TrimSpace(stringData(event.Data, "reason_code"))
	switch strings.TrimSpace(event.Type) {
	case "requested":
		if reasonCode != "" {
			return fmt.Sprintf("Approval required for %s (%s)", toolName, reasonCode)
		}
		return fmt.Sprintf("Approval required for %s", toolName)
	case "resolved":
		if approved, ok := boolData(event.Data, "approved"); ok {
			if approved {
				return fmt.Sprintf("Approval granted for %s", toolName)
			}
			if reasonCode != "" {
				return fmt.Sprintf("Approval denied for %s (%s)", toolName, reasonCode)
			}
			return fmt.Sprintf("Approval denied for %s", toolName)
		}
		return fmt.Sprintf("Approval resolved for %s", toolName)
	default:
		return ""
	}
}

func summarizeSlowestTraceEvent(trace RunTrace) (traceEventSummary, bool) {
	var best *TraceEvent
	bestKind := ""
	bestIndex := -1
	for i := range trace.Timeline {
		event := trace.Timeline[i]
		if event.DurationMS <= 0 || strings.TrimSpace(event.Error) != "" {
			continue
		}
		if event.Kind != "llm_call" && event.Kind != "tool_call" {
			continue
		}
		if best == nil || event.DurationMS > best.DurationMS {
			best = &event
			bestKind = event.Kind
			bestIndex = i
		}
	}
	if best == nil {
		return traceEventSummary{}, false
	}
	if bestKind == "tool_call" {
		return traceEventSummary{
			priority: 30,
			index:    bestIndex,
			message:  fmt.Sprintf("Slowest tool: %s (%s)", valueOrUnknown(best.ToolName, "tool"), formatDuration(best.DurationMS)),
		}, true
	}
	return traceEventSummary{
		priority: 30,
		index:    bestIndex,
		message:  fmt.Sprintf("Slowest LLM call: %s (%s)", valueOrUnknown(best.Model, "configured model"), formatDuration(best.DurationMS)),
	}, true
}

func formatTraceEventDetail(event TraceEvent) string {
	details := make([]string, 0, 10)
	switch event.Kind {
	case "session":
		details = append(details, "[session]", valueOrUnknown(event.Type, "event"))
	case "approval":
		details = append(details, "[approval]", valueOrUnknown(event.Type, "event"))
		details = append(details, "tool="+valueOrUnknown(event.ToolName, "tool"))
		if reasonCode := stringData(event.Data, "reason_code"); reasonCode != "" {
			details = append(details, "reason_code="+reasonCode)
		}
		if approved, ok := boolData(event.Data, "approved"); ok {
			details = append(details, fmt.Sprintf("approved=%t", approved))
		}
		if source := stringData(event.Data, "source"); source != "" {
			details = append(details, "source="+source)
		}
	case "llm_call":
		details = append(details, "[llm]", "model="+valueOrUnknown(event.Model, "configured model"))
		if stop := strings.TrimSpace(event.Type); stop != "" {
			details = append(details, "stop="+stop)
		}
		details = appendTraceCommonDetails(details, event)
		if event.TotalTokens > 0 {
			details = append(details, fmt.Sprintf("tokens=%d", event.TotalTokens))
		}
		if event.EstimatedCostUSD > 0 {
			details = append(details, fmt.Sprintf("cost=$%.6f", event.EstimatedCostUSD))
		}
	case "tool_call":
		details = append(details, "[tool]", valueOrUnknown(event.ToolName, "tool"))
		details = appendTraceCommonDetails(details, event)
	case "execution_event":
		details = append(details, "[execution]", valueOrUnknown(event.Type, "event"))
		if strings.TrimSpace(event.Model) != "" {
			details = append(details, "model="+event.Model)
		}
		if strings.TrimSpace(event.ToolName) != "" {
			details = append(details, "tool="+event.ToolName)
		}
		details = appendTraceCommonDetails(details, event)
		details = append(details, formatTraceDataPairs(event.Data)...)
	case "error":
		details = append(details, "[error]", valueOrUnknown(event.Type, "runtime"))
		if strings.TrimSpace(event.Error) != "" {
			details = append(details, "message="+event.Error)
		}
	default:
		details = append(details, "["+valueOrUnknown(event.Kind, "event")+"]")
		details = appendTraceCommonDetails(details, event)
		details = append(details, formatTraceDataPairs(event.Data)...)
	}
	return strings.Join(details, " ")
}

func appendTraceCommonDetails(details []string, event TraceEvent) []string {
	if event.DurationMS > 0 {
		details = append(details, "duration="+formatDuration(event.DurationMS))
	}
	if strings.TrimSpace(event.Error) != "" {
		details = append(details, "error="+event.Error)
	}
	return details
}

func formatTraceDataPairs(data map[string]any) []string {
	if len(data) == 0 {
		return nil
	}
	keys := []string{
		"candidate_model",
		"failover_to",
		"attempt_index",
		"candidate_retry",
		"breaker_state",
		"outcome",
		"stop_reason",
		"streamed",
		"tokens",
		"steps",
		"mode",
		"goal",
		"reason_code",
		"approved",
		"enforcement",
		"degraded",
		"details",
		"url",
		"method",
		"status_code",
		"follow_redirects",
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		v, ok := data[key]
		if !ok {
			continue
		}
		if value := formatTraceDataValue(v); value != "" {
			out = append(out, key+"="+value)
		}
	}
	return out
}

func formatTraceDataValue(v any) string {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value)
	case bool:
		if value {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return ""
	}
}

func normalizeRunTraceStatus(status, errMsg string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "completed", "failed", "cancelled":
		return strings.TrimSpace(strings.ToLower(status))
	}
	if strings.Contains(strings.ToLower(errMsg), "context canceled") {
		return "cancelled"
	}
	if strings.TrimSpace(errMsg) != "" {
		return "failed"
	}
	return "completed"
}

func formatDurationSuffix(durationMS int64) string {
	if durationMS <= 0 {
		return ""
	}
	return " (" + formatDuration(durationMS) + ")"
}

func formatDuration(durationMS int64) string {
	if durationMS < 1000 {
		return strconv.FormatInt(durationMS, 10) + "ms"
	}
	return (time.Duration(durationMS) * time.Millisecond).String()
}

func boolData(data map[string]any, key string) (bool, bool) {
	if len(data) == 0 {
		return false, false
	}
	v, ok := data[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func stringData(data map[string]any, key string) string {
	if len(data) == 0 {
		return ""
	}
	v, ok := data[key]
	if !ok {
		return ""
	}
	switch s := v.(type) {
	case string:
		return strings.TrimSpace(s)
	case fmt.Stringer:
		return strings.TrimSpace(s.String())
	default:
		return ""
	}
}

func valueOrUnknown(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
