package builtins

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type CommandPatternRule struct {
	Name   string
	Match  string
	Access PolicyDecision
}

type compiledCommandPatternRule struct {
	name   string
	match  string
	access PolicyDecision
	regex  *regexp.Regexp
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
				return denyResult("command.rule_denied", rule.message("denied command execution"))
			case RequireApproval:
				result = requireApprovalResult("command.rule_requires_approval", rule.message("requires approval"))
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
