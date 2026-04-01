package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

type notificationProgressMsg struct {
	Snapshot   executionProgressState
	SetCurrent bool
}

type executionProgressState struct {
	SessionID string
	Status    string
	Phase     string
	Message   string
	ToolName  string
	Iteration int
	MaxSteps  int
	StartedAt time.Time
	UpdatedAt time.Time
}

func (s executionProgressState) visible() bool {
	return strings.TrimSpace(s.SessionID) != "" && (!s.StartedAt.IsZero() || !s.UpdatedAt.IsZero() || strings.TrimSpace(s.Status) != "" || strings.TrimSpace(s.Message) != "")
}

func (s executionProgressState) terminal() bool {
	switch s.Status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (s executionProgressState) elapsed(now time.Time) time.Duration {
	if s.StartedAt.IsZero() {
		return 0
	}
	end := now
	if s.terminal() && !s.UpdatedAt.IsZero() {
		end = s.UpdatedAt
	}
	if end.Before(s.StartedAt) {
		return 0
	}
	return end.Sub(s.StartedAt).Round(time.Second)
}

func (s executionProgressState) renderLine(now time.Time, width int) string {
	if !s.visible() {
		return ""
	}
	parts := []string{progressStatusLabel(s.Status)}
	if strings.TrimSpace(s.Phase) != "" {
		parts = append(parts, progressPhaseLabel(s.Phase))
	}
	if s.Iteration > 0 {
		if s.MaxSteps > 0 {
			parts = append(parts, fmt.Sprintf("iter %d/%d", s.Iteration, s.MaxSteps))
		} else {
			parts = append(parts, fmt.Sprintf("iter %d", s.Iteration))
		}
	}
	if elapsed := s.elapsed(now); elapsed > 0 {
		parts = append(parts, elapsed.String())
	}
	if msg := strings.TrimSpace(s.Message); msg != "" {
		parts = append(parts, msg)
	}
	line := "Progress: " + strings.Join(parts, "  │  ")
	if width > 0 {
		line = truncateForQueue(line, width)
	}
	switch s.Status {
	case "running":
		return runningStyle.Render(line)
	case "waiting":
		return progressStyle.Render(line)
	case "failed", "cancelled":
		return errorStyle.Render(line)
	default:
		return mutedStyle.Render(line)
	}
}

func progressStatusLabel(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "running":
		return "Running"
	case "waiting":
		return "Waiting"
	case "completed":
		return "Completed"
	case "failed":
		return "Failed"
	case "cancelled":
		return "Cancelled"
	default:
		return "Idle"
	}
}

func progressPhaseLabel(phase string) string {
	switch strings.TrimSpace(strings.ToLower(phase)) {
	case "starting":
		return "starting"
	case "thinking":
		return "thinking"
	case "tools":
		return "using tools"
	case "approval":
		return "awaiting approval"
	case "completed":
		return "done"
	case "failed":
		return "error"
	case "cancelled":
		return "cancelled"
	default:
		return phase
	}
}

type executionProgressObserver struct {
	port.NoOpObserver
	bridge    *BridgeIO
	sessionID string
	mu        sync.Mutex
	state     executionProgressState
}

func newExecutionProgressObserver(bridge *BridgeIO, sess *session.Session) port.Observer {
	if bridge == nil || sess == nil || strings.TrimSpace(sess.ID) == "" {
		return nil
	}
	return &executionProgressObserver{
		bridge:    bridge,
		sessionID: strings.TrimSpace(sess.ID),
		state: executionProgressState{
			SessionID: strings.TrimSpace(sess.ID),
			MaxSteps:  sess.Budget.MaxSteps,
		},
	}
}

func (o *executionProgressObserver) OnExecutionEvent(_ context.Context, event port.ExecutionEvent) {
	if o == nil || o.bridge == nil || strings.TrimSpace(event.SessionID) != o.sessionID {
		return
	}
	o.mu.Lock()
	o.state = foldExecutionProgressEvent(o.state, event)
	snapshot := o.state
	o.mu.Unlock()
	o.bridge.SendProgress(snapshot, false)
}

