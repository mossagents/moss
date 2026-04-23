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
	MetadataThreadSwarmRunID       = "thread_swarm_run_id"
	MetadataThreadRole             = "thread_role"
)

func ensureSessionMetadata(sess *Session) {
	if sess == nil {
		return
	}
	sess.mu.Lock()
	if sess.Config.Metadata == nil {
		sess.Config.Metadata = make(map[string]any)
	}
	sess.mu.Unlock()
}

func SetThreadSource(sess *Session, source string) {
	if sess == nil {
		return
	}
	source = strings.TrimSpace(source)
	if source == "" {
		sess.DeleteMetadata(MetadataThreadSource)
		return
	}
	sess.SetMetadata(MetadataThreadSource, source)
}

func SetThreadParent(sess *Session, parentID string) {
	if sess == nil {
		return
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		sess.DeleteMetadata(MetadataThreadParentID)
		return
	}
	sess.SetMetadata(MetadataThreadParentID, parentID)
}

func SetThreadTaskID(sess *Session, taskID string) {
	if sess == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		sess.DeleteMetadata(MetadataThreadTaskID)
		return
	}
	sess.SetMetadata(MetadataThreadTaskID, taskID)
}

func SetThreadSwarmRunID(sess *Session, runID string) {
	if sess == nil {
		return
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		sess.DeleteMetadata(MetadataThreadSwarmRunID)
		return
	}
	sess.SetMetadata(MetadataThreadSwarmRunID, runID)
}

func SetThreadRole(sess *Session, role string) {
	if sess == nil {
		return
	}
	role = strings.TrimSpace(role)
	if role == "" {
		sess.DeleteMetadata(MetadataThreadRole)
		return
	}
	sess.SetMetadata(MetadataThreadRole, role)
}

func SetThreadArchived(sess *Session, archived bool) {
	if sess == nil {
		return
	}
	if !archived {
		sess.DeleteMetadata(MetadataThreadArchived)
		return
	}
	sess.SetMetadata(MetadataThreadArchived, true)
}

func TouchThreadActivity(sess *Session, when time.Time, kind string) {
	if sess == nil {
		return
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}
	sess.SetMetadata(MetadataThreadLastActivityAt, when.UTC().Format(time.RFC3339Nano))
	kind = strings.TrimSpace(kind)
	if kind == "" {
		sess.DeleteMetadata(MetadataThreadLastActivityKind)
		return
	}
	sess.SetMetadata(MetadataThreadLastActivityKind, kind)
}

func SetThreadPreview(sess *Session, preview string) {
	if sess == nil {
		return
	}
	preview = trimPreview(preview, 240)
	if preview == "" {
		sess.DeleteMetadata(MetadataThreadPreview)
		return
	}
	sess.SetMetadata(MetadataThreadPreview, preview)
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
	if v, ok := sess.GetMetadata(MetadataThreadPreview); ok {
		if preview, _ := v.(string); preview != "" {
			return trimPreview(preview, 240)
		}
	}
	msgs := sess.CopyMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
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
	if v, ok := sess.GetMetadata(MetadataThreadLastActivityAt); ok {
		if raw, _ := v.(string); raw != "" {
			if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				return ts.UTC()
			}
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

func ThreadMetadataValues(sess *Session) (source, parentID, taskID, swarmRunID, role, preview, activityKind string, archived bool, activityAt time.Time) {
	if sess == nil {
		return "", "", "", "", "", "", "", false, time.Time{}
	}
	meta := sess.CopyMetadata()
	return metadataString(meta, MetadataThreadSource),
		metadataString(meta, MetadataThreadParentID),
		metadataString(meta, MetadataThreadTaskID),
		metadataString(meta, MetadataThreadSwarmRunID),
		metadataString(meta, MetadataThreadRole),
		ThreadPreview(sess),
		metadataString(meta, MetadataThreadLastActivityKind),
		metadataBool(meta, MetadataThreadArchived),
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
