package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
)

func normalizeTraceItems(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("trace is required")
	}
	if strings.HasPrefix(raw, "[") {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, fmt.Errorf("parse trace array: %w", err)
		}
		return normalizeTraceObjects(arr), nil
	}
	lines := strings.Split(raw, "\n")
	objs := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		objs = append(objs, obj)
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("no valid trace items")
	}
	return normalizeTraceObjects(objs), nil
}

func normalizeTraceObjects(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		typ, _ := item["type"].(string)
		role, _ := item["role"].(string)
		content, _ := item["content"].(string)
		if content == "" {
			if payload, ok := item["payload"].(map[string]any); ok {
				if c, ok := payload["content"].(string); ok {
					content = c
				}
			}
		}
		line := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(typ), strings.TrimSpace(role), strings.TrimSpace(content)}, " "))
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
