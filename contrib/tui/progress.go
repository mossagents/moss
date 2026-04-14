package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/internal/stringutil"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/observe"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/runtime"
)

type notificationProgressMsg struct {
	Snapshot   executionProgressState
	SetCurrent bool
}

type executionProgressState struct {
	SessionID   string
	Status      string
	Phase       string
	Message     string
	ToolName    string
	ActivityKey string
	Iteration   int
	MaxSteps    int
	StartedAt   time.Time
	EndedAt     time.Time
	UpdatedAt   time.Time
}

func (s executionProgressState) signature() string {
	return strings.Join([]string{
		strings.TrimSpace(s.Status),
		strings.TrimSpace(s.Phase),
		strings.TrimSpace(s.Message),
		strings.TrimSpace(s.ToolName),
		strings.TrimSpace(s.ActivityKey),
		fmt.Sprintf("%d", s.Iteration),
		fmt.Sprintf("%d", s.MaxSteps),
	}, "\x00")
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
	if !s.EndedAt.IsZero() {
		end = s.EndedAt
	}
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
	parts := []string{strings.ToLower(progressStatusLabel(s.Status))}
	if strings.TrimSpace(s.Phase) != "" {
		if phase := strings.TrimSpace(progressPhaseLabel(s.Phase)); phase != "" {
			parts = append(parts, phase)
		}
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
	line := "  │ " + strings.Join(parts, "  ·  ")
	if width > 0 {
		line = truncateDisplayWidth(line, width)
	}
	switch s.Status {
	case "running":
		return eventPendingStyle.Render(line)
	case "waiting":
		return eventSummaryStyle.Render(line)
	case "failed", "cancelled":
		return eventErrorStyle.Render(line)
	default:
		return eventSummaryStyle.Render(line)
	}
}

func (s executionProgressState) renderTimelineEntry(now time.Time, width int, latest bool) string {
	phase := progressPhaseLabel(s.Phase)
	// 仅在 phase 本身为空时才回退到 status 标签；
	// thinking 等显式返回 "" 的 phase 不使用回退（避免显示 "running"）。
	if strings.TrimSpace(phase) == "" && strings.TrimSpace(s.Phase) == "" {
		phase = strings.ToLower(progressStatusLabel(s.Status))
	}
	label := "○"
	if latest {
		label = "●"
	}
	parts := []string{"  " + label}
	if strings.TrimSpace(phase) != "" {
		parts = append(parts, phase)
	}
	if msg := strings.TrimSpace(s.Message); msg != "" {
		parts = append(parts, msg)
	}
	if elapsed := s.elapsed(now); elapsed > 0 {
		parts = append(parts, elapsed.String())
	}
	line := strings.Join(parts, "  ")
	if width > 0 {
		line = truncateDisplayWidth(line, width)
	}
	if latest {
		switch s.Status {
		case "failed", "cancelled":
			return eventErrorStyle.Render(line)
		case "running", "waiting":
			return eventPendingStyle.Render(line)
		default:
			return eventSummaryStyle.Render(line)
		}
	}
	return eventDetailStyle.Render(line)
}

func (m *chatModel) recordProgressSnapshot(next executionProgressState, reset bool) (bool, bool) {
	didReset := reset || strings.TrimSpace(next.SessionID) != strings.TrimSpace(m.progress.SessionID) || progressRunChanged(m.progress, next)
	if didReset {
		m.progressTrail = nil
	}
	if !next.visible() {
		return didReset, false
	}
	if next.terminal() && next.EndedAt.IsZero() && !next.UpdatedAt.IsZero() {
		next.EndedAt = next.UpdatedAt
	}
	if len(m.progressTrail) > 0 && m.progressTrail[len(m.progressTrail)-1].signature() == next.signature() {
		next.EndedAt = m.progressTrail[len(m.progressTrail)-1].EndedAt
		m.progressTrail[len(m.progressTrail)-1] = next
		return didReset, false
	}
	if len(m.progressTrail) > 0 && shouldCoalesceProgressTrail(m.progressTrail[len(m.progressTrail)-1], next) {
		prev := m.progressTrail[len(m.progressTrail)-1]
		if next.StartedAt.IsZero() {
			next.StartedAt = prev.StartedAt
		}
		if next.EndedAt.IsZero() {
			next.EndedAt = prev.EndedAt
		}
		m.progressTrail[len(m.progressTrail)-1] = next
		return didReset, false
	}
	if len(m.progressTrail) > 0 {
		prev := m.progressTrail[len(m.progressTrail)-1]
		if prev.EndedAt.IsZero() {
			endAt := next.UpdatedAt
			if endAt.IsZero() {
				endAt = next.StartedAt
			}
			if endAt.IsZero() {
				endAt = m.now().UTC()
			}
			if !endAt.Before(prev.StartedAt) {
				prev.EndedAt = endAt
			}
		}
		m.progressTrail[len(m.progressTrail)-1] = prev
	}
	m.progressTrail = append(m.progressTrail, next)
	if len(m.progressTrail) > 6 {
		m.progressTrail = append([]executionProgressState(nil), m.progressTrail[len(m.progressTrail)-6:]...)
	}
	return didReset, true
}

func shouldAppendThinkingTranscript(snapshot executionProgressState) bool {
	if !snapshot.visible() || strings.TrimSpace(snapshot.Message) == "" {
		return false
	}
	// 只记录有意义的阶段：工具调用、审批、终态。
	// 跳过 starting/thinking — 这些是 LLM 内部处理阶段，对用户无意义，且在没有
	// thinking 模型时尤其产生噪音。
	switch strings.TrimSpace(snapshot.Phase) {
	case "tools", "approval", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func (m *chatModel) appendThinkingTranscript(snapshot executionProgressState, reset bool) {
	if reset {
		m.lastThinkingSignature = ""
	}
	if !shouldAppendThinkingTranscript(snapshot) {
		return
	}
	sig := snapshot.signature()
	if sig == "" || sig == m.lastThinkingSignature {
		return
	}
	timestamp := snapshot.UpdatedAt
	if timestamp.IsZero() {
		timestamp = m.now().UTC()
	}
	m.messages = append(m.messages, chatMessage{
		kind:    msgProgress,
		content: strings.TrimSpace(snapshot.Message),
		meta: map[string]any{
			"timestamp": timestamp,
			"phase":     strings.TrimSpace(snapshot.Phase),
			"status":    strings.TrimSpace(snapshot.Status),
		},
	})
	m.lastThinkingSignature = sig
}

func (m *chatModel) applyProgressSnapshot(next executionProgressState, reset bool) {
	didReset, appended := m.recordProgressSnapshot(next, reset)
	if appended {
		m.appendThinkingTranscript(next, didReset)
	}
}

func (m *chatModel) recordProgressDetail(status, phase, message string, updatedAt time.Time) {
	sessionID := strings.TrimSpace(m.currentSessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(m.progress.SessionID)
	}
	if sessionID == "" || strings.TrimSpace(message) == "" {
		return
	}
	snapshot := m.progress
	if strings.TrimSpace(status) != "" {
		snapshot.Status = status
	}
	if strings.TrimSpace(snapshot.Status) == "" {
		snapshot.Status = "running"
	}
	if strings.TrimSpace(phase) != "" {
		snapshot.Phase = phase
	}
	snapshot.SessionID = sessionID
	snapshot.Message = strings.TrimSpace(message)
	switch strings.TrimSpace(snapshot.Phase) {
	case "tools":
		snapshot.ActivityKey = "tool:" + stringutil.FirstNonEmpty(strings.TrimSpace(snapshot.ToolName), "active")
	case "approval":
		snapshot.ActivityKey = "approval:" + stringutil.FirstNonEmpty(strings.TrimSpace(snapshot.ToolName), "active")
	case "completed", "failed", "cancelled":
		snapshot.ActivityKey = "run:" + strings.TrimSpace(snapshot.Phase)
	default:
		snapshot.ActivityKey = fmt.Sprintf("iteration:%d", max(snapshot.Iteration, 1))
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	if snapshot.StartedAt.IsZero() {
		snapshot.StartedAt = updatedAt
	}
	snapshot.UpdatedAt = updatedAt
	m.applyProgressSnapshot(snapshot, false)
}

func progressRunChanged(prev, next executionProgressState) bool {
	if prev.StartedAt.IsZero() || next.StartedAt.IsZero() {
		return false
	}
	return !prev.StartedAt.Equal(next.StartedAt)
}

func shouldCoalesceProgressTrail(prev, next executionProgressState) bool {
	if strings.TrimSpace(prev.SessionID) == "" || strings.TrimSpace(prev.SessionID) != strings.TrimSpace(next.SessionID) {
		return false
	}
	if strings.TrimSpace(prev.ActivityKey) == "" || strings.TrimSpace(prev.ActivityKey) != strings.TrimSpace(next.ActivityKey) {
		return false
	}
	if prev.terminal() || next.terminal() {
		return false
	}
	if strings.TrimSpace(prev.Phase) == "approval" || strings.TrimSpace(next.Phase) == "approval" {
		return false
	}
	return true
}

func (m chatModel) renderProgressBlock(width int) string {
	if !m.progress.visible() {
		return ""
	}
	// 运行结束后不显示 timeline，结果已在 transcript 中，不需要重复展示。
	if m.progress.terminal() && !m.streaming {
		return ""
	}
	if len(m.progressTrail) <= 1 {
		return m.progress.renderLine(m.now(), width)
	}
	innerWidth := max(20, width-4)
	lines := make([]string, 0, len(m.progressTrail))
	for i, snapshot := range m.progressTrail {
		lines = append(lines, snapshot.renderTimelineEntry(m.now(), innerWidth, i == len(m.progressTrail)-1))
	}
	return panelMutedStyle.
		Width(max(24, width)).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

func summarizeTimelineToolStart(toolName, argsPreview string) string {
	name := toolPrettyName(toolName)
	summary := strings.TrimSpace(summarizeToolParams(toolName, argsPreview, 120))
	if summary == "" {
		return name
	}
	return name + " " + summary
}

func summarizeTimelineToolResult(toolName, content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if decoded, ok := parseJSONString(trimmed); ok {
		trimmed = decoded
	}
	if toolName == "run_command" || toolName == "powershell" {
		type shellResult struct {
			ExitCode int    `json:"exit_code"`
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
		}
		var result shellResult
		if err := json.Unmarshal([]byte(trimmed), &result); err == nil {
			parts := []string{fmt.Sprintf("exit=%d", result.ExitCode)}
			if out := firstNonEmptyLine(result.Stdout); out != "" {
				parts = append(parts, "stdout: "+truncateDisplayWidth(out, 72))
			} else if errLine := firstNonEmptyLine(result.Stderr); errLine != "" {
				parts = append(parts, "stderr: "+truncateDisplayWidth(errLine, 72))
			}
			return strings.Join(parts, " · ")
		}
	}
	if obj, ok := parseJSONObject(trimmed); ok {
		for _, key := range []string{"status", "exit_code", "message", "error", "result", "body"} {
			if value, ok := obj[key]; ok {
				switch v := value.(type) {
				case string:
					line := firstNonEmptyLine(v)
					if line == "" {
						continue
					}
					return key + ": " + truncateDisplayWidth(line, 72)
				default:
					return key + ": " + truncateDisplayWidth(fmt.Sprintf("%v", v), 72)
				}
			}
		}
	}
	if values, ok := parseJSONArray(trimmed); ok {
		return fmt.Sprintf("%d items", len(values))
	}
	return truncateDisplayWidth(firstNonEmptyLine(trimmed), 72)
}

func summarizeTimelineApproval(req *io.ApprovalRequest) string {
	if req == nil {
		return "approval required"
	}
	label := "approval required"
	if tool := strings.TrimSpace(req.ToolName); tool != "" {
		label += " for " + toolPrettyName(tool)
		if summary := strings.TrimSpace(summarizeToolParams(tool, strings.TrimSpace(string(req.Input)), 120)); summary != "" {
			label += " " + summary
		}
	}
	details := make([]string, 0, 2)
	if risk := strings.TrimSpace(req.Risk); risk != "" {
		details = append(details, "risk="+risk)
	}
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		details = append(details, reason)
	}
	if len(details) > 0 {
		label += " · " + strings.Join(details, " · ")
	}
	return truncateDisplayWidth(label, 120)
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
		return "" // 模型内部处理阶段，不显示标签（避免非 thinking 模型产生噪音）
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
	observe.NoOpObserver
	bridge    *BridgeIO
	sessionID string
	mu        sync.Mutex
	state     executionProgressState
}

func newExecutionProgressObserver(bridge *BridgeIO, sess *session.Session) observe.Observer {
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

func (o *executionProgressObserver) OnExecutionEvent(_ context.Context, event observe.ExecutionEvent) {
	if o == nil || o.bridge == nil || strings.TrimSpace(event.SessionID) != o.sessionID {
		return
	}
	o.mu.Lock()
	o.state = foldExecutionProgressEvent(o.state, event)
	snapshot := o.state
	o.mu.Unlock()
	o.bridge.SendProgress(snapshot, false)
}

func foldExecutionProgressEvent(current executionProgressState, event observe.ExecutionEvent) executionProgressState {
	next := current
	if id := strings.TrimSpace(event.SessionID); id != "" {
		next.SessionID = id
	}
	ts := event.Timestamp.UTC()
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	if next.StartedAt.IsZero() && event.Type != observe.ExecutionRunCompleted && event.Type != observe.ExecutionRunFailed && event.Type != observe.ExecutionRunCancelled {
		next.StartedAt = ts
	}
	next.UpdatedAt = ts
	if v, ok := intData(event.Metadata, "max_steps"); ok && v > 0 {
		next.MaxSteps = v
	}
	switch event.Type {
	case observe.ExecutionRunStarted:
		next.Status = "running"
		next.Phase = "starting"
		next.Message = "run started"
		next.ToolName = ""
		next.ActivityKey = "run"
		next.StartedAt = ts
	case observe.ExecutionIterationStarted:
		next.Status = "running"
		next.Phase = "thinking"
		next.ToolName = ""
		if v, ok := intData(event.Metadata, "iteration"); ok && v > 0 {
			next.Iteration = v
		}
		next.ActivityKey = fmt.Sprintf("iteration:%d", max(next.Iteration, 1))
		next.Message = fmt.Sprintf("iteration %d started", max(next.Iteration, 1))
	case observe.ExecutionLLMStarted:
		next.Status = "running"
		next.Phase = "thinking"
		next.ToolName = ""
		next.ActivityKey = fmt.Sprintf("thinking:%d", max(next.Iteration, 1))
		if model := strings.TrimSpace(event.Model); model != "" {
			next.Message = "calling " + model
		} else {
			next.Message = "calling model"
		}
	case observe.ExecutionToolStarted:
		next.Status = "running"
		next.Phase = "tools"
		next.ToolName = strings.TrimSpace(event.ToolName)
		next.ActivityKey = "tool:" + stringutil.FirstNonEmpty(strings.TrimSpace(event.ToolName), "unknown")
		if next.ToolName != "" {
			next.Message = "running " + next.ToolName
		} else {
			next.Message = "running tool"
		}
	case observe.ExecutionApprovalRequest:
		next.Status = "waiting"
		next.Phase = "approval"
		next.ToolName = strings.TrimSpace(event.ToolName)
		next.ActivityKey = "approval:" + stringutil.FirstNonEmpty(strings.TrimSpace(event.ToolName), "request")
		if next.ToolName != "" {
			next.Message = "approval required for " + next.ToolName
		} else if reason := strings.TrimSpace(stringData(event.Metadata, "reason")); reason != "" {
			next.Message = reason
		} else {
			next.Message = "approval required"
		}
	case observe.ExecutionApprovalResolved:
		next.Status = "running"
		next.Phase = "approval"
		next.ToolName = strings.TrimSpace(event.ToolName)
		next.ActivityKey = "approval:" + stringutil.FirstNonEmpty(strings.TrimSpace(event.ToolName), "request")
		if approved, ok := boolData(event.Metadata, "approved"); ok && !approved {
			next.Message = "approval denied"
		} else if next.ToolName != "" {
			next.Message = "approval granted for " + next.ToolName
		} else {
			next.Message = "approval resolved"
		}
	case observe.ExecutionIterationProgress:
		next.Status = "running"
		next.Phase = "thinking"
		next.ToolName = ""
		if v, ok := intData(event.Metadata, "iteration"); ok && v > 0 {
			next.Iteration = v
		}
		next.ActivityKey = fmt.Sprintf("iteration:%d", max(next.Iteration, 1))
		toolCalls, _ := intData(event.Metadata, "tool_calls")
		stopReason := strings.TrimSpace(stringData(event.Metadata, "stop_reason"))
		tokens, _ := intData(event.Metadata, "tokens")
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
	case observe.ExecutionRunCompleted:
		next.Status = "completed"
		next.Phase = "completed"
		next.ToolName = ""
		next.ActivityKey = "run:completed"
		steps, _ := intData(event.Metadata, "steps")
		tokens, _ := intData(event.Metadata, "tokens")
		switch {
		case steps > 0 && tokens > 0:
			next.Message = fmt.Sprintf("completed in %d steps (%d tokens)", steps, tokens)
		case steps > 0:
			next.Message = fmt.Sprintf("completed in %d steps", steps)
		default:
			next.Message = "run completed"
		}
	case observe.ExecutionRunFailed:
		next.Status = "failed"
		next.Phase = "failed"
		next.ToolName = ""
		next.ActivityKey = "run:failed"
		next.Message = stringutil.FirstNonEmpty(strings.TrimSpace(event.Error), "run failed")
	case observe.ExecutionRunCancelled:
		next.Status = "cancelled"
		next.Phase = "cancelled"
		next.ToolName = ""
		next.ActivityKey = "run:cancelled"
		next.Message = stringutil.FirstNonEmpty(strings.TrimSpace(event.Error), "run cancelled")
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
		if typ == observe.ExecutionRunStarted {
			break
		}
		if typ == observe.ExecutionIterationStarted || typ == observe.ExecutionIterationProgress || typ == observe.ExecutionRunCompleted || typ == observe.ExecutionRunFailed || typ == observe.ExecutionRunCancelled {
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
	Metadata    map[string]any `json:"data"`
}

func executionEventFromStateEntry(entry runtime.StateEntry) (observe.ExecutionEvent, bool) {
	if entry.Kind != runtime.StateKindExecutionEvent {
		return observe.ExecutionEvent{}, false
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
		return observe.ExecutionEvent{}, false
	}
	ts := entry.SortTime.UTC()
	if ts.IsZero() {
		ts = firstTime(entry.UpdatedAt.UTC(), entry.CreatedAt.UTC(), time.Now().UTC())
	}
	return observe.ExecutionEvent{
		Type:        observe.ExecutionEventType(eventType),
		SessionID:   strings.TrimSpace(entry.SessionID),
		Timestamp:   ts,
		ToolName:    strings.TrimSpace(meta.ToolName),
		Model:       strings.TrimSpace(meta.Model),
		Risk:        strings.TrimSpace(meta.Risk),
		ReasonCode:  strings.TrimSpace(meta.ReasonCode),
		Enforcement: io.EnforcementMode(strings.TrimSpace(meta.Enforcement)),
		Duration:    time.Duration(meta.DurationMS) * time.Millisecond,
		Error:       strings.TrimSpace(entry.Status),
		Metadata:    meta.Metadata,
	}, true
}

func stateEntryEventType(entry runtime.StateEntry) observe.ExecutionEventType {
	if len(entry.Metadata) > 0 {
		var meta map[string]json.RawMessage
		if err := json.Unmarshal(entry.Metadata, &meta); err == nil {
			if raw, ok := meta["event_type"]; ok {
				var eventType string
				if json.Unmarshal(raw, &eventType) == nil && strings.TrimSpace(eventType) != "" {
					return observe.ExecutionEventType(strings.TrimSpace(eventType))
				}
			}
		}
	}
	title := strings.TrimSpace(entry.Title)
	if idx := strings.Index(title, ":"); idx >= 0 {
		title = title[:idx]
	}
	return observe.ExecutionEventType(title)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
