package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/harness/runtime/scheduling"
)

type overlayID string

const (
	overlayTranscript overlayID = "transcript"
	overlayAsk        overlayID = "ask"
	overlaySchedule   overlayID = "schedule"
	overlayModel      overlayID = "model"
	overlayTheme      overlayID = "theme"
	overlayStatus     overlayID = "statusline"
	overlayMCP        overlayID = "mcp"
	overlayHelp       overlayID = "help"
	overlayReview     overlayID = "review"
	overlayResume     overlayID = "resume"
	overlayFork       overlayID = "fork"
	overlayAgent      overlayID = "agent"
	overlayMention    overlayID = "mention"
	overlayExt        overlayID = "ext" // custom extension overlay
	overlayCopy       overlayID = "copy"
)

type overlayDialog interface {
	ID() overlayID
	View(m chatModel, width, height int) string
	HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd)
}

type overlayStack struct {
	dialogs []overlayID
	active  map[overlayID]overlayDialog
}

func newOverlayStack() *overlayStack {
	return &overlayStack{active: make(map[overlayID]overlayDialog)}
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

func (o *overlayStack) TopDialog() overlayDialog {
	id := o.Top()
	if id == "" || o.active == nil {
		return nil
	}
	return o.active[id]
}

func (o *overlayStack) Open(id overlayID, d overlayDialog) {
	if o == nil || id == "" {
		return
	}
	o.Close(id)
	o.dialogs = append(o.dialogs, id)
	if o.active == nil {
		o.active = make(map[overlayID]overlayDialog)
	}
	o.active[id] = d
}

func (o *overlayStack) Close(id overlayID) {
	if o == nil || id == "" {
		return
	}
	for i := len(o.dialogs) - 1; i >= 0; i-- {
		if o.dialogs[i] == id {
			o.dialogs = append(o.dialogs[:i], o.dialogs[i+1:]...)
			delete(o.active, id)
			return
		}
	}
}

func (o *overlayStack) CloseTop() {
	if o == nil || len(o.dialogs) == 0 {
		return
	}
	id := o.dialogs[len(o.dialogs)-1]
	o.dialogs = o.dialogs[:len(o.dialogs)-1]
	delete(o.active, id)
}

type askOverlayDialog struct{}

func (askOverlayDialog) ID() overlayID { return overlayAsk }

func (askOverlayDialog) View(m chatModel, width, _ int) string {
	return m.renderAskForm(width)
}

func (askOverlayDialog) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Single Esc cancels the pending ask and the entire run (the agent is
		// blocked waiting for input, so there's nothing meaningful to resume).
		if m.cancelRunFn != nil && m.cancelRunFn() {
			m.streaming = false
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Input cancelled."})
		}
		m.resetAskFormState()
		m.refreshViewport()
		return m, nil
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
	m.overlays.Open(overlayAsk, askOverlayDialog{})
}

func (m *chatModel) closeAskOverlay() {
	if m.overlays != nil {
		m.overlays.Close(overlayAsk)
	}
}

func (m *chatModel) openScheduleOverlay(items []scheduling.ScheduleItem) {
	m.scheduleBrowser = newScheduleBrowserState(items)
	m.ensureOverlayStack()
	m.overlays.Open(overlaySchedule, scheduleOverlayDialog{})
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
	m.overlays.Open(overlayModel, modelOverlayDialog{})
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
	m.overlays.Open(overlayTheme, themeOverlayDialog{})
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
	m.overlays.Open(overlayStatus, statuslineOverlayDialog{})
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
	m.overlays.Open(overlayMCP, mcpOverlayDialog{})
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
	m.overlays.Open(overlayHelp, helpOverlayDialog{})
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
	m.overlays.Open(overlayReview, reviewOverlayDialog{})
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
	m.overlays.Open(overlayResume, resumeOverlayDialog{})
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
	m.overlays.Open(overlayFork, forkOverlayDialog{})
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
	m.overlays.Open(overlayAgent, agentOverlayDialog{})
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
	m.overlays.Open(overlayMention, mentionOverlayDialog{})
}

func (m chatModel) closeMentionOverlay() chatModel {
	m.mentionPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayMention)
	}
	m.refreshViewport()
	return m
}

func (m *chatModel) openCopyOverlay() {
	m.ensureOverlayStack()
	m.overlays.Open(overlayCopy, copyPickerOverlayDialog{})
}

func (m chatModel) closeCopyOverlay() chatModel {
	m.copyPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayCopy)
	}
	m.refreshViewport()
	return m
}

func (m chatModel) activeOverlay() overlayDialog {
	if m.overlays == nil {
		return nil
	}
	return m.overlays.TopDialog()
}

// extOverlayAdapter wraps a CustomOverlay so it satisfies overlayDialog,
// integrating custom overlays into the existing overlay stack.
type extOverlayAdapter struct {
	impl CustomOverlay
}

func (a extOverlayAdapter) ID() overlayID { return overlayExt }

func (a extOverlayAdapter) View(m chatModel, width, height int) string {
	return a.impl.View(m.overlayContext(width, height))
}

func (a extOverlayAdapter) HandleKey(m chatModel, msg tea.KeyMsg) (chatModel, tea.Cmd) {
	ctx := m.overlayContext(m.width, m.height)
	cmd := a.impl.HandleKey(ctx, msg)
	return m, cmd
}
