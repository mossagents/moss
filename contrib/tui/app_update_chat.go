package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/harness/appkit/product"
	configpkg "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/runtime"
	"github.com/mossagents/moss/harness/runtime/collaboration"
	"github.com/mossagents/moss/kernel/model"
	ksession "github.com/mossagents/moss/kernel/session"
)

func (m appModel) updateChatCore(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, model, cmd := m.handleControlMessages(msg); handled {
		return model, cmd
	}
	if handled, model, cmd := m.handleModeSwitch(msg); handled {
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

func (m appModel) handleModeSwitch(msg tea.Msg) (handled bool, model tea.Model, cmd tea.Cmd) {
	st, ok := msg.(switchModeMsg)
	if !ok {
		return false, nil, nil
	}

	nextMode := string(collaboration.NormalizeMode(st.mode))
	if nextMode == "" {
		nextMode = string(collaboration.ModeExecute)
	}

	checkpointMsg := ""
	if m.agent != nil {
		var err error
		checkpointMsg, err = m.agent.prepareModeSwitch(nextMode)
		if err != nil {
			m.chat.messages = append(m.chat.messages, chatMessage{kind: msgError, content: err.Error()})
			m.chat.streaming = false
			m.chat.refreshViewport()
			return true, m, nil
		}
	}
	m = m.stopAgentForKernelRebuild()
	m.config.CollaborationMode = nextMode
	m.chat.collaborationMode = nextMode
	m.postInitDisplayText = strings.TrimSpace(st.displayText)
	m.postInitRunText = strings.TrimSpace(st.prompt)
	m.postInitSystemNotice = fmt.Sprintf("Mode switched to %s.", collaborationModeDisplay(nextMode))
	m.suppressConnectNotice = true
	_ = checkpointMsg
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
	mode := firstNonEmptyTrimmed(m.config.CollaborationMode, string(collaboration.ModeExecute))
	if agent.sess != nil {
		_, _, _, sessionMode, _, _, _, _ := ksession.SessionFacetValues(agent.sess)
		mode = firstNonEmptyTrimmed(sessionMode, mode, string(collaboration.ModeExecute))
	}
	m.config.CollaborationMode = mode
	m.chat.collaborationMode = mode
	m.chat.approvalMode = m.config.ApprovalMode
	m.chat.scheduleCtrl = m.config.ScheduleController

	m.bindAgentOps(agent)
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
	if !m.suppressConnectNotice {
		m.chat.messages = append(m.chat.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Connected to %s", connInfo),
		})
	}
	m.suppressConnectNotice = false
	if notice := strings.TrimSpace(m.postInitSystemNotice); notice != "" {
		m.chat.messages = append(m.chat.messages, chatMessage{kind: msgSystem, content: notice})
		m.postInitSystemNotice = ""
	}
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

func (m *appModel) bindAgentOps(agent *agentState) {
	m.chat.session = &agentSessionOps{
		info:    agent.sessionSummary,
		list:    agent.listPersistedSessions,
		restore: agent.restoreSession,
		newSess: agent.newSession,
		offload: agent.offloadContext,
	}
	m.chat.checkpoint = &agentCheckpointOps{
		list:   agent.listPersistedCheckpoints,
		show:   agent.showPersistedCheckpoint,
		create: agent.createCheckpoint,
		fork:   agent.forkSession,
		replay: agent.replayCheckpoint,
	}
	m.chat.task = &agentTaskOps{
		list:   agent.listTasks,
		query:  agent.queryTask,
		cancel: agent.cancelTask,
	}
	m.chat.inspect = &agentInspectOps{
		permSummary: agent.permissionSummary,
		setPerm:     agent.setPermission,
		refreshSP:   agent.refreshSystemPrompt,
		debugPrompt: agent.debugPrompt,
	}
}

func (m *appModel) bindDebugCallbacks(agent *agentState) {
	m.chat.debugConfigFn = func() string {
		baseSource, dynamicSections, sourceChain := agent.promptDebugInfo()
		runMode, preset, _, collaborationMode, promptPack, permissionProfile, sessionPolicy, modelProfile := ksession.SessionFacetValues(agent.sess)
		report := product.BuildDebugConfigReport(
			configpkg.AppName(),
			m.config.Workspace,
			m.chat.provider,
			m.chat.model,
			m.chat.trust,
			m.chat.approvalMode,
			product.SessionSelectorReport{
				RunMode:           strings.TrimSpace(runMode),
				Preset:            strings.TrimSpace(preset),
				CollaborationMode: strings.TrimSpace(collaborationMode),
				PromptPack:        strings.TrimSpace(promptPack),
				PermissionProfile: strings.TrimSpace(permissionProfile),
				SessionPolicy:     strings.TrimSpace(sessionPolicy),
				ModelProfile:      strings.TrimSpace(modelProfile),
			},
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
	m.chat.skillItemsFn = func() []skillsPickerItem {
		manifests := runtime.LookupSkillManifests(agent.k)
		if len(manifests) == 0 {
			return nil
		}
		sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })
		manager, _ := runtime.LookupCapabilityManager(agent.k)
		items := make([]skillsPickerItem, 0, len(manifests))
		for _, mf := range manifests {
			enabled := false
			if manager != nil {
				_, enabled = manager.Get(mf.Name)
			}
			items = append(items, skillsPickerItem{
				name:        mf.Name,
				description: mf.Description,
				enabled:     enabled,
			})
		}
		return items
	}
	m.chat.skillToggleFn = func(name string, enable bool) error {
		if enable {
			return runtime.ActivateSkill(context.Background(), agent.k, name)
		}
		return runtime.DeactivateSkill(context.Background(), agent.k, name)
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
