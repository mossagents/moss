package builtins

import (
	"encoding/json"
	"fmt"
	"net/url"
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

// RequireApprovalForHTTPMethod 对不在允许集合中的 HTTP method 触发审批。
func RequireApprovalForHTTPMethod(methods ...string) PolicyRule {
	allowed := normalizeUpperSet(methods)
	return func(ctx PolicyContext) PolicyResult {
		if ctx.Tool.Name != "http_request" || len(allowed) == 0 {
			return allowResult()
		}
		method := extractHTTPMethod(ctx.Input)
		if method == "" || containsSet(allowed, method) {
			return allowResult()
		}
		return requireApprovalResult("network.http_method_requires_approval", fmt.Sprintf("http method %s requires approval", method))
	}
}

// RequireApprovalForURLHost 对不在允许集合中的 URL host 触发审批。
func RequireApprovalForURLHost(hosts ...string) PolicyRule {
	allowed := normalizeLowerSet(hosts)
	return func(ctx PolicyContext) PolicyResult {
		if ctx.Tool.Name != "http_request" || len(allowed) == 0 {
			return allowResult()
		}
		host := extractURLHost(ctx.Input)
		if host == "" || containsSet(allowed, host) {
			return allowResult()
		}
		return requireApprovalResult("network.host_requires_approval", fmt.Sprintf("url host %s requires approval", host))
	}
}

// DenyURLHost 直接拒绝命中的 URL host。
func DenyURLHost(hosts ...string) PolicyRule {
	denied := normalizeLowerSet(hosts)
	return func(ctx PolicyContext) PolicyResult {
		if ctx.Tool.Name != "http_request" || len(denied) == 0 {
			return allowResult()
		}
		host := extractURLHost(ctx.Input)
		if host == "" || !containsSet(denied, host) {
			return allowResult()
		}
		return denyResult("network.host_denied", fmt.Sprintf("url host %s is denied by policy", host))
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

func extractURLHost(input json.RawMessage) string {
	rawURL := extractStringField(input, "url")
	if rawURL == "" {
		return ""
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

func extractHTTPMethod(input json.RawMessage) string {
	method := strings.ToUpper(strings.TrimSpace(extractStringField(input, "method")))
	if method == "" {
		return "GET"
	}
	return method
}

func extractPolicyInputDetails(toolName string, input json.RawMessage) map[string]any {
	switch toolName {
	case "http_request":
		details := map[string]any{}
		if host := extractURLHost(input); host != "" {
			details["host"] = host
		}
		if method := extractHTTPMethod(input); method != "" {
			details["method"] = method
		}
		if rawURL := extractStringField(input, "url"); rawURL != "" {
			details["url"] = rawURL
		}
		if len(details) > 0 {
			return details
		}
	}
	return nil
}

func normalizeUpperSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func normalizeLowerSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func containsSet(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}
