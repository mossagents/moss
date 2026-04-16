package state

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/harness/stringutil"
	"github.com/mossagents/moss/kernel/observe"
)

func StateEntryFromExecutionEvent(event observe.ExecutionEvent) StateEntry {
	recordID := strings.TrimSpace(event.EventID)
	if recordID == "" {
		recordID = strings.TrimSpace(event.CallID)
	}
	if recordID == "" {
		recordID = fmt.Sprintf("%d-%s-%s", event.Timestamp.UTC().UnixNano(), event.Type, stringutil.FirstNonEmpty(strings.TrimSpace(event.ToolName), strings.TrimSpace(event.Model)))
	}
	title := string(event.Type)
	if strings.TrimSpace(event.ToolName) != "" {
		title = title + ":" + strings.TrimSpace(event.ToolName)
	}
	return StateEntry{
		Kind:      StateKindExecutionEvent,
		RecordID:  recordID,
		SessionID: strings.TrimSpace(event.SessionID),
		Status:    strings.TrimSpace(event.Error),
		Title:     title,
		Summary:   stringutil.FirstNonEmpty(strings.TrimSpace(event.ReasonCode), strings.TrimSpace(event.Risk)),
		SearchText: normalizeStateText(
			string(event.Type),
			event.SessionID,
			event.RunID,
			event.TurnID,
			event.Phase,
			event.PayloadKind,
			event.ToolName,
			event.Model,
			event.ReasonCode,
			event.Risk,
			event.Error,
		),
		SortTime:  event.Timestamp.UTC(),
		CreatedAt: event.Timestamp.UTC(),
		UpdatedAt: event.Timestamp.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"event_id":      event.EventID,
			"event_version": event.EventVersion,
			"run_id":        event.RunID,
			"turn_id":       event.TurnID,
			"parent_id":     event.ParentID,
			"event_type":    event.Type,
			"phase":         event.Phase,
			"actor":         event.Actor,
			"payload_kind":  event.PayloadKind,
			"tool_name":     event.ToolName,
			"model":         event.Model,
			"risk":          event.Risk,
			"reason_code":   event.ReasonCode,
			"enforcement":   event.Enforcement,
			"duration_ms":   event.Duration.Milliseconds(),
			"data":          event.Metadata,
		}),
	}
}

func (c *StateCatalog) AppendExecutionEvent(event observe.ExecutionEvent) error {
	if !c.Enabled() {
		return nil
	}
	record := executionEventJournalRecord(event)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := appendJSONL(filepath.Join(c.eventDir, event.Timestamp.UTC().Format("20060102")+".jsonl"), record); err != nil {
		c.lastError = err.Error()
		return err
	}
	entry := StateEntryFromExecutionEvent(event)
	c.entries[stateEntryKey(entry.Kind, entry.RecordID)] = entry
	c.updatedAt = time.Now().UTC()
	if err := c.persistLocked(); err != nil {
		c.lastError = err.Error()
		return err
	}
	c.lastError = ""
	return nil
}

func executionEventJournalRecord(event observe.ExecutionEvent) map[string]any {
	return map[string]any{
		"event_id":      event.EventID,
		"event_version": event.EventVersion,
		"run_id":        event.RunID,
		"turn_id":       event.TurnID,
		"parent_id":     event.ParentID,
		"type":          event.Type,
		"session_id":    event.SessionID,
		"timestamp":     event.Timestamp.UTC(),
		"phase":         event.Phase,
		"actor":         event.Actor,
		"payload_kind":  event.PayloadKind,
		"tool_name":     event.ToolName,
		"call_id":       event.CallID,
		"risk":          event.Risk,
		"reason_code":   event.ReasonCode,
		"enforcement":   event.Enforcement,
		"model":         event.Model,
		"duration_ms":   event.Duration.Milliseconds(),
		"error":         event.Error,
		"data":          event.Metadata,
	}
}

type stateCatalogObserver struct {
	observe.NoOpObserver
	catalog *StateCatalog
}

func NewStateCatalogObserver(catalog *StateCatalog) observe.Observer {
	if catalog == nil || !catalog.Enabled() {
		return nil
	}
	return &stateCatalogObserver{catalog: catalog}
}

func (o *stateCatalogObserver) OnExecutionEvent(_ context.Context, event observe.ExecutionEvent) {
	if err := o.catalog.AppendExecutionEvent(event); err != nil {
		o.catalog.MarkError(err)
	}
}

