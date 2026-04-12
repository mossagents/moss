package model

import (
	"encoding/json"
	"strings"
)

// RepairToolCallArguments best-effort repairs malformed tool-call arguments
// while preserving existing object/array semantics whenever possible.
func RepairToolCallArguments(args json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	repaired := repairTruncatedToolCallJSON(trimmed)
	if json.Valid([]byte(repaired)) {
		return json.RawMessage(repaired)
	}
	return args
}

// NormalizeToolCallArguments guarantees the returned payload is valid JSON so
// tool-call history can always be persisted safely.
func NormalizeToolCallArguments(args json.RawMessage) json.RawMessage {
	repaired := RepairToolCallArguments(args)
	if json.Valid([]byte(repaired)) {
		return append(json.RawMessage(nil), repaired...)
	}
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" {
		return json.RawMessage(`{}`)
	}
	encoded, err := json.Marshal(trimmed)
	if err != nil {
		return json.RawMessage(`""`)
	}
	return json.RawMessage(encoded)
}

// NormalizeToolCalls returns a copy of tool calls with persistence-safe
// argument payloads.
func NormalizeToolCalls(calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, len(calls))
	for i, call := range calls {
		out[i] = call
		if len(call.Arguments) > 0 {
			out[i].Arguments = NormalizeToolCallArguments(call.Arguments)
		}
	}
	return out
}

func repairTruncatedToolCallJSON(s string) string {
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
		// Preserve the observed trailing backslash as a literal character rather
		// than guessing which escape sequence the model intended to finish.
		s += `\`
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
