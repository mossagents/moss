package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/kernel/port"
)

type askFieldState struct {
	def         port.InputField
	text        string
	boolValue   bool
	singleIndex int
	singleSel   int
	multiCursor int
	multiSel    map[int]bool
}

type askFormState struct {
	prompt       string
	fields       []askFieldState
	focusIndex   int // [0..len(fields)]，最后一个是确认按钮
	confirmLabel string
}

func newAskFormState(req port.InputRequest) *askFormState {
	fields := req.Fields
	if len(fields) == 0 {
		fields = synthesizeFieldsFromInputRequest(req)
	}
	out := &askFormState{
		prompt:       req.Prompt,
		fields:       make([]askFieldState, 0, len(fields)),
		confirmLabel: strings.TrimSpace(req.ConfirmLabel),
	}
	if out.confirmLabel == "" {
		out.confirmLabel = "Confirm"
	}
	if req.Type == port.InputConfirm && req.Approval != nil {
		out.confirmLabel = "Apply decision"
	}
	for _, f := range fields {
		st := askFieldState{def: f, multiSel: map[int]bool{}}
		switch f.Type {
		case port.InputFieldBoolean:
			if b, ok := f.Default.(bool); ok {
				st.boolValue = b
			}
		case port.InputFieldSingleSelect:
			st.singleSel = 0
			st.singleIndex = 0
			if s, ok := f.Default.(string); ok {
				for i, opt := range f.Options {
					if opt == s {
						st.singleSel, st.singleIndex = i, i
						break
					}
				}
			}
		case port.InputFieldMultiSelect:
			if arr, ok := f.Default.([]string); ok {
				for _, v := range arr {
					for i, opt := range f.Options {
						if opt == v {
							st.multiSel[i] = true
						}
					}
				}
			}
		default:
			if s, ok := f.Default.(string); ok {
				st.text = s
			}
		}
		out.fields = append(out.fields, st)
	}
	return out
}

func synthesizeFieldsFromInputRequest(req port.InputRequest) []port.InputField {
	switch req.Type {
	case port.InputConfirm:
		if req.Approval != nil {
			return []port.InputField{{
				Name:        "decision",
				Type:        port.InputFieldSingleSelect,
				Title:       "Decision",
				Description: "Choose whether to allow this action once, remember similar actions for this thread, or deny it.",
				Required:    true,
				Options:     []string{"Allow once", "Allow for this thread", "Deny"},
				Default:     "Allow once",
			}}
		}
		return []port.InputField{{Name: "approved", Type: port.InputFieldBoolean, Title: req.Prompt, Required: true}}
	case port.InputSelect:
		return []port.InputField{{Name: "selected", Type: port.InputFieldSingleSelect, Title: req.Prompt, Required: true, Options: req.Options}}
	default:
		return []port.InputField{{Name: "value", Type: port.InputFieldString, Title: req.Prompt, Required: true}}
	}
}

func (m *chatModel) activateAskField() {
	if m.askForm == nil {
		return
	}
	if m.askForm.focusIndex >= len(m.askForm.fields) {
		m.textarea.Blur()
		return
	}
	f := m.askForm.fields[m.askForm.focusIndex]
	switch f.def.Type {
	case port.InputFieldString, port.InputFieldNumber, port.InputFieldInteger:
		m.textarea.SetValue(f.text)
		m.textarea.Focus()
	default:
		m.textarea.Blur()
	}
}

