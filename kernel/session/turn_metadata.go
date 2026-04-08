package session

import (
	"strings"
)

const (
	MetadataRunID              = "run_id"
	MetadataTurnID             = "turn_id"
	MetadataInstructionProfile = "instruction_profile"
	MetadataPromptVersion      = "prompt_version"
	MetadataPromptAssembly     = "prompt_assembly"
	MetadataModelLane          = "model_lane"
	MetadataVisibleTools       = "visible_tools"
	MetadataHiddenTools        = "hidden_tools"
	MetadataToolRouteDigest    = "tool_route_digest"
)

func metadataStrings(meta map[string]any, key string) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return nil
	}
	items := []string{}
	switch values := raw.(type) {
	case []string:
		items = append(items, values...)
	case []any:
		for _, value := range values {
			if text, ok := value.(string); ok {
				items = append(items, text)
			}
		}
	default:
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func MetadataValueString(meta map[string]any, key string) string {
	return metadataString(meta, key)
}

func MetadataValuesStrings(meta map[string]any, key string) []string {
	return metadataStrings(meta, key)
}
