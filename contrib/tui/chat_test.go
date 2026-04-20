package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/harness/appkit/product"
	runtimeenv "github.com/mossagents/moss/harness/appkit/product/runtimeenv"
	configpkg "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime/scheduling"
	userapproval "github.com/mossagents/moss/harness/userio/approval"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
	taskrt "github.com/mossagents/moss/kernel/task"
)

type fakeScheduleController struct {
	listFn     func() ([]scheduling.ScheduleItem, error)
	listTextFn func() (string, error)
	cancelFn   func(string) (string, error)
	runNowFn   func(string) (string, error)
}

func (f fakeScheduleController) List() ([]scheduling.ScheduleItem, error) {
	if f.listFn == nil {
		return nil, nil
	}
	return f.listFn()
}

func (f fakeScheduleController) ListText() (string, error) {
	if f.listTextFn == nil {
		return "", nil
	}
	return f.listTextFn()
}

func (f fakeScheduleController) Cancel(id string) (string, error) {
	if f.cancelFn == nil {
		return "", nil
	}
	return f.cancelFn(id)
}

func (f fakeScheduleController) RunNow(id string) (string, error) {
	if f.runNowFn == nil {
		return "", nil
	}
	return f.runNowFn(id)
}

func applyAsyncChatCmd(t *testing.T, m chatModel, cmd tea.Cmd) chatModel {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected async command")
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	return updated
}

func TestSlashCommandStatusSummary(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.trust = "trusted"
	m.profile = "coding"
	m.approvalMode = "confirm"
	m.theme = "plain"
	m.session = &agentSessionOps{info: func() string { return "session summary" }}
	updated, _ := m.handleSlashCommand("/status")
	if len(updated.messages) == 0 {
		t.Fatal("expected a system message")
	}
	last := updated.messages[len(updated.messages)-1]
	if !strings.Contains(last.content, "Runtime status:") || !strings.Contains(last.content, "session summary") {
		t.Fatalf("unexpected message content: %q", last.content)
	}
}

func TestSlashCommandThemeSwitch(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/theme plain")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "plain") || updated.theme != "plain" {
		t.Fatalf("unexpected theme switch output: %+v", last)
	}
}

func TestSlashCommandThemeOpensThemePicker(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.theme = themePlain
	updated, _ := m.handleSlashCommand("/theme")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayTheme {
		t.Fatal("expected theme picker overlay")
	}
	if updated.themePicker == nil || len(updated.themePicker.options) != 3 {
		t.Fatalf("unexpected theme picker: %#v", updated.themePicker)
	}
}

func TestThemePickerSelectionAppliesTheme(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.theme = themePlain
	updated, _ := m.handleSlashCommand("/theme")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.theme != themeDefault {
		t.Fatalf("expected theme %q, got %q", themeDefault, updated.theme)
	}
	if updated.activeOverlay() != nil {
		t.Fatal("expected theme overlay to close after selection")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, themeDefault) {
		t.Fatalf("unexpected theme selection message: %+v", last)
	}
}

func TestSlashCommandStatuslineOpensPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m := newChatModel("openai", "gpt-4o", ".")
	m.experimentalFeatures = []string{product.ExperimentalStatuslineCustomization}
	m.statusLineItems = []string{"model", "thread"}

	updated, _ := m.handleSlashCommand("/statusline")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayStatus {
		t.Fatal("expected statusline picker overlay")
	}
	if updated.statuslinePicker == nil || updated.statuslinePicker.list == nil {
		t.Fatal("expected statusline picker state")
	}
	if !updated.statuslinePicker.list.IsSelected(1) || !updated.statuslinePicker.list.IsSelected(6) {
		t.Fatalf("expected model and thread to be selected, got %#v", updated.statuslinePicker.list.Marked)
	}
}

func TestStatuslinePickerSelectionAppliesItems(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m := newChatModel("openai", "gpt-4o", ".")
	m.experimentalFeatures = []string{product.ExperimentalStatuslineCustomization}
	m.statusLineItems = []string{"model"}

	updated, _ := m.handleSlashCommand("/statusline")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeySpace})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.activeOverlay() != nil {
		t.Fatal("expected statusline overlay to close after selection")
	}
	if want := []string{"model", "workspace"}; !slices.Equal(updated.statusLineItems, want) {
		t.Fatalf("unexpected status line items: got %v want %v", updated.statusLineItems, want)
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Status line updated") {
		t.Fatalf("unexpected statusline selection message: %+v", last)
	}
}

func TestSlashCommandMCPOpensPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cfg := &configpkg.Config{
		Skills: []configpkg.SkillConfig{{
			Name:      "repo",
			Transport: "stdio",
			Command:   "node",
			Args:      []string{"server.js"},
			Enabled:   boolPtr(true),
		}},
	}
	if err := configpkg.SaveConfig(configpkg.DefaultGlobalConfigPath(), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := newChatModel("openai", "gpt-4o", ".")
	m.trust = configpkg.TrustTrusted

	updated, _ := m.handleSlashCommand("/mcp")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayMCP {
		t.Fatal("expected MCP picker overlay")
	}
	if updated.mcpPicker == nil || len(updated.mcpPicker.servers) != 1 {
		t.Fatalf("unexpected MCP picker state: %#v", updated.mcpPicker)
	}
}

func TestSlashCommandReviewOpensPicker(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", t.TempDir())
	updated, _ := m.handleSlashCommand("/review")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayReview {
		t.Fatal("expected review picker overlay")
	}
	if updated.reviewPicker == nil || len(updated.reviewPicker.options) != 3 {
		t.Fatalf("unexpected review picker state: %#v", updated.reviewPicker)
	}
}

func TestSlashCommandHelpOpensPickerAndInsertsCommand(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayHelp {
		t.Fatal("expected help picker overlay")
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.activeOverlay() != nil {
		t.Fatal("expected help overlay to close after insertion")
	}
	if got := updated.textarea.Value(); got != "/new " {
		t.Fatalf("expected inserted command, got %q", got)
	}
}

func TestSlashCommandDebugConfig(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.debugConfigFn = func() string { return "debug config" }
	updated, _ := m.handleSlashCommand("/debug-config")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || last.content != "debug config" {
		t.Fatalf("unexpected debug config output: %+v", last)
	}
}

func TestSlashCommandModelOpensModelPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &configpkg.Config{
		Models: []configpkg.ModelConfig{
			{Provider: configpkg.APITypeOpenAICompletions, Name: "OpenAI", Model: "gpt-4o", Default: true},
			{Provider: configpkg.APITypeClaude, Name: "Anthropic", Model: "claude-sonnet-4.5"},
		},
	}
	if err := configpkg.SaveConfig(configpkg.DefaultGlobalConfigPath(), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := newChatModel("OpenAI (openai-completions)", "gpt-4o", ".")
	m.setProviderIdentity(configpkg.APITypeOpenAICompletions, "OpenAI")
	m.trust = configpkg.TrustTrusted
	m.modelAuto = true

	updated, _ := m.handleSlashCommand("/model")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayModel {
		t.Fatal("expected model picker overlay")
	}
	if updated.modelPicker == nil || len(updated.modelPicker.options) != 3 {
		t.Fatalf("expected auto plus configured models, got %#v", updated.modelPicker)
	}
	if updated.modelPicker.options[0].title != "Auto" || updated.modelPicker.options[1].title != "gpt-4o" {
		t.Fatalf("unexpected picker options: %#v", updated.modelPicker.options)
	}
}

func TestModelPickerSelectionReturnsSwitchModelMsg(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &configpkg.Config{
		Models: []configpkg.ModelConfig{
			{Provider: configpkg.APITypeOpenAICompletions, Name: "OpenAI", Model: "gpt-4o", Default: true},
			{Provider: configpkg.APITypeClaude, Name: "Anthropic", Model: "claude-sonnet-4.5"},
		},
	}
	if err := configpkg.SaveConfig(configpkg.DefaultGlobalConfigPath(), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	m := newChatModel("OpenAI (openai-completions)", "gpt-4o", ".")
	m.setProviderIdentity(configpkg.APITypeOpenAICompletions, "OpenAI")
	m.trust = configpkg.TrustTrusted
	m.modelAuto = true

	updated, _ := m.handleSlashCommand("/model")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected switch model command")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchModelMsg)
	if !ok {
		t.Fatalf("expected switchModelMsg, got %T", msg)
	}
	if switchMsg.provider != configpkg.APITypeOpenAICompletions || switchMsg.model != "gpt-4o" || switchMsg.auto {
		t.Fatalf("unexpected switch model message: %+v", switchMsg)
	}
	if updated.activeOverlay() != nil {
		t.Fatal("expected model overlay to close after selection")
	}
	if !updated.streaming {
		t.Fatal("expected model switch to mark chat as streaming")
	}
}

func TestThinkingTimelineShowsToolAndApprovalDetails(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.currentSessionID = "sess_1"
	now := time.Now().UTC()
	m.progress = executionProgressState{
		SessionID: "sess_1",
		Status:    "running",
		Phase:     "thinking",
		Message:   "calling gpt-4o",
		StartedAt: now,
		UpdatedAt: now,
	}
	m.applyProgressSnapshot(m.progress, true)

	updated, _ := m.handleBridge(bridgeMsg{output: &io.OutputMessage{
		Type:    io.OutputToolStart,
		Content: "http_request",
		Meta: map[string]any{
			"tool":         "http_request",
			"call_id":      "call-http",
			"args_preview": `{"url":"https://wttr.in/hangzhou?format=j1"}`,
		},
	}})
	transcript := renderAllMessages(updated.messages, 120, false)
	if !strings.Contains(transcript, "│ tool · Http Request") {
		t.Fatalf("expected compact tool item in transcript, got %q", transcript)
	}
	if strings.Contains(transcript, "starting Http Request https://wttr.in/hangzhou?format=j1") {
		t.Fatalf("expected noisy tool lifecycle progress to stay hidden, got %q", transcript)
	}

	updated, _ = updated.handleBridge(bridgeMsg{ask: &bridgeAsk{
		request: io.InputRequest{
			Type: io.InputConfirm,
			Approval: &io.ApprovalRequest{
				ID:          "approval-1",
				SessionID:   "sess_1",
				ToolName:    "http_request",
				Risk:        "medium",
				Reason:      "network access",
				Input:       json.RawMessage(`{"url":"https://wttr.in/hangzhou?format=j1"}`),
				RequestedAt: now.Add(time.Second),
			},
		},
		replyCh: make(chan io.InputResponse, 1),
	}})
	transcript = renderAllMessages(updated.messages, 120, false)
	if !strings.Contains(transcript, "Approval required. Review the requested action and choose how to proceed.") {
		t.Fatalf("expected concise approval notice in transcript, got %q", transcript)
	}
	for _, unwanted := range []string{"approval required for Http Request https://wttr.in/hangzhou?format=j1", "risk=medium"} {
		if strings.Contains(transcript, unwanted) {
			t.Fatalf("expected approval transcript to hide verbose detail %q in %q", unwanted, transcript)
		}
	}

	updated, _ = updated.handleBridge(bridgeMsg{output: &io.OutputMessage{
		Type:    io.OutputToolResult,
		Content: `{"status":200,"body":"ok"}`,
		Meta: map[string]any{
			"tool":        "http_request",
			"call_id":     "call-http",
			"duration_ms": int64(174),
		},
	}})
	transcript = renderAllMessages(updated.messages, 120, false)
	if !strings.Contains(transcript, `"status": 200`) {
		t.Fatalf("expected tool result summary in transcript, got %q", transcript)
	}
	if strings.Count(transcript, "Http Request") != 1 {
		t.Fatalf("expected a single compact tool entry after completion, got %q", transcript)
	}
}

func TestHandleBridge_AppendsReasoningTranscript(t *testing.T) {
	m := newChatModel("openai-completions", "deepseek-reasoner", ".")
	m.streaming = true

	updated, _ := m.handleBridge(bridgeMsg{output: &io.OutputMessage{
		Type:    io.OutputReasoning,
		Content: "First inspect the redirect chain.",
	}})
	updated, _ = updated.handleBridge(bridgeMsg{output: &io.OutputMessage{
		Type:    io.OutputReasoning,
		Content: " Then query the API.",
	}})

	if len(updated.messages) == 0 {
		t.Fatal("expected reasoning message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgReasoning {
		t.Fatalf("last kind = %v, want reasoning", last.kind)
	}
	if !strings.Contains(last.content, "First inspect the redirect chain. Then query the API.") {
		t.Fatalf("unexpected reasoning content: %q", last.content)
	}
}

func TestHandleBridge_AppendsAdjacentReasoningWhenNotStreaming(t *testing.T) {
	m := newChatModel("openai-completions", "deepseek-reasoner", ".")

	updated, _ := m.handleBridge(bridgeMsg{output: &io.OutputMessage{
		Type:    io.OutputReasoning,
		Content: "这个",
	}})
	updated, _ = updated.handleBridge(bridgeMsg{output: &io.OutputMessage{
		Type:    io.OutputReasoning,
		Content: "项目的其他文件",
	}})

	if len(updated.messages) != 1 {
		t.Fatalf("expected 1 reasoning message, got %d", len(updated.messages))
	}
	if updated.messages[0].kind != msgReasoning {
		t.Fatalf("kind = %v, want reasoning", updated.messages[0].kind)
	}
	if updated.messages[0].content != "这个项目的其他文件" {
		t.Fatalf("content = %q", updated.messages[0].content)
	}
}

func TestSlashCommandDebugToggleAndPreview(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.debugConfigFn = func() string { return "debug config" }
	m.inspect = &agentInspectOps{debugPrompt: func() string { return "prompt preview body" }}

	updated, _ := m.handleSlashCommand("/debug on")
	if !updated.debugPromptPreview {
		t.Fatal("expected debug preview enabled")
	}
	updated, _ = updated.handleSlashCommand("/debug-config")
	last := updated.messages[len(updated.messages)-1]
	if !strings.Contains(last.content, "debug config") || !strings.Contains(last.content, "Prompt preview:\nprompt preview body") {
		t.Fatalf("unexpected debug-config preview output: %q", last.content)
	}

	updated, _ = updated.handleSlashCommand("/debug off")
	if updated.debugPromptPreview {
		t.Fatal("expected debug preview disabled")
	}
}

func TestSlashCommandResumeRestoresSession(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.session = &agentSessionOps{restore: func(sessionID string) (string, error) {
		if sessionID != "sess-123" {
			t.Fatalf("unexpected session id: %s", sessionID)
		}
		return "Restored session sess-123.", nil
	}}
	updated, cmd := m.handleSlashCommand("/resume sess-123")
	if !updated.streaming {
		t.Fatal("expected /resume to enter busy state")
	}
	updated = applyAsyncChatCmd(t, updated, cmd)
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "sess-123") {
		t.Fatalf("unexpected resume output: %+v", last)
	}
}

func TestSlashCommandResumeOpensPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	store, err := session.NewFileStore(runtimeenv.SessionStoreDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	sess := &session.Session{
		ID:        "sess-picker",
		Status:    session.StatusRunning,
		Config:    session.SessionConfig{Goal: "resume picker", Mode: "interactive", Profile: "default"},
		CreatedAt: time.Now().Add(-time.Minute),
	}
	session.SetThreadPreview(sess, "resume picker")
	session.TouchThreadActivity(sess, time.Now().UTC(), "assistant")
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	m := newChatModel("openai", "gpt-4o", ".")
	m.session = &agentSessionOps{restore: func(sessionID string) (string, error) {
		if sessionID != "sess-picker" {
			t.Fatalf("unexpected session id: %s", sessionID)
		}
		return "Restored session sess-picker.", nil
	}}
	updated, _ := m.handleSlashCommand("/resume")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayResume {
		t.Fatal("expected resume picker overlay")
	}
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.streaming {
		t.Fatal("expected resume picker selection to enter busy state")
	}
	updated = applyAsyncChatCmd(t, updated, cmd)
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "sess-picker") {
		t.Fatalf("unexpected resume picker output: %+v", last)
	}
}

func TestLegacySessionRestoreCommandShowsGuidance(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/session restore sess-123")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "/resume") {
		t.Fatalf("unexpected legacy session guidance: %+v", last)
	}
}

