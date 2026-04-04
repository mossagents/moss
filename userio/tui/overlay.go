package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/runtime"
)

type overlayID string

const (
	overlayAsk      overlayID = "ask"
	overlaySchedule overlayID = "schedule"
)

type overlayDialog interface {
	ID() overlayID
	View(m chatModel, width, height int) string
	HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd)
}

type overlayStack struct {
	dialogs []overlayID
}

func newOverlayStack() *overlayStack {
	return &overlayStack{}
}

func (o *overlayStack) HasDialogs() bool {
	return o != nil && len(o.dialogs) > 0
}

func (o *overlayStack) Top() overlayID {
	if o == nil || len(o.dialogs) == 0 {
		return ""
	}
	return o.dialogs[len(o.dialogs)-1]
}

func (o *overlayStack) Open(id overlayID) {
	if o == nil || id == "" {
		return
	}
	o.Close(id)
	o.dialogs = append(o.dialogs, id)
}

func (o *overlayStack) Close(id overlayID) {
	if o == nil || id == "" {
		return
	}
	for i := len(o.dialogs) - 1; i >= 0; i-- {
		if o.dialogs[i] == id {
			o.dialogs = append(o.dialogs[:i], o.dialogs[i+1:]...)
			return
		}
	}
}

func (o *overlayStack) CloseTop() {
	if o == nil || len(o.dialogs) == 0 {
		return
	}
	o.dialogs = o.dialogs[:len(o.dialogs)-1]
}

type askOverlayDialog struct{}

func (askOverlayDialog) ID() overlayID { return overlayAsk }

func (askOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderAskForm(width)
}

func (askOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.handleEsc()
	default:
		return m.handleAskKey(msg)
	}
}

type scheduleOverlayDialog struct{}

func (scheduleOverlayDialog) ID() overlayID { return overlaySchedule }

func (scheduleOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderScheduleBrowser(width)
}

func (scheduleOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeScheduleOverlay(), nil
	default:
		return m.handleScheduleBrowserKey(msg)
	}
}

func (m *chatModel) ensureOverlayStack() {
	if m.overlays == nil {
		m.overlays = newOverlayStack()
	}
}

func (m *chatModel) openAskOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayAsk)
}

func (m *chatModel) closeAskOverlay() {
	if m.overlays != nil {
		m.overlays.Close(overlayAsk)
	}
}

func (m *chatModel) openScheduleOverlay(items []runtime.ScheduleItem) {
	m.scheduleBrowser = newScheduleBrowserState(items)
	m.ensureOverlayStack()
	m.overlays.Open(overlaySchedule)
}

func (m chatModel) closeScheduleOverlay() chatModel {
	m.scheduleBrowser = nil
	if m.overlays != nil {
		m.overlays.Close(overlaySchedule)
	}
	m.refreshViewport()
	return m
}

func (m chatModel) activeOverlay() overlayDialog {
	if m.overlays != nil {
		switch m.overlays.Top() {
		case overlayAsk:
			if m.askForm != nil && m.pendAsk != nil {
				return askOverlayDialog{}
			}
		case overlaySchedule:
			if m.scheduleBrowser != nil {
				return scheduleOverlayDialog{}
			}
		}
	}
	if m.askForm != nil && m.pendAsk != nil {
		return askOverlayDialog{}
	}
	if m.scheduleBrowser != nil {
		return scheduleOverlayDialog{}
	}
	return nil
}
