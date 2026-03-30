package builtins

import (
	"encoding/json"
	"strings"
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
	return func(ctx PolicyContext) PolicyResult {
		path := extractStringField(ctx.Input, "path")
		if path == "" {
			return allowResult()
		}
		clean := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
		for _, p := range normalized {
			if strings.HasPrefix(clean, p) {
				return requireApprovalResult("path.protected_prefix", "path is protected and requires approval")
			}
		}
		return allowResult()
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
	return func(ctx PolicyContext) PolicyResult {
		if ctx.Tool.Name != "run_command" {
			return allowResult()
		}
		command := strings.ToLower(extractStringField(ctx.Input, "command"))
		for _, f := range parts {
			if strings.Contains(command, f) {
				return denyResult("command.fragment_denied", "command contains denied fragment")
			}
		}
		return allowResult()
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
