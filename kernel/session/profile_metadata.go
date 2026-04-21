package session

import (
	"strings"
)

const (
	MetadataEffectiveTrust    = "effective_trust"
	MetadataEffectiveApproval = "effective_approval"
	MetadataTaskMode          = "task_mode"
)

func metadataString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	value, ok := meta[key]
	if !ok {
		return ""
	}
	actual, _ := value.(string)
	return strings.TrimSpace(actual)
}

func ProfileMetadataValues(sess *Session) (effectiveTrust, effectiveApproval, taskMode string) {
	if sess == nil {
		return "", "", ""
	}
	meta := sess.CopyMetadata()
	return metadataString(meta, MetadataEffectiveTrust),
		metadataString(meta, MetadataEffectiveApproval),
		metadataString(meta, MetadataTaskMode)
}
