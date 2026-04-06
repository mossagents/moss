package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/appkit/product"
	configpkg "github.com/mossagents/moss/config"
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
	errorText    string
}

func newAskFormState(req port.InputRequest, workspace string) *askFormState {
	fields := req.Fields
	if len(fields) == 0 {
		fields = synthesizeFieldsFromInputRequest(req, workspace)
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

func synthesizeFieldsFromInputRequest(req port.InputRequest, workspace string) []port.InputField {
	switch req.Type {
	case port.InputConfirm:
		if req.Approval != nil {
			explicitScopes := len(req.Approval.AllowedScopes) > 0
			normalized := port.NormalizeApprovalRequest(req.Approval)
			display := buildApprovalDisplay(normalized, "")
			options := []string{}
			if !explicitScopes || approvalScopeAllowed(normalized, port.DecisionScopeOnce) {
				options = append(options, approvalChoiceAllowOnce)
			}
			if strings.TrimSpace(display.RuleKey) != "" && (!explicitScopes || approvalScopeAllowed(normalized, port.DecisionScopeSession)) {
				options = append(options, approvalChoiceAllowSession)
			}
			if strings.TrimSpace(workspace) != "" && approvalProjectAmendment(normalized) != nil && (!explicitScopes || approvalScopeAllowed(normalized, port.DecisionScopeProject)) {
				options = append(options, approvalChoiceAllowProject)
			}
			if len(options) == 0 {
				options = append(options, approvalChoiceAllowOnce)
			}
			options = append(options, approvalChoiceDeny)
			defaultChoice := approvalChoiceForScope(normalized.DefaultScope)
			if indexOfApprovalOption(options, defaultChoice) < 0 {
				defaultChoice = approvalChoiceAllowOnce
			}
			return []port.InputField{{
				Name:        "decision",
				Type:        port.InputFieldSingleSelect,
				Title:       "Decision",
				Description: "Choose whether to allow this action once, remember matching actions, or deny it.",
				Required:    true,
				Options:     options,
				Default:     defaultChoice,
			}}
		}
		return []port.InputField{{Name: "approved", Type: port.InputFieldBoolean, Title: req.Prompt, Required: true}}
	case port.InputSelect:
		return []port.InputField{{Name: "selected", Type: port.InputFieldSingleSelect, Title: req.Prompt, Required: true, Options: req.Options}}
	default:
		return []port.InputField{{Name: "value", Type: port.InputFieldString, Title: req.Prompt, Required: true}}
	}
}

func approvalScopeAllowed(req *port.ApprovalRequest, scope port.DecisionScope) bool {
	if req == nil {
		return scope == port.DecisionScopeOnce
	}
	for _, allowed := range req.AllowedScopes {
		if allowed == scope {
			return true
		}
	}
	return false
}

func approvalChoiceForScope(scope port.DecisionScope) string {
	switch scope {
	case port.DecisionScopeSession:
		return approvalChoiceAllowSession
	case port.DecisionScopeProject:
		return approvalChoiceAllowProject
	default:
		return approvalChoiceAllowOnce
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
	m.askForm.errorText = ""
	if m.isApprovalAskActive() {
		return m.handleApprovalAskKey(msg)
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

func (m chatModel) isApprovalAskActive() bool {
	return m.askForm != nil && m.pendAsk != nil && m.pendAsk.request.Type == port.InputConfirm && m.pendAsk.request.Approval != nil
}

func (m chatModel) handleApprovalAskKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if !m.isApprovalAskActive() || len(m.askForm.fields) == 0 {
		return m, nil
	}
	m.askForm.errorText = ""
	field := &m.askForm.fields[0]
	if len(field.def.Options) == 0 {
		return m, nil
	}
	selectIndex := func(idx int) {
		if idx < 0 {
			idx = len(field.def.Options) - 1
		}
		if idx >= len(field.def.Options) {
			idx = 0
		}
		field.singleIndex = idx
		field.singleSel = idx
		m.refreshViewport()
	}
	switch msg.String() {
	case "left", "up", "shift+tab":
		selectIndex(field.singleIndex - 1)
	case "right", "down", "tab":
		selectIndex(field.singleIndex + 1)
	case "a":
		selectIndex(indexOfApprovalOption(field.def.Options, approvalChoiceAllowOnce))
	case "s":
		selectIndex(indexOfApprovalOption(field.def.Options, approvalChoiceAllowSession))
	case "p":
		selectIndex(indexOfApprovalOption(field.def.Options, approvalChoiceAllowProject))
	case "d":
		selectIndex(indexOfApprovalOption(field.def.Options, approvalChoiceDeny))
	case "enter":
		return m.submitAskForm()
	}
	return m, nil
}

func indexOfApprovalOption(options []string, target string) int {
	for i, opt := range options {
		if opt == target {
			return i
		}
	}
	return -1
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
				m.askForm.errorText = fmt.Sprintf("Field %s must be a number", f.def.Title)
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
				m.askForm.errorText = fmt.Sprintf("Field %s must be an integer", f.def.Title)
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
					m.askForm.errorText = fmt.Sprintf("Field %s is required", f.def.Title)
					m.refreshViewport()
					return m, nil
				}
			case []string:
				if len(v) == 0 {
					m.askForm.errorText = fmt.Sprintf("Field %s is required", f.def.Title)
					m.refreshViewport()
					return m, nil
				}
			}
		}
	}

	ask := m.pendAsk
	if ask.request.Type == port.InputConfirm && ask.request.Approval != nil {
		return m.submitApprovalAskForm(ask, formValues)
	}

	m.resetAskFormState()

	if ask.request.Type == port.InputForm {
		ask.replyCh <- port.InputResponse{Form: formValues}
	} else {
		switch ask.request.Type {
		case port.InputConfirm:
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

func (m *chatModel) resetAskFormState() {
	m.pendAsk = nil
	m.askForm = nil
	m.closeAskOverlay()
	m.textarea.Reset()
	m.textarea.Focus()
}

func (m chatModel) submitApprovalAskForm(ask *bridgeAsk, formValues map[string]any) (chatModel, tea.Cmd) {
	selected, _ := formValues["decision"].(string)
	approved := selected != approvalChoiceDeny
	resp := port.InputResponse{
		Approved: approved,
		Decision: &port.ApprovalDecision{
			RequestID: ask.request.Approval.ID,
			Type:      port.ApprovalDecisionDeny,
			Approved:  approved,
			Source:    "tui-approval",
			DecidedAt: m.now().UTC(),
		},
	}
	if ask.request.Approval != nil {
		normalized := port.NormalizeApprovalRequest(ask.request.Approval)
		resp.Decision.RuleBinding = normalized.RuleBinding
		resp.Decision.CacheKey = normalized.CacheKey
	}
	notice := "Approval denied."
	if approved {
		notice = "Approval granted for this action."
	}
	switch selected {
	case approvalChoiceAllowSession:
		if rule, ok := approvalMemoryRuleFor(ask.request.Approval, m.currentSessionID, approvalRuleScopeSession, m.now()); ok {
			m.rememberApprovalRule(rule)
		}
		if perms := approvalSessionPermissions(ask.request.Approval); perms != nil {
			resp.Decision.Type = port.ApprovalDecisionGrantPermission
			resp.Decision.GrantedPermissions = perms
			resp.Decision.Source = "tui-session-rule"
			resp.Decision.Reason = "grant requested permissions for this session"
			resp.Decision.Scope = port.DecisionScopeSession
			resp.Decision.Persistence = port.DecisionPersistenceSession
			notice = "Approval granted. The requested permissions are now available for this session."
		} else {
			resp.Decision.Type = port.ApprovalDecisionApproveSession
			resp.Decision.Source = "tui-session-rule"
			resp.Decision.Reason = "remember similar actions for this session"
			resp.Decision.Scope = port.DecisionScopeSession
			resp.Decision.Persistence = port.DecisionPersistenceSession
			notice = "Approval granted. Similar actions will be allowed automatically for this session."
		}
	case approvalChoiceAllowProject:
		amendment := approvalProjectAmendment(ask.request.Approval)
		if amendment == nil {
			m.askForm.errorText = "This approval cannot be remembered for the current project."
			m.refreshViewport()
			return m, nil
		}
		rule, ok := approvalMemoryRuleFor(ask.request.Approval, m.currentSessionID, approvalRuleScopeProject, m.now())
		if !ok {
			m.askForm.errorText = "This approval cannot be remembered for the current project."
			m.refreshViewport()
			return m, nil
		}
		if err := m.rememberProjectApprovalRule(rule); err != nil {
			m.askForm.errorText = err.Error()
			m.refreshViewport()
			return m, nil
		}
		if err := product.PersistProjectApprovalAmendment(m.workspace, m.profile, amendment); err != nil {
			m.askForm.errorText = err.Error()
			m.refreshViewport()
			return m, nil
		}
		resp.Decision.Type = port.ApprovalDecisionPolicyAmendment
		resp.Decision.PolicyAmendment = amendment
		resp.Decision.Source = "tui-project-rule"
		resp.Decision.Reason = "persist matching policy amendment for this project"
		resp.Decision.Scope = port.DecisionScopeProject
		resp.Decision.Persistence = port.DecisionPersistenceProject
		notice = "Approval granted. The project execution policy has been updated."
	case approvalChoiceAllowOnce:
		resp.Decision.Type = port.ApprovalDecisionApprove
		resp.Decision.Source = "tui-allow-once"
		resp.Decision.Scope = port.DecisionScopeOnce
		resp.Decision.Persistence = port.DecisionPersistenceRequest
	default:
		resp.Decision.Type = port.ApprovalDecisionDeny
		resp.Decision.Source = "tui-deny"
		resp.Decision.Scope = port.DecisionScopeOnce
		resp.Decision.Persistence = port.DecisionPersistenceRequest
	}
	resp.Decision = port.NormalizeApprovalDecisionForRequest(ask.request.Approval, resp.Decision)
	m.resetAskFormState()
	ask.replyCh <- resp
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: notice})
	m.refreshViewport()
	return m, nil
}

func (m *chatModel) rememberApprovalRule(rule approvalMemoryRule) {
	if rule.scope() != approvalRuleScopeSession || strings.TrimSpace(rule.SessionID) == "" || strings.TrimSpace(rule.Key) == "" {
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

func (m *chatModel) rememberProjectApprovalRule(rule approvalMemoryRule) error {
	if rule.scope() != approvalRuleScopeProject || strings.TrimSpace(rule.Key) == "" {
		return errors.New("project approval rule is invalid")
	}
	if strings.TrimSpace(m.workspace) == "" {
		return errors.New("current workspace is unavailable")
	}
	for _, existing := range m.projectApprovalRules {
		if existing.Key == rule.Key {
			return nil
		}
	}
	nextRules := append(append([]approvalMemoryRule(nil), m.projectApprovalRules...), rule)
	if _, err := product.UpdateProjectTUIConfig(m.workspace, func(cfg *configpkg.TUIConfig) error {
		cfg.ProjectApprovalRules = approvalProjectRuleConfigs(nextRules)
		return nil
	}); err != nil {
		return fmt.Errorf("save project approval rule: %w", err)
	}
	m.projectApprovalRules = nextRules
	return nil
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
	if strings.TrimSpace(m.askForm.errorText) != "" {
		sb.WriteString("\n\n")
		sb.WriteString(errorStyle.Render(wrapText(m.askForm.errorText, width-4)))
	}
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
	decisionField := m.askForm.fields[0]
	selected := approvalChoiceAllowOnce
	if len(decisionField.def.Options) > 0 {
		idx := decisionField.singleSel
		if idx >= 0 && idx < len(decisionField.def.Options) {
			selected = decisionField.def.Options[idx]
		}
	}
	var sb strings.Builder
	sectionLabel := func(title, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		sb.WriteString(dialogAccentStyle.Render(title))
		sb.WriteString("\n")
		sb.WriteString(dialogItemStyle.Render(wrapText(value, width-10)))
		sb.WriteString("\n\n")
	}

	sb.WriteString(dialogAccentStyle.Render(display.Title))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("Review the action below before continuing. You can approve once, cache for this session, or amend project policy when available."))
	if strings.TrimSpace(m.askForm.errorText) != "" {
		sb.WriteString("\n\n")
		sb.WriteString(errorStyle.Render(wrapText(m.askForm.errorText, width-8)))
	}
	sb.WriteString("\n\n")
	metaItems := []string{
		dialogItemStyle.Render("tool  " + valueOrDefaultString(display.ToolName, "(unknown)")),
		dialogItemStyle.Render("risk  " + valueOrDefaultString(display.Risk, "(unspecified)")),
	}
	sb.WriteString(renderApprovalButtonRows(metaItems, width-8))
	sb.WriteString("\n\n")
	sectionLabel("Reason", display.Reason)
	sectionLabel(display.ActionLabel, display.ActionValue)
	sectionLabel(display.ScopeLabel, display.ScopeValue)
	sb.WriteString(dialogAccentStyle.Render("Decision"))
	sb.WriteString("\n")
	sb.WriteString(renderApprovalButtonRows(renderApprovalDecisionButtons(decisionField), width-8))
	sb.WriteString("\n")
	if note := approvalDecisionNote(selected, display); strings.TrimSpace(note) != "" {
		sb.WriteString("\n")
		sb.WriteString(mutedStyle.Render(wrapText(note, width-8)))
		sb.WriteString("\n\n")
	}
	return renderDialogFrame(width, "Approval", []string{strings.TrimSpace(sb.String())}, approvalDecisionHelp(decisionField.def.Options))
}

func renderApprovalDecisionButtons(field askFieldState) []string {
	buttons := make([]string, 0, len(field.def.Options))
	for idx, opt := range field.def.Options {
		label := "[ " + approvalDecisionButtonLabel(opt) + " ]"
		if idx == field.singleSel {
			buttons = append(buttons, dialogSelectedItemStyle.Render(label))
			continue
		}
		buttons = append(buttons, dialogItemStyle.Render(label))
	}
	return buttons
}

func approvalDecisionButtonLabel(option string) string {
	switch option {
	case approvalChoiceAllowOnce:
		return "Allow once"
	case approvalChoiceAllowSession:
		return "Session"
	case approvalChoiceAllowProject:
		return "Project"
	case approvalChoiceDeny:
		return "Deny"
	default:
		return option
	}
}

func renderApprovalButtonRows(items []string, maxWidth int) string {
	if maxWidth < 20 {
		maxWidth = 20
	}
	rows := make([]string, 0, len(items))
	current := make([]string, 0, len(items))
	currentWidth := 0
	for _, item := range items {
		itemWidth := lipgloss.Width(item)
		if len(current) > 0 && currentWidth+2+itemWidth > maxWidth {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, current...))
			current = current[:0]
			currentWidth = 0
		}
		if len(current) > 0 {
			current = append(current, "  ")
			currentWidth += 2
		}
		current = append(current, item)
		currentWidth += itemWidth
	}
	if len(current) > 0 {
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, current...))
	}
	return strings.Join(rows, "\n")
}

