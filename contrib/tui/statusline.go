package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mossagents/moss/harness/appkit/product"
	"github.com/mossagents/moss/kernel/io"
)

type statusLineItemDef struct {
	Name    string
	Summary string
}

var statusLineItemCatalog = []statusLineItemDef{
	{Name: "state", Summary: "Current run state"},
	{Name: "model", Summary: "Provider and model"},
	{Name: "workspace", Summary: "Workspace root"},
	{Name: "profile", Summary: "Resolved profile"},
	{Name: "trust", Summary: "Current trust posture"},
	{Name: "approval", Summary: "Current approval mode"},
	{Name: "thread", Summary: "Current thread id"},
	{Name: "messages", Summary: "Visible message count"},
	{Name: "theme", Summary: "Active TUI theme"},
	{Name: "personality", Summary: "Current response personality"},
	{Name: "fast", Summary: "Fast mode on/off"},
	{Name: "version", Summary: "mosscode version"},
}

var defaultStatusLineItems = []string{"model", "workspace", "profile", "approval", "thread", "messages"}

func normalizeStatusLineItems(items []string) []string {
	if len(items) == 0 {
		items = defaultStatusLineItems
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item))
		if !isStatusLineItemSupported(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return append([]string(nil), defaultStatusLineItems...)
	}
	return out
}

func parseStatusLineItems(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s", renderStatusLineUsage(nil))
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '|' || r == ' '
	})
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if !isStatusLineItemSupported(name) {
			return nil, fmt.Errorf("unknown status-line item %q", part)
		}
		items = append(items, name)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s", renderStatusLineUsage(nil))
	}
	return normalizeStatusLineItems(items), nil
}

func renderStatusLineUsage(current []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Current status line: %s\n", strings.Join(normalizeStatusLineItems(current), ", "))
	b.WriteString("Available items:\n")
	for _, item := range statusLineItemCatalog {
		fmt.Fprintf(&b, "- %s — %s\n", item.Name, item.Summary)
	}
	b.WriteString("\nUsage:\n")
	b.WriteString("  /statusline\n")
	b.WriteString("  /statusline set model,workspace,profile,approval,thread,messages\n")
	b.WriteString("  /statusline reset")
	return strings.TrimRight(b.String(), "\n")
}

func isStatusLineItemSupported(name string) bool {
	for _, item := range statusLineItemCatalog {
		if item.Name == name {
			return true
		}
	}
	return false
}

func effectiveExperimentalFeatures(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if name := product.NormalizeExperimentalFeature(item); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func setExperimentalFeature(items []string, name string, enabled bool) []string {
	name = product.NormalizeExperimentalFeature(name)
	if name == "" {
		return effectiveExperimentalFeatures(items)
	}
	current := effectiveExperimentalFeatures(items)
	filtered := make([]string, 0, len(current))
	found := false
	for _, item := range current {
		if item == name {
			found = true
			if enabled {
				filtered = append(filtered, item)
			}
			continue
		}
		filtered = append(filtered, item)
	}
	if enabled && !found {
		filtered = append(filtered, name)
	}
	return filtered
}

func onOff(enabled bool) string {
	if enabled {
		return "on"
	}
	return "off"
}

func (m chatModel) experimentalEnabled(name string) bool {
	name = product.NormalizeExperimentalFeature(name)
	if name == "" {
		return false
	}
	for _, item := range effectiveExperimentalFeatures(m.experimentalFeatures) {
		if item == name {
			return true
		}
	}
	return false
}

func (m chatModel) runtimeStateLabel() string {
	switch {
	case m.pendAsk != nil && m.askForm != nil && m.pendAsk.request.Type == io.InputConfirm && m.pendAsk.request.Approval != nil:
		return "approval"
	case m.pendAsk != nil:
		return "input"
	case m.streaming && m.hasRunningToolCalls():
		return "tool"
	case m.streaming:
		return "running"
	default:
		return "idle"
	}
}

func (m chatModel) progressStatusSummary() string {
	if !m.progress.visible() {
		return ""
	}
	if msg := strings.TrimSpace(m.progress.Message); msg != "" {
		return truncateDisplayWidth(humanizeProgressMessage(msg, m.progress.Phase, m.progress.ToolName), 48)
	}
	if strings.EqualFold(strings.TrimSpace(m.progress.Phase), "tools") && strings.TrimSpace(m.progress.ToolName) != "" {
		return truncateDisplayWidth("Using "+toolPrettyName(m.progress.ToolName), 48)
	}
	if phase := strings.TrimSpace(progressPhaseLabel(m.progress.Phase)); phase != "" {
		return truncateDisplayWidth(titleCaseWord(phase), 48)
	}
	if status := strings.TrimSpace(progressStatusLabel(m.progress.Status)); status != "" {
		return status
	}
	return ""
}

func humanizeProgressMessage(message, phase, toolName string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	toolName = strings.TrimSpace(toolName)
	switch {
	case toolName != "" && strings.EqualFold(message, "running "+toolName):
		return "Using " + toolPrettyName(toolName)
	case toolName != "" && strings.EqualFold(message, "approval required for "+toolName):
		return "Approval required for " + toolPrettyName(toolName)
	case toolName != "" && strings.EqualFold(message, "approval granted for "+toolName):
		return "Approval granted for " + toolPrettyName(toolName)
	case strings.EqualFold(strings.TrimSpace(phase), "tools") && toolName != "" && strings.EqualFold(message, "running tool"):
		return "Using " + toolPrettyName(toolName)
	default:
		return titleCaseWord(message)
	}
}

func (m chatModel) composerMetaSummary() (string, string) {
	switch {
	case m.isApprovalAskActive():
		return "Approval", "confirmation needed  •  " + approvalDecisionHelp(m.askForm.fields[0].def.Options)
	case m.isSimpleConfirmAskActive():
		return "Confirm", "confirmation needed  •  " + simpleConfirmHelp()
	case m.pendAsk != nil:
		return "Input", "answer required  •  Enter confirm"
	case m.mentionPopup != nil:
		return "Attach", "file picker  •  Tab attach"
	case m.slashPopup != nil:
		return "Command", fmt.Sprintf("%s  •  Tab complete", m.composerSlashContext())
	case m.streaming:
		summary := valueOrDefaultString(strings.TrimSpace(m.progressStatusSummary()), "Running")
		detailParts := []string{"Esc Esc cancel"}
		if queued := len(m.queuedInputs); queued > 0 {
			detailParts = append(detailParts, fmt.Sprintf("%d queued", queued))
		}
		if m.lastTrace != nil && m.lastTrace.Trace.TotalTokens > 0 {
			detailParts = append(detailParts, formatTokenCount(m.lastTrace.Trace.TotalTokens)+" tokens")
		}
		return spinnerFrame(m.now()) + " " + summary, strings.Join(detailParts, "  •  ")
	case len(m.pendingAttachments) > 0:
		return "Attach", fmt.Sprintf("%d ready  •  Ctrl+X remove latest", len(m.pendingAttachments))
	case len(m.queuedInputs) > 0:
		return "Queued", fmt.Sprintf("%d waiting  •  keep typing", len(m.queuedInputs))
	case strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "/"):
		return "Command", fmt.Sprintf("%s  •  Tab complete", m.composerSlashContext())
	case strings.HasPrefix(strings.TrimSpace(m.textarea.Value()), "@"):
		return "Attach", "file picker  •  Tab attach"
	case strings.TrimSpace(m.textarea.Value()) != "":
		return "Draft", "Enter send  •  Shift+Enter newline"
	default:
		// Ready state: show posture context + last-run token count + input hints.
		var parts []string
		if posture := m.compactPostureSummary(); posture != "" {
			parts = append(parts, posture)
		}
		if m.lastTrace != nil && m.lastTrace.Trace.TotalTokens > 0 {
			parts = append(parts, formatTokenCount(m.lastTrace.Trace.TotalTokens)+" tokens")
		}
		parts = append(parts, "/ commands  •  @ files")
		return "Ready", strings.Join(parts, "  •  ")
	}
}

