package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

type normalizedTrace struct {
	Lines        []string
	Participant  []string
	SourceCount  int
	MessageCount int
}


func normalizeTrace(raw string) (*normalizedTrace, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("trace is required")
	}
	if strings.HasPrefix(raw, "[") {
		var items []map[string]any
		if err := json.Unmarshal([]byte(raw), &items); err != nil {
			return nil, fmt.Errorf("parse trace array: %w", err)
		}
		return normalizeTraceObjects(items)
	}
	lines := strings.Split(raw, "\n")
	items := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || (!strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[")) {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			continue
		}
		switch current := value.(type) {
		case map[string]any:
			items = append(items, current)
		case []any:
			for _, child := range current {
				obj, ok := child.(map[string]any)
				if ok {
					items = append(items, obj)
				}
			}
		}
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no valid trace items")
	}
	return normalizeTraceObjects(items)
}

func normalizeTraceObjects(items []map[string]any) (*normalizedTrace, error) {
	out := &normalizedTrace{
		Lines:       make([]string, 0, len(items)),
		Participant: make([]string, 0, 4),
		SourceCount: len(items),
	}
	seenRoles := make(map[string]struct{}, 4)
	for _, item := range items {
		for _, payload := range flattenTracePayload(item) {
			line, role := normalizeTracePayload(payload)
			if line == "" {
				continue
			}
			if role != "" {
				if _, ok := seenRoles[role]; !ok {
					seenRoles[role] = struct{}{}
					out.Participant = append(out.Participant, role)
				}
				out.MessageCount++
			}
			out.Lines = append(out.Lines, line)
		}
	}
	if len(out.Lines) == 0 {
		return nil, fmt.Errorf("no valid trace items after normalization")
	}
	return out, nil
}

func flattenTracePayload(item map[string]any) []map[string]any {
	if item == nil {
		return nil
	}
	if payload, ok := item["payload"]; ok {
		switch current := payload.(type) {
		case map[string]any:
			return []map[string]any{current}
		case []any:
			out := make([]map[string]any, 0, len(current))
			for _, child := range current {
				obj, ok := child.(map[string]any)
				if ok {
					out = append(out, obj)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return []map[string]any{item}
}

func normalizeTracePayload(item map[string]any) (string, string) {
	itemType := strings.TrimSpace(stringValue(item["type"]))
	role := strings.TrimSpace(stringValue(item["role"]))
	content := collectTraceContent(item)
	if content == "" {
		content = strings.TrimSpace(stringValue(item["summary"]))
	}
	if content == "" {
		return "", role
	}
	label := strings.TrimSpace(strings.Join([]string{itemType, role}, " "))
	label = strings.Join(strings.Fields(label), " ")
	content = strings.Join(strings.Fields(content), " ")
	if label == "" {
		return content, role
	}
	return label + ": " + content, role
}

func collectTraceContent(item map[string]any) string {
	parts := make([]string, 0, 4)
	parts = appendTraceContent(parts, item["content"])
	if len(parts) == 0 {
		parts = appendTraceContent(parts, item["text"])
	}
	if len(parts) == 0 {
		parts = appendTraceContent(parts, item["message"])
	}
	return strings.Join(parts, " ")
}

func appendTraceContent(dst []string, value any) []string {
	switch current := value.(type) {
	case string:
		current = strings.TrimSpace(current)
		if current != "" {
			dst = append(dst, current)
		}
	case []any:
		for _, child := range current {
			dst = appendTraceContent(dst, child)
		}
	case map[string]any:
		for _, key := range []string{"text", "content", "message", "input", "output"} {
			dst = appendTraceContent(dst, current[key])
		}
	}
	return dst
}

func stringValue(value any) string {
	switch current := value.(type) {
	case string:
		return current
	case json.Number:
		return current.String()
	case fmt.Stringer:
		return current.String()
	default:
		return ""
	}
}