func foldExecutionProgressEvent(current executionProgressState, event port.ExecutionEvent) executionProgressState {
	next := current
	if id := strings.TrimSpace(event.SessionID); id != "" {
		next.SessionID = id
	}
	ts := event.Timestamp.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	if next.StartedAt.IsZero() && event.Type != port.ExecutionRunCompleted && event.Type != port.ExecutionRunFailed && event.Type != port.ExecutionRunCancelled {
		next.StartedAt = ts
	}
	next.UpdatedAt = ts
	if v, ok := intData(event.Data, "max_steps"); ok && v > 0 {
		next.MaxSteps = v
	}
	switch event.Type {
	case port.ExecutionRunStarted:
		next.Status = "running"
		next.Phase = "starting"
		next.Message = "run started"
		next.ToolName = ""
		next.StartedAt = ts
	case port.ExecutionIterationStarted:
		next.Status = "running"
		next.Phase = "thinking"
		next.ToolName = ""
		if v, ok := intData(event.Data, "iteration"); ok && v > 0 {
			next.Iteration = v
		}
		next.Message = fmt.Sprintf("iteration %d started", maxInt(next.Iteration, 1))
	case port.ExecutionLLMStarted:
		next.Status = "running"
		next.Phase = "thinking"
		next.ToolName = ""
		if model := strings.TrimSpace(event.Model); model != "" {
			next.Message = "calling " + model
		} else {
			next.Message = "calling model"
		}
	case port.ExecutionToolStarted:
		next.Status = "running"
		next.Phase = "tools"
		next.ToolName = strings.TrimSpace(event.ToolName)
		if next.ToolName != "" {
			next.Message = "running " + next.ToolName
		} else {
			next.Message = "running tool"
		}
	case port.ExecutionApprovalRequest:
		next.Status = "waiting"
		next.Phase = "approval"
		next.ToolName = strings.TrimSpace(event.ToolName)
		if next.ToolName != "" {
			next.Message = "approval required for " + next.ToolName
		} else if reason := strings.TrimSpace(stringData(event.Data, "reason")); reason != "" {
			next.Message = reason
		} else {
			next.Message = "approval required"
		}
	case port.ExecutionApprovalResolved:
		next.Status = "running"
		next.Phase = "approval"
		next.ToolName = strings.TrimSpace(event.ToolName)
		if approved, ok := boolData(event.Data, "approved"); ok && !approved {
			next.Message = "approval denied"
		} else if next.ToolName != "" {
			next.Message = "approval granted for " + next.ToolName
		} else {
			next.Message = "approval resolved"
		}
	case port.ExecutionIterationProgress:
		next.Status = "running"
		next.Phase = "thinking"
		next.ToolName = ""
		if v, ok := intData(event.Data, "iteration"); ok && v > 0 {
			next.Iteration = v
		}
		toolCalls, _ := intData(event.Data, "tool_calls")
		stopReason := strings.TrimSpace(stringData(event.Data, "stop_reason"))
		tokens, _ := intData(event.Data, "tokens")
		switch {
		case toolCalls > 0:
			next.Phase = "tools"
			if toolCalls == 1 {
				next.Message = "1 tool call completed"
			} else {
				next.Message = fmt.Sprintf("%d tool calls completed", toolCalls)
			}
		case stopReason == "end_turn":
			next.Message = "assistant response ready"
		case tokens > 0:
			next.Message = fmt.Sprintf("iteration updated (%d tokens)", tokens)
		default:
			next.Message = "iteration updated"
		}
	case port.ExecutionRunCompleted:
		next.Status = "completed"
		next.Phase = "completed"
		next.ToolName = ""
		steps, _ := intData(event.Data, "steps")
		tokens, _ := intData(event.Data, "tokens")
		switch {
		case steps > 0 && tokens > 0:
			next.Message = fmt.Sprintf("completed in %d steps (%d tokens)", steps, tokens)
		case steps > 0:
			next.Message = fmt.Sprintf("completed in %d steps", steps)
		default:
			next.Message = "run completed"
		}
	case port.ExecutionRunFailed:
		next.Status = "failed"
		next.Phase = "failed"
		next.ToolName = ""
		next.Message = firstNonEmptyProgress(strings.TrimSpace(event.Error), "run failed")
	case port.ExecutionRunCancelled:
		next.Status = "cancelled"
		next.Phase = "cancelled"
		next.ToolName = ""
		next.Message = firstNonEmptyProgress(strings.TrimSpace(event.Error), "run cancelled")
	}
	return next
}

func rebuildExecutionProgress(catalog *runtime.StateCatalog, sess *session.Session) executionProgressState {
	state := executionProgressState{}
	if sess == nil {
		return state
	}
	state.SessionID = strings.TrimSpace(sess.ID)
	state.MaxSteps = sess.Budget.MaxSteps
	if catalog == nil || !catalog.Enabled() || strings.TrimSpace(sess.ID) == "" {
		return state
	}
	page, err := catalog.Query(runtime.StateQuery{
		Kinds:     []runtime.StateKind{runtime.StateKindExecutionEvent},
		SessionID: sess.ID,
		Limit:     128,
	})
	if err != nil || len(page.Items) == 0 {
		return state
	}
	items := latestRunEntries(page.Items)
	slices.Reverse(items)
	for _, item := range items {
		event, ok := executionEventFromStateEntry(item)
		if !ok {
			continue
		}
		state = foldExecutionProgressEvent(state, event)
	}
	return state
}