func approvalDecisionNote(selected string, display approvalDisplay) string {
	switch selected {
	case approvalChoiceAllowSession:
		return display.SessionDecisionNote
	case approvalChoiceAllowProject:
		return display.ProjectDecisionNote
	case approvalChoiceDeny:
		return "Deny this action."
	default:
		return "Approve only this action."
	}
}

func approvalDecisionHelp(options []string) string {
	parts := []string{"←/→ choose", "Enter apply", "A allow once"}
	if indexOfApprovalOption(options, approvalChoiceAllowSession) >= 0 {
		parts = append(parts, "S session")
	}
	if indexOfApprovalOption(options, approvalChoiceAllowProject) >= 0 {
		parts = append(parts, "P project")
	}
	parts = append(parts, "D deny", "Esc cancel")
	return strings.Join(parts, " • ")
}

func approvalSessionPermissions(req *port.ApprovalRequest) *port.PermissionProfile {
	if req == nil {
		return nil
	}
	if req.ProposedPermissions != nil {
		return req.ProposedPermissions
	}
	if strings.TrimSpace(req.ToolName) != "http_request" {
		return nil
	}
	_, pattern := parseApprovalRequestTarget(req)
	fields := strings.Fields(pattern)
	if len(fields) < 2 {
		return nil
	}
	return &port.PermissionProfile{HTTPHosts: []string{fields[1]}}
}