func TestLegacySessionCommandShowsGuidance(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/session")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "/status") {
		t.Fatalf("unexpected legacy session guidance: %+v", last)
	}
}

func TestSlashCommandTraceUnavailable(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/trace")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "No run trace is available yet") {
		t.Fatalf("unexpected /trace unavailable output: %+v", last)
	}
}

func TestSlashCommandTraceRendersLastTrace(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.lastTrace = &product.RunTraceSummary{
		Status: "completed",
		Steps:  2,
		Trace: product.RunTrace{
			PromptTokens:     12,
			CompletionTokens: 6,
			TotalTokens:      18,
			LLMCalls:         1,
			Timeline: []product.TraceEvent{
				{Kind: "session", Type: "running"},
				{Kind: "llm_call", Model: "gpt-5", Type: "end_turn", DurationMS: 42, TotalTokens: 18},
			},
		},
	}
	updated, _ := m.handleSlashCommand("/trace 1")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system trace message, got %v", last.kind)
	}
	for _, want := range []string{"Last run trace:", "timeline: showing 1 of 2 events", "[llm] model=gpt-5 stop=end_turn duration=42ms tokens=18"} {
		if !strings.Contains(last.content, want) {
			t.Fatalf("trace output missing %q:\n%s", want, last.content)
		}
	}
}

func TestSlashCommandAgentValidation(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.task = &agentTaskOps{list: func(status string, limit int) (string, error) { return "ok", nil }}
	updated, _ := m.handleSlashCommand("/agent bad")
	if len(updated.messages) == 0 {
		t.Fatal("expected validation message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
	}
}

func TestSlashCommandAgentOpensPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	rt, err := taskrt.NewFileTaskRuntime(runtimeenv.TaskRuntimeDir())
	if err != nil {
		t.Fatalf("new task runtime: %v", err)
	}
	if err := rt.UpsertTask(context.Background(), taskrt.TaskRecord{
		ID:        "task-1",
		AgentName: "code-review",
		Goal:      "Review changes",
		Status:    taskrt.TaskRunning,
		SessionID: "sess-picker",
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}

	m := newChatModel("openai", "gpt-4o", ".")
	m.task = &agentTaskOps{list: func(status string, limit int) (string, error) { return "legacy list", nil }}
	updated, _ := m.handleSlashCommand("/agent")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayAgent {
		t.Fatal("expected agent picker overlay")
	}
	if updated.agentPicker == nil || len(updated.agentPicker.tasks) != 1 {
		t.Fatalf("unexpected agent picker state: %#v", updated.agentPicker)
	}
}

func TestSlashCommandAgentCancel(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.task = &agentTaskOps{
		cancel: func(taskID, reason string) (string, error) {
			if taskID != "t1" {
				t.Fatalf("unexpected taskID: %s", taskID)
			}
			return "cancelled", nil
		},
		list: func(status string, limit int) (string, error) { return "ok", nil },
	}
	updated, _ := m.handleSlashCommand("/agent cancel t1 because")
	if len(updated.messages) == 0 {
		t.Fatal("expected cancel output message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.content != "cancelled" {
		t.Fatalf("unexpected message content: %q", last.content)
	}
}

func TestLegacyTasksCommandsShowGuidance(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/tasks running")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "/agent") {
		t.Fatalf("unexpected /tasks guidance: %+v", last)
	}
	updated, _ = m.handleSlashCommand("/task cancel t1")
	last = updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "/agent") {
		t.Fatalf("unexpected /task guidance: %+v", last)
	}
}

func TestSlashCommandSchedules(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.scheduleCtrl = fakeScheduleController{
		listFn: func() ([]scheduling.ScheduleItem, error) {
			return []scheduling.ScheduleItem{{ID: "review", Schedule: "@every 10m", Goal: "Run review"}}, nil
		},
	}
	updated, _ := m.handleSlashCommand("/schedules")
	if updated.scheduleBrowser == nil {
		t.Fatal("expected interactive schedule browser")
	}
	if len(updated.scheduleBrowser.items) != 1 {
		t.Fatalf("unexpected schedule count: %d", len(updated.scheduleBrowser.items))
	}
	if updated.scheduleBrowser.items[0].ID != "review" {
		t.Fatalf("unexpected schedule id: %q", updated.scheduleBrowser.items[0].ID)
	}
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlaySchedule {
		t.Fatal("expected schedule browser to open via overlay stack")
	}
}

