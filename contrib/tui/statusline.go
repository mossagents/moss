package tui

import (
	"fmt"
	"github.com/mossagents/moss/appkit/product"
	intr "github.com/mossagents/moss/kernel/io"
	"path/filepath"
	"strings"
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

func (m chatModel) renderStatusLine() string {
	values := []string{m.runtimeStateLabel()}
	if label := strings.TrimSpace(m.progressStatusSummary()); label != "" && label != m.runtimeStateLabel() {
		values = append(values, label)
	}
	if strings.TrimSpace(m.provider) != "" && (!m.modelAuto || strings.TrimSpace(m.model) != "") {
		label := strings.TrimSpace(m.provider)
		if strings.TrimSpace(m.model) != "" {
			label = strings.TrimSpace(m.model)
		}
		values = append(values, label)
	}
	if profile := strings.TrimSpace(m.profile); profile != "" && !strings.EqualFold(profile, "default") {
		values = append(values, profile)
	}
	if threadID := strings.TrimSpace(m.currentSessionID); threadID != "" {
		values = append(values, "thread "+shortThreadID(threadID))
	}
	if workspace := strings.TrimSpace(m.workspace); workspace != "" && workspace != "." {
		values = append(values, filepath.Base(workspace))
	}
	if m.streaming && !m.runStartedAt.IsZero() {
		values = append(values, formatElapsed(m.runStartedAt, m.now()))
	}
	return strings.Join(values, " │ ")
}

func (m chatModel) runtimeStateLabel() string {
	switch {
	case m.pendAsk != nil && m.askForm != nil && m.pendAsk.request.Type == intr.InputConfirm && m.pendAsk.request.Approval != nil:
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
		return truncateDisplayWidth(msg, 40)
	}
	if phase := strings.TrimSpace(progressPhaseLabel(m.progress.Phase)); phase != "" {
		return phase
	}
	if status := strings.TrimSpace(progressStatusLabel(m.progress.Status)); status != "" {
		return strings.ToLower(status)
	}
	return ""
}
