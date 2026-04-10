package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/model"
	userattachments "github.com/mossagents/moss/userio/attachments"
	userlocation "github.com/mossagents/moss/userio/location"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type slashCommandHandler func(chatModel, []string, string, string) (chatModel, tea.Cmd)

var slashCommandRegistry = map[string]slashCommandHandler{
	"/exit":         handleExitSlashCommand,
	"/quit":         handleExitSlashCommand,
	"/model":        handleModelSlashCommand,
	"/models":       handleModelSlashCommand,
	"/fast":         handleFastSlashCommand,
	"/personality":  handlePersonalitySlashCommand,
	"/clear":        handleClearSlashCommand,
	"/copy":         handleCopySlashCommand,
	"/skills":       handleSkillsSlashCommand,
	"/skill":        handleSkillSlashCommand,
	"/session":      handleSessionLegacySlashCommand,
	"/status":       handleStatusSlashCommand,
	"/resume":       handleResumeSlashCommand,
	"/trace":        handleTraceSlashCommand,
	"/new":          handleNewSlashCommand,
	"/checkpoint":   handleCheckpointSlashCommand,
	"/fork":         handleForkSlashCommand,
	"/changes":      handleChangesSlashCommand,
	"/apply":        handleApplySlashCommand,
	"/rollback":     handleRollbackSlashCommand,
	"/diff":         handleDiffSlashCommand,
	"/review":       handleReviewSlashCommand,
	"/inspect":      handleInspectSlashCommand,
	"/sessions":     handleSessionsLegacySlashCommand,
	"/mcp":          handleMCPSlashCommand,
	"/compact":      handleCompactSlashCommand,
	"/offload":      handleOffloadLegacySlashCommand,
	"/agent":        handleAgentSlashCommand,
	"/ps":           handlePSSlashCommand,
	"/tasks":        handleTasksLegacySlashCommand,
	"/task":         handleTaskLegacySlashCommand,
	"/config":       handleConfigSlashCommand,
	"/schedules":    handleSchedulesSlashCommand,
	"/git":          handleGitSlashCommand,
	"/budget":       handleBudgetLegacySlashCommand,
	"/permissions":  handlePermissionsSlashCommand,
	"/trust":        handleTrustSlashCommand,
	"/approval":     handleApprovalSlashCommand,
	"/profile":      handleProfileSlashCommand,
	"/plan":         handlePlanSlashCommand,
	"/help":         handleHelpSlashCommand,
	"/debug":        handleDebugSlashCommand,
	"/debug-config": handleDebugConfigSlashCommand,
	"/theme":        handleThemeSlashCommand,
	"/statusline":   handleStatuslineSlashCommand,
	"/experimental": handleExperimentalSlashCommand,
	"/search":       handleSearchSlashCommand,
	"/open":         handleOpenSlashCommand,
	"/image":        handleImageSlashCommand,
	"/media":        handleMediaSlashCommand,
	"/mention":      handleMentionSlashCommand,
	"/init":         handleInitSlashCommand,
}

// handleSlashCommand 处理 / 开头的斜杠命令。
func (m chatModel) handleSlashCommand(input string) (chatModel, tea.Cmd) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return m, nil
	}
	cmd := strings.ToLower(parts[0])
	args := parts[1:]
	draft := strings.TrimSpace(m.textarea.Value())

	m.textarea.Reset()
	if handler, ok := slashCommandRegistry[cmd]; ok {
		return handler(m, args, input, draft)
	}
	// Extension slash commands (after built-ins, before workspace custom commands).
	ctx := m.tuiContext()
	for _, ext := range m.extensions {
		if handler, ok := ext.SlashCommands[cmd]; ok {
			return m, handler(ctx, args)
		}
	}
	if custom, ok := m.findCustomCommand(cmd); ok {
		runText := product.RenderCustomCommandPrompt(custom, strings.TrimSpace(strings.Join(args, " ")), m.workspace)
		if runText == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("custom command /%s is empty", custom.Name)})
			m.refreshViewport()
			return m, nil
		}
		return m.dispatchUserSubmission(input, runText, []model.ContentPart{model.TextPart(runText)})
	}
	if strings.HasPrefix(cmd, "/") && len(cmd) > 1 {
		name := strings.TrimSpace(strings.TrimPrefix(cmd, "/"))
		task := strings.TrimSpace(strings.Join(args, " "))
		if task == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /<skill_or_tool_name> <task...>"})
			m.refreshViewport()
			return m, nil
		}
		return m.invokeSkillLikeCommand(name, task, input)
	}
	m.messages = append(m.messages, chatMessage{
		kind:    msgSystem,
		content: fmt.Sprintf("Unknown command: %s (use /help to list commands)", cmd),
	})
	m.refreshViewport()
	return m, nil
}

func handleExitSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Goodbye"})
	m.refreshViewport()
	return m, func() tea.Msg { return cancelMsg{} }
}

func handleModelSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		return m.openModelPicker()
	}
	return m.switchModelByQuery(strings.Join(args, " "))
}

func handleFastSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		state := "off"
		if m.fastMode {
			state = "on"
		}
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Fast mode: %s\nUsage: /fast <on|off|status>", state),
		})
		m.refreshViewport()
		return m, nil
	}
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on":
		enabled = true
	case "off":
		enabled = false
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /fast <on|off|status>"})
		m.refreshViewport()
		return m, nil
	}
	if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
		cfg.FastMode = boolPtr(enabled)
		return nil
	}); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update fast mode: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.fastMode = enabled
	if m.refreshSystemPromptFn != nil {
		if err := m.refreshSystemPromptFn(); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("fast mode saved but failed to refresh the current thread prompt: %v", err)})
			m.refreshViewport()
			return m, nil
		}
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Fast mode %s. New turns in the current thread will use the updated interaction mode.", onOff(enabled))})
	m.refreshViewport()
	return m, nil
}

func handlePersonalitySlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Current personality: %s\nUsage: /personality <friendly|pragmatic|none>", valueOrDefaultString(m.personality, product.PersonalityFriendly)),
		})
		m.refreshViewport()
		return m, nil
	}
	personality := product.NormalizePersonality(args[0])
	if personality == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /personality <friendly|pragmatic|none>"})
		m.refreshViewport()
		return m, nil
	}
	if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
		cfg.Personality = personality
		return nil
	}); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update personality: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.personality = personality
	if m.refreshSystemPromptFn != nil {
		if err := m.refreshSystemPromptFn(); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("personality saved but failed to refresh the current thread prompt: %v", err)})
			m.refreshViewport()
			return m, nil
		}
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Personality set to %s for this product surface and the current thread.", personality)})
	m.refreshViewport()
	return m, nil
}

func handleClearSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = nil
	m.messages = append(m.messages, chatMessage{
		kind:    msgSystem,
		content: "Conversation cleared.",
	})
	m.refreshViewport()
	return m, nil
}

func handleCopySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	content := m.latestCopiableContent()
	if strings.TrimSpace(content) == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "No completed output is available to copy yet."})
		m.refreshViewport()
		return m, nil
	}
	if err := writeClipboard(content); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to copy output: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Copied the latest completed output to the clipboard."})
	}
	m.refreshViewport()
	return m, nil
}

func handleSkillsSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	info := "Skill information is unavailable."
	if m.skillListFn != nil {
		info = m.skillListFn()
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
	m.refreshViewport()
	return m, nil
}

func handleSkillSlashCommand(m chatModel, args []string, input string, _ string) (chatModel, tea.Cmd) {
	if len(args) < 2 {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /skill <name> <task...>"})
		m.refreshViewport()
		return m, nil
	}
	name := strings.TrimSpace(args[0])
	task := strings.TrimSpace(strings.Join(args[1:], " "))
	return m.invokeSkillLikeCommand(name, task, input)
}

func handleSessionLegacySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Current thread summary moved to /status. Use /resume to continue saved threads."})
	m.refreshViewport()
	return m, nil
}

func handleStatusSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.renderStatusSummary()})
	m.refreshViewport()
	return m, nil
}

func handleResumeSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		return m.openResumePicker()
	}
	if len(args) > 1 || m.sessionRestoreFn == nil {
		if m.sessionRestoreFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Resume is unavailable."})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /resume [session_id|latest]"})
		}
		m.refreshViewport()
		return m, nil
	}
	id := strings.TrimSpace(args[0])
	if id == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /resume [session_id|latest]"})
		m.refreshViewport()
		return m, nil
	}
	return m.startThreadSwitch(fmt.Sprintf("Resuming thread %s...", id), func() (string, error) {
		out, err := m.sessionRestoreFn(id)
		if err != nil {
			return "", fmt.Errorf("failed to resume thread: %v", err)
		}
		return out, nil
	})
}

func handleTraceSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	limit := 20
	if len(args) >= 1 {
		v, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || v <= 0 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /trace [limit:int]"})
			m.refreshViewport()
			return m, nil
		}
		limit = v
	}
	if m.lastTrace == nil {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "No run trace is available yet. Run a task first."})
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderRunTraceDetail(*m.lastTrace, limit)})
	m.refreshViewport()
	return m, nil
}

func handleNewSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.newSessionFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "New thread creation is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	return m.startThreadSwitch("Starting a fresh thread...", func() (string, error) {
		out, err := m.newSessionFn()
		if err != nil {
			return "", fmt.Errorf("failed to create new thread: %v", err)
		}
		return out, nil
	})
}

func handleChangesSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /changes list [limit]\n  /changes show <change_id>"})
		m.refreshViewport()
		return m, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		if m.changeListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change list is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		limit := 20
		if len(args) >= 2 {
			v, err := strconv.Atoi(args[1])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /changes list [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.changeListFn(limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list changes: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	case "show":
		if m.changeShowFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change detail is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /changes show <change_id>"})
			m.refreshViewport()
			return m, nil
		}
		out, err := m.changeShowFn(strings.TrimSpace(args[1]))
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to show change: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /changes list|show ..."})
		m.refreshViewport()
		return m, nil
	}
}

func handleApplySlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.applyChangeFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change apply is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /apply <patch_file> [summary...]"})
		m.refreshViewport()
		return m, nil
	}
	patchFile := strings.TrimSpace(args[0])
	summary := strings.TrimSpace(strings.Join(args[1:], " "))
	out, err := m.applyChangeFn(patchFile, summary)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to apply change: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleRollbackSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.rollbackChangeFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Change rollback is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /rollback <change_id>"})
		m.refreshViewport()
		return m, nil
	}
	out, err := m.rollbackChangeFn(strings.TrimSpace(args[0]))
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to roll back change: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleDiffSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.gitRunFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Diff inspection is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	cmdArgs := []string{"--no-pager", "diff"}
	if len(args) > 0 {
		cmdArgs = append(cmdArgs, "--")
		cmdArgs = append(cmdArgs, args...)
	}
	out, err := m.gitRunFn("git", cmdArgs)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git diff failed: %v", err)})
	} else if strings.TrimSpace(out) == "" {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "No diff."})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleReviewSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		return m.openReviewPicker()
	}
	report, err := product.BuildReviewReport(context.Background(), m.workspace, args)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("review failed: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderReviewReport(report)})
	}
	m.refreshViewport()
	return m, nil
}

func handleInspectSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	report, err := product.BuildInspectReportForTrust(context.Background(), m.workspace, m.trust, args)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("inspect failed: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderInspectReport(report)})
	}
	m.refreshViewport()
	return m, nil
}

func handleSessionsLegacySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Saved-session browsing moved to /resume. Use /resume to list recent sessions or /resume <session_id> to continue one."})
	m.refreshViewport()
	return m, nil
}

func handleMCPSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 || strings.EqualFold(args[0], "list") {
		return m.openMCPPicker()
	}
	if strings.EqualFold(args[0], "show") && len(args) == 2 {
		servers, err := product.GetMCPServer(m.workspace, m.trust, args[1])
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to show MCP server: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderMCPServerDetail(servers)})
		}
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /mcp\n  /mcp list\n  /mcp show <name>"})
	m.refreshViewport()
	return m, nil
}

func handleCompactSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.offloadFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Context compaction is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	keepRecent := 20
	note := "manual compact from TUI"
	if len(args) >= 1 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v <= 0 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /compact [keep_recent:int] [note...]"})
			m.refreshViewport()
			return m, nil
		}
		keepRecent = v
	}
	if len(args) >= 2 {
		note = strings.Join(args[1:], " ")
	}
	out, err := m.offloadFn(keepRecent, note)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("compact failed: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleOffloadLegacySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Transcript compaction moved to /compact. Use /compact [keep_recent] [note]."})
	m.refreshViewport()
	return m, nil
}

func handlePSSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if !m.experimentalEnabled(product.ExperimentalBackgroundPS) {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background activity view is disabled. Use /experimental enable background-ps to turn it back on."})
		m.refreshViewport()
		return m, nil
	}
	if m.taskListFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background activity view is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	status := "running"
	limit := 10
	if len(args) >= 1 {
		status = strings.ToLower(strings.TrimSpace(args[0]))
		switch status {
		case "running", "completed", "failed", "cancelled", "all":
		default:
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /ps [running|completed|failed|cancelled|all] [limit:int]"})
			m.refreshViewport()
			return m, nil
		}
		if status == "all" {
			status = ""
		}
	}
	if len(args) >= 2 {
		v, err := strconv.Atoi(strings.TrimSpace(args[1]))
		if err != nil || v <= 0 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /ps [running|completed|failed|cancelled|all] [limit:int]"})
			m.refreshViewport()
			return m, nil
		}
		limit = v
	}
	out, err := m.taskListFn(status, limit)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to inspect background activity: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleTasksLegacySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background agent controls moved to /agent. Use /agent [list], /agent current, /agent <id>, or /agent cancel <id>."})
	m.refreshViewport()
	return m, nil
}

func handleTaskLegacySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Background agent controls moved to /agent. Use /agent <id> or /agent cancel <id> [reason]."})
	m.refreshViewport()
	return m, nil
}

func handleConfigSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	return m.handleConfigCommand(args)
}

func handleSchedulesSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.scheduleCtrl == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Schedule listing is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	items, err := m.scheduleCtrl.List()
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list schedules: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	if len(items) > 0 {
		m.openScheduleOverlay(items)
		m.refreshViewport()
		return m, nil
	}
	out, err := m.scheduleCtrl.ListText()
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list schedules: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleBudgetLegacySlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Budget summary moved into /status."})
	m.refreshViewport()
	return m, nil
}

func handleTrustSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current trust: %s\nUsage: /trust <trusted|restricted>", m.trust)})
		m.refreshViewport()
		return m, nil
	}
	nextTrust := strings.ToLower(strings.TrimSpace(args[0]))
	if nextTrust != "trusted" && nextTrust != "restricted" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "trust must be trusted or restricted"})
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching trust to %s...", nextTrust)})
	m.streaming = true
	m.refreshViewport()
	return m, func() tea.Msg { return switchTrustMsg{trust: nextTrust} }
}

func handleApprovalSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current approval mode: %s\nUsage: /approval <read-only|confirm|full-auto>", m.displayApprovalMode())})
		m.refreshViewport()
		return m, nil
	}
	nextMode := product.NormalizeApprovalMode(args[0])
	if err := product.ValidateApprovalMode(nextMode); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switching approval mode to %s...", nextMode)})
	m.streaming = true
	m.refreshViewport()
	return m, func() tea.Msg { return switchApprovalMsg{mode: nextMode} }
}

func handlePlanSlashCommand(m chatModel, args []string, input string, _ string) (chatModel, tea.Cmd) {
	prompt := strings.TrimSpace(strings.Join(args, " "))
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Switching to planning mode..."})
	m.streaming = true
	m.refreshViewport()
	return m, func() tea.Msg {
		return switchProfileMsg{
			profile:     "planning",
			prompt:      prompt,
			displayText: input,
		}
	}
}

func handleHelpSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	return m.openHelpPicker()
}

func handleDebugSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 || strings.EqualFold(args[0], "status") {
		state := "off"
		if m.debugPromptPreview {
			state = "on"
		}
		m.messages = append(m.messages, chatMessage{
			kind:    msgSystem,
			content: fmt.Sprintf("Debug prompt preview: %s\nUsage: /debug <on|off|status>", state),
		})
		m.refreshViewport()
		return m, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "on":
		m.debugPromptPreview = true
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Debug prompt preview enabled."})
	case "off":
		m.debugPromptPreview = false
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Debug prompt preview disabled."})
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /debug <on|off|status>"})
	}
	m.refreshViewport()
	return m, nil
}

func handleDebugConfigSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.debugConfigFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Debug config view is unavailable."})
	} else {
		content := m.debugConfigFn()
		if m.debugPromptPreview {
			if m.debugPromptFn == nil {
				content += "\n\nPrompt preview: unavailable."
			} else if preview := strings.TrimSpace(m.debugPromptFn()); preview == "" {
				content += "\n\nPrompt preview: unavailable."
			} else {
				content += "\n\nPrompt preview:\n" + preview
			}
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: content})
	}
	m.refreshViewport()
	return m, nil
}

func handleThemeSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		return m.openThemePicker()
	}
	raw := strings.ToLower(strings.TrimSpace(args[0]))
	if raw != themeDefault && raw != themePlain {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /theme [default|plain]"})
		m.refreshViewport()
		return m, nil
	}
	m.theme = raw
	applyTheme(raw)
	if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
		cfg.Theme = raw
		return nil
	}); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("theme switched locally but failed to persist: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Switched theme to %s and saved it to config.", raw)})
	m.refreshViewport()
	return m, nil
}

func handleSearchSlashCommand(m chatModel, args []string, input string, _ string) (chatModel, tea.Cmd) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /search <query>"})
		m.refreshViewport()
		return m, nil
	}
	return m.invokeSkillLikeCommand("web_search", query, input)
}

func handleOpenSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	target := strings.TrimSpace(strings.Join(args, " "))
	if target == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /open <path[:line]>"})
		m.refreshViewport()
		return m, nil
	}
	out, err := userlocation.OpenWorkspacePath(m.workspace, target)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("open failed: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleImageSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "attach") {
		return handleMediaAttachSlashCommand(m, args[1:], "image")
	}
	return handleMediaOpenSave(m, args, "image", "Usage: /image <open|save|attach> [path]")
}

func handleMediaSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "attach") {
		return handleMediaAttachSlashCommand(m, args[1:], "")
	}
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "clear") {
		m.pendingAttachments = nil
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Cleared pending composer attachments."})
		m.refreshViewport()
		return m, nil
	}
	return handleMediaOpenSave(m, args, "", "Usage: /media <open|save|attach> [path]")
}

func handleMediaOpenSave(m chatModel, args []string, mediaKind string, usage string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: usage})
		m.refreshViewport()
		return m, nil
	}
	meta := latestMediaMessageMeta(m.messages, mediaKind)
	if meta == nil {
		targetKind := mediaKind
		if strings.TrimSpace(targetKind) == "" {
			targetKind = "media"
		}
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("No generated %s found in current transcript.", targetKind)})
		m.refreshViewport()
		return m, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "open":
		target := strings.TrimSpace(toString(meta["media_source_path"]))
		if target == "" {
			target = strings.TrimSpace(toString(meta["media_path"]))
		}
		if target == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Latest media has no local file path to open."})
			m.refreshViewport()
			return m, nil
		}
		out, err := userlocation.OpenWorkspacePath(m.workspace, target)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("open failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	case "save":
		encoded := strings.TrimSpace(toString(meta["media_data_base64"]))
		mimeType := strings.TrimSpace(toString(meta["media_mime_type"]))
		if encoded == "" || mimeType == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Latest media has no inline data to save."})
			m.refreshViewport()
			return m, nil
		}
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("decode media failed: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		target := ""
		if len(args) >= 2 {
			target = strings.TrimSpace(strings.Join(args[1:], " "))
		}
		if target == "" {
			kind := strings.TrimSpace(toString(meta["media_kind"]))
			if kind == "" {
				kind = "media"
			}
			ext := extensionForMediaMIME(mimeType)
			target = filepath.Join(m.workspace, "generated-"+kind+ext)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(m.workspace, target)
		}
		if err := os.WriteFile(target, raw, 0600); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("save media failed: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Saved media to %s", target)})
		m.refreshViewport()
		return m, nil
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: usage})
		m.refreshViewport()
		return m, nil
	}
}

