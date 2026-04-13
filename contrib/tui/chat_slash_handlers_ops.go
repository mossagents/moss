package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	config "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/checkpoint"
	"github.com/mossagents/moss/runtime"
	"strconv"
	"strings"
)

func handleCheckpointSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /checkpoint list [limit]\n  /checkpoint show <checkpoint_id|latest>\n  /checkpoint create [note]\n  /checkpoint replay [<checkpoint_id|latest>] [resume|rerun] [restore]"})
		m.refreshViewport()
		return m, nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "list":
		if m.checkpointListFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint list is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		limit := 20
		if len(args) >= 2 {
			v, err := strconv.Atoi(args[1])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint list [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.checkpointListFn(limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list checkpoints: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	case "show":
		if m.checkpointShowFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint detail is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint show <checkpoint_id|latest>"})
			m.refreshViewport()
			return m, nil
		}
		out, err := m.checkpointShowFn(strings.TrimSpace(args[1]))
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to show checkpoint: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	case "create":
		if m.checkpointCreateFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint creation is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		note := strings.TrimSpace(strings.Join(args[1:], " "))
		out, err := m.checkpointCreateFn(note)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to create checkpoint: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	case "replay":
		if m.checkpointReplayFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint replay is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		checkpointID := ""
		mode := ""
		restore := false
		rest := args[1:]
		if len(rest) > 0 {
			first := strings.ToLower(strings.TrimSpace(rest[0]))
			switch first {
			case "", "resume", "rerun", "restore", "--restore-worktree":
			default:
				checkpointID = strings.TrimSpace(rest[0])
				rest = rest[1:]
			}
		}
		for _, item := range rest {
			token := strings.ToLower(strings.TrimSpace(item))
			switch token {
			case "resume", "rerun":
				mode = token
			case "restore", "--restore-worktree":
				restore = true
			default:
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint replay [<checkpoint_id|latest>] [resume|rerun] [restore]"})
				m.refreshViewport()
				return m, nil
			}
		}
		label := "latest"
		if strings.TrimSpace(checkpointID) != "" {
			label = checkpointID
		}
		return m.startThreadSwitch(fmt.Sprintf("Replaying checkpoint %s...", label), func() (string, error) {
			out, err := m.checkpointReplayFn(checkpointID, mode, restore)
			if err != nil {
				return "", fmt.Errorf("failed to replay checkpoint: %v", err)
			}
			return out, nil
		})
	case "fork":
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Checkpoint branching moved to /fork. Use /fork [session <id>|checkpoint <id|latest>|latest] [restore]."})
		m.refreshViewport()
		return m, nil
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /checkpoint list|show|create|replay ..."})
		m.refreshViewport()
		return m, nil
	}
}

func handleForkSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.checkpointForkFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Fork is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	if len(args) == 0 {
		return m.openForkPicker()
	}
	sourceKind := string(checkpoint.ForkSourceSession)
	sourceID := ""
	restore := false
	rest := args
	if len(rest) > 0 {
		switch strings.ToLower(strings.TrimSpace(rest[0])) {
		case "session", "checkpoint":
			sourceKind = strings.ToLower(strings.TrimSpace(rest[0]))
			if sourceKind == string(checkpoint.ForkSourceSession) && (len(rest) < 2 || strings.TrimSpace(rest[1]) == "") {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /fork [session <id>|checkpoint <id|latest>|latest] [restore]"})
				m.refreshViewport()
				return m, nil
			}
			if len(rest) >= 2 && strings.TrimSpace(rest[1]) != "" {
				sourceID = strings.TrimSpace(rest[1])
				rest = rest[2:]
			} else {
				rest = rest[1:]
			}
		case "latest":
			sourceKind = string(checkpoint.ForkSourceCheckpoint)
			rest = rest[1:]
		}
	}
	for _, item := range rest {
		token := strings.ToLower(strings.TrimSpace(item))
		if token == "restore" || token == "--restore-worktree" {
			restore = true
			continue
		}
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /fork [session <id>|checkpoint <id|latest>|latest] [restore]"})
		m.refreshViewport()
		return m, nil
	}
	label := sourceKind
	if strings.TrimSpace(sourceID) != "" {
		label += " " + sourceID
	}
	return m.startThreadSwitch(fmt.Sprintf("Forking from %s...", strings.TrimSpace(label)), func() (string, error) {
		out, err := m.checkpointForkFn(sourceKind, sourceID, restore)
		if err != nil {
			return "", fmt.Errorf("failed to fork thread: %v", err)
		}
		return out, nil
	})
}

func handleAgentSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		return m.openAgentPicker()
	}
	if m.taskListFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread controls are unavailable."})
		m.refreshViewport()
		return m, nil
	}
	if args[0] == "list" {
		if len(args) == 1 {
			return m.openAgentPicker()
		}
		listArgs := args
		if len(listArgs) > 0 && listArgs[0] == "list" {
			listArgs = listArgs[1:]
		}
		status := ""
		limit := 20
		if len(listArgs) >= 1 {
			status = strings.TrimSpace(strings.ToLower(listArgs[0]))
			switch status {
			case "", "running", "completed", "failed", "cancelled":
			default:
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /agent [list] [running|completed|failed|cancelled] [limit]"})
				m.refreshViewport()
				return m, nil
			}
		}
		if len(listArgs) >= 2 {
			v, err := strconv.Atoi(listArgs[1])
			if err != nil || v <= 0 {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /agent [list] [status] [limit:int]"})
				m.refreshViewport()
				return m, nil
			}
			limit = v
		}
		out, err := m.taskListFn(status, limit)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list agent threads: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	}
	if args[0] == "current" {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.renderStatusSummary()})
		m.refreshViewport()
		return m, nil
	}
	if args[0] == "cancel" {
		if m.taskCancelFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread cancellation is unavailable."})
			m.refreshViewport()
			return m, nil
		}
		if len(args) < 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /agent cancel <agent_id> [reason...]"})
			m.refreshViewport()
			return m, nil
		}
		agentID := strings.TrimSpace(args[1])
		reason := "cancelled by user from TUI"
		if len(args) >= 3 {
			reason = strings.Join(args[2:], " ")
		}
		out, err := m.taskCancelFn(agentID, reason)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to cancel agent thread: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	}
	if m.taskQueryFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Agent thread query is unavailable."})
		m.refreshViewport()
		return m, nil
	}
	agentID := strings.TrimSpace(args[0])
	out, err := m.taskQueryFn(agentID)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to query agent thread: %v", err)})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
	}
	m.refreshViewport()
	return m, nil
}

func handleGitSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if m.gitRunFn == nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Git workflow commands are unavailable."})
		m.refreshViewport()
		return m, nil
	}
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Usage:\n  /git status\n  /git diff [path]\n  /git commit <message>\n  /git pr [args...]"})
		m.refreshViewport()
		return m, nil
	}
	sub := strings.ToLower(args[0])
	switch sub {
	case "status":
		out, err := m.gitRunFn("git", []string{"--no-pager", "status", "--short"})
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git status failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
	case "diff":
		cmdArgs := []string{"--no-pager", "diff"}
		if len(args) > 1 {
			cmdArgs = append(cmdArgs, args[1:]...)
		}
		out, err := m.gitRunFn("git", cmdArgs)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git diff failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
	case "commit":
		if len(args) < 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /git commit <message>"})
		} else {
			msg := strings.Join(args[1:], " ")
			out, err := m.gitRunFn("git", []string{"commit", "-m", msg})
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("git commit failed: %v", err)})
			} else {
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
			}
		}
	case "pr":
		prArgs := []string{"pr"}
		if len(args) > 1 {
			prArgs = append(prArgs, args[1:]...)
		} else {
			prArgs = append(prArgs, "status")
		}
		out, err := m.gitRunFn("gh", prArgs)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("gh pr failed: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /git status | /git diff [path] | /git commit <message> | /git pr [args...]"})
	}
	m.refreshViewport()
	return m, nil
}

func handlePermissionsSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		info := "Permission policy information is unavailable."
		if m.permissionSummaryFn != nil {
			info = m.permissionSummaryFn()
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: info})
		m.refreshViewport()
		return m, nil
	}
	if strings.ToLower(args[0]) == "trust" {
		if len(args) < 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /permissions trust <trusted|restricted>"})
			m.refreshViewport()
			return m, nil
		}
		nextTrust := strings.ToLower(strings.TrimSpace(args[1]))
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
	if strings.ToLower(args[0]) == "set" {
		if len(args) < 3 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /permissions set <tool_name> <allow|ask|deny|reset>"})
			m.refreshViewport()
			return m, nil
		}
		if m.setPermissionFn == nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Runtime permissions are unavailable."})
			m.refreshViewport()
			return m, nil
		}
		toolName := strings.TrimSpace(args[1])
		mode := strings.ToLower(strings.TrimSpace(args[2]))
		out, err := m.setPermissionFn(toolName, mode)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to set permission: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: out})
		}
		m.refreshViewport()
		return m, nil
	}
	m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /permissions\n  /permissions trust <trusted|restricted>\n  /permissions set <tool_name> <allow|ask|deny|reset>"})
	m.refreshViewport()
	return m, nil
}

func handleProfileSlashCommand(m chatModel, args []string, input string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		current := valueOrDefaultString(m.profile, "default")
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Current profile: %s\nUsage: /profile list\n       /profile set <name>", current)})
		m.refreshViewport()
		return m, nil
	}
	if strings.EqualFold(args[0], "list") {
		names, err := runtime.ProfileNamesForWorkspace(m.workspace, m.trust)
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list profiles: %v", err)})
		} else {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Available profiles:\n- %s", strings.Join(names, "\n- "))})
		}
		m.refreshViewport()
		return m, nil
	}
	profileName := strings.TrimSpace(args[0])
	if strings.EqualFold(args[0], "set") {
		if len(args) < 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /profile set <name>"})
			m.refreshViewport()
			return m, nil
		}
		profileName = strings.TrimSpace(args[1])
	}
	return m.queueProfileSwitch(profileName, input)
}

func handleStatuslineSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if !m.experimentalEnabled(product.ExperimentalStatuslineCustomization) {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Status-line customization is disabled. Use /experimental enable statusline-customization to turn it back on."})
		m.refreshViewport()
		return m, nil
	}
	if len(args) == 0 {
		return m.openStatuslinePicker()
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "reset":
		m.statusLineItems = append([]string(nil), defaultStatusLineItems...)
		if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
			cfg.StatusLine = nil
			return nil
		}); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to reset status line: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Status line reset to the default footer items."})
		m.refreshViewport()
		return m, nil
	case "set":
		if len(args) < 2 {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: renderStatusLineUsage(m.statusLineItems)})
			m.refreshViewport()
			return m, nil
		}
		items, err := parseStatusLineItems(strings.Join(args[1:], " "))
		if err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: err.Error()})
			m.refreshViewport()
			return m, nil
		}
		m.statusLineItems = items
		if _, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
			cfg.StatusLine = append([]string(nil), items...)
			return nil
		}); err != nil {
			m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update status line: %v", err)})
			m.refreshViewport()
			return m, nil
		}
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Status line updated: %s", strings.Join(items, ", "))})
		m.refreshViewport()
		return m, nil
	default:
		m.messages = append(m.messages, chatMessage{kind: msgError, content: renderStatusLineUsage(m.statusLineItems)})
		m.refreshViewport()
		return m, nil
	}
}

func handleExperimentalSlashCommand(m chatModel, args []string, _ string, _ string) (chatModel, tea.Cmd) {
	if len(args) == 0 {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderExperimentalFeatures(config.TUIConfig{Experimental: m.experimentalFeatures})})
		m.refreshViewport()
		return m, nil
	}
	if len(args) != 2 {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /experimental [enable|disable] <feature>"})
		m.refreshViewport()
		return m, nil
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	name := product.NormalizeExperimentalFeature(args[1])
	if name == "" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("unknown experimental feature %q", args[1])})
		m.refreshViewport()
		return m, nil
	}
	enabled := action == "enable"
	if action != "enable" && action != "disable" {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Usage: /experimental [enable|disable] <feature>"})
		m.refreshViewport()
		return m, nil
	}
	cfg, err := product.UpdateTUIConfig(func(cfg *config.TUIConfig) error {
		cfg.Experimental = setExperimentalFeature(cfg.Experimental, name, enabled)
		return nil
	})
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update experimental features: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.experimentalFeatures = effectiveExperimentalFeatures(cfg.Experimental)
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Experimental feature %s %s.", name, onOff(enabled))})
	m.refreshViewport()
	return m, nil
}
