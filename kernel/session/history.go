package session

import (
	"strconv"
	"strings"
)

const (
	// HistoryHiddenMetadataKey 标记该 Session 为内部持久化工件，不应出现在普通历史列表中。
	HistoryHiddenMetadataKey = "history_hidden"

	// checkpointSnapshotHiddenMetadataKey 兼容既有 checkpoint snapshot 隐藏语义。
	checkpointSnapshotHiddenMetadataKey = "checkpoint_snapshot_hidden"
)

// MarkHistoryHidden 标记一个 Session 为内部历史快照。
func MarkHistoryHidden(sess *Session) {
	if sess == nil {
		return
	}
	if sess.Config.Metadata == nil {
		sess.Config.Metadata = make(map[string]any)
	}
	sess.Config.Metadata[HistoryHiddenMetadataKey] = true
}

// VisibleInHistory 返回 Session 是否应出现在普通历史查询中。
func VisibleInHistory(sess *Session) bool {
	if sess == nil {
		return false
	}
	return !metadataBool(sess.Config.Metadata, checkpointSnapshotHiddenMetadataKey) &&
		!metadataBool(sess.Config.Metadata, HistoryHiddenMetadataKey)
}

func metadataBool(metadata map[string]any, key string) bool {
	if len(metadata) == 0 {
		return false
	}
	raw, ok := metadata[key]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && parsed
	default:
		return false
	}
}