func handleMediaAttachSlashCommand(m chatModel, args []string, expectedKind string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		usage := "Usage: /media attach <path>"
		if expectedKind == "image" {
			usage = "Usage: /image attach <path>"
		}
		m.messages = append(m.messages, chatMessage{kind: msgError, content: usage})
		m.refreshViewport()
		return m, nil
	}
	draft, err := userattachments.BuildAttachmentDraft(m.workspace, strings.Join(args, " "))
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
		m.refreshViewport()
		return m, nil
	}
	if expectedKind != "" && draft.Kind != expectedKind {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("Expected a %s attachment, got %s.", expectedKind, draft.Kind)})
		m.refreshViewport()
		return m, nil
	}
	m.appendPendingAttachment(draft)
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Attached %s to the composer.", draft.Label)})
	m.refreshViewport()
	return m, nil
}

func latestMediaMessageMeta(messages []chatMessage, mediaKind string) map[string]any {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.kind != msgAssistant || msg.meta == nil {
			continue
		}
		if isMedia, _ := msg.meta["is_media"].(bool); isMedia {
			if strings.TrimSpace(mediaKind) == "" || strings.EqualFold(strings.TrimSpace(toString(msg.meta["media_kind"])), mediaKind) {
				return msg.meta
			}
		}
		if isImage, _ := msg.meta["is_image"].(bool); isImage {
			if strings.TrimSpace(mediaKind) == "" || strings.EqualFold(mediaKind, "image") {
				return map[string]any{
					"is_media":          true,
					"media_kind":        "image",
					"media_path":        toString(msg.meta["image_path"]),
					"media_url":         toString(msg.meta["image_url"]),
					"media_source_path": toString(msg.meta["image_source_path"]),
					"media_mime_type":   toString(msg.meta["image_mime_type"]),
					"media_data_base64": toString(msg.meta["image_data_base64"]),
				}
			}
		}
	}
	return nil
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func extensionForMediaMIME(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/mp3", "audio/mpeg":
		return ".mp3"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ".img"
	}
}

func handleMentionSlashCommand(m chatModel, args []string, input string, draft string) (chatModel, tea.Cmd) {
	if !m.experimentalEnabled(product.ExperimentalComposerMentions) {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Composer mentions are disabled. Use /experimental enable composer-mentions to turn them back on."})
		m.refreshViewport()
		return m, nil
	}
	if len(args) == 0 {
		return m.openMentionPicker("", "")
	}
	query := strings.Join(args, " ")
	attachment, err := userattachments.BuildAttachmentDraft(m.workspace, query)
	if err == nil {
		m.appendPendingAttachment(attachment)
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Attached %s to the composer.", attachment.Label)})
		m.refreshViewport()
		return m, nil
	}
	_ = input
	_ = draft
	return m.openMentionPicker(query, "")
}

func handleInitSlashCommand(m chatModel, _ []string, _ string, _ string) (chatModel, tea.Cmd) {
	out, err := product.InitWorkspaceBootstrap(m.workspace, config.AppName())
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("init failed: %v", err)})
	} else {
		if notice := m.syncCustomCommands(); notice != "" {
			out += "\n" + notice
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}