func latestRunEntries(items []runtime.StateEntry) []runtime.StateEntry {
	if len(items) == 0 {
		return nil
	}
	collected := make([]runtime.StateEntry, 0, len(items))
	sawProgress := false
	for _, item := range items {
		collected = append(collected, item)
		typ := stateEntryEventType(item)
		if typ == port.ExecutionRunStarted {
			break
		}
		if typ == port.ExecutionIterationStarted || typ == port.ExecutionIterationProgress || typ == port.ExecutionRunCompleted || typ == port.ExecutionRunFailed || typ == port.ExecutionRunCancelled {
			sawProgress = true
		}
	}
	if sawProgress {
		return collected
	}
	return items
}

type stateEntryExecutionMetadata struct {
	EventType   string         `json:"event_type"`
	ToolName    string         `json:"tool_name"`
	Model       string         `json:"model"`
	Risk        string         `json:"risk"`
	ReasonCode  string         `json:"reason_code"`
	Enforcement string         `json:"enforcement"`
	DurationMS  int64          `json:"duration_ms"`
	Data        map[string]any `json:"data"`
}

func executionEventFromStateEntry(entry runtime.StateEntry) (port.ExecutionEvent, bool) {
	if entry.Kind != runtime.StateKindExecutionEvent {
		return port.ExecutionEvent{}, false
	}
	meta := stateEntryExecutionMetadata{}
	if len(entry.Metadata) > 0 {
		_ = json.Unmarshal(entry.Metadata, &meta)
	}
	eventType := strings.TrimSpace(meta.EventType)
	if eventType == "" {
		eventType = string(stateEntryEventType(entry))
	}
	if eventType == "" {
		return port.ExecutionEvent{}, false
	}
	ts := entry.SortTime.UTC()
	if ts.IsZero() {
		ts = firstTime(entry.UpdatedAt.UTC(), entry.CreatedAt.UTC(), time.Now().UTC())
	}
	return port.ExecutionEvent{
		Type:        port.ExecutionEventType(eventType),
		SessionID:   strings.TrimSpace(entry.SessionID),
		Timestamp:   ts,
		ToolName:    strings.TrimSpace(meta.ToolName),
		Model:       strings.TrimSpace(meta.Model),
		Risk:        strings.TrimSpace(meta.Risk),
		ReasonCode:  strings.TrimSpace(meta.ReasonCode),
		Enforcement: port.EnforcementMode(strings.TrimSpace(meta.Enforcement)),
		Duration:    time.Duration(meta.DurationMS) * time.Millisecond,
		Error:       strings.TrimSpace(entry.Status),
		Data:        meta.Data,
	}, true
}

func stateEntryEventType(entry runtime.StateEntry) port.ExecutionEventType {
	if len(entry.Metadata) > 0 {
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(entry.Metadata, &meta); err == nil {
			if raw, ok := meta["event_type"]; ok {
				var eventType string
				if json.Unmarshal(raw, &eventType) == nil && strings.TrimSpace(eventType) != "" {
					return port.ExecutionEventType(strings.TrimSpace(eventType))
				}
			}
		}
	}
	title := strings.TrimSpace(entry.Title)
	if idx := strings.Index(title, ":"); idx >= 0 {
		title = title[:idx]
	}
	return port.ExecutionEventType(title)
}

func publishProgressReplay(bridge *BridgeIO, k *kernel.Kernel, sess *session.Session) {
	if bridge == nil || sess == nil {
		return
	}
	bridge.SendProgress(rebuildExecutionProgress(runtime.StateCatalogOf(k), sess), true)
}

func intData(data map[string]any, key string) (int, bool) {
	if data == nil {
		return 0, false
	}
	v, ok := data[key]
	if !ok {
		return 0, false
	}
	switch actual := v.(type) {
	case int:
		return actual, true
	case int64:
		return int(actual), true
	case float64:
		return int(actual), true
	case json.Number:
		n, err := actual.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func boolData(data map[string]any, key string) (bool, bool) {
	if data == nil {
		return false, false
	}
	v, ok := data[key]
	if !ok {
		return false, false
	}
	actual, ok := v.(bool)
	return actual, ok
}

func stringData(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	v, ok := data[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func firstTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func firstNonEmptyProgress(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
