package tui

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	configpkg "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/port"
)

type fakeScheduleController struct {
	listFn     func() ([]runtime.ScheduleItem, error)
	listTextFn func() (string, error)
	cancelFn   func(string) (string, error)
	runNowFn   func(string) (string, error)
}

func (f fakeScheduleController) List() ([]runtime.ScheduleItem, error) {
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

func TestSlashCommandStatusSummary(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.trust = "trusted"
	m.profile = "coding"
	m.approvalMode = "confirm"
	m.theme = "plain"
	m.sessionInfoFn = func() string { return "session summary" }
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

func TestSlashCommandDebugConfig(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.debugConfigFn = func() string { return "debug config" }
	updated, _ := m.handleSlashCommand("/debug-config")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || last.content != "debug config" {
		t.Fatalf("unexpected debug config output: %+v", last)
	}
}

func TestSlashCommandResumeRestoresSession(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.sessionRestoreFn = func(sessionID string) (string, error) {
		if sessionID != "sess-123" {
			t.Fatalf("unexpected session id: %s", sessionID)
		}
		return "Restored session sess-123.", nil
	}
	updated, _ := m.handleSlashCommand("/resume sess-123")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "sess-123") {
		t.Fatalf("unexpected resume output: %+v", last)
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
	m.taskListFn = func(status string, limit int) (string, error) { return "ok", nil }
	updated, _ := m.handleSlashCommand("/agent bad")
	if len(updated.messages) == 0 {
		t.Fatal("expected validation message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
	}
}

func TestSlashCommandAgentCancel(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.taskCancelFn = func(taskID, reason string) (string, error) {
		if taskID != "t1" {
			t.Fatalf("unexpected taskID: %s", taskID)
		}
		return "cancelled", nil
	}
	m.taskListFn = func(status string, limit int) (string, error) { return "ok", nil }
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
		listFn: func() ([]runtime.ScheduleItem, error) {
			return []runtime.ScheduleItem{{ID: "review", Schedule: "@every 10m", Goal: "Run review"}}, nil
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
}

func TestScheduleBrowserDelete(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	items := []runtime.ScheduleItem{
		{ID: "old-job", Schedule: "@every 1h"},
		{ID: "keep-job", Schedule: "@every 2h"},
	}
	m.scheduleCtrl = fakeScheduleController{
		listFn: func() ([]runtime.ScheduleItem, error) {
			cp := make([]runtime.ScheduleItem, len(items))
			copy(cp, items)
			return cp, nil
		},
		cancelFn: func(id string) (string, error) {
			filtered := make([]runtime.ScheduleItem, 0, len(items))
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
		listFn: func() ([]runtime.ScheduleItem, error) {
			return []runtime.ScheduleItem{{ID: "review", Schedule: "@every 10m"}}, nil
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
	m.width = 14
	m.textarea.SetWidth(m.inputWrapWidth())
	m.textarea.SetValue("123456789012345")
	m.adjustInputHeight()
	if got := m.textarea.Height(); got != 2 {
		t.Fatalf("wrapped textarea height=%d, want 2", got)
	}
}

func TestNewChatModelSupportsMultilineBindings(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	keys := m.textarea.KeyMap.InsertNewline.Keys()
	for _, want := range []string{"shift+enter", "alt+enter", "ctrl+j"} {
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
	m.sendFn = func(text string) { dispatched = text }
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
	m.sendFn = func(text string) { dispatched = text }
	updated, _ := m.handleSlashCommand("/search recent golang releases")
	if !updated.streaming {
		t.Fatal("expected /search to start a run")
	}
	if !strings.Contains(dispatched, "jina_search") || !strings.Contains(dispatched, "golang releases") {
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
	m.newSessionFn = func() (string, error) {
		return "Previous session sess_1 auto-saved.\nSwitched to new session sess_2.", nil
	}

	updated, _ := m.handleSlashCommand("/new")
	if len(updated.messages) != 1 {
		t.Fatalf("expected transcript reset to one notice, got %d messages", len(updated.messages))
	}
	last := updated.messages[0]
	if last.kind != msgSystem {
		t.Fatalf("expected system message, got %v", last.kind)
	}
	if !strings.Contains(last.content, "Switched to new session sess_2") {
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
	m.newSessionFn = func() (string, error) {
		return "", errors.New("cannot create a new session while a run is active")
	}

	updated, _ := m.handleSlashCommand("/new")
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
	m.checkpointListFn = func(limit int) (string, error) {
		if limit != 20 {
			t.Fatalf("limit = %d, want 20", limit)
		}
		return "Checkpoints:\n- cp-1", nil
	}
	updated, _ := m.handleSlashCommand("/checkpoint list")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "cp-1") {
		t.Fatalf("unexpected checkpoint list output: %+v", last)
	}
}

func TestSlashCommandCheckpointShowSuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpointShowFn = func(checkpointID string) (string, error) {
		if checkpointID != "cp-1" {
			t.Fatalf("checkpointID = %q, want cp-1", checkpointID)
		}
		return "Checkpoint: cp-1\n  metadata: source, trigger", nil
	}
	updated, _ := m.handleSlashCommand("/checkpoint show cp-1")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "Checkpoint: cp-1") {
		t.Fatalf("unexpected checkpoint show output: %+v", last)
	}
}

func TestSlashCommandCheckpointShowLatestSuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.checkpointShowFn = func(checkpointID string) (string, error) {
		if checkpointID != "latest" {
			t.Fatalf("checkpointID = %q, want latest", checkpointID)
		}
		return "Checkpoint: cp-latest", nil
	}
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
	m.checkpointShowFn = func(checkpointID string) (string, error) {
		return "", nil
	}
	updated, _ := m.handleSlashCommand("/checkpoint show")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "Usage: /checkpoint show <checkpoint_id|latest>") {
		t.Fatalf("unexpected checkpoint show validation output: %+v", last)
	}
}

func TestSlashCommandChangesListSuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.changeListFn = func(limit int) (string, error) {
		if limit != 20 {
			t.Fatalf("limit = %d, want 20", limit)
		}
		return "Changes:\n- change-1", nil
	}
	updated, _ := m.handleSlashCommand("/changes list")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "change-1") {
		t.Fatalf("unexpected changes list output: %+v", last)
	}
}

func TestSlashCommandApplySuccess(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.applyChangeFn = func(patchFile, summary string) (string, error) {
		if patchFile != "fix.patch" {
			t.Fatalf("patchFile = %q, want fix.patch", patchFile)
		}
		if summary != "update tracked file" {
			t.Fatalf("summary = %q, want update tracked file", summary)
		}
		return "Change: change-1", nil
	}
	updated, _ := m.handleSlashCommand("/apply fix.patch update tracked file")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgSystem || !strings.Contains(last.content, "change-1") {
		t.Fatalf("unexpected apply output: %+v", last)
	}
}

func TestSlashCommandRollbackValidation(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.rollbackChangeFn = func(changeID string) (string, error) {
		return "", nil
	}
	updated, _ := m.handleSlashCommand("/rollback")
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError || !strings.Contains(last.content, "Usage: /rollback <change_id>") {
		t.Fatalf("unexpected rollback validation output: %+v", last)
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
	m.checkpointReplayFn = func(checkpointID, mode string, restore bool) (string, error) {
		if checkpointID != "cp-1" || mode != "rerun" || !restore {
			t.Fatalf("unexpected replay args id=%q mode=%q restore=%v", checkpointID, mode, restore)
		}
		return "Switched to replay session sess_2 from checkpoint cp-1 (rerun).", nil
	}
	updated, _ := m.handleSlashCommand("/checkpoint replay cp-1 rerun restore")
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
	m.checkpointReplayFn = func(checkpointID, mode string, restore bool) (string, error) {
		if checkpointID != "" || mode != "rerun" || !restore {
			t.Fatalf("unexpected replay args id=%q mode=%q restore=%v", checkpointID, mode, restore)
		}
		return "Switched to replay session sess_latest from checkpoint cp-latest (rerun).", nil
	}
	updated, _ := m.handleSlashCommand("/checkpoint replay rerun restore")
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
	m.checkpointForkFn = func(sourceKind, sourceID string, restore bool) (string, error) {
		if sourceKind != string(port.ForkSourceCheckpoint) || sourceID != "" || !restore {
			t.Fatalf("unexpected fork args kind=%q id=%q restore=%v", sourceKind, sourceID, restore)
		}
		return "Switched to forked session sess_latest from checkpoint cp-latest.", nil
	}
	updated, _ := m.handleSlashCommand("/fork latest restore")
	last := updated.messages[0]
	if last.kind != msgSystem || !strings.Contains(last.content, "cp-latest") {
		t.Fatalf("unexpected fork latest output: %+v", last)
	}
}

func TestHelpIncludesCheckpointAndCoreRecoveryCommands(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	last := updated.messages[len(updated.messages)-1]
	if !strings.Contains(last.content, "/status") || !strings.Contains(last.content, "/resume") || !strings.Contains(last.content, "/fork") || !strings.Contains(last.content, "/compact") || !strings.Contains(last.content, "/plan") || !strings.Contains(last.content, "/init") || !strings.Contains(last.content, "/debug-config") || !strings.Contains(last.content, "/theme") || !strings.Contains(last.content, "/agent") || !strings.Contains(last.content, "/checkpoint replay [<id|latest>]") || !strings.Contains(last.content, "/trace") {
		t.Fatalf("help missing checkpoint commands: %q", last.content)
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
	last := updated.messages[len(updated.messages)-1]
	if !strings.Contains(last.content, "Custom commands:") || !strings.Contains(last.content, "/review-pr") {
		t.Fatalf("help missing custom commands: %q", last.content)
	}
}

func TestHelpIncludesChangeCommands(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	last := updated.messages[len(updated.messages)-1]
	for _, want := range []string{"/diff", "/review", "/changes", "/apply", "/rollback"} {
		if !strings.Contains(last.content, want) {
			t.Fatalf("help missing %q in %q", want, last.content)
		}
	}
}

func TestHelpIncludesNewCommand(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/help")
	if len(updated.messages) == 0 {
		t.Fatal("expected help message")
	}
	last := updated.messages[len(updated.messages)-1]
	if !strings.Contains(last.content, "/new") {
		t.Fatalf("help missing /new command: %q", last.content)
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

	replyCh := make(chan port.InputResponse, 1)
	ask := &bridgeAsk{
		request: port.InputRequest{
			Type:   port.InputForm,
			Prompt: "Choose one",
			Fields: []port.InputField{
				{Name: "database", Type: port.InputFieldSingleSelect, Options: []string{"PostgreSQL", "MySQL"}, Required: true},
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

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

	replyCh := make(chan port.InputResponse, 1)
	ask := &bridgeAsk{
		request: port.InputRequest{
			Type:   port.InputForm,
			Prompt: "Choose features",
			Fields: []port.InputField{
				{Name: "features", Type: port.InputFieldMultiSelect, Options: []string{"A", "B", "C"}, Required: true},
			},
		},
		replyCh: replyCh,
	}
	updated, _ := m.handleBridge(bridgeMsg{ask: ask})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeySpace})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeySpace})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})

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
	m.sendFn = func(text string) { sent = text }

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
	m.sendFn = func(text string) { sent = text }

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
	if !slices.Contains(hints, "/apply") {
		t.Fatalf("expected /apply in hints, got %v", hints)
	}

	m.textarea.SetValue("/r")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	if !slices.Contains(hints, "/rollback") || !slices.Contains(hints, "/resume") {
		t.Fatalf("expected /rollback and /resume in hints, got %v", hints)
	}

	m.textarea.SetValue("/ch")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	if !slices.Contains(hints, "/changes") {
		t.Fatalf("expected /changes in hints, got %v", hints)
	}

	m.textarea.SetValue("/tr")
	m.refreshSlashHints()
	hints = m.currentSlashHints()
	if !slices.Contains(hints, "/trace") {
		t.Fatalf("expected /trace in hints, got %v", hints)
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

func TestSessionResult_DequeuesAndRunsNext(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()
	m.streaming = true
	m.queuedInputs = []string{"next one"}
	sent := ""
	m.sendFn = func(text string) { sent = text }

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
	for _, msg := range updated.messages {
		if msg.kind == msgUser && msg.content == "next one" {
			t.Fatal("queued message should not be appended to chat message list before execution output")
		}
	}
}

func TestSessionResult_AppendsTraceSummary(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.ready = true
	m.width = 120
	m.height = 40
	m.recalcLayout()

	updated, _ := m.Update(sessionResultMsg{
		traceSummary: "Run summary:\n  status: completed\n  steps: 2",
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

	if idleHeight != runningHeight+1 {
		t.Fatalf("viewport height after running=%d idle=%d, want idle to recover one line", runningHeight, idleHeight)
	}
}