func TestSlashCommandForkOpensPicker(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	store, err := session.NewFileStore(runtimeenv.SessionStoreDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	sess := &session.Session{
		ID:        "sess-fork",
		Status:    session.StatusRunning,
		Config:    session.SessionConfig{Goal: "fork picker", Mode: "interactive", Profile: "default"},
		CreatedAt: time.Now().Add(-time.Minute),
	}
	session.SetThreadPreview(sess, "fork picker")
	session.TouchThreadActivity(sess, time.Now().UTC(), "assistant")
	if err := store.Save(context.Background(), sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpoint = &agentCheckpointOps{fork: func(sourceKind, sourceID string, restoreWorktree bool) (string, error) {
		if sourceKind != string(checkpoint.ForkSourceSession) || sourceID != "sess-fork" || !restoreWorktree {
			t.Fatalf("unexpected fork args kind=%q id=%q restore=%v", sourceKind, sourceID, restoreWorktree)
		}
		return "Forked thread sess-fork.", nil
	}}
	updated, _ := m.handleSlashCommand("/fork")
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayFork {
		t.Fatal("expected fork picker overlay")
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	updated, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.streaming {
		t.Fatal("expected fork picker selection to enter busy state")
	}
	updated = applyAsyncChatCmd(t, updated, cmd)
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "sess-fork") {
		t.Fatalf("unexpected fork picker output: %+v", last)
	}
}

func TestScheduleBrowserDelete(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	items := []scheduling.ScheduleItem{
		{ID: "old-job", Schedule: "@every 1h"},
		{ID: "keep-job", Schedule: "@every 2h"},
	}
	m.scheduleCtrl = fakeScheduleController{
		listFn: func() ([]scheduling.ScheduleItem, error) {
			cp := make([]scheduling.ScheduleItem, len(items))
			copy(cp, items)
			return cp, nil
		},
		cancelFn: func(id string) (string, error) {
			filtered := make([]scheduling.ScheduleItem, 0, len(items))
			for _, item := range items {
				if item.ID != id {
					filtered = append(filtered, item)
				}
			}
			items = filtered
			return "deleted " + id, nil
		},
	}
	updated, _ := m.handleSlashCommand("/schedules")
	if updated.scheduleBrowser == nil {
		t.Fatal("expected schedule browser")
	}
	updated, _ = updated.handleScheduleBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if updated.scheduleBrowser == nil {
		t.Fatal("expected schedule browser to stay open")
	}
	if len(updated.scheduleBrowser.items) != 1 {
		t.Fatalf("expected one remaining schedule, got %d", len(updated.scheduleBrowser.items))
	}
	if updated.scheduleBrowser.items[0].ID != "keep-job" {
		t.Fatalf("unexpected remaining schedule: %q", updated.scheduleBrowser.items[0].ID)
	}
	if !strings.Contains(updated.scheduleBrowser.message, "deleted old-job") {
		t.Fatalf("unexpected browser message: %q", updated.scheduleBrowser.message)
	}
}

func TestScheduleBrowserRunNow(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	called := ""
	m.scheduleCtrl = fakeScheduleController{
		listFn: func() ([]scheduling.ScheduleItem, error) {
			return []scheduling.ScheduleItem{{ID: "review", Schedule: "@every 10m"}}, nil
		},
		runNowFn: func(id string) (string, error) {
			called = id
			return "started " + id, nil
		},
	}
	updated, _ := m.handleSlashCommand("/schedules")
	if updated.scheduleBrowser == nil {
		t.Fatal("expected schedule browser")
	}
	updated, _ = updated.handleScheduleBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if called != "review" {
		t.Fatalf("unexpected run-now id: %q", called)
	}
	if !strings.Contains(updated.scheduleBrowser.message, "started review") {
		t.Fatalf("unexpected browser message: %q", updated.scheduleBrowser.message)
	}
}

func TestNewChatModelInputHeightDefaultAndClamp(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	if got := m.textarea.Height(); got != 1 {
		t.Fatalf("default textarea height=%d, want 1", got)
	}
	m.textarea.SetValue("1\n2\n3\n4\n5\n6")
	m.adjustInputHeight()
	if got := m.textarea.Height(); got != 5 {
		t.Fatalf("clamped textarea height=%d, want 5", got)
	}
}

func TestAdjustInputHeightCountsSoftWrappedLines(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 44
	m.textarea.SetWidth(m.inputWrapWidth())
	m.textarea.SetValue(strings.Repeat("x", m.inputWrapWidth()+1))
	m.adjustInputHeight()
	if got := m.textarea.Height(); got != 2 {
		t.Fatalf("wrapped textarea height=%d, want 2", got)
	}
}

func TestNewChatModelRemovesTextareaPrompt(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.textarea.SetWidth(80)
	if strings.Contains(m.textarea.View(), "┃ ") {
		t.Fatalf("expected composer textarea to omit internal prompt, got %q", m.textarea.View())
	}
}

func TestInputWrapWidthUsesMainColumnWidth(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 160
	want := m.mainWidth() - inputBorderStyle.GetHorizontalFrameSize()
	if got := m.inputWrapWidth(); got != want {
		t.Fatalf("inputWrapWidth=%d, want %d", got, want)
	}
}

func TestComposerRenderMatchesMainColumnWidth(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 160
	m.height = 30
	m.recalcLayout()
	rendered := inputBorderStyle.Render(m.textarea.View())
	if got, want := lipgloss.Width(rendered), m.mainWidth(); got != want {
		t.Fatalf("composer width=%d, want %d; rendered=%q", got, want, rendered)
	}
}

func TestGenerateLayoutSeparatesMainAndEditorRegions(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 160
	m.height = 40
	m.textarea.SetValue("hello")
	m.adjustInputHeight()

	layout := m.generateLayout()
	if layout.MainWidth != m.mainWidth() {
		t.Fatalf("main width=%d, want %d", layout.MainWidth, m.mainWidth())
	}
	if layout.EditorHeight <= 0 {
		t.Fatalf("expected positive editor height, got %d", layout.EditorHeight)
	}
	if layout.ViewportHeight < 3 {
		t.Fatalf("expected viewport height >= 3, got %d", layout.ViewportHeight)
	}
}

func TestGenerateLayoutHidesEditorWhenOverlayActive(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 160
	m.height = 40
	m.openScheduleOverlay(nil)

	layout := m.generateLayout()
	if layout.EditorHeight != 0 {
		t.Fatalf("expected overlay to hide editor region, got %d", layout.EditorHeight)
	}
}

func TestRenderEditorPaneIncludesProgressBlock(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 120
	m.height = 30
	m.progress = executionProgressState{
		SessionID: "sess-1",
		Status:    "running",
		Phase:     "thinking",
		Message:   "planning changes",
		StartedAt: time.Now().Add(-time.Second),
		UpdatedAt: time.Now(),
	}
	m.textarea.SetValue("hello")
	m.adjustInputHeight()
	m.recalcLayout()

	rendered := m.renderEditorPane(m.generateLayout())
	if strings.Contains(rendered, "Progress:") || strings.Contains(rendered, "Thinking") || strings.Contains(rendered, "planning changes") {
		t.Fatalf("expected progress block to move out of editor pane, got %q", rendered)
	}
}

func TestNewChatModelSupportsMultilineBindings(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	keys := m.textarea.KeyMap.InsertNewline.Keys()
	for _, want := range []string{"shift+enter", "alt+enter"} {
		if !slices.Contains(keys, want) {
			t.Fatalf("missing multiline binding %q in %v", want, keys)
		}
	}
}

func TestSlashCommandResumeUnavailable(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/resume")
	if len(updated.messages) == 0 {
		t.Fatal("expected message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
	}
}

func TestLegacyOffloadCommandShowsGuidance(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/offload")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "/compact") {
		t.Fatalf("unexpected legacy offload guidance: %+v", last)
	}
}

func TestRetiredSlashCommandsShowGuidance(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{command: "/budget", want: "/status"},
		{command: "/models", want: "/model"},
		{command: "/quit", want: "/exit"},
	}
	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			m := newChatModel("openai", "gpt-4o", ".")
			updated, _ := m.handleSlashCommand(tt.command)
			last := updated.messages[len(updated.messages)-1]
			if last.kind != msgError || !strings.Contains(last.content, tt.want) {
				t.Fatalf("unexpected retired command guidance for %s: %+v", tt.command, last)
			}
		})
	}
}

func TestSlashCommandInitScaffoldsWorkspaceBootstrap(t *testing.T) {
	configpkg.SetAppName("mosscode")
	workspace := t.TempDir()
	m := newChatModel("openai", "gpt-4o", workspace)
	updated, _ := m.handleSlashCommand("/init")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "AGENTS.md") {
		t.Fatalf("unexpected /init output: %+v", last)
	}
	if _, err := os.Stat(filepath.Join(workspace, "AGENTS.md")); err != nil {
		t.Fatalf("expected AGENTS.md: %v", err)
	}
}

func TestCustomSlashCommandDispatchesPrompt(t *testing.T) {
	configpkg.SetAppName("mosscode")
	workspace := t.TempDir()
	commandDir := filepath.Join(workspace, ".mosscode", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandDir, "review-pr.md"), []byte("Review this change: {{args}}"), 0o600); err != nil {
		t.Fatalf("write command: %v", err)
	}
	m := newChatModel("openai", "gpt-4o", workspace)
	if notice := m.syncCustomCommands(); notice != "" {
		t.Fatalf("syncCustomCommands notice: %s", notice)
	}
	dispatched := ""
	m.sendFn = func(text string, _ []model.ContentPart) { dispatched = text }
	updated, _ := m.handleSlashCommand("/review-pr focus tests")
	if !updated.streaming {
		t.Fatal("expected custom command to start a run")
	}
	if !strings.Contains(dispatched, "focus tests") {
		t.Fatalf("unexpected dispatched prompt: %q", dispatched)
	}
}

func TestSlashCommandSearchDispatchesPrompt(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	dispatched := ""
	m.sendFn = func(text string, _ []model.ContentPart) { dispatched = text }
	updated, _ := m.handleSlashCommand("/search recent golang releases")
	if !updated.streaming {
		t.Fatal("expected /search to start a run")
	}
	if !strings.Contains(dispatched, "web_search") || !strings.Contains(dispatched, "golang releases") {
		t.Fatalf("unexpected /search prompt: %q", dispatched)
	}
}

func TestSlashCommandPlanReturnsPlanningSwitchMsg(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, cmd := m.handleSlashCommand("/plan Draft a migration plan")
	if !updated.streaming {
		t.Fatal("expected /plan to mark chat as streaming")
	}
	if cmd == nil {
		t.Fatal("expected switch command")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchProfileMsg)
	if !ok {
		t.Fatalf("expected switchProfileMsg, got %T", msg)
	}
	if switchMsg.profile != "planning" {
		t.Fatalf("profile = %q, want planning", switchMsg.profile)
	}
	if !strings.Contains(switchMsg.prompt, "migration plan") {
		t.Fatalf("unexpected inline plan prompt: %q", switchMsg.prompt)
	}
}

func TestSlashCommandNewSuccessClearsVisibleTranscript(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.messages = []chatMessage{
		{kind: msgUser, content: "old message"},
		{kind: msgAssistant, content: "old answer"},
	}
	m.streaming = true
	m.finished = true
	m.result = "done"
	m.queuedInputs = []string{"queued"}
	m.textarea.SetValue("/new")
	m.session = &agentSessionOps{newSess: func() (string, error) {
		return "Previous thread sess_1 auto-saved.\nSwitched to new thread sess_2.", nil
	}}

	updated, cmd := m.handleSlashCommand("/new")
	if !updated.streaming {
		t.Fatal("expected /new to enter busy state")
	}
	updated = applyAsyncChatCmd(t, updated, cmd)
	if len(updated.messages) != 1 {
		t.Fatalf("expected transcript reset to one notice, got %d messages", len(updated.messages))
	}
	last := updated.messages[0]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
	if !strings.Contains(last.content, "Switched to new thread sess_2") {
		t.Fatalf("unexpected /new output: %q", last.content)
	}
	if updated.streaming || updated.finished {
		t.Fatal("expected fresh idle chat state after /new")
	}
	if updated.result != "" || len(updated.queuedInputs) != 0 {
		t.Fatal("expected result and queue reset after /new")
	}
	if updated.textarea.Value() != "" {
		t.Fatalf("expected cleared textarea, got %q", updated.textarea.Value())
	}
}

func TestSlashCommandNewBusySessionRejected(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.session = &agentSessionOps{newSess: func() (string, error) {
		return "", errors.New("cannot create a new thread while a run is active")
	}}

	updated, cmd := m.handleSlashCommand("/new")
	updated = applyAsyncChatCmd(t, updated, cmd)
	if len(updated.messages) == 0 {
		t.Fatal("expected rejection message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
	}
	if !strings.Contains(last.content, "run is active") {
		t.Fatalf("unexpected busy error: %q", last.content)
	}
}

func TestSlashCommandCheckpointListSuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpoint = &agentCheckpointOps{list: func(limit int) (string, error) {
		if limit != 20 {
			t.Fatalf("limit = %d, want 20", limit)
		}
		return "Checkpoints:\n- cp-1", nil
	}}
	updated, _ := m.handleSlashCommand("/checkpoint list")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "cp-1") {
		t.Fatalf("unexpected checkpoint list output: %+v", last)
	}
}

func TestSlashCommandCheckpointShowSuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpoint = &agentCheckpointOps{show: func(checkpointID string) (string, error) {
		if checkpointID != "cp-1" {
			t.Fatalf("checkpointID = %q, want cp-1", checkpointID)
		}
		return "Checkpoint: cp-1\n  metadata: source, trigger", nil
	}}
	updated, _ := m.handleSlashCommand("/checkpoint show cp-1")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Checkpoint: cp-1") {
		t.Fatalf("unexpected checkpoint show output: %+v", last)
	}
}

func TestSlashCommandCheckpointShowLatestSuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpoint = &agentCheckpointOps{show: func(checkpointID string) (string, error) {
		if checkpointID != "latest" {
			t.Fatalf("checkpointID = %q, want latest", checkpointID)
		}
		return "Checkpoint: cp-latest", nil
	}}
	updated, _ := m.handleSlashCommand("/checkpoint show latest")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "cp-latest") {
		t.Fatalf("unexpected checkpoint show latest output: %+v", last)
	}
}

func TestSlashCommandCheckpointShowUnavailable(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/checkpoint show cp-1")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "Checkpoint detail is unavailable") {
		t.Fatalf("unexpected checkpoint show unavailable output: %+v", last)
	}
}

func TestSlashCommandCheckpointShowRequiresID(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpoint = &agentCheckpointOps{show: func(checkpointID string) (string, error) {
		return "", nil
	}}
	updated, _ := m.handleSlashCommand("/checkpoint show")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "Usage: /checkpoint show <checkpoint_id|latest>") {
		t.Fatalf("unexpected checkpoint show validation output: %+v", last)
	}
}

func TestSlashCommandCheckpointReplaySwitchesTranscript(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.messages = []chatMessage{{kind: msgUser, content: "old"}}
	m.streaming = true
	m.finished = true
	m.result = "done"
	m.queuedInputs = []string{"queued"}
	m.checkpoint = &agentCheckpointOps{replay: func(checkpointID, mode string, restore bool) (string, error) {
		if checkpointID != "cp-1" || mode != "rerun" || !restore {
			t.Fatalf("unexpected replay args id=%q mode=%q restore=%v", checkpointID, mode, restore)
		}
		return "Switched to replay session sess_2 from checkpoint cp-1 (rerun).", nil
	}}
	updated, cmd := m.handleSlashCommand("/checkpoint replay cp-1 rerun restore")
	if !updated.streaming {
		t.Fatal("expected replay to enter busy state")
	}
	updated = applyAsyncChatCmd(t, updated, cmd)
	if len(updated.messages) != 1 {
		t.Fatalf("expected transcript reset, got %d messages", len(updated.messages))
	}
	last := updated.messages[0]
	if last.kind != msgSystem || !strings.Contains(last.content, "sess_2") {
		t.Fatalf("unexpected replay output: %+v", last)
	}
	if updated.streaming || updated.finished || updated.result != "" || len(updated.queuedInputs) != 0 {
		t.Fatal("expected fresh idle state after replay switch")
	}
}

