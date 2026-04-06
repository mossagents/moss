package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/runtime"
)

type overlayID string

const (
	overlayTranscript overlayID = "transcript"
	overlayAsk        overlayID = "ask"
	overlaySchedule overlayID = "schedule"
	overlayModel    overlayID = "model"
	overlayTheme    overlayID = "theme"
	overlayStatus   overlayID = "statusline"
	overlayMCP      overlayID = "mcp"
	overlayHelp     overlayID = "help"
	overlayReview   overlayID = "review"
	overlayResume   overlayID = "resume"
	overlayFork     overlayID = "fork"
	overlayAgent    overlayID = "agent"
	overlayMention  overlayID = "mention"
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

type modelOverlayDialog struct{}

func (modelOverlayDialog) ID() overlayID { return overlayModel }

func (modelOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderModelPicker(width)
}

func (modelOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeModelOverlay(), nil
	default:
		return m.handleModelPickerKey(msg)
	}
}

type themeOverlayDialog struct{}

func (themeOverlayDialog) ID() overlayID { return overlayTheme }

func (themeOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderThemePicker(width)
}

func (themeOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeThemeOverlay(), nil
	default:
		return m.handleThemePickerKey(msg)
	}
}

type statuslineOverlayDialog struct{}

func (statuslineOverlayDialog) ID() overlayID { return overlayStatus }
func (statuslineOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderStatuslinePicker(width)
}
func (statuslineOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeStatuslineOverlay(), nil
	default:
		return m.handleStatuslinePickerKey(msg)
	}
}

type mcpOverlayDialog struct{}

func (mcpOverlayDialog) ID() overlayID { return overlayMCP }
func (mcpOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderMCPPicker(width)
}
func (mcpOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeMCPOverlay(), nil
	default:
		return m.handleMCPPickerKey(msg)
	}
}

type helpOverlayDialog struct{}

func (helpOverlayDialog) ID() overlayID { return overlayHelp }
func (helpOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderHelpPicker(width)
}
func (helpOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeHelpOverlay(), nil
	default:
		return m.handleHelpPickerKey(msg)
	}
}

type reviewOverlayDialog struct{}

func (reviewOverlayDialog) ID() overlayID { return overlayReview }
func (reviewOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderReviewPicker(width)
}
func (reviewOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeReviewOverlay(), nil
	default:
		return m.handleReviewPickerKey(msg)
	}
}

type resumeOverlayDialog struct{}

func (resumeOverlayDialog) ID() overlayID { return overlayResume }
func (resumeOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderResumePicker(width)
}
func (resumeOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeResumeOverlay(), nil
	default:
		return m.handleResumePickerKey(msg)
	}
}

type forkOverlayDialog struct{}

func (forkOverlayDialog) ID() overlayID { return overlayFork }
func (forkOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderForkPicker(width)
}
func (forkOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeForkOverlay(), nil
	default:
		return m.handleForkPickerKey(msg)
	}
}

type agentOverlayDialog struct{}

func (agentOverlayDialog) ID() overlayID { return overlayAgent }
func (agentOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderAgentPicker(width)
}
func (agentOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeAgentOverlay(), nil
	default:
		return m.handleAgentPickerKey(msg)
	}
}

type mentionOverlayDialog struct{}

func (mentionOverlayDialog) ID() overlayID { return overlayMention }
func (mentionOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderMentionPicker(width)
}
func (mentionOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.closeMentionOverlay(), nil
	default:
		return m.handleMentionPickerKey(msg)
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

func (m *chatModel) openModelOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayModel)
}

func (m chatModel) closeModelOverlay() chatModel {
	m.modelPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayModel)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openThemeOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayTheme)
}

func (m chatModel) closeThemeOverlay() chatModel {
	m.themePicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayTheme)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openStatuslineOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayStatus)
}

func (m chatModel) closeStatuslineOverlay() chatModel {
	m.statuslinePicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayStatus)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openMCPOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayMCP)
}

func (m chatModel) closeMCPOverlay() chatModel {
	m.mcpPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayMCP)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openHelpOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayHelp)
}

func (m chatModel) closeHelpOverlay() chatModel {
	m.helpPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayHelp)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openReviewOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayReview)
}

func (m chatModel) closeReviewOverlay() chatModel {
	m.reviewPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayReview)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openResumeOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayResume)
}

func (m chatModel) closeResumeOverlay() chatModel {
	m.resumePicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayResume)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openForkOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayFork)
}

func (m chatModel) closeForkOverlay() chatModel {
	m.forkPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayFork)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openAgentOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayAgent)
}

func (m chatModel) closeAgentOverlay() chatModel {
	m.agentPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayAgent)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openMentionOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayMention)
}

func (m chatModel) closeMentionOverlay() chatModel {
	m.mentionPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayMention)
	}
	m.refreshViewport()
	return m
}

func (m chatModel) activeOverlay() overlayDialog {
	if m.overlays != nil {
		switch m.overlays.Top() {
		case overlayTranscript:
			if m.transcriptOverlay != nil {
				return transcriptOverlayDialog{}
			}
		case overlayAsk:
			if m.askForm != nil && m.pendAsk != nil {
				return askOverlayDialog{}
			}
		case overlaySchedule:
			if m.scheduleBrowser != nil {
				return scheduleOverlayDialog{}
			}
		case overlayModel:
			if m.modelPicker != nil {
				return modelOverlayDialog{}
			}
		case overlayTheme:
			if m.themePicker != nil {
				return themeOverlayDialog{}
			}
		case overlayStatus:
			if m.statuslinePicker != nil {
				return statuslineOverlayDialog{}
			}
		case overlayMCP:
			if m.mcpPicker != nil {
				return mcpOverlayDialog{}
			}
		case overlayHelp:
			if m.helpPicker != nil {
				return helpOverlayDialog{}
			}
		case overlayReview:
			if m.reviewPicker != nil {
				return reviewOverlayDialog{}
			}
		case overlayResume:
			if m.resumePicker != nil {
				return resumeOverlayDialog{}
			}
		case overlayFork:
			if m.forkPicker != nil {
				return forkOverlayDialog{}
			}
		case overlayAgent:
			if m.agentPicker != nil {
				return agentOverlayDialog{}
			}
		case overlayMention:
			if m.mentionPicker != nil {
				return mentionOverlayDialog{}
			}
		}
	}
	if m.transcriptOverlay != nil {
		return transcriptOverlayDialog{}
	}
	if m.askForm != nil && m.pendAsk != nil {
		return askOverlayDialog{}
	}
	if m.scheduleBrowser != nil {
		return scheduleOverlayDialog{}
	}
	if m.modelPicker != nil {
		return modelOverlayDialog{}
	}
	if m.themePicker != nil {
		return themeOverlayDialog{}
	}
	if m.statuslinePicker != nil {
		return statuslineOverlayDialog{}
	}
	if m.mcpPicker != nil {
		return mcpOverlayDialog{}
	}
	if m.helpPicker != nil {
		return helpOverlayDialog{}
	}
	if m.reviewPicker != nil {
		return reviewOverlayDialog{}
	}
	if m.resumePicker != nil {
		return resumeOverlayDialog{}
	}
	if m.forkPicker != nil {
		return forkOverlayDialog{}
	}
	if m.agentPicker != nil {
		return agentOverlayDialog{}
	}
	if m.mentionPicker != nil {
		return mentionOverlayDialog{}
	}
	return nil
}
