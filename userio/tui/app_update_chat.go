package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	"github.com/mossagents/moss/appkit/runtime"
	configpkg "github.com/mossagents/moss/config"
)

func (m appModel) updateChatCore(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 取消并退出
	if _, ok := msg.(cancelMsg); ok {
		if m.agent != nil && m.agent.cancel != nil {
			m.agent.cancel()
		}
		return m, tea.Quit
	}

	// 切换模型：关闭旧 kernel，用新 model 重建
	if sm, ok := msg.(switchModelMsg); ok {
		m = m.stopAgentForKernelRebuild()
		m.config.Model = sm.model
		return m.rebuildKernelWithModel(sm.model)
	}

	// 切换 trust：关闭旧 kernel，用新 trust 重建
	if st, ok := msg.(switchTrustMsg); ok {
		m = m.stopAgentForKernelRebuild()
		m.config.Trust = st.trust
		return m.rebuildKernelWithModel(m.config.Model)
	}

	if st, ok := msg.(switchApprovalMsg); ok {
		m = m.stopAgentForKernelRebuild()
		m.config.ApprovalMode = st.mode
		return m.rebuildKernelWithModel(m.config.Model)
	}

	if st, ok := msg.(switchProfileMsg); ok {
		checkpointMsg := ""
		if m.agent != nil {
			var err error
			checkpointMsg, err = m.agent.prepareProfileSwitch(st.profile)
			if err != nil {
				m.chat.messages = append(m.chat.messages, chatMessage{kind: msgError, content: err.Error()})
				m.chat.streaming = false
				m.chat.refreshViewport()
				return m, nil
			}
		}
		m = m.stopAgentForKernelRebuild()
		resolved, err := runtime.ResolveProfileForWorkspace(runtime.ProfileResolveOptions{
			Workspace:        m.config.Workspace,
			RequestedProfile: st.profile,
		})
		if err != nil {
			m.chat.messages = append(m.chat.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to switch profile: %v", err)})
			m.chat.streaming = false
			m.chat.refreshViewport()
			return m, nil
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
		return m.rebuildKernelWithModel(m.config.Model)
	}

	// kernel 就绪：设置 sendFn 为多轮复用 session 的方式
	if ready, ok := msg.(kernelReadyMsg); ok {
		m.agent = ready.agent
		agent := ready.agent
		m.chat.sendFn = func(text string) {
			go agent.appendAndRun(text)
		}
		m.chat.cancelRunFn = agent.cancelCurrentRun
		m.chat.trust = m.config.Trust
		m.chat.profile = m.config.Profile
		m.chat.approvalMode = m.config.ApprovalMode
		m.chat.sessionInfoFn = agent.sessionSummary
		m.chat.sessionListFn = func(limit int) (string, error) {
			return agent.listPersistedSessions(limit)
		}
		m.chat.changeListFn = func(limit int) (string, error) {
			return agent.listPersistedChanges(limit)
		}
		m.chat.changeShowFn = func(changeID string) (string, error) {
			return agent.showPersistedChange(changeID)
		}
		m.chat.applyChangeFn = func(patchFile, summary string) (string, error) {
			return agent.applyChange(patchFile, summary)
		}
		m.chat.rollbackChangeFn = func(changeID string) (string, error) {
			return agent.rollbackChange(changeID)
		}
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
		m.chat.sessionRestoreFn = func(sessionID string) (string, error) {
			return agent.restoreSession(sessionID)
		}
		m.chat.newSessionFn = func() (string, error) {
			return agent.newSession()
		}
		m.chat.offloadFn = func(keepRecent int, note string) (string, error) {
			return agent.offloadContext(keepRecent, note)
		}
		m.chat.taskListFn = func(status string, limit int) (string, error) {
			return agent.listTasks(status, limit)
		}
		m.chat.taskQueryFn = func(taskID string) (string, error) {
			return agent.queryTask(taskID)
		}
		m.chat.taskCancelFn = func(taskID, reason string) (string, error) {
			return agent.cancelTask(taskID, reason)
		}
		m.chat.scheduleCtrl = m.config.ScheduleController
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
		m.chat.gitRunFn = func(cmd string, args []string) (string, error) {
			raw, err := agent.invokeTool(agent.ctx, "run_command", map[string]any{
				"command": cmd,
				"args":    args,
			})
			if err != nil {
				return "", err
			}
			return formatJSON(raw), nil
		}
		m.chat.skillListFn = func() string {
			return renderSkillsSummary(agent, m.config.Workspace)
		}
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
			nextChat, cmd := m.chat.dispatchUserSubmission(displayText, runText)
			m.chat = nextChat
			m.chat.refreshViewport()
			go agent.publishProgressReplay()
			return m, cmd
		}
		m.chat.refreshViewport()
		go agent.publishProgressReplay()
		return m, nil
	}

	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(msg)
	m.theme = m.chat.theme
	return m, cmd
}