func TestSlashCommandCheckpointReplayDefaultsToLatest(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.checkpoint = &agentCheckpointOps{replay: func(checkpointID, mode string, restore bool) (string, error) {
		if checkpointID != "" || mode != "rerun" || !restore {
			t.Fatalf("unexpected replay args id=%q mode=%q restore=%v", checkpointID, mode, restore)
		}
		return "Switched to replay session sess_latest from checkpoint cp-latest (rerun).", nil
	}}
	updated, cmd := m.handleSlashCommand("/checkpoint replay rerun restore")
	updated = applyAsyncChatCmd(t, updated, cmd)
	last := updated.messages[0]
	if last.kind != msgSystem || !strings.Contains(last.content, "cp-latest") {
		t.Fatalf("unexpected replay latest output: %+v", last)
	}
}

func TestSlashCommandForkLatestShorthand(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.checkpoint = &agentCheckpointOps{fork: func(sourceKind, sourceID string, restore bool) (string, error) {
		if sourceKind != string(checkpoint.ForkSourceCheckpoint) || sourceID != "" || !restore {
			t.Fatalf("unexpected fork args kind=%q id=%q restore=%v", sourceKind, sourceID, restore)
		}
		return "Switched to forked thread sess_latest from checkpoint cp-latest.", nil
	}}
	updated, cmd := m.handleSlashCommand("/fork latest restore")
	updated = applyAsyncChatCmd(t, updated, cmd)
	last := updated.messages[0]
	if last.kind != msgSystem || !strings.Contains(last.content, "cp-latest") {
		t.Fatalf("unexpected fork latest output: %+v", last)
	}
}

func TestHelpIncludesCheckpointAndCoreRecoveryCommands(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	help := updated.renderHelpPicker(120)
	if !strings.Contains(help, "/status") || !strings.Contains(help, "/resume") || !strings.Contains(help, "/fork") || !strings.Contains(help, "/compact") || !strings.Contains(help, "/plan") || !strings.Contains(help, "/init") || !strings.Contains(help, "/debug-config") || !strings.Contains(help, "/theme") || !strings.Contains(help, "/agent") || !strings.Contains(help, "/trace") {
		t.Fatalf("help missing checkpoint commands: %q", help)
	}
	if !strings.Contains(help, "Press Enter to insert the command into the composer.") {
		t.Fatalf("help missing picker guidance: %q", help)
	}
}

func TestHelpIncludesCustomCommands(t *testing.T) {
	configpkg.SetAppName("mosscode")
	workspace := t.TempDir()
	commandDir := filepath.Join(workspace, ".mosscode", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandDir, "review-pr.md"), []byte("# Review repo"), 0o600); err != nil {
		t.Fatalf("write command: %v", err)
	}
	m := newChatModel("openai", "gpt-4o", workspace)
	if notice := m.syncCustomCommands(); notice != "" {
		t.Fatalf("syncCustomCommands notice: %s", notice)
	}
	updated, _ := m.handleSlashCommand("/help")
	if updated.helpPicker == nil {
		t.Fatal("expected help picker state")
	}
	found := false
	for _, entry := range updated.helpPicker.entries {
		if entry.command == "/review-pr" && strings.Contains(entry.detail, "Custom commands") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("help missing custom commands: %#v", updated.helpPicker.entries)
	}
}

func TestHelpIncludesChangeCommands(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	help := updated.renderHelpPicker(120)
	for _, want := range []string{"/review", "/inspect"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q in %q", want, help)
		}
	}
	// /diff, /changes, /apply, /rollback, /git are mosscode extension commands —
	// they are no longer in the base tui help catalog.
	for _, moved := range []string{"/diff", "/changes", "/apply", "/rollback"} {
		if strings.Contains(help, moved) {
			t.Fatalf("help should not contain moved command %q", moved)
		}
	}
}

func TestHelpIncludesNewCommand(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	help := updated.renderHelpPicker(120)
	if !strings.Contains(help, "/new") {
		t.Fatalf("help missing /new command: %q", help)
	}
}

func TestSlashCommandCopyCopiesLatestCompletedOutput(t *testing.T) {
	previous := writeClipboard
	defer func() { writeClipboard = previous }()
	copied := ""
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}

	m := newChatModel("openai", "gpt-4o", ".")
	m.messages = []chatMessage{
		{kind: msgUser, content: "user"},
		{kind: msgAssistant, content: "assistant output"},
	}
	updated, _ := m.handleSlashCommand("/copy")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Copied") {
		t.Fatalf("unexpected /copy output: %+v", last)
	}
	if copied != "assistant output" {
		t.Fatalf("copied = %q, want assistant output", copied)
	}
}

func TestSlashCommandMentionAddsComposerAttachment(t *testing.T) {
	configpkg.SetAppName("mosscode")
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	m := newChatModel("openai", "gpt-4o", workspace)
	updated, _ := m.handleSlashCommand("/mention note.txt")
	if len(updated.pendingAttachments) != 1 || updated.pendingAttachments[0].Label != "note.txt" {
		t.Fatalf("unexpected attachments: %#v", updated.pendingAttachments)
	}
}

func TestMentionPickerOpensFromComposerTab(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatalf("write note: %v", err)
	}
	m := newChatModel("openai", "gpt-4o", workspace)
	m.textarea.SetValue("@not")
	// refreshMentionPopup 会在 textarea 更新时触发；直接调用模拟
	m.refreshMentionPopup()
	if m.mentionPopup == nil || len(m.mentionPopup.items) == 0 {
		t.Fatal("expected inline mention popup to be visible after typing @not")
	}
	// Tab 应用补全：选中 note.txt，移除 @token，添加 attachment
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if len(updated.pendingAttachments) != 1 || updated.pendingAttachments[0].Label != "note.txt" {
		t.Fatalf("unexpected attachments after popup tab: %#v", updated.pendingAttachments)
	}
	if updated.mentionPopup != nil {
		t.Fatal("expected mention popup to be closed after applying completion")
	}
}

func TestSlashCommandStatuslineSetPersistsSelection(t *testing.T) {
	configpkg.SetAppName("mosscode")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/statusline set model,thread,fast")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Status line updated") {
		t.Fatalf("unexpected /statusline output: %+v", last)
	}
	if got := strings.Join(updated.statusLineItems, ","); got != "model,thread,fast" {
		t.Fatalf("statusLineItems = %q", got)
	}
}

func TestStatusLineBarRendersConfiguredItems(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", "/home/user/myproject")
	m.ready = true
	m.width = 160
	m.height = 40
	m.recalcLayout()
	m.currentSessionID = "abcdef1234567890"
	m.statusLineItems = []string{"model", "workspace", "thread"}
	m.messages = []chatMessage{
		{kind: msgUser, content: "hello"},
		{kind: msgAssistant, content: "hi"},
	}

	bar := m.renderStatusPane(160)
	for _, want := range []string{"gpt-4o", "myproject", "thread abcdef"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("status bar missing %q:\n%s", want, bar)
		}
	}
}

func TestStatusLineBarSkipsEmptyItems(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.statusLineItems = []string{"model", "thread", "fast"}
	m.fastMode = false
	bar := m.renderStatusLineBar()
	if strings.Contains(bar, "fast") {
		t.Fatalf("expected fast to be omitted when fastMode=false, got %q", bar)
	}
	m.fastMode = true
	bar = m.renderStatusLineBar()
	if !strings.Contains(bar, "fast") {
		t.Fatalf("expected fast to appear when fastMode=true, got %q", bar)
	}
}

func TestSlashCommandFastUpdatesPromptMode(t *testing.T) {
	configpkg.SetAppName("mosscode")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	refreshed := false
	m := newChatModel("openai", "gpt-4o", ".")
	m.inspect = &agentInspectOps{refreshSP: func() error {
		refreshed = true
		return nil
	}}
	updated, _ := m.handleSlashCommand("/fast on")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Fast mode on") {
		t.Fatalf("unexpected /fast output: %+v", last)
	}
	if !updated.fastMode || !refreshed {
		t.Fatalf("expected fastMode enabled and prompt refreshed: fast=%v refreshed=%v", updated.fastMode, refreshed)
	}
}

func TestSlashCommandPersonalityUpdatesPromptMode(t *testing.T) {
	configpkg.SetAppName("mosscode")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	refreshed := false
	m := newChatModel("openai", "gpt-4o", ".")
	m.inspect = &agentInspectOps{refreshSP: func() error {
		refreshed = true
		return nil
	}}
	updated, _ := m.handleSlashCommand("/personality pragmatic")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "pragmatic") {
		t.Fatalf("unexpected /personality output: %+v", last)
	}
	if updated.personality != product.PersonalityPragmatic || !refreshed {
		t.Fatalf("expected pragmatic personality and prompt refresh, got personality=%q refreshed=%v", updated.personality, refreshed)
	}
}

func TestSlashCommandExperimentalDisableAffectsFeatureGate(t *testing.T) {
	configpkg.SetAppName("mosscode")
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())

	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/experimental disable background-ps")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "background-ps") {
		t.Fatalf("unexpected /experimental output: %+v", last)
	}
	if updated.experimentalEnabled(product.ExperimentalBackgroundPS) {
		t.Fatal("expected background-ps feature to be disabled")
	}
}

func TestCtrlCSingleClearsInput(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.textarea.SetValue("hello")
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(cancelMsg); ok {
			t.Fatal("single ctrl+c should not quit")
		}
	}
	if updated.textarea.Value() != "" {
		t.Fatalf("expected cleared input, got %q", updated.textarea.Value())
	}
}

func TestCtrlCDoubleQuits(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	now = now.Add(200 * time.Millisecond)
	updated2, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("expected quit command on double ctrl+c")
	}
	if _, ok := cmd().(cancelMsg); !ok {
		t.Fatal("expected cancelMsg on double ctrl+c")
	}
	_ = updated2
}

func TestMouseClickDoesNotInsertComposerGarbage(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	updated, _ := m.Update(tea.MouseMsg{
		X:      8,
		Y:      8,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if updated.textarea.Value() != "" {
		t.Fatalf("expected mouse click to be ignored, got %q", updated.textarea.Value())
	}

	updated.textarea.SetValue("hello")
	updated, _ = updated.Update(tea.MouseMsg{
		X:      12,
		Y:      8,
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
	})
	if updated.textarea.Value() != "hello" {
		t.Fatalf("expected mouse motion to preserve composer text, got %q", updated.textarea.Value())
	}
}

func TestMouseWheelScrollsViewportWithoutTouchingComposer(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 16
	for i := 0; i < 40; i++ {
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: strings.Repeat("line ", 12) + strconv.Itoa(i),
		})
	}
	m.textarea.SetValue("draft")
	m.recalcLayout()
	initialOffset := m.viewport.YOffset

	updated, _ := m.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
	})
	if updated.textarea.Value() != "draft" {
		t.Fatalf("expected mouse wheel to preserve composer text, got %q", updated.textarea.Value())
	}
	if updated.viewport.YOffset >= initialOffset {
		t.Fatalf("expected wheel up to scroll transcript up, offset %d -> %d", initialOffset, updated.viewport.YOffset)
	}
	if updated.pinnedToBottom {
		t.Fatal("expected wheel up to unpin the viewport from bottom")
	}

	down, _ := updated.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	if down.viewport.YOffset <= updated.viewport.YOffset {
		t.Fatalf("expected wheel down to scroll transcript down, offset %d -> %d", updated.viewport.YOffset, down.viewport.YOffset)
	}
}