func (m chatModel) handleAskKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.askForm == nil || m.pendAsk == nil {
		return m, nil
	}
	form := m.askForm
	if msg.String() == "tab" {
		form.focusIndex = (form.focusIndex + 1) % (len(form.fields) + 1)
		m.activateAskField()
		m.refreshViewport()
		return m, nil
	}
	if msg.String() == "shift+tab" {
		form.focusIndex = (form.focusIndex - 1 + len(form.fields) + 1) % (len(form.fields) + 1)
		m.activateAskField()
		m.refreshViewport()
		return m, nil
	}
	if form.focusIndex >= len(form.fields) {
		if msg.String() == "enter" {
			return m.submitAskForm()
		}
		return m, nil
	}
	field := &form.fields[form.focusIndex]
	switch field.def.Type {
	case port.InputFieldBoolean:
		if msg.String() == "enter" || msg.String() == " " {
			field.boolValue = !field.boolValue
			m.refreshViewport()
		}
	case port.InputFieldSingleSelect:
		switch msg.String() {
		case "up":
			if field.singleIndex > 0 {
				field.singleIndex--
			}
		case "down":
			if field.singleIndex < len(field.def.Options)-1 {
				field.singleIndex++
			}
		case "enter":
			field.singleSel = field.singleIndex
			form.focusIndex = (form.focusIndex + 1) % (len(form.fields) + 1)
			m.activateAskField()
		}
		m.refreshViewport()
	case port.InputFieldMultiSelect:
		switch msg.String() {
		case "up":
			if field.multiCursor > 0 {
				field.multiCursor--
			}
		case "down":
			if field.multiCursor < len(field.def.Options)-1 {
				field.multiCursor++
			}
		case " ":
			if field.multiSel[field.multiCursor] {
				delete(field.multiSel, field.multiCursor)
			} else {
				field.multiSel[field.multiCursor] = true
			}
		case "enter":
			form.focusIndex = (form.focusIndex + 1) % (len(form.fields) + 1)
			m.activateAskField()
		}
		m.refreshViewport()
	default:
		if msg.String() == "enter" {
			field.text = strings.TrimSpace(m.textarea.Value())
			form.focusIndex = (form.focusIndex + 1) % (len(form.fields) + 1)
			m.activateAskField()
			m.refreshViewport()
			return m, nil
		}
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		field.text = m.textarea.Value()
		m.refreshViewport()
		return m, cmd
	}
	return m, nil
}

func (m chatModel) submitAskForm() (chatModel, tea.Cmd) {
	if m.askForm == nil || m.pendAsk == nil {
		return m, nil
	}
	formValues := map[string]any{}
	for _, f := range m.askForm.fields {
		switch f.def.Type {
		case port.InputFieldBoolean:
			formValues[f.def.Name] = f.boolValue
		case port.InputFieldSingleSelect:
			if len(f.def.Options) == 0 {
				formValues[f.def.Name] = ""
			} else {
				idx := f.singleSel
				if idx < 0 || idx >= len(f.def.Options) {
					idx = 0
				}
				formValues[f.def.Name] = f.def.Options[idx]
			}
		case port.InputFieldMultiSelect:
			values := make([]string, 0, len(f.multiSel))
			for i, opt := range f.def.Options {
				if f.multiSel[i] {
					values = append(values, opt)
				}
			}
			formValues[f.def.Name] = values
		case port.InputFieldNumber:
			s := strings.TrimSpace(f.text)
			if s == "" {
				formValues[f.def.Name] = float64(0)
				break
			}
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("Field %s must be a number", f.def.Title)})
				m.refreshViewport()
				return m, nil
			}
			formValues[f.def.Name] = n
		case port.InputFieldInteger:
			s := strings.TrimSpace(f.text)
			if s == "" {
				formValues[f.def.Name] = 0
				break
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("Field %s must be an integer", f.def.Title)})
				m.refreshViewport()
				return m, nil
			}
			formValues[f.def.Name] = n
		default:
			formValues[f.def.Name] = strings.TrimSpace(f.text)
		}
		if f.def.Required {
			switch v := formValues[f.def.Name].(type) {
			case string:
				if strings.TrimSpace(v) == "" {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("Field %s is required", f.def.Title)})
					m.refreshViewport()
					return m, nil
				}
			case []string:
				if len(v) == 0 {
					m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("Field %s is required", f.def.Title)})
					m.refreshViewport()
					return m, nil
				}
			}
		}
	}

	ask := m.pendAsk
	m.pendAsk = nil
	m.askForm = nil
	m.closeAskOverlay()
	m.textarea.Reset()
	m.textarea.Focus()

	if ask.request.Type == port.InputForm {
		ask.replyCh <- port.InputResponse{Form: formValues}
	} else {
		switch ask.request.Type {
		case port.InputConfirm:
			if ask.request.Approval != nil {
				selected, _ := formValues["decision"].(string)
				approved := selected == "Allow once" || selected == "Allow for this thread"
				resp := port.InputResponse{
					Approved: approved,
					Decision: &port.ApprovalDecision{
						RequestID: ask.request.Approval.ID,
						Approved:  approved,
						Source:    "tui-approval",
						DecidedAt: m.now().UTC(),
					},
				}
				notice := "Approval denied."
				if approved {
					notice = "Approval granted for this action."
				}
				if selected == "Allow for this thread" {
					rule, ok := approvalMemoryRuleFor(ask.request.Approval, m.currentSessionID, m.now())
					if ok {
						m.rememberApprovalRule(rule)
						resp.Decision.Source = "tui-thread-rule"
						resp.Decision.Reason = "remember similar actions for this thread"
						notice = "Approval granted. Similar actions will be allowed automatically for this thread."
					}
				} else if selected == "Allow once" {
					resp.Decision.Source = "tui-allow-once"
				} else {
					resp.Decision.Source = "tui-deny"
				}
				ask.replyCh <- resp
				m.messages = append(m.messages, chatMessage{kind: msgSystem, content: notice})
				m.refreshViewport()
				return m, nil
			}
			approved, _ := formValues["approved"].(bool)
			ask.replyCh <- port.InputResponse{Approved: approved}
		case port.InputSelect:
			selectedValue, _ := formValues["selected"].(string)
			idx := 0
			for i, opt := range ask.request.Options {
				if opt == selectedValue {
					idx = i
					break
				}
			}
			ask.replyCh <- port.InputResponse{Selected: idx}
		default:
			val, _ := formValues["value"].(string)
			ask.replyCh <- port.InputResponse{Value: val}
		}
	}
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: "Form submitted."})
	m.refreshViewport()
	return m, nil
}

