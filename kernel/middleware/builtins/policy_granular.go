package builtins

import (
	"encoding/json"
	"strings"

	"github.com/mossagents/moss/kernel/tool"
)

// RequireApprovalForPathPrefix 对路径参数命中指定前缀的调用触发审批。
func RequireApprovalForPathPrefix(prefixes ...string) PolicyRule {
	normalized := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		p = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(p, "\\", "/")))
		if p == "" {
			continue
		}
		normalized = append(normalized, p)
	}
	return func(_ tool.ToolSpec, input json.RawMessage) PolicyDecision {
		path := extractStringField(input, "path")
		if path == "" {
			return Allow
		}
		clean := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
		for _, p := range normalized {
			if strings.HasPrefix(clean, p) {
				return RequireApproval
			}
		}
		return Allow
	}
}

// DenyCommandContaining 对 run_command 的危险片段直接拒绝。
func DenyCommandContaining(fragments ...string) PolicyRule {
	parts := make([]string, 0, len(fragments))
	for _, f := range fragments {
		f = strings.TrimSpace(strings.ToLower(f))
		if f == "" {
			continue
		}
		parts = append(parts, f)
	}
	return func(spec tool.ToolSpec, input json.RawMessage) PolicyDecision {
		if spec.Name != "run_command" {
			return Allow
		}
		command := strings.ToLower(extractStringField(input, "command"))
		for _, f := range parts {
			if strings.Contains(command, f) {
				return Deny
			}
		}
		return Allow
	}
}

func extractStringField(input json.RawMessage, field string) string {
	if len(input) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(input, &obj); err != nil {
		return ""
	}
	raw, ok := obj[field]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