func (m chatModel) composerSlashContext() string {
	hints := m.currentSlashHints()
	if len(hints) == 0 {
		return "type to filter"
	}
	if len(hints) > 3 {
		hints = hints[:3]
	}
	return strings.Join(hints, ", ")
}

// renderStatusLineBar renders the configured status-line items as a compact
// "•"-joined string suitable for the bottom bar's right side.
func (m chatModel) renderStatusLineBar() string {
	items := normalizeStatusLineItems(m.statusLineItems)
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if seg := m.statusLineItemValue(item); seg != "" {
			parts = append(parts, seg)
		}
	}
	return strings.Join(parts, "  •  ")
}

func (m chatModel) statusLineItemValue(name string) string {
	switch name {
	case "state":
		return m.runtimeStateLabel()
	case "model":
		return valueOrDefaultString(m.model, m.provider)
	case "workspace":
		ws := strings.TrimSpace(m.workspace)
		if ws == "" {
			ws = "."
		}
		abs, err := filepath.Abs(ws)
		if err != nil {
			abs = ws
		}
		display := shortenHomePath(abs)
		if branch := gitBranchInDir(abs); branch != "" {
			display += " (" + branch + ")"
		}
		return display
	case "profile":
		return valueOrDefaultString(m.profile, "default")
	case "trust":
		return valueOrDefaultString(m.trust, "trusted")
	case "approval":
		return valueOrDefaultString(m.approvalMode, "auto")
	case "thread":
		if id := strings.TrimSpace(m.currentSessionID); id != "" {
			return "thread " + shortThreadID(id)
		}
		return ""
	case "messages":
		n := 0
		for _, msg := range m.messages {
			if msg.kind == msgUser || msg.kind == msgAssistant {
				n++
			}
		}
		if n == 0 {
			return ""
		}
		return fmt.Sprintf("%d msgs", n)
	case "theme":
		return valueOrDefaultString(m.theme, themeDefault)
	case "personality":
		return m.personality
	case "fast":
		if m.fastMode {
			return "fast"
		}
		return ""
	case "version":
		return "" // injected via build; not available in chatModel
	default:
		return ""
	}
}

// shortenHomePath replaces the user's home directory prefix with "~".
func shortenHomePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}

// gitBranchInDir reads the current branch from <dir>/.git/HEAD without
// spawning a subprocess. Returns "" if the directory is not a git repo or
// is in a detached-HEAD state.
func gitBranchInDir(dir string) string {
	headPath := filepath.Join(dir, ".git", "HEAD")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	const prefix = "ref: refs/heads/"
	if strings.HasPrefix(line, prefix) {
		return strings.TrimPrefix(line, prefix)
	}
	return "" // detached HEAD — don't show a branch label
}
