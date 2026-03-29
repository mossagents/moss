package tui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/runtime"
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

func TestSlashCommandSessionInfo(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.sessionInfoFn = func() string { return "session summary" }
	updated, _ := m.handleSlashCommand("/session")
	if len(updated.messages) == 0 {
		t.Fatal("expected a system message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.content != "session summary" {
		t.Fatalf("unexpected message content: %q", last.content)
	}
}

func TestSlashCommandTasksValidation(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/tasks bad")
	if len(updated.messages) == 0 {
		t.Fatal("expected validation message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
	}
}

func TestSlashCommandTaskCancel(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	m.taskCancelFn = func(taskID, reason string) (string, error) {
		if taskID != "t1" {
			t.Fatalf("unexpected taskID: %s", taskID)
		}
		return "cancelled", nil
	}
	updated, _ := m.handleSlashCommand("/task cancel t1 because")
	if len(updated.messages) == 0 {
		t.Fatal("expected cancel output message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.content != "cancelled" {
		t.Fatalf("unexpected message content: %q", last.content)
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

func TestSlashCommandSessionsUnavailable(t *testing.T) {
	m := newChatModel("openai", "gpt-4o", ".")
	updated, _ := m.handleSlashCommand("/sessions")
	if len(updated.messages) == 0 {
		t.Fatal("expected message")
	}
	last := updated.messages[len(updated.messages)-1]
	if last.kind != msgError {
		t.Fatalf("expected error kind, got %v", last.kind)
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
