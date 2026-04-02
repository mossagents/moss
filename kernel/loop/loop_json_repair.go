package loop

import (
	"bytes"
	"encoding/json"
	"strings"
)

func repairToolArguments(args json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	repaired := repairTruncatedJSON(trimmed)
	if json.Valid([]byte(repaired)) {
		return json.RawMessage(repaired)
	}
	return args
}

func previewToolArguments(args json.RawMessage) string {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "{}" {
		return ""
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(trimmed)); err == nil {
		trimmed = compact.String()
	}
	if len(trimmed) > 160 {
		return trimmed[:160] + "..."
	}
	return trimmed
}

func repairTruncatedJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	stack := make([]rune, 0, 8)
	inString := false
	escaped := false

	for _, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, r)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if inString && escaped {
		s += `\`
		escaped = false
	}
	if inString {
		s += `"`
	}
	s = strings.TrimRight(s, ", \t\r\n")
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
}
