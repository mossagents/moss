package tui

import (
	"context"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/appkit/product"
	runtimeenv "github.com/mossagents/moss/appkit/product/runtimeenv"
	configpkg "github.com/mossagents/moss/config"
	"strings"
)

type statuslinePickerState struct {
	list *selectionListState
}

func newStatuslinePickerState(current []string) *statuslinePickerState {
	items := make([]selectionListItem, 0, len(statusLineItemCatalog))
	marked := make(map[string]struct{}, len(current))
	for _, item := range normalizeStatusLineItems(current) {
		marked[item] = struct{}{}
	}
	for _, item := range statusLineItemCatalog {
		items = append(items, selectionListItem{
			Key:    item.Name,
			Title:  item.Name,
			Detail: item.Summary,
		})
	}
	state := &statuslinePickerState{
		list: &selectionListState{
			Title:        "Status line",
			Footer:       "↑↓ choose • Space toggle • Enter apply • Esc close",
			EmptyMessage: "No footer items available.",
			Message:      "Select the footer items to keep visible. Clearing all items restores the default footer.",
			Items:        items,
			MultiSelect:  true,
			Marked:       marked,
		},
	}
	for i, item := range items {
		if _, ok := marked[item.Key]; ok {
			state.list.Cursor = i
			break
		}
	}
	return state
}

func (m chatModel) openStatuslinePicker() (chatModel, tea.Cmd) {
	if !m.experimentalEnabled(product.ExperimentalStatuslineCustomization) {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: "Status-line customization is disabled. Use /experimental enable statusline-customization to turn it back on."})
		m.refreshViewport()
		return m, nil
	}
	m.statuslinePicker = newStatuslinePickerState(m.statusLineItems)
	m.openStatuslineOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleStatuslinePickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.statuslinePicker == nil || m.statuslinePicker.list == nil {
		return m.closeStatuslineOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.statuslinePicker.list.Move(-1)
	case "down":
		m.statuslinePicker.list.Move(1)
	case " ", "space":
		m.statuslinePicker.list.ToggleSelected()
	case "enter":
		return m.applyStatuslinePickerSelection(m.statuslinePicker.list.SelectedKeys())
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) applyStatuslinePickerSelection(items []string) (chatModel, tea.Cmd) {
	items = normalizeStatusLineItems(items)
	reset := len(items) == len(defaultStatusLineItems)
	if reset {
		same := true
		for i := range items {
			if items[i] != defaultStatusLineItems[i] {
				same = false
				break
			}
		}
		reset = same
	}
	m.statusLineItems = append([]string(nil), items...)
	if _, err := product.UpdateTUIConfig(func(cfg *configpkg.TUIConfig) error {
		if reset {
			cfg.StatusLine = nil
			return nil
		}
		cfg.StatusLine = append([]string(nil), items...)
		return nil
	}); err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to update status line: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	if reset {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Status line reset to the default footer items."})
	} else {
		m.messages = append(m.messages, chatMessage{kind: msgSystem, content: fmt.Sprintf("Status line updated: %s", strings.Join(items, ", "))})
	}
	return m.closeStatuslineOverlay(), nil
}

func (m chatModel) renderStatuslinePicker(width int) string {
	if m.statuslinePicker == nil || m.statuslinePicker.list == nil {
		return ""
	}
	return renderSelectionListDialog(width, m.statuslinePicker.list)
}

type mcpPickerState struct {
	servers []product.MCPServerConfigView
	details []string
	list    *selectionListState
}

