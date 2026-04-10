package session

import (
	"fmt"
	"github.com/mossagents/moss/kernel/model"
	"strings"
	"time"
)

const (
	MetadataThreadSource           = "thread_source"
	MetadataThreadParentID         = "thread_parent_id"
	MetadataThreadArchived         = "thread_archived"
	MetadataThreadLastActivityAt   = "thread_last_activity_at"
	MetadataThreadLastActivityKind = "thread_last_activity_kind"
	MetadataThreadPreview          = "thread_preview"
	MetadataThreadTaskID           = "thread_task_id"
)

func ensureSessionMetadata(sess *Session) map[string]any {
	if sess == nil {
		return nil
	}
	if sess.Config.Metadata == nil {
		sess.Config.Metadata = make(map[string]any)
	}
	return sess.Config.Metadata
}

func SetThreadSource(sess *Session, source string) {
	meta := ensureSessionMetadata(sess)
	if meta == nil {
		return
	}
	source = strings.TrimSpace(source)
	if source == "" {
		delete(meta, MetadataThreadSource)
		return
	}
	meta[MetadataThreadSource] = source
}

func SetThreadParent(sess *Session, parentID string) {
	meta := ensureSessionMetadata(sess)
	if meta == nil {
		return
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		delete(meta, MetadataThreadParentID)
		return
	}
	meta[MetadataThreadParentID] = parentID
}

func SetThreadTaskID(sess *Session, taskID string) {
	meta := ensureSessionMetadata(sess)
	if meta == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		delete(meta, MetadataThreadTaskID)
		return
	}
	meta[MetadataThreadTaskID] = taskID
}

func SetThreadArchived(sess *Session, archived bool) {
	meta := ensureSessionMetadata(sess)
	if meta == nil {
		return
	}
	if !archived {
		delete(meta, MetadataThreadArchived)
		return
	}
	meta[MetadataThreadArchived] = true
}

func TouchThreadActivity(sess *Session, when time.Time, kind string) {
	meta := ensureSessionMetadata(sess)
	if meta == nil {
		return
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}
	meta[MetadataThreadLastActivityAt] = when.UTC().Format(time.RFC3339Nano)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		delete(meta, MetadataThreadLastActivityKind)
		return
	}
	meta[MetadataThreadLastActivityKind] = kind
}

func SetThreadPreview(sess *Session, preview string) {
	meta := ensureSessionMetadata(sess)
	if meta == nil {
		return
	}
	preview = trimPreview(preview, 240)
	if preview == "" {
		delete(meta, MetadataThreadPreview)
		return
	}
	meta[MetadataThreadPreview] = preview
}

func RefreshThreadMetadata(sess *Session, when time.Time, kind string) {
	if sess == nil {
		return
	}
	TouchThreadActivity(sess, when, kind)
	SetThreadPreview(sess, ThreadPreview(sess))
}

func ThreadPreview(sess *Session) string {
	if sess == nil {
		return ""
	}
	if preview := metadataString(sess.Config.Metadata, MetadataThreadPreview); preview != "" {
		return trimPreview(preview, 240)
	}
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		msg := sess.Messages[i]
		if msg.Role == model.RoleSystem {
			continue
		}
		text := trimPreview(model.ContentPartsToPlainText(msg.ContentParts), 240)
		if text != "" {
			return text
		}
	}
	return ""
}

func ThreadActivityTime(sess *Session) time.Time {
	if sess == nil {
		return time.Time{}
	}
	if raw := metadataString(sess.Config.Metadata, MetadataThreadLastActivityAt); raw != "" {
		if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
			return ts.UTC()
		}
	}
	if !sess.EndedAt.IsZero() {
		return sess.EndedAt.UTC()
	}
	if !sess.CreatedAt.IsZero() {
		return sess.CreatedAt.UTC()
	}
	return time.Time{}
}

func ThreadMetadataValues(sess *Session) (source, parentID, taskID, preview, activityKind string, archived bool, activityAt time.Time) {
	if sess == nil {
		return "", "", "", "", "", false, time.Time{}
	}
	return metadataString(sess.Config.Metadata, MetadataThreadSource),
		metadataString(sess.Config.Metadata, MetadataThreadParentID),
		metadataString(sess.Config.Metadata, MetadataThreadTaskID),
		ThreadPreview(sess),
		metadataString(sess.Config.Metadata, MetadataThreadLastActivityKind),
		metadataBool(sess.Config.Metadata, MetadataThreadArchived),
		ThreadActivityTime(sess)
}

func trimPreview(text string, limit int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	text = strings.Join(strings.Fields(text), " ")
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return fmt.Sprintf("%s...", strings.TrimSpace(text[:limit-3]))
}