func TestArrowKeysNavigateInputHistory(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.inputHistory = []string{"first prompt", "second prompt"}
	m.historyCursor = len(m.inputHistory)
	m.textarea.SetValue("draft prompt")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := updated.textarea.Value(); got != "second prompt" {
		t.Fatalf("expected latest history item on first up, got %q", got)
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := updated.textarea.Value(); got != "first prompt" {
		t.Fatalf("expected previous history item on second up, got %q", got)
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := updated.textarea.Value(); got != "second prompt" {
		t.Fatalf("expected to move forward in history, got %q", got)
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := updated.textarea.Value(); got != "draft prompt" {
		t.Fatalf("expected to restore draft after leaving history, got %q", got)
	}
}

func TestApprovalDecisionButtonLabelsStayCompact(t *testing.T) {
	cases := map[string]string{
		userapproval.ChoiceAllowOnce:    "Allow once",
		userapproval.ChoiceAllowSession: "Session",
		userapproval.ChoiceAllowProject: "Project",
		userapproval.ChoiceDeny:         "Deny",
	}
	for input, want := range cases {
		if got := approvalDecisionButtonLabel(input); got != want {
			t.Fatalf("approvalDecisionButtonLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestApprovalAskRespectsAllowedScopes(t *testing.T) {
	fields := synthesizeFieldsFromInputRequest(io.InputRequest{
		Type: io.InputConfirm,
		Approval: &io.ApprovalRequest{
			ToolName:      "run_command",
			AllowedScopes: []io.DecisionScope{io.DecisionScopeOnce, io.DecisionScopeSession},
			DefaultScope:  io.DecisionScopeSession,
			CacheKey:      "run_command|git push",
			Input:         json.RawMessage(`{"command":"git","args":["push"]}`),
		},
	}, "D:\\Codes\\qiulin\\moss")
	if len(fields) != 1 {
		t.Fatalf("unexpected fields: %#v", fields)
	}
	if slices.Contains(fields[0].Options, userapproval.ChoiceAllowProject) {
		t.Fatalf("project scope should be hidden when not allowed: %#v", fields[0].Options)
	}
	if fields[0].Default != userapproval.ChoiceAllowSession {
		t.Fatalf("unexpected default approval choice: %q", fields[0].Default)
	}
}

func TestEscDoubleCancelsRun(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	cancelled := false
	m.cancelRunFn = func() bool {
		cancelled = true
		return true
	}
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	now = now.Add(200 * time.Millisecond)
	updated2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Fatal("expected run to be cancelled on double esc")
	}
	last := updated2.messages[len(updated2.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
}

func TestAskFormSingleSelectAndConfirm(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:   io.InputForm,
			Prompt: "Choose one",
			Fields: []io.InputField{
				{Name: "database", Type: io.InputFieldSingleSelect, Options: []string{"PostgreSQL", "MySQL"}, Required: true},
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayAsk {
		t.Fatal("expected ask form to open via overlay stack")
	}
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-replyCh:
		got, _ := resp.Form["database"].(string)
		if got != "MySQL" {
			t.Fatalf("expected MySQL, got %q", got)
		}
	default:
		t.Fatal("expected form response")
	}
}

func TestAskFormMultiSelectToggle(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:   io.InputForm,
			Prompt: "Choose features",
			Fields: []io.InputField{
				{Name: "features", Type: io.InputFieldMultiSelect, Options: []string{"A", "B", "C"}, Required: true},
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeySpace})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeySpace})
	_, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-replyCh:
		arr, _ := resp.Form["features"].([]string)
		if len(arr) != 2 || arr[0] != "A" || arr[1] != "C" {
			t.Fatalf("unexpected selected features: %#v", arr)
		}
	default:
		t.Fatal("expected form response")
	}
}

func TestAskFormEscCancelsRunAndClearsForm(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	cancelled := false
	m.cancelRunFn = func() bool {
		cancelled = true
		return true
	}
	m.streaming = true

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:   io.InputForm,
			Prompt: "Choose one",
			Fields: []io.InputField{
				{Name: "opt", Type: io.InputFieldSingleSelect, Options: []string{"A", "B"}, Required: true},
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayAsk {
		t.Fatal("expected ask overlay to be active")
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if !cancelled {
		t.Fatal("expected cancelRunFn to be called on single Esc in ask dialog")
	}
	if updated.pendAsk != nil || updated.askForm != nil {
		t.Fatal("expected ask form to be cleared after Esc")
	}
	if updated.activeOverlay() != nil {
		t.Fatal("expected overlay to be closed after Esc")
	}
	if updated.streaming {
		t.Fatal("expected streaming to be false after Esc cancel")
	}

	select {
	case <-replyCh:
		t.Fatal("unexpected reply on replyCh — channel should not be sent after Esc cancel")
	default:
	}
}

func TestConfirmAskFormUsesBottomSheetStyle(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:         io.InputConfirm,
			Prompt:       "Do you trust the files in this folder?",
			ConfirmLabel: "Confirm folder trust",
			Meta: map[string]any{
				"workspace": `D:\Codes\qiulin\moss`,
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	rendered := updated.renderAskForm(110)
	for _, want := range []string{
		"Confirm folder trust",
		`D:\Codes\qiulin\moss`,
		"Do you trust the files in this folder?",
		"1. Yes",
		"2. No",
		"↑↓ navigate",
		"Enter select",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered confirm form missing %q:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "› 2. No") {
		t.Fatalf("expected confirm dialog to preserve default deny selection:\n%s", rendered)
	}
}

func TestSimpleConfirmAskArrowSelectionAndSubmit(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:   io.InputConfirm,
			Prompt: "Continue?",
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-replyCh:
		if !resp.Approved {
			t.Fatal("expected simple confirm to approve after selecting yes")
		}
	default:
		t.Fatal("expected confirm response")
	}
}

func TestApprovalAskFormShowsStructuredCommandAndOptions(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.currentSessionID = "thread-1"
	m.recalcLayout()

	input, err := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type: io.InputConfirm,
			Approval: &io.ApprovalRequest{
				ID:        "req-1",
				SessionID: "thread-1",
				ToolName:  "run_command",
				Risk:      "high",
				Reason:    "tool requires approval by policy",
				Input:     input,
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	rendered := updated.renderAskForm(100)
	for _, want := range []string{
		"Approval required",
		"Command",
		"git push origin main",
		"Matching rule",
		"1. Allow once",
		"2. Session",
		"3. Project",
		"4. Deny",
		"↑↓ navigate",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered form missing %q:\n%s", want, rendered)
		}
	}
}

func TestConfirmOverlayPlacementUsesBottomSheet(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:   io.InputConfirm,
			Prompt: "Continue?",
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	dialog := updated.activeOverlay()
	if dialog == nil || dialog.ID() != overlayAsk {
		t.Fatal("expected confirm overlay to be active")
	}
	layout := updated.generateLayout()
	width, vertical := updated.overlayPlacement(dialog, layout)
	wantWidth := min(layout.MainWidth, max(56, layout.MainWidth-2))
	if width != wantWidth {
		t.Fatalf("confirm overlay width = %d, want %d", width, wantWidth)
	}
	if vertical != lipgloss.Bottom {
		t.Fatalf("confirm overlay vertical placement = %v, want %v", vertical, lipgloss.Bottom)
	}
}

func TestFormOverlayPlacementRemainsCentered(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type:   io.InputForm,
			Prompt: "Choose one",
			Fields: []io.InputField{
				{Name: "database", Type: io.InputFieldSingleSelect, Options: []string{"PostgreSQL", "MySQL"}, Required: true},
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	dialog := updated.activeOverlay()
	if dialog == nil || dialog.ID() != overlayAsk {
		t.Fatal("expected form overlay to be active")
	}
	layout := updated.generateLayout()
	width, vertical := updated.overlayPlacement(dialog, layout)
	wantWidth := min(84, max(48, layout.MainWidth-12))
	if width != wantWidth {
		t.Fatalf("form overlay width = %d, want %d", width, wantWidth)
	}
	if vertical != lipgloss.Center {
		t.Fatalf("form overlay vertical placement = %v, want %v", vertical, lipgloss.Center)
	}
}

func TestApprovalAllowForSessionRemembersSimilarCommands(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.currentSessionID = "thread-1"
	m.recalcLayout()

	makeAsk := func(id string, args []string) *bridgeAsk {
		input, err := json.Marshal(map[string]any{
			"command": "git",
			"args":    args,
		})
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		return &bridgeAsk{
			request: io.InputRequest{
				Type: io.InputConfirm,
				Approval: &io.ApprovalRequest{
					ID:        id,
					SessionID: "thread-1",
					ToolName:  "run_command",
					Risk:      "high",
					Reason:    "tool requires approval by policy",
					Input:     input,
				},
			},
			replyCh: make(chan io.InputResponse, 1),
		}
	}

	first := makeAsk("req-1", []string{"push", "origin", "main"})
	updated, _ := m.handleBridge(bridgeMsg{ask: first})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRight})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-first.replyCh:
		if !resp.Approved {
			t.Fatal("expected first approval to be granted")
		}
		if resp.Decision == nil || resp.Decision.Source != "tui-session-rule" {
			t.Fatalf("unexpected decision: %#v", resp.Decision)
		}
	default:
		t.Fatal("expected first approval response")
	}
	if got := len(updated.approvalRules["thread-1"]); got != 1 {
		t.Fatalf("remembered rules = %d, want 1", got)
	}

	second := makeAsk("req-2", []string{"push", "origin", "dev"})
	updated, _ = updated.handleBridge(bridgeMsg{ask: second})
	if updated.pendAsk != nil {
		t.Fatal("expected remembered approval to skip interactive form")
	}
	select {
	case resp := <-second.replyCh:
		if !resp.Approved {
			t.Fatal("expected second approval to be auto-approved")
		}
		if resp.Decision == nil || resp.Decision.Source != "tui-session-rule-auto" {
			t.Fatalf("unexpected auto decision: %#v", resp.Decision)
		}
	default:
		t.Fatal("expected second approval response")
	}
}

func TestApprovalSessionRuleDoesNotMatchDifferentCommandPattern(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.currentSessionID = "thread-1"
	m.recalcLayout()
	m.rememberApprovalRule(userapproval.MemoryRule{
		SessionID: "thread-1",
		Key:       "run_command|git push",
		Label:     "git push",
	})

	input, err := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"pull", "origin", "main"},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type: io.InputConfirm,
			Approval: &io.ApprovalRequest{
				ID:        "req-3",
				SessionID: "thread-1",
				ToolName:  "run_command",
				Risk:      "high",
				Reason:    "tool requires approval by policy",
				Input:     input,
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	if updated.pendAsk == nil {
		t.Fatal("expected approval to remain interactive for a different command pattern")
	}
	select {
	case resp := <-replyCh:
		t.Fatalf("unexpected auto approval: %#v", resp)
	default:
	}
}

func TestApprovalAllowForProjectPersistsAndAutoApproves(t *testing.T) {
	configpkg.SetAppName("moss")
	workspace := t.TempDir()
	m := newChatModel("openai", "gpt-4o", workspace)
	m.ready = true
	m.width = 120
	m.height = 40
	m.currentSessionID = "thread-1"
	m.recalcLayout()

	makeAsk := func(id, sessionID string, args []string) *bridgeAsk {
		input, err := json.Marshal(map[string]any{
			"command": "git",
			"args":    args,
		})
		if err != nil {
			t.Fatalf("marshal input: %v", err)
		}
		return &bridgeAsk{
			request: io.InputRequest{
				Type: io.InputConfirm,
				Approval: &io.ApprovalRequest{
					ID:        id,
					SessionID: sessionID,
					ToolName:  "run_command",
					Risk:      "high",
					Reason:    "tool requires approval by policy",
					Input:     input,
				},
			},
			replyCh: make(chan io.InputResponse, 1),
		}
	}

	first := makeAsk("req-1", "thread-1", []string{"push", "origin", "main"})
	updated, _ := m.handleBridge(bridgeMsg{ask: first})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRight})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRight})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case resp := <-first.replyCh:
		if !resp.Approved {
			t.Fatal("expected project approval to be granted")
		}
		if resp.Decision == nil || resp.Decision.Source != "tui-project-rule" {
			t.Fatalf("unexpected decision: %#v", resp.Decision)
		}
	default:
		t.Fatal("expected first approval response")
	}
	if got := len(updated.projectApprovalRules); got != 1 {
		t.Fatalf("remembered project rules = %d, want 1", got)
	}

	projectCfg, err := configpkg.LoadProjectConfig(workspace)
	if err != nil {
		t.Fatalf("load project config: %v", err)
	}
	if got := len(projectCfg.TUI.ProjectApprovalRules); got != 1 {
		t.Fatalf("persisted project rules = %d, want 1", got)
	}

	reloaded := newChatModel("openai", "gpt-4o", workspace)
	reloaded.ready = true
	reloaded.width = 120
	reloaded.height = 40
	reloaded.currentSessionID = "thread-2"
	reloaded.recalcLayout()

	second := makeAsk("req-2", "thread-2", []string{"push", "origin", "dev"})
	reloaded, _ = reloaded.handleBridge(bridgeMsg{ask: second})
	if reloaded.pendAsk != nil {
		t.Fatal("expected project rule to skip interactive approval")
	}
	select {
	case resp := <-second.replyCh:
		if !resp.Approved {
			t.Fatal("expected project approval to auto-approve")
		}
		if resp.Decision == nil || resp.Decision.Source != "tui-project-rule-auto" {
			t.Fatalf("unexpected auto decision: %#v", resp.Decision)
		}
	default:
		t.Fatal("expected second approval response")
	}
}

