package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mossagents/moss/harness/appkit/product"
	configpkg "github.com/mossagents/moss/harness/config"
	userapproval "github.com/mossagents/moss/harness/userio/approval"
	"github.com/mossagents/moss/kernel/io"
)

type askFieldState struct {
	def         io.InputField
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

func newAskFormState(req io.InputRequest, workspace string) *askFormState {
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
	if req.Type == io.InputConfirm && req.Approval != nil {
		out.confirmLabel = "Apply decision"
	}
	for _, f := range fields {
		st := askFieldState{def: f, multiSel: map[int]bool{}}
		switch f.Type {
		case io.InputFieldBoolean:
			if b, ok := f.Default.(bool); ok {
				st.boolValue = b
			}
		case io.InputFieldSingleSelect:
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
		case io.InputFieldMultiSelect:
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

func synthesizeFieldsFromInputRequest(req io.InputRequest, workspace string) []io.InputField {
	switch req.Type {
	case io.InputConfirm:
		if req.Approval != nil {
			explicitScopes := len(req.Approval.AllowedScopes) > 0
			normalized := io.NormalizeApprovalRequest(req.Approval)
			display := userapproval.BuildDisplay(normalized, "")
			options := []string{}
			if !explicitScopes || approvalScopeAllowed(normalized, io.DecisionScopeOnce) {
				options = append(options, userapproval.ChoiceAllowOnce)
			}
			if strings.TrimSpace(display.RuleKey) != "" && (!explicitScopes || approvalScopeAllowed(normalized, io.DecisionScopeSession)) {
				options = append(options, userapproval.ChoiceAllowSession)
			}
			if strings.TrimSpace(workspace) != "" && approvalProjectAmendment(normalized) != nil && (!explicitScopes || approvalScopeAllowed(normalized, io.DecisionScopeProject)) {
				options = append(options, userapproval.ChoiceAllowProject)
			}
			if len(options) == 0 {
				options = append(options, userapproval.ChoiceAllowOnce)
			}
			options = append(options, userapproval.ChoiceDeny)
			defaultChoice := approvalChoiceForScope(normalized.DefaultScope)
			if indexOfApprovalOption(options, defaultChoice) < 0 {
				defaultChoice = userapproval.ChoiceAllowOnce
			}
			return []io.InputField{{
				Name:        "decision",
				Type:        io.InputFieldSingleSelect,
				Title:       "Decision",
				Description: "Choose whether to allow this action once, remember matching actions, or deny it.",
				Required:    true,
				Options:     options,
				Default:     defaultChoice,
			}}
		}
		return []io.InputField{{Name: "approved", Type: io.InputFieldBoolean, Title: req.Prompt, Required: true}}
	case io.InputSelect:
		return []io.InputField{{Name: "selected", Type: io.InputFieldSingleSelect, Title: req.Prompt, Required: true, Options: req.Options}}
	default:
		return []io.InputField{{Name: "value", Type: io.InputFieldString, Title: req.Prompt, Required: true}}
	}
}

func approvalScopeAllowed(req *io.ApprovalRequest, scope io.DecisionScope) bool {
	if req == nil {
		return scope == io.DecisionScopeOnce
	}
	for _, allowed := range req.AllowedScopes {
		if allowed == scope {
			return true
		}
	}
	return false
}

func approvalChoiceForScope(scope io.DecisionScope) string {
	switch scope {
	case io.DecisionScopeSession:
		return userapproval.ChoiceAllowSession
	case io.DecisionScopeProject:
		return userapproval.ChoiceAllowProject
	default:
		return userapproval.ChoiceAllowOnce
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
	case io.InputFieldString, io.InputFieldNumber, io.InputFieldInteger:
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
	if m.isSimpleConfirmAskActive() {
		return m.handleSimpleConfirmAskKey(msg)
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
	case io.InputFieldBoolean:
		if msg.String() == "enter" || msg.String() == " " {
			field.boolValue = !field.boolValue
			m.refreshViewport()
		}
	case io.InputFieldSingleSelect:
		switch msg.String() {
		case "up":
			if field.singleIndex > 0 {
				field.singleIndex--
			}
		case "down":
			maxIdx := len(field.def.Options) - 1
			if m.isInlineSelectAsk() {
				// インラインセレクトでは「Chat about this」も含む
				maxIdx = len(field.def.Options)
			}
			if field.singleIndex < maxIdx {
				field.singleIndex++
			}
		case "enter":
			if m.isInlineSelectAsk() && field.singleIndex >= len(field.def.Options) {
				// 「Chat about this」を選択した場合：実行をキャンセルしてフォームを閉じる
				return m.handleChatAboutThis()
			}
			field.singleSel = field.singleIndex
			form.focusIndex = (form.focusIndex + 1) % (len(form.fields) + 1)
			m.activateAskField()
		}
		m.refreshViewport()
	case io.InputFieldMultiSelect:
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
	return m.askForm != nil && m.pendAsk != nil && m.pendAsk.request.Type == io.InputConfirm && m.pendAsk.request.Approval != nil
}

func (m chatModel) isSimpleConfirmAskActive() bool {
	return m.askForm != nil &&
		m.pendAsk != nil &&
		m.pendAsk.request.Type == io.InputConfirm &&
		m.pendAsk.request.Approval == nil &&
		len(m.askForm.fields) == 1 &&
		m.askForm.fields[0].def.Type == io.InputFieldBoolean
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
		selectIndex(indexOfApprovalOption(field.def.Options, userapproval.ChoiceAllowOnce))
	case "s":
		selectIndex(indexOfApprovalOption(field.def.Options, userapproval.ChoiceAllowSession))
	case "p":
		selectIndex(indexOfApprovalOption(field.def.Options, userapproval.ChoiceAllowProject))
	case "d":
		selectIndex(indexOfApprovalOption(field.def.Options, userapproval.ChoiceDeny))
	case "enter":
		return m.submitAskForm()
	default:
		if idx, ok := confirmOptionKeyIndex(msg.String(), len(field.def.Options)); ok {
			selectIndex(idx)
		}
	}
	return m, nil
}

func (m chatModel) handleSimpleConfirmAskKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if !m.isSimpleConfirmAskActive() {
		return m, nil
	}
	field := &m.askForm.fields[0]
	setApproved := func(approved bool) {
		field.boolValue = approved
		m.refreshViewport()
	}
	switch msg.String() {
	case "up", "left", "y":
		setApproved(true)
	case "down", "right", "n":
		setApproved(false)
	case "tab", "shift+tab", " ":
		setApproved(!field.boolValue)
	case "enter":
		return m.submitAskForm()
	default:
		if idx, ok := confirmOptionKeyIndex(msg.String(), 2); ok {
			setApproved(idx == 0)
		}
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

// handleChatAboutThis は「Chat about this」オプションが選択されたとき、実行をキャンセルして
// 入力フォームを閉じ、ユーザーがテキストを入力できる状態に戻す。
func (m chatModel) handleChatAboutThis() (chatModel, tea.Cmd) {
	if m.cancelRunFn != nil {
		m.cancelRunFn()
		m.streaming = false
	}
	m.resetAskFormState()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) submitAskForm() (chatModel, tea.Cmd) {
	if m.askForm == nil || m.pendAsk == nil {
		return m, nil
	}
	formValues := map[string]any{}
	for _, f := range m.askForm.fields {
		switch f.def.Type {
		case io.InputFieldBoolean:
			formValues[f.def.Name] = f.boolValue
		case io.InputFieldSingleSelect:
			if len(f.def.Options) == 0 {
				formValues[f.def.Name] = ""
			} else {
				idx := f.singleSel
				if idx < 0 || idx >= len(f.def.Options) {
					idx = 0
				}
				formValues[f.def.Name] = f.def.Options[idx]
			}
		case io.InputFieldMultiSelect:
			values := make([]string, 0, len(f.multiSel))
			for i, opt := range f.def.Options {
				if f.multiSel[i] {
					values = append(values, opt)
				}
			}
			formValues[f.def.Name] = values
		case io.InputFieldNumber:
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
		case io.InputFieldInteger:
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
	if ask.request.Type == io.InputConfirm && ask.request.Approval != nil {
		return m.submitApprovalAskForm(ask, formValues)
	}

	m.resetAskFormState()

	if ask.request.Type == io.InputForm {
		ask.replyCh <- io.InputResponse{Form: formValues}
	} else {
		switch ask.request.Type {
		case io.InputConfirm:
			approved, _ := formValues["approved"].(bool)
			ask.replyCh <- io.InputResponse{Approved: approved}
		case io.InputSelect:
			selectedValue, _ := formValues["selected"].(string)
			idx := 0
			for i, opt := range ask.request.Options {
				if opt == selectedValue {
					idx = i
					break
				}
			}
			ask.replyCh <- io.InputResponse{Selected: idx}
		default:
			val, _ := formValues["value"].(string)
			ask.replyCh <- io.InputResponse{Value: val}
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
	approved := selected != userapproval.ChoiceDeny
	resp := io.InputResponse{
		Approved: approved,
		Decision: &io.ApprovalDecision{
			RequestID: ask.request.Approval.ID,
			Type:      io.ApprovalDecisionDeny,
			Approved:  approved,
			Source:    "tui-approval",
			DecidedAt: m.now().UTC(),
		},
	}
	if ask.request.Approval != nil {
		normalized := io.NormalizeApprovalRequest(ask.request.Approval)
		resp.Decision.RuleBinding = normalized.RuleBinding
		resp.Decision.CacheKey = normalized.CacheKey
	}
	notice := "Approval denied."
	if approved {
		notice = "Approval granted."
	}
	switch selected {
	case userapproval.ChoiceAllowSession:
		if rule, ok := userapproval.MemoryRuleFor(ask.request.Approval, m.currentSessionID, userapproval.RuleScopeSession, m.now()); ok {
			m.rememberApprovalRule(rule)
		}
		if perms := approvalSessionPermissions(ask.request.Approval); perms != nil {
			resp.Decision.Type = io.ApprovalDecisionGrantPermission
			resp.Decision.GrantedPermissions = perms
			resp.Decision.Source = "tui-session-rule"
			resp.Decision.Reason = "grant requested permissions for this session"
			resp.Decision.Scope = io.DecisionScopeSession
			resp.Decision.Persistence = io.DecisionPersistenceSession
			notice = "Permission granted for this session."
		} else {
			resp.Decision.Type = io.ApprovalDecisionApproveSession
			resp.Decision.Source = "tui-session-rule"
			resp.Decision.Reason = "remember similar actions for this session"
			resp.Decision.Scope = io.DecisionScopeSession
			resp.Decision.Persistence = io.DecisionPersistenceSession
			notice = "Approval granted for this session."
		}
	case userapproval.ChoiceAllowProject:
		amendment := approvalProjectAmendment(ask.request.Approval)
		if amendment == nil {
			m.askForm.errorText = "This approval cannot be remembered for the current project."
			m.refreshViewport()
			return m, nil
		}
		rule, ok := userapproval.MemoryRuleFor(ask.request.Approval, m.currentSessionID, userapproval.RuleScopeProject, m.now())
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
		if err := product.PersistProjectApprovalAmendment(m.workspace, amendment); err != nil {
			m.askForm.errorText = err.Error()
			m.refreshViewport()
			return m, nil
		}
		resp.Decision.Type = io.ApprovalDecisionPolicyAmendment
		resp.Decision.PolicyAmendment = amendment
		resp.Decision.Source = "tui-project-rule"
		resp.Decision.Reason = "persist matching policy amendment for this project"
		resp.Decision.Scope = io.DecisionScopeProject
		resp.Decision.Persistence = io.DecisionPersistenceProject
		notice = "Project policy updated."
	case userapproval.ChoiceAllowOnce:
		resp.Decision.Type = io.ApprovalDecisionApprove
		resp.Decision.Source = "tui-allow-once"
		resp.Decision.Scope = io.DecisionScopeOnce
		resp.Decision.Persistence = io.DecisionPersistenceRequest
	default:
		resp.Decision.Type = io.ApprovalDecisionDeny
		resp.Decision.Source = "tui-deny"
		resp.Decision.Scope = io.DecisionScopeOnce
		resp.Decision.Persistence = io.DecisionPersistenceRequest
	}
	resp.Decision = io.NormalizeApprovalDecisionForRequest(ask.request.Approval, resp.Decision)
	m.resetAskFormState()
	ask.replyCh <- resp
	m.messages = append(m.messages, chatMessage{kind: msgSystem, content: notice})
	m.refreshViewport()
	return m, nil
}

func (m *chatModel) rememberApprovalRule(rule userapproval.MemoryRule) {
	if rule.ScopeValue() != userapproval.RuleScopeSession || strings.TrimSpace(rule.SessionID) == "" || strings.TrimSpace(rule.Key) == "" {
		return
	}
	if m.approvalRules == nil {
		m.approvalRules = map[string][]userapproval.MemoryRule{}
	}
	rules := m.approvalRules[rule.SessionID]
	for _, existing := range rules {
		if existing.Key == rule.Key {
			return
		}
	}
	m.approvalRules[rule.SessionID] = append(rules, rule)
}

func (m *chatModel) rememberProjectApprovalRule(rule userapproval.MemoryRule) error {
	if rule.ScopeValue() != userapproval.RuleScopeProject || strings.TrimSpace(rule.Key) == "" {
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
	nextRules := append(append([]userapproval.MemoryRule(nil), m.projectApprovalRules...), rule)
	if _, err := product.UpdateProjectTUIConfig(m.workspace, func(cfg *configpkg.TUIConfig) error {
		cfg.ProjectApprovalRules = userapproval.ProjectRuleConfigs(nextRules)
		return nil
	}); err != nil {
		return fmt.Errorf("save project approval rule: %w", err)
	}
	m.projectApprovalRules = nextRules
	return nil
}

// isInlineSelectAsk は InputSelect タイプのリクエストで、インラインリスト形式で表示するかどうかを返す。
func (m chatModel) isInlineSelectAsk() bool {
	return m.askForm != nil &&
		m.pendAsk != nil &&
		m.pendAsk.request.Type == io.InputSelect &&
		len(m.askForm.fields) == 1 &&
		m.askForm.fields[0].def.Type == io.InputFieldSingleSelect
}

// renderInlineSelectAsk は InputSelect タイプのリクエストをスクリーンショット設計に従ってインラインで描画する。
// 外枠なし・番号付きリスト・選択中は「> 」プレフィックスで表示。
func (m chatModel) renderInlineSelectAsk(width int) string {
	if !m.isInlineSelectAsk() {
		return ""
	}
	field := m.askForm.fields[0]
	options := field.def.Options

	var sb strings.Builder

	// タイトルバッジ
	title := strings.TrimSpace(m.pendAsk.request.Prompt)
	if title == "" {
		title = strings.TrimSpace(field.def.Title)
	}
	if title == "" {
		title = "Select an option"
	}
	sb.WriteString(dialogAccentStyle.Render("□  " + title))
	sb.WriteString("\n\n")

	if err := strings.TrimSpace(m.askForm.errorText); err != "" {
		sb.WriteString(errorStyle.Render(wrapText(err, max(20, width-6))))
		sb.WriteString("\n\n")
	}

	// 番号付きオプションリスト
	for i, opt := range options {
		// オプションテキストに "\n" が含まれる場合、2行目以降を説明文として扱う
		parts := strings.SplitN(opt, "\n", 2)
		optTitle := strings.TrimSpace(parts[0])
		optDesc := ""
		if len(parts) > 1 {
			optDesc = strings.TrimSpace(parts[1])
		}

		numLabel := fmt.Sprintf("%d. ", i+1)
		isSelected := field.singleIndex == i
		if isSelected {
			sb.WriteString(dialogTitleStyle.Render("> " + numLabel + optTitle))
		} else {
			sb.WriteString("  " + numLabel + optTitle)
		}
		sb.WriteString("\n")

		if optDesc != "" {
			indent := "     "
			wrapW := max(20, width-len(indent)-2)
			sb.WriteString(mutedStyle.Render(indent + wrapText(optDesc, wrapW)))
			sb.WriteString("\n")
		}
	}

	// 区切り線 + 「Chat about this」エスケープオプション
	sb.WriteString("\n")
	ruleLen := min(max(20, width-4), 48)
	sb.WriteString(mutedStyle.Render(strings.Repeat("─", ruleLen)))
	sb.WriteString("\n\n")

	chatIdx := len(options)
	chatLabel := fmt.Sprintf("%d. Chat about this", chatIdx+1)
	if field.singleIndex == chatIdx {
		sb.WriteString(dialogTitleStyle.Render("> " + chatLabel))
	} else {
		sb.WriteString("  " + chatLabel)
	}
	sb.WriteString("\n\n")

	// フッターヒント
	sb.WriteString(mutedStyle.Render("Enter to select  ·  ↑/↓ to navigate  ·  Esc to cancel"))
	return sb.String()
}

func (m chatModel) renderAskForm(width int) string {
	if m.askForm == nil {
		return ""
	}
	if width < 30 {
		width = 30
	}
	var sb strings.Builder
	if m.pendAsk != nil && m.pendAsk.request.Type == io.InputConfirm && m.pendAsk.request.Approval != nil {
		return m.renderApprovalAskForm(width)
	}
	if m.isSimpleConfirmAskActive() {
		return m.renderSimpleConfirmAskForm(width)
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
		case io.InputFieldBoolean:
			val := "false"
			if f.boolValue {
				val = "true"
			}
			sb.WriteString("    [toggle] " + val + "\n")
		case io.InputFieldSingleSelect:
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
		case io.InputFieldMultiSelect:
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
	display := userapproval.BuildDisplay(m.pendAsk.request.Approval, m.currentSessionID)
	decisionField := m.askForm.fields[0]
	selected := userapproval.ChoiceAllowOnce
	if len(decisionField.def.Options) > 0 {
		idx := decisionField.singleSel
		if idx >= 0 && idx < len(decisionField.def.Options) {
			selected = decisionField.def.Options[idx]
		}
	}
	sections := []string{
		mutedStyle.Render(wrapText("Review the action below before continuing. You can approve once, cache for this session, or amend project policy when available.", confirmSheetWrapWidth(width))),
		renderConfirmMetaRows([][2]string{
			{"Tool", valueOrDefaultString(display.ToolName, "(unknown)")},
			{"Risk", valueOrDefaultString(display.Risk, "(unspecified)")},
		}),
	}
	if action := renderConfirmBoxSection(width, display.ActionLabel, display.ActionValue); strings.TrimSpace(action) != "" {
		sections = append(sections, action)
	}
	if strings.TrimSpace(m.askForm.errorText) != "" {
		sections = append(sections, errorStyle.Render(wrapText(m.askForm.errorText, confirmSheetWrapWidth(width))))
	}
	if reason := renderConfirmTextSection(width, "Reason", display.Reason); strings.TrimSpace(reason) != "" {
		sections = append(sections, reason)
	}
	if diff := renderConfirmDiffSection("Changes", display.DiffPreview); strings.TrimSpace(diff) != "" {
		sections = append(sections, diff)
	}
	if scope := renderConfirmTextSection(width, display.ScopeLabel, display.ScopeValue); strings.TrimSpace(scope) != "" {
		sections = append(sections, scope)
	}
	sections = append(sections, renderConfirmChoiceSection("Choose how to proceed", approvalDecisionLabels(decisionField), decisionField.singleSel))
	if note := approvalDecisionNote(selected, display); strings.TrimSpace(note) != "" {
		sections = append(sections, mutedStyle.Render(wrapText(note, confirmSheetWrapWidth(width))))
	}
	return renderConfirmSheetFrame(width, display.Title, sections, approvalDecisionHelp(decisionField.def.Options))
}

func (m chatModel) renderSimpleConfirmAskForm(width int) string {
	if !m.isSimpleConfirmAskActive() {
		return ""
	}
	field := m.askForm.fields[0]
	selected := 1
	if field.boolValue {
		selected = 0
	}
	sections := make([]string, 0, 4)
	if summary := renderSimpleConfirmSummary(width, m.pendAsk.request); strings.TrimSpace(summary) != "" {
		sections = append(sections, summary)
	}
	if prompt := strings.TrimSpace(m.pendAsk.request.Prompt); prompt != "" {
		sections = append(sections, mutedStyle.Render(wrapText(prompt, confirmSheetWrapWidth(width))))
	}
	if strings.TrimSpace(m.askForm.errorText) != "" {
		sections = append(sections, errorStyle.Render(wrapText(m.askForm.errorText, confirmSheetWrapWidth(width))))
	}
	sections = append(sections, renderConfirmChoiceSection("", []string{"Yes", "No"}, selected))
	return renderConfirmSheetFrame(width, confirmDialogTitle(m.pendAsk.request), sections, simpleConfirmHelp())
}

func confirmOptionKeyIndex(key string, optionCount int) (int, bool) {
	idx, err := strconv.Atoi(strings.TrimSpace(key))
	if err != nil || idx < 1 || idx > optionCount {
		return 0, false
	}
	return idx - 1, true
}

func confirmDialogTitle(req io.InputRequest) string {
	if title := strings.TrimSpace(req.ConfirmLabel); title != "" {
		return title
	}
	return "Confirm"
}

func renderSimpleConfirmSummary(width int, req io.InputRequest) string {
	label, value := confirmRequestSummary(req)
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return renderConfirmBoxSection(width, label, value)
}

func confirmRequestSummary(req io.InputRequest) (string, string) {
	candidates := []struct {
		key   string
		label string
	}{
		{key: "workspace", label: "Folder"},
		{key: "source", label: "Source"},
		{key: "target", label: "Target"},
	}
	for _, candidate := range candidates {
		raw, ok := req.Meta[candidate.key]
		if !ok {
			continue
		}
		value := strings.TrimSpace(fmt.Sprint(raw))
		if value != "" {
			return candidate.label, value
		}
	}
	return "", ""
}

func confirmSheetFrameStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)
}

func confirmSheetBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Padding(0, 1)
}

func confirmSheetContentWidth(width int) int {
	if width < 56 {
		width = 56
	}
	contentWidth := width - confirmSheetFrameStyle().GetHorizontalFrameSize()
	if contentWidth < 1 {
		contentWidth = 1
	}
	return contentWidth
}

func confirmSheetWrapWidth(width int) int {
	return max(20, confirmSheetContentWidth(width)-2)
}

func renderConfirmSheetFrame(width int, title string, sections []string, footer string) string {
	contentWidth := confirmSheetContentWidth(width)
	rule := shellRuleStyle.Width(contentWidth).Render(strings.Repeat("─", max(1, contentWidth-1)))
	parts := []string{
		baseStyle.Copy().Bold(true).Render(valueOrDefaultString(strings.TrimSpace(title), "Confirm")),
		rule,
	}
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			parts = append(parts, section)
		}
	}
	if strings.TrimSpace(footer) != "" {
		parts = append(parts, dialogHelpStyle.Render(footer))
	}
	return confirmSheetFrameStyle().Width(contentWidth).Render(strings.Join(parts, "\n\n"))
}

func renderConfirmBoxSection(width int, title, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := make([]string, 0, 2)
	if strings.TrimSpace(title) != "" {
		parts = append(parts, dialogAccentStyle.Render(strings.TrimSpace(title)))
	}
	boxStyle := confirmSheetBoxStyle()
	boxContentWidth := confirmSheetContentWidth(width) - boxStyle.GetHorizontalFrameSize()
	if boxContentWidth < 1 {
		boxContentWidth = 1
	}
	parts = append(parts, boxStyle.Width(boxContentWidth).Render(wrapText(value, boxContentWidth)))
	return strings.Join(parts, "\n")
}

func renderConfirmTextSection(width int, title, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := make([]string, 0, 2)
	if strings.TrimSpace(title) != "" {
		parts = append(parts, dialogAccentStyle.Render(strings.TrimSpace(title)))
	}
	parts = append(parts, wrapText(value, confirmSheetWrapWidth(width)))
	return strings.Join(parts, "\n")
}

func renderConfirmDiffSection(title, diff string) string {
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return ""
	}
	var sb strings.Builder
	if strings.TrimSpace(title) != "" {
		sb.WriteString(dialogAccentStyle.Render(strings.TrimSpace(title)))
		sb.WriteString("\n")
	}
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+"):
			sb.WriteString(toolResultStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			sb.WriteString(toolErrorStyle.Render(line))
		default:
			sb.WriteString(mutedStyle.Render(line))
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderConfirmMetaRows(items [][2]string) string {
	rows := make([]string, 0, len(items))
	for _, item := range items {
		value := strings.TrimSpace(item[1])
		if value == "" {
			continue
		}
		rows = append(rows, fmt.Sprintf("%s  %s", strings.ToLower(strings.TrimSpace(item[0])), value))
	}
	return strings.Join(rows, "\n")
}

func renderConfirmChoiceSection(title string, options []string, selected int) string {
	if len(options) == 0 {
		return ""
	}
	if selected < 0 || selected >= len(options) {
		selected = 0
	}
	var sb strings.Builder
	if strings.TrimSpace(title) != "" {
		sb.WriteString(dialogAccentStyle.Render(strings.TrimSpace(title)))
		sb.WriteString("\n")
	}
	for idx, option := range options {
		line := fmt.Sprintf("%d. %s", idx+1, option)
		if idx == selected {
			sb.WriteString(dialogAccentStyle.Render("› " + line))
		} else {
			sb.WriteString(dialogItemStyle.Render("  " + line))
		}
		if idx < len(options)-1 {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func approvalDecisionLabels(field askFieldState) []string {
	labels := make([]string, 0, len(field.def.Options))
	for _, opt := range field.def.Options {
		labels = append(labels, approvalDecisionButtonLabel(opt))
	}
	return labels
}

func approvalDecisionButtonLabel(option string) string {
	switch option {
	case userapproval.ChoiceAllowOnce:
		return "Allow once"
	case userapproval.ChoiceAllowSession:
		return "Session"
	case userapproval.ChoiceAllowProject:
		return "Project"
	case userapproval.ChoiceDeny:
		return "Deny"
	default:
		return option
	}
}

func approvalDecisionNote(selected string, display userapproval.Display) string {
	switch selected {
	case userapproval.ChoiceAllowSession:
		return display.SessionDecisionNote
	case userapproval.ChoiceAllowProject:
		return display.ProjectDecisionNote
	case userapproval.ChoiceDeny:
		return "Deny this action."
	default:
		return "Approve only this action."
	}
}

func approvalDecisionHelp(options []string) string {
	parts := []string{"↑↓ navigate", "Enter apply"}
	switch len(options) {
	case 1:
		parts = append(parts, "1 choose")
	case 0:
	default:
		parts = append(parts, fmt.Sprintf("1-%d choose", len(options)))
	}
	parts = append(parts, "Esc cancel")
	return strings.Join(parts, " • ")
}

func simpleConfirmHelp() string {
	return "↑↓ navigate • Enter select • 1/2 choose • Esc cancel"
}

func approvalSessionPermissions(req *io.ApprovalRequest) *io.PermissionProfile {
	if req == nil {
		return nil
	}
	if req.ProposedPermissions != nil {
		return req.ProposedPermissions
	}
	if strings.TrimSpace(req.ToolName) != "http_request" {
		return nil
	}
	_, pattern := userapproval.ParseRequestTarget(req)
	fields := strings.Fields(pattern)
	if len(fields) < 2 {
		return nil
	}
	return &io.PermissionProfile{HTTPHosts: []string{fields[1]}}
}

func approvalProjectAmendment(req *io.ApprovalRequest) *io.ExecPolicyAmendment {
	if req == nil {
		return nil
	}
	if req.ProposedAmendment != nil {
		return req.ProposedAmendment
	}
	switch strings.TrimSpace(req.ToolName) {
	case "run_command":
		_, pattern := userapproval.ParseCommand(req)
		if strings.TrimSpace(pattern) == "" {
			return nil
		}
		return &io.ExecPolicyAmendment{
			CommandRule: &io.ExecPolicyCommandRule{
				Name:  "allow-" + approvalRuleSlug(pattern),
				Match: pattern + "*",
			},
		}
	case "http_request":
		_, pattern := userapproval.ParseRequestTarget(req)
		fields := strings.Fields(pattern)
		if len(fields) < 2 {
			return nil
		}
		return &io.ExecPolicyAmendment{
			HTTPRule: &io.ExecPolicyHTTPRule{
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
