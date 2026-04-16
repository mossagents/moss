package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/harness/appkit/product"
	configpkg "github.com/mossagents/moss/harness/config"
	rprofile "github.com/mossagents/moss/harness/runtime/profile"
	"github.com/mossagents/moss/kernel/model"
)

func (m appModel) updateChatCore(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, model, cmd := m.handleControlMessages(msg); handled {
		return model, cmd
	}
	if handled, model, cmd := m.handleProfileSwitch(msg); handled {
		return model, cmd
	}
	if handled, model, cmd := m.handleKernelReady(msg); handled {
		return model, cmd
	}
	return m.fallbackChatUpdate(msg)
}

func (m appModel) handleControlMessages(msg tea.Msg) (handled bool, model tea.Model, cmd tea.Cmd) {
	if _, ok := msg.(cancelMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		// Fire OnSessionEnd hooks before quitting.
		hookCmds := m.fireExtensionSessionEnd()
		if len(hookCmds) > 0 {
			// Batch hook commands with quit so cleanups can run.
			return true, m, tea.Batch(append(hookCmds, tea.Quit)...)
		}
		return true, m, tea.Quit
	}

	if sm, ok := msg.(switchModelMsg); ok {
		if err := persistModelOverride(sm); err != nil {
			m.chat.messages = append(m.chat.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to save model selection: %v", err)})
			m.chat.streaming = false
			m.chat.refreshViewport()
			return true, m, nil
		}
		prevModel := m.config.Model
		m = m.stopAgentForKernelRebuild()
		m.config.Provider = sm.provider
		m.config.ProviderName = sm.providerName
		m.config.Model = sm.model
		m.chat.modelAuto = sm.auto
		nextModel, nextCmd := m.rebuildKernelWithSelection(sm.provider, sm.providerName, sm.model)
		// Fire OnModelSwitch hooks.
		hookCmds := m.fireExtensionModelSwitch(prevModel, sm.model)
		if len(hookCmds) > 0 {
			return true, nextModel, tea.Batch(append(hookCmds, nextCmd)...)
		}
		return true, nextModel, nextCmd
	}

	if st, ok := msg.(switchTrustMsg); ok {
		m = m.stopAgentForKernelRebuild()
		m.config.Trust = st.trust
		nextModel, nextCmd := m.rebuildKernelWithSelection(m.config.Provider, m.config.ProviderName, m.config.Model)
		return true, nextModel, nextCmd
	}

	if st, ok := msg.(switchApprovalMsg); ok {
		m = m.stopAgentForKernelRebuild()
		m.config.ApprovalMode = st.mode
		nextModel, nextCmd := m.rebuildKernelWithSelection(m.config.Provider, m.config.ProviderName, m.config.Model)
		return true, nextModel, nextCmd
	}

	return false, nil, nil
}

func (m appModel) handleProfileSwitch(msg tea.Msg) (handled bool, model tea.Model, cmd tea.Cmd) {
	st, ok := msg.(switchProfileMsg)
	if !ok {
		return false, nil, nil
	}

	checkpointMsg := ""
	if m.agent != nil {
		var err error
		checkpointMsg, err = m.agent.prepareProfileSwitch(st.profile)
		if err != nil {
			m.chat.messages = append(m.chat.messages, chatMessage{kind: msgError, content: err.Error()})
			m.chat.streaming = false
			m.chat.refreshViewport()
			return true, m, nil
		}
	}
	m = m.stopAgentForKernelRebuild()
	resolved, err := rprofile.ResolveProfileForWorkspace(rprofile.ProfileResolveOptions{
		Workspace:        m.config.Workspace,
		RequestedProfile: st.profile,
	})
	if err != nil {
		m.chat.messages = append(m.chat.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to switch profile: %v", err)})
		m.chat.streaming = false
		m.chat.refreshViewport()
		return true, m, nil
	}
	m.config.Profile = resolved.Name
	m.config.Trust = resolved.Trust
	m.config.ApprovalMode = resolved.ApprovalMode
	m.chat.profile = resolved.Name
	m.chat.trust = resolved.Trust
	m.chat.approvalMode = resolved.ApprovalMode
	m.postInitDisplayText = strings.TrimSpace(st.displayText)
	m.postInitRunText = strings.TrimSpace(st.prompt)
	if strings.TrimSpace(checkpointMsg) != "" {
		m.chat.messages = append(m.chat.messages, chatMessage{
			kind:    msgSystem,
			content: checkpointMsg + "\nStarting a fresh session with the new profile.",
		})
		m.chat.refreshViewport()
	}
	nextModel, nextCmd := m.rebuildKernelWithSelection(m.config.Provider, m.config.ProviderName, m.config.Model)
	return true, nextModel, nextCmd
}

func (m appModel) handleKernelReady(msg tea.Msg) (handled bool, result tea.Model, cmd tea.Cmd) {
	ready, ok := msg.(kernelReadyMsg)
	if !ok {
		return false, nil, nil
	}

	m.agent = ready.agent
	agent := ready.agent

	m.chat.trust = m.config.Trust
	m.chat.profile = m.config.Profile
	m.chat.approvalMode = m.config.ApprovalMode
	m.chat.scheduleCtrl = m.config.ScheduleController

	m.bindSessionCallbacks(agent)
	m.bindCheckpointCallbacks(agent)
	m.bindTaskCallbacks(agent)
	m.bindDebugCallbacks(agent)
	m.bindToolingCallbacks(agent)

	m.chat.setDiscoveredSkills(discoveredSkillNames(agent, m.config.Workspace))
	if agent.sess != nil {
		m.chat.currentSessionID = agent.sess.ID
	}
	if notice := m.chat.syncCustomCommands(); strings.TrimSpace(notice) != "" {
		ready.notices = append(ready.notices, notice)
	}
	connInfo := m.chat.provider
	if m.config.Model != "" {
		m.chat.model = m.config.Model
		connInfo += " (" + m.config.Model + ")"
	}
	if m.config.Trust != "" {
		connInfo += " [" + m.config.Trust + "]"
	}
	if strings.TrimSpace(m.config.ApprovalMode) != "" {
		connInfo += " {" + m.config.ApprovalMode + "}"
	}
	m.chat.streaming = false
	m.chat.messages = append(m.chat.messages, chatMessage{
		kind:    msgSystem,
		content: fmt.Sprintf("Connected to %s", connInfo),
	})
	for _, notice := range ready.notices {
		if strings.TrimSpace(notice) == "" {
			continue
		}
		m.chat.messages = append(m.chat.messages, chatMessage{kind: msgSystem, content: strings.TrimSpace(notice)})
	}
	if strings.TrimSpace(m.postInitRunText) != "" {
		displayText := m.postInitDisplayText
		runText := m.postInitRunText
		m.postInitDisplayText = ""
		m.postInitRunText = ""
		nextChat, dispatchCmd := m.chat.dispatchUserSubmission(displayText, runText, []model.ContentPart{model.TextPart(runText)})
		m.chat = nextChat
		m.chat.refreshViewport()
		go agent.publishProgressReplay()
		cmds := append(m.fireExtensionSessionStart(), dispatchCmd)
		return true, m, tea.Batch(cmds...)
	}
	m.chat.refreshViewport()
	go agent.publishProgressReplay()
	// Fire OnSessionStart lifecycle hooks.
	hookCmds := m.fireExtensionSessionStart()
	if len(hookCmds) > 0 {
		return true, m, tea.Batch(hookCmds...)
	}
	return true, m, nil
}

func (m appModel) fallbackChatUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	m.theme = m.chat.theme
	return m, cmd
}

func (m *appModel) bindSessionCallbacks(agent *agentState) {
	m.chat.sessionInfoFn = agent.sessionSummary
	m.chat.sessionListFn = func(limit int) (string, error) {
		return agent.listPersistedSessions(limit)
	}
	m.chat.sessionRestoreFn = func(sessionID string) (string, error) {
		return agent.restoreSession(sessionID)
	}
	m.chat.newSessionFn = func() (string, error) {
		return agent.newSession()
	}
	m.chat.offloadFn = func(keepRecent int, note string) (string, error) {
		return agent.offloadContext(keepRecent, note)
	}
}

func (m *appModel) bindCheckpointCallbacks(agent *agentState) {
	m.chat.checkpointListFn = func(limit int) (string, error) {
		return agent.listPersistedCheckpoints(limit)
	}
	m.chat.checkpointShowFn = func(checkpointID string) (string, error) {
		return agent.showPersistedCheckpoint(checkpointID)
	}
	m.chat.checkpointCreateFn = func(note string) (string, error) {
		return agent.createCheckpoint(note)
	}
	m.chat.checkpointForkFn = func(sourceKind, sourceID string, restoreWorktree bool) (string, error) {
		return agent.forkSession(sourceKind, sourceID, restoreWorktree)
	}
	m.chat.checkpointReplayFn = func(checkpointID, mode string, restoreWorktree bool) (string, error) {
		return agent.replayCheckpoint(checkpointID, mode, restoreWorktree)
	}
}

func (m *appModel) bindTaskCallbacks(agent *agentState) {
	m.chat.taskListFn = func(status string, limit int) (string, error) {
		return agent.listTasks(status, limit)
	}
	m.chat.taskQueryFn = func(taskID string) (string, error) {
		return agent.queryTask(taskID)
	}
	m.chat.taskCancelFn = func(taskID, reason string) (string, error) {
		return agent.cancelTask(taskID, reason)
	}
}

func (m *appModel) bindDebugCallbacks(agent *agentState) {
	m.chat.permissionSummaryFn = agent.permissionSummary
	m.chat.setPermissionFn = agent.setPermission
	m.chat.refreshSystemPromptFn = agent.refreshSystemPrompt
	m.chat.debugPromptFn = func() string {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		if agent.sess == nil {
			return ""
		}
		return strings.TrimSpace(agent.sess.Config.SystemPrompt)
	}
	m.chat.debugConfigFn = func() string {
		baseSource, dynamicSections, sourceChain := agent.promptDebugInfo()
		report := product.BuildDebugConfigReport(
			configpkg.AppName(),
			m.config.Workspace,
			m.chat.provider,
			m.chat.model,
			m.chat.trust,
			m.chat.approvalMode,
			m.chat.profile,
			m.chat.theme,
			baseSource,
			dynamicSections,
			sourceChain,
		)
		return product.RenderDebugConfigReport(report)
	}
}

func (m *appModel) bindToolingCallbacks(agent *agentState) {
	m.chat.sendFn = func(text string, parts []model.ContentPart) {
		go agent.appendAndRun(text, parts)
	}
	m.chat.cancelRunFn = agent.cancelCurrentRun
	m.chat.skillListFn = func() string {
		return renderSkillsSummary(agent, m.config.Workspace)
	}
}

// fireExtensionSessionStart calls OnSessionStart on all extensions and returns
// the non-nil Cmds. The TUI context is captured from the current chat state.
func (m appModel) fireExtensionSessionStart() []tea.Cmd {
	ctx := m.chat.tuiContext()
	var cmds []tea.Cmd
	for _, ext := range m.chat.extensions {
		if ext == nil || ext.OnSessionStart == nil {
			continue
		}
		if cmd := ext.OnSessionStart(ctx); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// fireExtensionSessionEnd calls OnSessionEnd on all extensions and returns the non-nil Cmds.
func (m appModel) fireExtensionSessionEnd() []tea.Cmd {
	ctx := m.chat.tuiContext()
	var cmds []tea.Cmd
	for _, ext := range m.chat.extensions {
		if ext == nil || ext.OnSessionEnd == nil {
			continue
		}
		if cmd := ext.OnSessionEnd(ctx); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

// fireExtensionModelSwitch calls OnModelSwitch on all extensions and returns the non-nil Cmds.
func (m appModel) fireExtensionModelSwitch(prevModel, nextModel string) []tea.Cmd {
	ctx := m.chat.tuiContext()
	var cmds []tea.Cmd
	for _, ext := range m.chat.extensions {
		if ext == nil || ext.OnModelSwitch == nil {
			continue
		}
		if cmd := ext.OnModelSwitch(ctx, prevModel, nextModel); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}