func TestApprovalProjectPersistenceErrorStaysInlineInDialog(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", "")
	m.ready = true
	m.width = 120
	m.height = 40
	m.currentSessionID = "thread-1"
	m.recalcLayout()

	input, err := json.Marshal(map[string]any{
		"command": "git",
		"args":    []string{"push", "origin", "main"},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	replyCh := make(chan io.InputResponse, 1)
	ask := &bridgeAsk{
		request: io.InputRequest{
			Type: io.InputConfirm,
			Approval: &io.ApprovalRequest{
				ID:        "req-1",
				SessionID: "thread-1",
				ToolName:  "run_command",
				Risk:      "high",
				Reason:    "tool requires approval by policy",
				Input:     input,
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	updated.askForm.fields[0].def.Options = []string{userapproval.ChoiceAllowOnce, userapproval.ChoiceAllowProject, userapproval.ChoiceDeny}
	updated.askForm.fields[0].singleIndex = 1
	updated.askForm.fields[0].singleSel = 1

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.askForm == nil {
		t.Fatal("expected approval dialog to remain open")
	}
	if !strings.Contains(updated.askForm.errorText, "workspace is unavailable") {
		t.Fatalf("unexpected inline error: %q", updated.askForm.errorText)
	}
	select {
	case resp := <-replyCh:
		t.Fatalf("expected no approval response while dialog remains open, got %#v", resp)
	default:
	}
}

func TestSendWhileRunning_QueuesInput(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.streaming = true

	m.textarea.SetValue("queued message")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(updated.queuedInputs) != 1 {
		t.Fatalf("queued inputs=%d, want 1", len(updated.queuedInputs))
	}
	if updated.queuedInputs[0] != "queued message" {
		t.Fatalf("queued input=%q", updated.queuedInputs[0])
	}
	if updated.textarea.Value() != "" {
		t.Fatalf("expected textarea reset, got %q", updated.textarea.Value())
	}
}

func TestSlashSkillCommandDispatchesPrompt(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	sent := ""
	m.sendFn = func(text string, _ []model.ContentPart) { sent = text }

	updated, _ := m.handleSlashCommand("/skill http_request 访问 https://mossagents.github.io/ ，告诉我主要内容")
	if !updated.streaming {
		t.Fatal("expected streaming true after slash skill command")
	}
	if !strings.Contains(sent, "http_request") || !strings.Contains(sent, "mossagents.github.io") {
		t.Fatalf("unexpected dispatched prompt: %q", sent)
	}
}

func TestSlashShortcutCommandDispatchesPrompt(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	sent := ""
	m.sendFn = func(text string, _ []model.ContentPart) { sent = text }

	updated, _ := m.handleSlashCommand("/http_request 访问 https://mossagents.github.io/ ，告诉我主要内容")
	if !updated.streaming {
		t.Fatal("expected streaming true after slash shortcut command")
	}
	if !strings.Contains(sent, "http_request") || !strings.Contains(sent, "mossagents.github.io") {
		t.Fatalf("unexpected dispatched prompt: %q", sent)
	}
}

func TestSlashAutocompleteHintsAndTabCompletion(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	m.textarea.SetValue("/sk")
	m.refreshSlashHints()
	hints := m.currentSlashHints()
	if len(hints) == 0 {
		t.Fatal("expected slash hints for /sk")
	}
	if hints[0] != "/skill" && hints[0] != "/skills" {
		t.Fatalf("unexpected first hint: %q", hints[0])
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if !strings.HasPrefix(updated.textarea.Value(), "/skill") && !strings.HasPrefix(updated.textarea.Value(), "/skills") {
		t.Fatalf("expected tab completion, got %q", updated.textarea.Value())
	}
}

func TestSlashAutocompleteHintsIncludesNew(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	m.textarea.SetValue("/n")
	m.refreshSlashHints()
	hints := m.currentSlashHints()
	if !slices.Contains(hints, "/new") {
		t.Fatalf("expected /new in hints, got %v", hints)
	}

	m.textarea.SetValue("/c")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	if !slices.Contains(hints, "/checkpoint") || !slices.Contains(hints, "/compact") {
		t.Fatalf("expected /checkpoint and /compact in hints, got %v", hints)
	}

	m.textarea.SetValue("/a")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	// /apply is now a mosscode extension command — not in base tui autocomplete.
	if slices.Contains(hints, "/apply") {
		t.Fatalf("extension command /apply should not appear in base tui hints, got %v", hints)
	}

	m.textarea.SetValue("/r")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	if !slices.Contains(hints, "/resume") {
		t.Fatalf("expected /resume in hints, got %v", hints)
	}
	// /rollback is now a mosscode extension command — not in base tui autocomplete.
	if slices.Contains(hints, "/rollback") {
		t.Fatalf("extension command /rollback should not appear in base tui hints, got %v", hints)
	}

	m.textarea.SetValue("/ch")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	// /changes is now a mosscode extension command — not in base tui autocomplete.
	if slices.Contains(hints, "/changes") {
		t.Fatalf("extension command /changes should not appear in base tui hints, got %v", hints)
	}

	m.textarea.SetValue("/tr")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	if !slices.Contains(hints, "/trace") {
		t.Fatalf("expected /trace in hints, got %v", hints)
	}
}

func TestVisibleInputHeightDoesNotChangeWithSlashHints(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	m.textarea.SetValue("hello")
	m.refreshSlashHints()
	withoutHints := m.visibleInputHeight()

	m.textarea.SetValue("/re")
	m.refreshSlashHints()
	withHints := m.visibleInputHeight()

	if withoutHints != withHints {
		t.Fatalf("visible input height changed from %d to %d when slash hints appeared", withoutHints, withHints)
	}
}

func TestRenderHeaderMetaLineIsEmptyWithoutExtensions(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.profile = "planning"
	m.trust = "trusted"
	m.approvalMode = "confirm"
	m.fastMode = true

	// Posture info (profile/trust/approval) moved to composer meta line;
	// header meta line is empty when no extensions provide HeaderMetaWidgets.
	line := m.renderHeaderMetaLine()
	if strings.Contains(line, "planning") || strings.Contains(line, "trusted") {
		t.Fatalf("posture should not appear in header meta without extensions: %q", line)
	}
}

func TestComposerMetaLineShowsPostureInReadyState(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.profile = "planning"
	m.trust = "trusted"
	m.approvalMode = "confirm"
	m.fastMode = true

	line := m.renderComposerMetaLine(120)
	if !strings.Contains(line, "planning · trusted · confirm · fast") {
		t.Fatalf("expected posture in composer meta ready state, got: %q", line)
	}
	// State and thread should not appear here.
	if strings.Contains(line, "Idle") || strings.Contains(line, "thread") {
		t.Fatalf("composer meta should not contain state or thread: %q", line)
	}
}

func TestComposerMetaLineDefaultProfile(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.profile = ""
	m.trust = "trusted"
	m.approvalMode = "confirm"

	line := m.renderComposerMetaLine(120)
	if !strings.Contains(line, "default · trusted · confirm") {
		t.Fatalf("expected default profile in composer meta, got %q", line)
	}
}

func TestRenderSlashHintLineUsesStableFallback(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	// "Ready" state (empty textarea) should show slash hint fallback.
	ready := m.renderComposerMetaLine(120)
	if !strings.Contains(ready, "/ commands") {
		t.Fatalf("expected slash hint in ready state: %q", ready)
	}
}

func TestRenderComposerMetaLineHighlightsReadyAndDraftStates(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	ready := m.renderComposerMetaLine(120)
	if !strings.Contains(ready, "Ready") || !strings.Contains(ready, "/ commands") || !strings.Contains(ready, "@ files") {
		t.Fatalf("unexpected ready composer meta: %q", ready)
	}
	// Model name must not appear in the composer meta bar (it belongs in the header).
	if strings.Contains(ready, "gpt-4o") {
		t.Fatalf("model should not appear in ready composer meta: %q", ready)
	}

	m.textarea.SetValue("hello")
	draft := m.renderComposerMetaLine(120)
	if !strings.Contains(draft, "Draft") || !strings.Contains(draft, "Enter send") {
		t.Fatalf("unexpected draft composer meta: %q", draft)
	}
	if strings.Contains(draft, "gpt-4o") {
		t.Fatalf("model should not appear in draft composer meta: %q", draft)
	}
}

func TestRenderComposerMetaLineShowsRunningContext(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.streaming = true
	m.progress = executionProgressState{
		SessionID: "sess-1",
		Status:    "running",
		Phase:     "tools",
		ToolName:  "run_command",
		Message:   "running run_command",
	}
	line := m.renderComposerMetaLine(120)
	if !strings.Contains(line, "Using Bash") || !strings.Contains(line, "Esc Esc cancel") {
		t.Fatalf("unexpected running composer meta: %q", line)
	}
}

func TestOutputProgressUpdatesRunningSummary(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.streaming = true
	m.currentSessionID = "sess-1"
	m.progress = executionProgressState{
		SessionID: "sess-1",
		Status:    "running",
		Phase:     "tools",
		ToolName:  "powershell",
		StartedAt: time.Now().Add(-time.Second),
	}
	updated, _ := m.handleBridge(bridgeMsg{
		output: &io.OutputMessage{
			Type:    io.OutputProgress,
			Content: "inspecting memory tests",
		},
	})
	if got := updated.progress.Message; got != "inspecting memory tests" {
		t.Fatalf("progress message = %q, want progress update content", got)
	}
	progressCount := 0
	for _, msg := range updated.messages {
		if msg.kind == msgProgress {
			progressCount++
		}
	}
	if progressCount != 1 {
		t.Fatalf("progress transcript count = %d, want 1", progressCount)
	}
	line := updated.renderComposerMetaLine(120)
	if !strings.Contains(line, "Inspecting memory tests") || !strings.Contains(line, "Esc Esc cancel") {
		t.Fatalf("unexpected progress-driven composer meta: %q", line)
	}
}

func TestRenderComposerMetaLineUsesActiveRunSummaryForGenericThinkingState(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.dispatchUserSubmission("杭州天气详情", "杭州天气详情", []model.ContentPart{model.TextPart("杭州天气详情")})
	updated.progress = executionProgressState{
		SessionID: "sess-1",
		Status:    "running",
		Phase:     "thinking",
		Message:   "calling model",
	}
	line := updated.renderComposerMetaLine(120)
	if !strings.Contains(line, "杭州天气详情") || strings.Contains(line, "Calling model") {
		t.Fatalf("expected active run summary to replace generic thinking status, got %q", line)
	}
}

func TestRenderEditorPaneDoesNotRepeatRunningMetaWithExtraStreamingRow(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 120
	m.height = 30
	m.streaming = true
	m.progress = executionProgressState{
		SessionID: "sess-1",
		Status:    "running",
		Phase:     "model",
		Message:   "calling model",
	}
	m.runStartedAt = time.Now().Add(-400 * time.Millisecond)
	m.recalcLayout()

	rendered := m.renderEditorPane(m.generateLayout())
	if strings.Count(rendered, "Esc Esc cancel") != 1 {
		t.Fatalf("expected a single cancel hint in editor pane, got %q", rendered)
	}
	if strings.Contains(rendered, "working") {
		t.Fatalf("expected extra streaming row to be removed, got %q", rendered)
	}
}

func TestRenderEditorPaneDoesNotRepeatIdleComposerHints(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 120
	m.height = 30
	m.recalcLayout()

	rendered := m.renderEditorPane(m.generateLayout())
	if strings.Count(rendered, "/ commands") != 1 {
		t.Fatalf("expected composer hints once, got %q", rendered)
	}
	if strings.Contains(rendered, "Tab completes") {
		t.Fatalf("expected duplicate slash hint row to be removed, got %q", rendered)
	}
}

func TestSlashAutocompleteHintsIncludeCustomCommands(t *testing.T) {
	configpkg.SetAppName("mosscode")
	workspace := t.TempDir()
	commandDir := filepath.Join(workspace, ".mosscode", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("mkdir commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandDir, "review-pr.md"), []byte("Review repo"), 0o600); err != nil {
		t.Fatalf("write command: %v", err)
	}
	m := newChatModel("openai", "gpt-4o", workspace)
	if notice := m.syncCustomCommands(); notice != "" {
		t.Fatalf("syncCustomCommands notice: %s", notice)
	}
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.textarea.SetValue("/review-")
	m.refreshSlashHints()
	hints := m.currentSlashHints()
	if !slices.Contains(hints, "/review-pr") {
		t.Fatalf("expected /review-pr custom hint, got %v", hints)
	}
}

func TestSlashAutocompleteHintsIncludeDiscoveredSkills(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.setDiscoveredSkills([]string{"wiki-researcher", "brainstorming"})
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.textarea.SetValue("/br")
	m.refreshSlashHints()
	hints := m.currentSlashHints()
	if !slices.Contains(hints, "/brainstorming") {
		t.Fatalf("expected /brainstorming discovered skill hint, got %v", hints)
	}
}

func TestSessionResult_DequeuesAndRunsNext(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.streaming = true
	m.queuedInputs = []string{"next one"}
	sent := ""
	var sentParts []model.ContentPart
	m.sendFn = func(text string, parts []model.ContentPart) {
		sent = text
		sentParts = parts
	}
	m.queuedParts = [][]model.ContentPart{{model.TextPart("next one")}}

	updated, _ := m.Update(sessionResultMsg{})
	if sent != "next one" {
		t.Fatalf("sendFn called with %q, want next one", sent)
	}
	if len(updated.queuedInputs) != 0 {
		t.Fatalf("expected queue drained, got %d", len(updated.queuedInputs))
	}
	if !updated.streaming {
		t.Fatal("expected streaming to continue with dequeued message")
	}
	if len(sentParts) != 1 || sentParts[0].Type != model.ContentPartText {
		t.Fatalf("queued parts not forwarded: %+v", sentParts)
	}
	for _, msg := range updated.messages {
		if msg.kind == msgUser && msg.content == "next one" {
			t.Fatal("queued message should not be appended to chat message list before execution output")
		}
	}
}

func TestSessionResult_AppendsOutputImageMessage(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	updated, _ := m.Update(sessionResultMsg{
		outputMedia: []model.ContentPart{
			model.ImageURLPart(model.ContentPartOutputImage, "https://example.com/image.png", ""),
		},
	})
	if len(updated.messages) == 0 {
		t.Fatal("expected image message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgAssistant || !strings.Contains(last.content, "Generated image:") {
		t.Fatalf("unexpected image output message: %+v", last)
	}
}

func TestSessionResult_AppendsTraceSummary(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	updated, _ := m.Update(sessionResultMsg{
		traceSummary: "Run summary: | status=completed | steps=2",
	})
	if len(updated.messages) == 0 {
		t.Fatal("expected summary message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
	if !strings.Contains(last.content, "Run summary:") {
		t.Fatalf("unexpected summary message: %q", last.content)
	}
}

func TestSessionResult_StoresTraceForLaterCommand(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	trace := &product.RunTraceSummary{
		Status: "completed",
		Steps:  1,
		Trace: product.RunTrace{
			LLMCalls: 1,
			Timeline: []product.TraceEvent{{Kind: "llm_call", Model: "gpt-5", Type: "end_turn"}},
		},
	}
	updated, _ := m.Update(sessionResultMsg{
		trace:        trace,
		traceSummary: "Run summary:\n  status: completed",
	})
	if updated.lastTrace == nil {
		t.Fatal("expected last trace to be stored")
	}
	updated, _ = updated.handleSlashCommand("/trace")
	last := updated.messages[len(updated.messages)-1]
	if !strings.Contains(last.content, "Last run trace:") {
		t.Fatalf("expected trace detail output, got %q", last.content)
	}
}

func TestRefreshViewportRecalculatesHeightWhenRunningStateChanges(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 24
	m.recalcLayout()

	m.streaming = true
	m.refreshViewport()
	runningHeight := m.viewport.Height

	m.streaming = false
	m.refreshViewport()
	idleHeight := m.viewport.Height

	if idleHeight != runningHeight {
		t.Fatalf("viewport height after running=%d idle=%d, want matching height after removing duplicate running row", runningHeight, idleHeight)
	}
}

func TestRefreshViewportShowsStartupBannerBeforeConversationBegins(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 24
	m.startupBanner = "MOSSCODE BANNER"
	m.messages = []chatMessage{{kind: msgSystem, content: "Connected to openai"}}
	m.recalcLayout()

	m.refreshViewport()
	if !strings.Contains(m.viewport.View(), "MOSSCODE BANNER") {
		t.Fatalf("expected startup banner in initial chat viewport, got %q", m.viewport.View())
	}

	m.messages = append(m.messages, chatMessage{kind: msgUser, content: "hello"})
	m.refreshViewport()
	if strings.Contains(m.viewport.View(), "MOSSCODE BANNER") {
		t.Fatalf("expected startup banner to disappear after conversation starts, got %q", m.viewport.View())
	}
}

func TestChatViewDoesNotRenderSidebarSections(t *testing.T) {
	m := newChatModel("openai-completions", "gpt-4o", ".")
	m.ready = true
	m.width = 160
	m.height = 30
	m.messages = []chatMessage{{kind: msgAssistant, content: "Ready."}}
	m.recalcLayout()
	m.refreshViewport()

	out := m.View()
	for _, unwanted := range []string{"Session", "Shortcuts", "No product-specific context is available yet."} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("expected chat shell not to contain %q after sidebar removal, got %q", unwanted, out)
		}
	}
}

func TestRenderShellHeaderIsSingleLine(t *testing.T) {
	m := newChatModel("openai-completions", "gpt-4o", ".")
	m.width = 120
	header := m.renderShellHeader()
	if strings.Contains(header, "\n") {
		t.Fatalf("expected simplified shell header to be single-line, got %q", header)
	}
	if strings.Contains(header, "╱") || strings.Contains(header, "─") {
		t.Fatalf("expected simplified shell header to remove decorative separators, got %q", header)
	}
}

func TestShellProductTitleUsesCompactBrandDisplay(t *testing.T) {
	previous := configpkg.AppName()
	t.Cleanup(func() { configpkg.SetAppName(previous) })

	for _, tc := range []struct {
		name string
		app  string
		want string
	}{
		{name: "generic chat surface", app: "chat", want: "moss"},
		{name: "mosscode surface", app: "mosscode", want: "moss"},
		{name: "custom brand stays intact", app: "acme", want: "acme"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configpkg.SetAppName(tc.app)
			m := newChatModel("openai-completions", "gpt-4o", ".")
			if got := m.shellProductTitle(); got != tc.want {
				t.Fatalf("shellProductTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSlashCommandImageOpenWithoutLocalPathShowsError(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", t.TempDir())
	m.messages = append(m.messages, chatMessage{
		kind:    msgAssistant,
		content: "Generated image",
		meta: map[string]any{
			"is_media":   true,
			"media_kind": "image",
			"media_url":  "https://example.com/latest.png",
		},
	})
	updated, _ := m.handleSlashCommand("/image open")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "no local file path") {
		t.Fatalf("unexpected /image open output: %+v", last)
	}
}

func TestSlashCommandImageSavePersistsInlineData(t *testing.T) {
	workspace := t.TempDir()
	m := newChatModel("openai", "gpt-4o", workspace)
	m.messages = append(m.messages, chatMessage{
		kind:    msgAssistant,
		content: "Generated image",
		meta: map[string]any{
			"is_media":          true,
			"media_kind":        "image",
			"media_mime_type":   "image/png",
			"media_data_base64": "iVBORw0K",
		},
	})
	target := filepath.Join(workspace, "saved.png")
	updated, _ := m.handleSlashCommand("/image save " + target)
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Saved media") {
		t.Fatalf("unexpected /image save output: %+v", last)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected saved image file: %v", err)
	}
}

func TestSlashCommandMediaSavePersistsAudioData(t *testing.T) {
	workspace := t.TempDir()
	m := newChatModel("openai", "gpt-4o", workspace)
	m.messages = append(m.messages, chatMessage{
		kind:    msgAssistant,
		content: "Generated audio",
		meta: map[string]any{
			"is_media":          true,
			"media_kind":        "audio",
			"media_mime_type":   "audio/wav",
			"media_data_base64": "UklGRg==",
		},
	})
	target := filepath.Join(workspace, "saved.wav")
	updated, _ := m.handleSlashCommand("/media save " + target)
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Saved media") {
		t.Fatalf("unexpected /media save output: %+v", last)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected saved audio file: %v", err)
	}
}

// --- Extension system tests ---

func TestInstallExtensionsDuplicateSlashCommandReturnsError(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	ext1 := &Extension{Name: "alpha", SlashCommands: map[string]SlashCommandDef{"/foo": {Handler: func(ctx TUIContext, args []string) tea.Cmd { return nil }}}}
	ext2 := &Extension{Name: "beta", SlashCommands: map[string]SlashCommandDef{"/foo": {Handler: func(ctx TUIContext, args []string) tea.Cmd { return nil }}}}
	if err := m.installExtensions([]*Extension{ext1, ext2}); err == nil {
		t.Fatal("expected error for duplicate slash command, got nil")
	}
}

func TestInstallExtensionsCoreKeyOverrideReturnsError(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	ext := &Extension{Name: "bad", KeyBindings: map[string]KeyHandlerFunc{"ctrl+c": func(ctx TUIContext) (bool, tea.Cmd) { return true, nil }}}
	if err := m.installExtensions([]*Extension{ext}); err == nil {
		t.Fatal("expected error for core key override, got nil")
	}
}

func TestExtensionSlashCommandDispatch(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	called := false
	ext := &Extension{
		Name: "test",
		SlashCommands: map[string]SlashCommandDef{
			"/hello": {Handler: func(ctx TUIContext, args []string) tea.Cmd {
				called = true
				return nil
			}},
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	m, _ = m.handleSlashCommand("/hello world")
	if !called {
		t.Fatal("extension slash handler was not called")
	}
}

func TestExtensionSlashCommandDoesNotOverrideBuiltin(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	extCalled := false
	ext := &Extension{
		Name: "test",
		SlashCommands: map[string]SlashCommandDef{
			"/help": {Handler: func(ctx TUIContext, args []string) tea.Cmd {
				extCalled = true
				return nil
			}},
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	m, _ = m.handleSlashCommand("/help")
	if extCalled {
		t.Fatal("extension should not override built-in /help command")
	}
}

func TestExtensionKeyBindingFires(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	fired := false
	ext := &Extension{
		Name: "test",
		KeyBindings: map[string]KeyHandlerFunc{
			"ctrl+k": func(ctx TUIContext) (bool, tea.Cmd) {
				fired = true
				return true, nil
			},
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if !fired {
		t.Fatal("extension key binding was not fired")
	}
}

func TestExtensionStatusWidget(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	ext := &Extension{
		Name:          "test",
		StatusWidgets: []WidgetFunc{func(ctx TUIContext) string { return "my-status" }},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	m.width = 120
	bar := m.renderStatusPane(120)
	if !strings.Contains(bar, "my-status") {
		t.Fatalf("expected extension status widget in status pane, got %q", bar)
	}
}

func TestExtensionHeaderMetaWidget(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	ext := &Extension{
		Name:              "test",
		HeaderMetaWidgets: []WidgetFunc{func(ctx TUIContext) string { return "meta-extra" }},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	line := m.renderHeaderMetaLine()
	if !strings.Contains(line, "meta-extra") {
		t.Fatalf("expected extension meta widget in header meta line, got %q", line)
	}
}

func TestExtensionCustomOverlayOpenClose(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	ext := &Extension{
		Name: "test",
		Overlays: map[string]func() CustomOverlay{
			"my-overlay": func() CustomOverlay { return &testOverlay{} },
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	// Open the overlay via message.
	m, _ = m.Update(openCustomOverlayMsg{id: "my-overlay"})
	if m.activeOverlay() == nil || m.activeOverlay().ID() != overlayExt {
		t.Fatal("activeOverlay should return extOverlayAdapter after open")
	}
	// Close the overlay.
	m, _ = m.Update(closeCustomOverlayMsg{})
	if m.activeOverlay() != nil {
		t.Fatal("activeOverlay should return nil after close")
	}
}

type testOverlay struct{}

func (o *testOverlay) ID() string                                           { return "my-overlay" }
func (o *testOverlay) View(ctx OverlayContext) string                       { return "test overlay" }
func (o *testOverlay) HandleKey(ctx OverlayContext, key tea.KeyMsg) tea.Cmd { return nil }

func TestCtrlYOpensCopyPicker(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.messages = []chatMessage{
		{kind: msgAssistant, content: "Hello from the AI."},
		{kind: msgUser, content: "Hi"},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if updated.activeOverlay() == nil || updated.activeOverlay().ID() != overlayCopy {
		t.Fatal("expected copy picker overlay to be active after ctrl+y")
	}
	if updated.copyPicker == nil || len(updated.copyPicker.items) == 0 {
		t.Fatal("expected copy picker to have items")
	}
}

func TestCtrlYNoMessagesShowsError(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.messages = []chatMessage{
		{kind: msgUser, content: "Hi"},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if updated.activeOverlay() != nil {
		t.Fatal("expected no overlay when there are no copiable messages")
	}
	if len(updated.messages) == 0 {
		t.Fatal("expected an error message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error message, got kind=%d content=%q", last.kind, last.content)
	}
}

func TestCopyPickerEnterCopiesToClipboard(t *testing.T) {
	var copied string
	original := writeClipboard
	writeClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { writeClipboard = original })

	m := newChatModel("openai", "gpt-4o", ".")
	m.messages = []chatMessage{
		{kind: msgAssistant, content: "The answer is 42."},
	}
	// Open picker.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if m.activeOverlay() == nil || m.activeOverlay().ID() != overlayCopy {
		t.Fatal("expected copy picker to be open")
	}
	// Confirm with Enter.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.activeOverlay() != nil {
		t.Fatal("expected copy picker to close after Enter")
	}
	if copied != "The answer is 42." {
		t.Fatalf("expected clipboard content %q, got %q", "The answer is 42.", copied)
	}
	last := m.messages[len(m.messages)-1]
	if last.kind != msgSystem {
		t.Fatalf("expected system message after copy, got kind=%d", last.kind)
	}
}

func TestCopyPickerYKeyCopiesToClipboard(t *testing.T) {
	var copied string
	original := writeClipboard
	writeClipboard = func(s string) error { copied = s; return nil }
	t.Cleanup(func() { writeClipboard = original })

	m := newChatModel("openai", "gpt-4o", ".")
	m.messages = []chatMessage{
		{kind: msgAssistant, content: "Copy via y key."},
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if m.activeOverlay() != nil {
		t.Fatal("expected copy picker to close after y")
	}
	if copied != "Copy via y key." {
		t.Fatalf("unexpected clipboard content: %q", copied)
	}
}

func TestCopyPickerEscCloses(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.messages = []chatMessage{
		{kind: msgAssistant, content: "Some output."},
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlY})
	if m.activeOverlay() == nil || m.activeOverlay().ID() != overlayCopy {
		t.Fatal("expected copy picker to be open")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.activeOverlay() != nil {
		t.Fatal("expected copy picker to close after Esc")
	}
}

func TestCtrlVPastesFromClipboard(t *testing.T) {
	original := readClipboard
	readClipboard = func() (string, error) { return "pasted text", nil }
	t.Cleanup(func() { readClipboard = original })

	m := newChatModel("openai", "gpt-4o", ".")
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if !strings.Contains(m.textarea.Value(), "pasted text") {
		t.Fatalf("expected %q in textarea, got %q", "pasted text", m.textarea.Value())
	}
}

func TestCtrlVEmptyClipboardIsNoop(t *testing.T) {
	original := readClipboard
	readClipboard = func() (string, error) { return "   ", nil }
	t.Cleanup(func() { readClipboard = original })

	m := newChatModel("openai", "gpt-4o", ".")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.textarea.Value() != "" {
		t.Fatalf("expected empty textarea after blank paste, got %q", m.textarea.Value())
	}
}

func TestIsCopyableMessage(t *testing.T) {
	for _, tc := range []struct {
		kind    msgKind
		content string
		want    bool
	}{
		{msgAssistant, "hello", true},
		{msgSystem, "system message", true},
		{msgToolResult, "result", true},
		{msgToolError, "error", true},
		{msgUser, "user", false},
		{msgProgress, "progress", false},
		{msgAssistant, "   ", false}, // empty after trim
		{msgSystem, "", false},       // blank
	} {
		msg := chatMessage{kind: tc.kind, content: tc.content}
		got := isCopyableMessage(msg)
		if got != tc.want {
			t.Errorf("isCopyableMessage(%d, %q) = %v, want %v", tc.kind, tc.content, got, tc.want)
		}
	}
}

func TestNewCopyPickerStateNewestFirst(t *testing.T) {
	messages := []chatMessage{
		{kind: msgAssistant, content: "first"},
		{kind: msgUser, content: "user"},
		{kind: msgAssistant, content: "second"},
		{kind: msgAssistant, content: "third"},
	}
	state := newCopyPickerState(messages)
	if len(state.items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(state.items))
	}
	if state.items[0].content != "third" {
		t.Errorf("expected newest first (third), got %q", state.items[0].content)
	}
	if state.items[2].content != "first" {
		t.Errorf("expected oldest last (first), got %q", state.items[2].content)
	}
}

// Phase 7 — BridgeIO, overlayStack, Extension lifecycle hooks

func TestBridgeIONilProgramSend(t *testing.T) {
	b := newBridgeIO()
	// Send with no program should not panic and return nil error.
	if err := b.Send(nil, io.OutputMessage{}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestBridgeIONilProgramRefresh(t *testing.T) {
	b := newBridgeIO()
	// Refresh with no program should not panic.
	b.Refresh()
}

func TestBridgeIONilProgramAsk(t *testing.T) {
	b := newBridgeIO()
	// Ask with no program should return empty response immediately (no block).
	resp, err := b.Ask(context.Background(), io.InputRequest{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp.Value != "" {
		t.Fatalf("expected empty response, got %q", resp.Value)
	}
}

func TestOverlayStackOpenAndTopDialog(t *testing.T) {
	s := newOverlayStack()
	if s.HasDialogs() {
		t.Fatal("expected no dialogs initially")
	}
	s.Open(overlayModel, modelOverlayDialog{})
	if !s.HasDialogs() {
		t.Fatal("expected dialogs after Open")
	}
	if s.Top() != overlayModel {
		t.Fatalf("expected top=%s, got %s", overlayModel, s.Top())
	}
	d := s.TopDialog()
	if d == nil {
		t.Fatal("expected non-nil TopDialog after Open")
	}
	if d.ID() != overlayModel {
		t.Fatalf("expected dialog ID=%s, got %s", overlayModel, d.ID())
	}
}

func TestOverlayStackCloseRemovesDialog(t *testing.T) {
	s := newOverlayStack()
	s.Open(overlayModel, modelOverlayDialog{})
	s.Open(overlayTheme, themeOverlayDialog{})
	s.Close(overlayModel)
	if s.Top() != overlayTheme {
		t.Fatalf("expected top=%s after closing model, got %s", overlayTheme, s.Top())
	}
	if s.active[overlayModel] != nil {
		t.Fatal("expected model dialog removed from active map after Close")
	}
}

func TestOverlayStackCloseTopRemovesDialog(t *testing.T) {
	s := newOverlayStack()
	s.Open(overlayModel, modelOverlayDialog{})
	s.CloseTop()
	if s.HasDialogs() {
		t.Fatal("expected no dialogs after CloseTop")
	}
	if s.TopDialog() != nil {
		t.Fatal("expected nil TopDialog after CloseTop")
	}
}

func TestOverlayStackReopenUpdatesDialog(t *testing.T) {
	s := newOverlayStack()
	s.Open(overlayModel, modelOverlayDialog{})
	s.Open(overlayTheme, themeOverlayDialog{})
	// Re-opening model brings it to top with new dialog.
	s.Open(overlayModel, modelOverlayDialog{})
	if s.Top() != overlayModel {
		t.Fatalf("expected reopened overlay on top, got %s", s.Top())
	}
	if len(s.dialogs) != 2 {
		t.Fatalf("expected 2 dialogs (no dup), got %d", len(s.dialogs))
	}
}

func TestActiveOverlayUsesStack(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	if m.activeOverlay() != nil {
		t.Fatal("expected nil activeOverlay initially")
	}
	m.openModelOverlay()
	d := m.activeOverlay()
	if d == nil {
		t.Fatal("expected non-nil activeOverlay after openModelOverlay")
	}
	if d.ID() != overlayModel {
		t.Fatalf("expected overlayModel, got %s", d.ID())
	}
	m = m.closeModelOverlay()
	if m.activeOverlay() != nil {
		t.Fatal("expected nil activeOverlay after close")
	}
}

func TestExtensionOnSessionStartHook(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	fired := false
	ext := &Extension{
		Name: "lifecycle-test",
		OnSessionStart: func(ctx TUIContext) tea.Cmd {
			fired = true
			return nil
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	ctx := m.tuiContext()
	for _, e := range m.extensions {
		if e.OnSessionStart != nil {
			e.OnSessionStart(ctx)
		}
	}
	if !fired {
		t.Fatal("OnSessionStart hook was not called")
	}
}

func TestExtensionOnSessionEndHook(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	fired := false
	ext := &Extension{
		Name: "lifecycle-test",
		OnSessionEnd: func(ctx TUIContext) tea.Cmd {
			fired = true
			return nil
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	ctx := m.tuiContext()
	for _, e := range m.extensions {
		if e.OnSessionEnd != nil {
			e.OnSessionEnd(ctx)
		}
	}
	if !fired {
		t.Fatal("OnSessionEnd hook was not called")
	}
}

func TestExtensionOnModelSwitchHook(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	var gotPrev, gotNext string
	ext := &Extension{
		Name: "lifecycle-test",
		OnModelSwitch: func(ctx TUIContext, prev, next string) tea.Cmd {
			gotPrev = prev
			gotNext = next
			return nil
		},
	}
	if err := m.installExtensions([]*Extension{ext}); err != nil {
		t.Fatalf("installExtensions: %v", err)
	}
	ctx := m.tuiContext()
	for _, e := range m.extensions {
		if e.OnModelSwitch != nil {
			e.OnModelSwitch(ctx, "gpt-4o", "claude-3-5-sonnet")
		}
	}
	if gotPrev != "gpt-4o" || gotNext != "claude-3-5-sonnet" {
		t.Fatalf("expected prev=gpt-4o next=claude-3-5-sonnet, got prev=%q next=%q", gotPrev, gotNext)
	}
}
