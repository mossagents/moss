package builtins

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type CommandPatternRule struct {
	Name   string
	Match  string
	Access PolicyDecision
}

type HTTPPatternRule struct {
	Name    string
	Match   string
	Methods []string
	Access  PolicyDecision
}

type compiledCommandPatternRule struct {
	name   string
	match  string
	access PolicyDecision
	regex  *regexp.Regexp
}

type compiledHTTPPatternRule struct {
	name    string
	match   string
	methods map[string]struct{}
	access  PolicyDecision
	regex   *regexp.Regexp
}

func CommandRules(rules ...CommandPatternRule) PolicyRule {
	compiled := compileCommandPatternRules(rules)
	return func(ctx PolicyContext) PolicyResult {
		if ctx.Tool.Name != "run_command" || len(compiled) == 0 {
			return allowResult()
		}
		command, line := extractCommandTargets(ctx.Input)
		if command == "" && line == "" {
			return allowResult()
		}
		result := allowResult()
		for _, rule := range compiled {
			if !rule.matches(command, line) {
				continue
			}
			switch rule.access {
			case Deny:
				result := denyResult("command.rule_denied", rule.message("denied command execution"))
				result.Meta = rule.meta("command")
				return result
			case RequireApproval:
				result = requireApprovalResult("command.rule_requires_approval", rule.message("requires approval"))
				result.Meta = rule.meta("command")
			default:
				result.Meta = rule.meta("command")
			}
		}
		return result
	}
}

func HTTPRules(rules ...HTTPPatternRule) PolicyRule {
	compiled := compileHTTPPatternRules(rules)
	return func(ctx PolicyContext) PolicyResult {
		if ctx.Tool.Name != "http_request" || len(compiled) == 0 {
			return allowResult()
		}
		rawURL, host, method := extractHTTPRuleTargets(ctx.Input)
		if rawURL == "" && host == "" {
			return allowResult()
		}
		result := allowResult()
		for _, rule := range compiled {
			if !rule.matches(rawURL, host, method) {
				continue
			}
			switch rule.access {
			case Deny:
				out := denyResult("http.rule_denied", rule.message("denied http request"))
				out.Meta = rule.meta(method)
				return out
			case RequireApproval:
				result = requireApprovalResult("http.rule_requires_approval", rule.message("requires approval"))
				result.Meta = rule.meta(method)
			default:
				result.Meta = rule.meta(method)
			}
		}
		return result
	}
}

func compileCommandPatternRules(rules []CommandPatternRule) []compiledCommandPatternRule {
	out := make([]compiledCommandPatternRule, 0, len(rules))
	for _, rule := range rules {
		match := strings.TrimSpace(rule.Match)
		if match == "" {
			continue
		}
		out = append(out, compiledCommandPatternRule{
			name:   strings.TrimSpace(rule.Name),
			match:  match,
			access: rule.Access,
			regex:  globToRegex(match),
		})
	}
	return out
}

func (r compiledCommandPatternRule) matches(command, line string) bool {
	if r.regex == nil {
		return false
	}
	return r.regex.MatchString(command) || r.regex.MatchString(line)
}

func (r compiledCommandPatternRule) message(suffix string) string {
	label := strings.TrimSpace(r.name)
	if label == "" {
		label = strings.TrimSpace(r.match)
	}
	return fmt.Sprintf("command rule %q %s", label, suffix)
}

func (r compiledCommandPatternRule) meta(kind string) map[string]any {
	policyRule := strings.TrimSpace(r.name)
	if policyRule == "" {
		policyRule = strings.TrimSpace(r.match)
	}
	return map[string]any{
		"policy_rule": policyRule,
		"rule_kind":   kind,
		"rule_name":   strings.TrimSpace(r.name),
		"rule_match":  strings.TrimSpace(r.match),
		"rule_action": strings.ToLower(string(r.access)),
	}
}

func (r compiledHTTPPatternRule) matches(rawURL, host, method string) bool {
	if r.regex == nil {
		return false
	}
	if len(r.methods) > 0 {
		if _, ok := r.methods[method]; !ok {
			return false
		}
	}
	return r.regex.MatchString(rawURL) || r.regex.MatchString(host)
}

func (r compiledHTTPPatternRule) message(suffix string) string {
	label := strings.TrimSpace(r.name)
	if label == "" {
		label = strings.TrimSpace(r.match)
	}
	return fmt.Sprintf("http rule %q %s", label, suffix)
}

func (r compiledHTTPPatternRule) meta(method string) map[string]any {
	policyRule := strings.TrimSpace(r.name)
	if policyRule == "" {
		policyRule = strings.TrimSpace(r.match)
	}
	meta := map[string]any{
		"policy_rule": policyRule,
		"rule_kind":   "http",
		"rule_name":   strings.TrimSpace(r.name),
		"rule_match":  strings.TrimSpace(r.match),
		"rule_action": strings.ToLower(string(r.access)),
	}
	if method != "" {
		meta["rule_method"] = method
	}
	return meta
}

func globToRegex(pattern string) *regexp.Regexp {
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	if pattern == "" {
		return nil
	}
	replaced := regexp.QuoteMeta(pattern)
	replaced = strings.ReplaceAll(replaced, "\\*", ".*")
	replaced = strings.ReplaceAll(replaced, "\\?", ".")
	return regexp.MustCompile("^" + replaced + "$")
}

func extractCommandTargets(input json.RawMessage) (command, line string) {
	if len(input) == 0 {
		return "", ""
	}
	var payload struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", ""
	}
	command = strings.ToLower(strings.TrimSpace(payload.Command))
	parts := []string{}
	if command != "" {
		parts = append(parts, command)
	}
	for _, arg := range payload.Args {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			parts = append(parts, strings.ToLower(arg))
		}
	}
	line = strings.Join(parts, " ")
	return command, line
}

func compileHTTPPatternRules(rules []HTTPPatternRule) []compiledHTTPPatternRule {
	out := make([]compiledHTTPPatternRule, 0, len(rules))
	for _, rule := range rules {
		match := strings.TrimSpace(rule.Match)
		if match == "" {
			continue
		}
		methods := make(map[string]struct{}, len(rule.Methods))
		for _, method := range rule.Methods {
			method = strings.ToUpper(strings.TrimSpace(method))
			if method != "" {
				methods[method] = struct{}{}
			}
		}
		out = append(out, compiledHTTPPatternRule{
			name:    strings.TrimSpace(rule.Name),
			match:   match,
			methods: methods,
			access:  rule.Access,
			regex:   globToRegex(match),
		})
	}
	return out
}

func extractHTTPRuleTargets(input json.RawMessage) (rawURL, host, method string) {
	if len(input) == 0 {
		return "", "", ""
	}
	var payload struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return "", "", ""
	}
	rawURL = strings.ToLower(strings.TrimSpace(payload.URL))
	method = strings.ToUpper(strings.TrimSpace(payload.Method))
	if method == "" {
		method = "GET"
	}
	if rawURL == "" {
		return "", "", method
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		host = strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	}
	return rawURL, host, method
}