func (m *chatModel) rememberApprovalRule(rule approvalMemoryRule) {
	if strings.TrimSpace(rule.SessionID) == "" || strings.TrimSpace(rule.Key) == "" {
		return
	}
	if m.approvalRules == nil {
		m.approvalRules = map[string][]approvalMemoryRule{}
	}
	rules := m.approvalRules[rule.SessionID]
	for _, existing := range rules {
		if existing.Key == rule.Key {
			return
		}
	}
	m.approvalRules[rule.SessionID] = append(rules, rule)
}

func (m chatModel) renderAskForm(width int) string {
	if m.askForm == nil {
		return ""
	}
	if width < 30 {
		width = 30
	}
	var sb strings.Builder
	if m.pendAsk != nil && m.pendAsk.request.Type == port.InputConfirm && m.pendAsk.request.Approval != nil {
		return m.renderApprovalAskForm(width)
	}
	sb.WriteString(wrapText(m.askForm.prompt, width-4))
	sb.WriteString("\n\n")

	for i, f := range m.askForm.fields {
		label := f.def.Title
		if strings.TrimSpace(label) == "" {
			label = f.def.Name
		}
		prefix := "  "
		if m.askForm.focusIndex == i {
			prefix = "▸ "
		}
		required := ""
		if f.def.Required {
			required = " *"
		}
		line := prefix + label + required
		if m.askForm.focusIndex == i {
			sb.WriteString(dialogAccentStyle.Render(line))
		} else {
			sb.WriteString(dialogItemStyle.Render(line))
		}
		sb.WriteString("\n")
		if strings.TrimSpace(f.def.Description) != "" {
			sb.WriteString(mutedStyle.Render("    " + f.def.Description))
			sb.WriteString("\n")
		}
		switch f.def.Type {
		case port.InputFieldBoolean:
			val := "false"
			if f.boolValue {
				val = "true"
			}
			sb.WriteString("    [toggle] " + val + "\n")
		case port.InputFieldSingleSelect:
			for idx, opt := range f.def.Options {
				cursor := " "
				if idx == f.singleIndex && m.askForm.focusIndex == i {
					cursor = "❯"
				}
				chosen := " "
				if idx == f.singleSel {
					chosen = "●"
				}
				line := fmt.Sprintf("    %s [%s] %s", cursor, chosen, opt)
				if idx == f.singleIndex && m.askForm.focusIndex == i {
					sb.WriteString(dialogSelectedItemStyle.Render(line) + "\n")
				} else {
					sb.WriteString(dialogItemStyle.Render(line) + "\n")
				}
			}
		case port.InputFieldMultiSelect:
			for idx, opt := range f.def.Options {
				cursor := " "
				if idx == f.multiCursor && m.askForm.focusIndex == i {
					cursor = "❯"
				}
				chosen := " "
				if f.multiSel[idx] {
					chosen = "x"
				}
				line := fmt.Sprintf("    %s [%s] %s", cursor, chosen, opt)
				if idx == f.multiCursor && m.askForm.focusIndex == i {
					sb.WriteString(dialogSelectedItemStyle.Render(line) + "\n")
				} else {
					sb.WriteString(dialogItemStyle.Render(line) + "\n")
				}
			}
		default:
			val := strings.TrimSpace(f.text)
			if val == "" {
				val = "(empty)"
			}
			sb.WriteString(dialogItemStyle.Render("    " + val))
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	confirmPrefix := "  "
	if m.askForm.focusIndex == len(m.askForm.fields) {
		confirmPrefix = "▸ "
	}
	confirm := confirmPrefix + "[ " + m.askForm.confirmLabel + " ]"
	if m.askForm.focusIndex == len(m.askForm.fields) {
		sb.WriteString(dialogSelectedItemStyle.Render(confirm))
	} else {
		sb.WriteString(dialogItemStyle.Render(confirm))
	}
	return renderDialogFrame(width, "Ask user", []string{strings.TrimSpace(sb.String())}, "Tab/Shift+Tab move • Enter confirm • Esc cancel")
}

func (m chatModel) renderApprovalAskForm(width int) string {
	if m.askForm == nil || m.pendAsk == nil || m.pendAsk.request.Approval == nil {
		return ""
	}
	display := buildApprovalDisplay(m.pendAsk.request.Approval, m.currentSessionID)
	var sb strings.Builder
	sectionLabel := func(title, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		sb.WriteString(lipgloss.NewStyle().Bold(true).Render(title))
		sb.WriteString("\n")
		sb.WriteString("  ")
		sb.WriteString(wrapText(value, width-6))
		sb.WriteString("\n\n")
	}

	sb.WriteString(dialogAccentStyle.Render(display.Title))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("Review the action below before continuing."))
	sb.WriteString("\n\n")
	sectionLabel("Tool", valueOrDefaultString(display.ToolName, "(unknown)"))
	sectionLabel("Risk", valueOrDefaultString(display.Risk, "(unspecified)"))
	sectionLabel("Reason", display.Reason)
	sectionLabel(display.ActionLabel, display.ActionValue)
	sectionLabel(display.ScopeLabel, display.ScopeValue)
	if strings.TrimSpace(display.DecisionNote) != "" {
		sb.WriteString(mutedStyle.Render(wrapText(display.DecisionNote, width)))
		sb.WriteString("\n\n")
	}
	for i, f := range m.askForm.fields {
		prefix := "  "
		if m.askForm.focusIndex == i {
			prefix = "▸ "
		}
		if m.askForm.focusIndex == i {
			sb.WriteString(dialogAccentStyle.Render(prefix + f.def.Title))
		} else {
			sb.WriteString(dialogItemStyle.Render(prefix + f.def.Title))
		}
		sb.WriteString("\n")
		if strings.TrimSpace(f.def.Description) != "" {
			sb.WriteString(mutedStyle.Render("    " + f.def.Description))
			sb.WriteString("\n")
		}
		for idx, opt := range f.def.Options {
			cursor := " "
			if idx == f.singleIndex && m.askForm.focusIndex == i {
				cursor = "❯"
			}
			chosen := " "
			if idx == f.singleSel {
				chosen = "●"
			}
			line := "    " + cursor + " [" + chosen + "] " + opt
			if idx == f.singleIndex && m.askForm.focusIndex == i {
				sb.WriteString(dialogSelectedItemStyle.Render(line) + "\n")
			} else {
				sb.WriteString(dialogItemStyle.Render(line) + "\n")
			}
		}
		sb.WriteString("\n")
	}
	confirmPrefix := "  "
	if m.askForm.focusIndex == len(m.askForm.fields) {
		confirmPrefix = "▸ "
	}
	confirm := confirmPrefix + "[ " + m.askForm.confirmLabel + " ]"
	if m.askForm.focusIndex == len(m.askForm.fields) {
		sb.WriteString(dialogSelectedItemStyle.Render(confirm))
	} else {
		sb.WriteString(dialogItemStyle.Render(confirm))
	}
	return renderDialogFrame(width, "Approval", []string{strings.TrimSpace(sb.String())}, "↑↓ choose • Enter apply • Esc cancel")
}