func newMCPPickerState(workspace, trust string) (*mcpPickerState, error) {
	servers, err := product.ListMCPServers(workspace, trust)
	if err != nil {
		return nil, err
	}
	items := make([]selectionListItem, 0, len(servers))
	details := make([]string, 0, len(servers))
	for _, server := range servers {
		details = append(details, product.RenderMCPServerDetail([]product.MCPServerConfigView{server}))
		items = append(items, selectionListItem{
			Key:    strings.ToLower(strings.TrimSpace(server.Name) + "\x00" + strings.TrimSpace(string(server.Source))),
			Title:  fmt.Sprintf("%s [%s]", server.Name, server.Source),
			Detail: fmt.Sprintf("%s · enabled=%t · %s", valueOrDefaultString(server.Transport, "-"), server.Enabled, valueOrDefaultString(server.Status, "-")),
		})
	}
	return &mcpPickerState{
		servers: servers,
		details: details,
		list: &selectionListState{
			Title:        "MCP servers",
			Footer:       "↑↓ choose • Enter send detail • Esc close",
			EmptyMessage: "No MCP servers configured.",
			Message:      "Project and global MCP servers are merged here using the current trust posture.",
			Items:        items,
		},
	}, nil
}

func (m chatModel) openMCPPicker() (chatModel, tea.Cmd) {
	state, err := newMCPPickerState(m.workspace, m.trust)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to list MCP servers: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.mcpPicker = state
	m.openMCPOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleMCPPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.mcpPicker == nil || m.mcpPicker.list == nil || len(m.mcpPicker.servers) == 0 {
		return m.closeMCPOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.mcpPicker.list.Move(-1)
	case "down":
		m.mcpPicker.list.Move(1)
	case "enter":
		idx := m.mcpPicker.list.SelectedIndex()
		if idx >= 0 {
			server := m.mcpPicker.servers[idx]
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: product.RenderMCPServerDetail([]product.MCPServerConfigView{server})})
			return m.closeMCPOverlay(), nil
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderMCPPicker(width int) string {
	if m.mcpPicker == nil || m.mcpPicker.list == nil {
		return ""
	}
	if idx := m.mcpPicker.list.SelectedIndex(); idx >= 0 && idx < len(m.mcpPicker.details) {
		m.mcpPicker.list.Message = m.mcpPicker.details[idx]
	}
	return renderSelectionListDialog(width, m.mcpPicker.list)
}

type helpPickerEntry struct {
	command string
	detail  string
}

type helpPickerState struct {
	entries []helpPickerEntry
	list    *selectionListState
}

func newHelpPickerState(customCommands []product.CustomCommand) *helpPickerState {
	entries := make([]helpPickerEntry, 0, len(slashCommandCatalog)+len(customCommands))
	items := make([]selectionListItem, 0, len(slashCommandCatalog)+len(customCommands))
	for _, cmd := range slashCommandCatalog {
		if cmd.HiddenInNav {
			continue
		}
		detail := cmd.Summary
		if strings.TrimSpace(cmd.Section) != "" {
			detail += "\n\nSection: " + cmd.Section
		}
		entries = append(entries, helpPickerEntry{command: cmd.Name, detail: detail})
		items = append(items, selectionListItem{
			Key:    cmd.Name,
			Title:  cmd.Name,
			Detail: cmd.Summary,
		})
	}
	for _, cmd := range customCommands {
		name := "/" + strings.TrimSpace(cmd.Name)
		detail := strings.TrimSpace(cmd.Summary)
		if detail == "" {
			detail = "Custom command"
		}
		detail += "\n\nSection: Custom commands"
		entries = append(entries, helpPickerEntry{command: name, detail: detail})
		items = append(items, selectionListItem{
			Key:    name,
			Title:  name,
			Detail: strings.TrimSpace(cmd.Summary),
		})
	}
	return &helpPickerState{
		entries: entries,
		list: &selectionListState{
			Title:        "Commands & Shortcuts",
			Footer:       "↑↓ choose • Enter insert/confirm • Esc close",
			EmptyMessage: "No commands available.",
			Message:      "Press Enter to place the selected command into the composer.",
			Items:        items,
		},
	}
}

func (m chatModel) openHelpPicker() (chatModel, tea.Cmd) {
	m.helpPicker = newHelpPickerState(m.customCommands)
	m.openHelpOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleHelpPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.helpPicker == nil || m.helpPicker.list == nil || len(m.helpPicker.entries) == 0 {
		return m.closeHelpOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.helpPicker.list.Move(-1)
	case "down":
		m.helpPicker.list.Move(1)
	case "enter":
		idx := m.helpPicker.list.SelectedIndex()
		if idx >= 0 {
			cmd := m.helpPicker.entries[idx].command
			if strings.HasPrefix(cmd, "/") {
				m.textarea.SetValue(cmd + " ")
				m.refreshSlashHints()
			}
			return m.closeHelpOverlay(), nil
		}
	}
	if selected := m.helpPicker.list.SelectedIndex(); selected >= 0 && selected < len(m.helpPicker.entries) {
		m.helpPicker.list.Items[selected].Detail = strings.SplitN(m.helpPicker.entries[selected].detail, "\n", 2)[0]
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderHelpPicker(width int) string {
	if m.helpPicker == nil || m.helpPicker.list == nil {
		return ""
	}
	if idx := m.helpPicker.list.SelectedIndex(); idx >= 0 && idx < len(m.helpPicker.entries) {
		entry := m.helpPicker.entries[idx]
		if strings.HasPrefix(entry.command, "/") {
			m.helpPicker.list.Message = entry.detail + "\n\nPress Enter to insert the command into the composer."
		} else {
			m.helpPicker.list.Message = entry.detail
		}
	}
	return renderSelectionListDialog(width, m.helpPicker.list)
}

type reviewPickerOption struct {
	mode   string
	title  string
	detail string
}

type reviewPickerState struct {
	options []reviewPickerOption
	list    *selectionListState
}

func newReviewPickerState(workspace string) (*reviewPickerState, error) {
	raw := []reviewPickerOption{
		{mode: "status", title: "Working tree"},
		{mode: "snapshots", title: "Snapshots"},
		{mode: "changes", title: "Changes"},
	}
	items := make([]selectionListItem, 0, len(raw))
	options := make([]reviewPickerOption, 0, len(raw))
	for _, option := range raw {
		report, err := runtimeenv.BuildReviewReport(context.Background(), workspace, []string{option.mode})
		if err != nil {
			return nil, err
		}
		option.detail = runtimeenv.RenderReviewReport(report)
		options = append(options, option)
		items = append(items, selectionListItem{
			Key:    option.mode,
			Title:  option.title,
			Detail: option.mode,
		})
	}
	return &reviewPickerState{
		options: options,
		list: &selectionListState{
			Title:        "Review workspace",
			Footer:       "↑↓ choose • Enter send report • Esc close",
			EmptyMessage: "No review views available.",
			Message:      "Browse the current workspace state without flooding the transcript. Enter sends the selected report to the transcript when you need a persistent record.",
			Items:        items,
		},
	}, nil
}

func (m chatModel) openReviewPicker() (chatModel, tea.Cmd) {
	state, err := newReviewPickerState(m.workspace)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("review failed: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.reviewPicker = state
	m.openReviewOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleReviewPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.reviewPicker == nil || m.reviewPicker.list == nil || len(m.reviewPicker.options) == 0 {
		return m.closeReviewOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.reviewPicker.list.Move(-1)
	case "down":
		m.reviewPicker.list.Move(1)
	case "enter":
		idx := m.reviewPicker.list.SelectedIndex()
		if idx >= 0 {
			m.messages = append(m.messages, chatMessage{kind: msgSystem, content: m.reviewPicker.options[idx].detail})
			return m.closeReviewOverlay(), nil
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) renderReviewPicker(width int) string {
	if m.reviewPicker == nil || m.reviewPicker.list == nil {
		return ""
	}
	if idx := m.reviewPicker.list.SelectedIndex(); idx >= 0 && idx < len(m.reviewPicker.options) {
		m.reviewPicker.list.Message = m.reviewPicker.options[idx].detail
	}
	return renderSelectionListDialog(width, m.reviewPicker.list)
}
