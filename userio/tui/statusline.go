package tui

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/appkit/product"
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
	items := m.statusLineItems
	if !m.experimentalEnabled(product.ExperimentalStatuslineCustomization) {
		items = defaultStatusLineItems
	}
	values := make([]string, 0, len(items))
	for _, item := range normalizeStatusLineItems(items) {
		switch item {
		case "state":
			values = append(values, "state="+valueOrDefaultRunState(m.streaming))
		case "model":
			label := strings.TrimSpace(m.provider)
			if strings.TrimSpace(m.model) != "" {
				label += " (" + strings.TrimSpace(m.model) + ")"
			}
			values = append(values, "model="+label)
		case "workspace":
			values = append(values, "workspace="+valueOrDefaultString(m.workspace, "."))
		case "profile":
			values = append(values, "profile="+valueOrDefaultString(m.profile, "default"))
		case "trust":
			values = append(values, "trust="+valueOrDefaultString(m.trust, "trusted"))
		case "approval":
			values = append(values, "approval="+valueOrDefaultString(m.approvalMode, "confirm"))
		case "thread":
			values = append(values, "thread="+valueOrDefaultString(m.currentSessionID, "(none)"))
		case "messages":
			values = append(values, fmt.Sprintf("messages=%d", len(m.messages)))
		case "theme":
			values = append(values, "theme="+valueOrDefaultString(m.theme, themeDefault))
		case "personality":
			values = append(values, "personality="+valueOrDefaultString(m.personality, product.PersonalityFriendly))
		case "fast":
			values = append(values, "fast="+onOff(m.fastMode))
		case "version":
			values = append(values, "version="+appVersion)
		}
	}
	return strings.Join(values, " │ ")
}