func approvalProjectAmendment(req *port.ApprovalRequest) *port.ExecPolicyAmendment {
	if req == nil {
		return nil
	}
	if req.ProposedAmendment != nil {
		return req.ProposedAmendment
	}
	switch strings.TrimSpace(req.ToolName) {
	case "run_command":
		_, pattern := parseApprovalCommand(req)
		if strings.TrimSpace(pattern) == "" {
			return nil
		}
		return &port.ExecPolicyAmendment{
			CommandRule: &port.ExecPolicyCommandRule{
				Name:  "allow-" + approvalRuleSlug(pattern),
				Match: pattern + "*",
			},
		}
	case "http_request":
		_, pattern := parseApprovalRequestTarget(req)
		fields := strings.Fields(pattern)
		if len(fields) < 2 {
			return nil
		}
		return &port.ExecPolicyAmendment{
			HTTPRule: &port.ExecPolicyHTTPRule{
				Name:    "allow-" + approvalRuleSlug(fields[1]),
				Match:   fields[1],
				Methods: []string{strings.ToUpper(fields[0])},
			},
		}
	default:
		return nil
	}
}

func approvalRuleSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "-", "*", "", "/", "-", "\\", "-", ".", "-", ":", "-", "_", "-")
	value = replacer.Replace(value)
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	value = strings.Trim(value, "-")
	if value == "" {
		return "rule"
	}
	return value
}
