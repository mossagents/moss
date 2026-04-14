package session

import (
	"encoding/json"
	"strings"
)

const (
	MetadataToolPolicy        = "tool_policy"
	MetadataToolPolicySummary = "tool_policy_summary"

	ToolPolicyMetadataVersion = 1
)

type ToolPolicySummary struct {
	Version               int      `json:"version"`
	Trust                 string   `json:"trust"`
	ApprovalMode          string   `json:"approval_mode"`
	CommandAccess         string   `json:"command_access"`
	HTTPAccess            string   `json:"http_access"`
	WorkspaceWriteAccess  string   `json:"workspace_write_access"`
	MemoryWriteAccess     string   `json:"memory_write_access"`
	GraphMutationAccess   string   `json:"graph_mutation_access"`
	ProtectedPathPrefixes []string `json:"protected_path_prefixes,omitempty"`
	ApprovalRequiredClasses []string `json:"approval_required_classes,omitempty"`
	DeniedClasses         []string `json:"denied_classes,omitempty"`
}

func EncodeToolPolicySummary(summary ToolPolicySummary) map[string]any {
	if summary.Version <= 0 {
		summary.Version = ToolPolicyMetadataVersion
	}
	return map[string]any{
		"version":                   summary.Version,
		"trust":                     strings.TrimSpace(summary.Trust),
		"approval_mode":             strings.TrimSpace(summary.ApprovalMode),
		"command_access":            strings.TrimSpace(summary.CommandAccess),
		"http_access":               strings.TrimSpace(summary.HTTPAccess),
		"workspace_write_access":    strings.TrimSpace(summary.WorkspaceWriteAccess),
		"memory_write_access":       strings.TrimSpace(summary.MemoryWriteAccess),
		"graph_mutation_access":     strings.TrimSpace(summary.GraphMutationAccess),
		"protected_path_prefixes":   append([]string(nil), summary.ProtectedPathPrefixes...),
		"approval_required_classes": append([]string(nil), summary.ApprovalRequiredClasses...),
		"denied_classes":            append([]string(nil), summary.DeniedClasses...),
	}
}

func ToolPolicySummaryFromSession(sess *Session) (ToolPolicySummary, bool) {
	if sess == nil {
		return ToolPolicySummary{}, false
	}
	value, ok := sess.GetMetadata(MetadataToolPolicySummary)
	if !ok {
		return ToolPolicySummary{}, false
	}
	return decodeToolPolicySummary(value)
}

func ToolPolicySummaryFromMetadata(meta map[string]any) (ToolPolicySummary, bool) {
	if meta == nil {
		return ToolPolicySummary{}, false
	}
	value, ok := meta[MetadataToolPolicySummary]
	if !ok {
		return ToolPolicySummary{}, false
	}
	return decodeToolPolicySummary(value)
}

func decodeToolPolicySummary(value any) (ToolPolicySummary, bool) {
	if value == nil {
		return ToolPolicySummary{}, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ToolPolicySummary{}, false
	}
	var summary ToolPolicySummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return ToolPolicySummary{}, false
	}
	if summary.Version <= 0 {
		return ToolPolicySummary{}, false
	}
	summary.Trust = strings.TrimSpace(summary.Trust)
	summary.ApprovalMode = strings.TrimSpace(summary.ApprovalMode)
	summary.CommandAccess = strings.TrimSpace(summary.CommandAccess)
	summary.HTTPAccess = strings.TrimSpace(summary.HTTPAccess)
	summary.WorkspaceWriteAccess = strings.TrimSpace(summary.WorkspaceWriteAccess)
	summary.MemoryWriteAccess = strings.TrimSpace(summary.MemoryWriteAccess)
	summary.GraphMutationAccess = strings.TrimSpace(summary.GraphMutationAccess)
	summary.ProtectedPathPrefixes = cloneCompactStrings(summary.ProtectedPathPrefixes)
	summary.ApprovalRequiredClasses = cloneCompactStrings(summary.ApprovalRequiredClasses)
	summary.DeniedClasses = cloneCompactStrings(summary.DeniedClasses)
	return summary, true
}

func cloneCompactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
