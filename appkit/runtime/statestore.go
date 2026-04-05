package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/port"
	"github.com/mossagents/moss/kernel/session"
)

const (
	stateCatalogSchemaVersion = 1
	stateCatalogStateKey      = kernel.ExtensionStateKey("statecatalog.state")
	DisableStateCatalogEnv    = "MOSSCODE_DISABLE_STATE_CATALOG"
)

type StateKind string

const (
	StateKindSession        StateKind = "session"
	StateKindCheckpoint     StateKind = "checkpoint"
	StateKindChange         StateKind = "change"
	StateKindTask           StateKind = "task"
	StateKindJob            StateKind = "job"
	StateKindJobItem        StateKind = "job_item"
	StateKindMemory         StateKind = "memory"
	StateKindExecutionEvent StateKind = "execution_event"
)

type StateEntry struct {
	Kind       StateKind       `json:"kind"`
	RecordID   string          `json:"record_id"`
	SessionID  string          `json:"session_id,omitempty"`
	Workspace  string          `json:"workspace,omitempty"`
	RepoRoot   string          `json:"repo_root,omitempty"`
	Status     string          `json:"status,omitempty"`
	Title      string          `json:"title,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	SearchText string          `json:"search_text,omitempty"`
	SortTime   time.Time       `json:"sort_time"`
	CreatedAt  time.Time       `json:"created_at,omitempty"`
	UpdatedAt  time.Time       `json:"updated_at,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type StateQuery struct {
	Kinds     []StateKind
	SessionID string
	RepoRoot  string
	Workspace string
	Status    string
	Text      string
	Since     time.Time
	Until     time.Time
	Limit     int
	Cursor    string
}

type StatePage struct {
	Items         []StateEntry `json:"items"`
	NextCursor    string       `json:"next_cursor,omitempty"`
	TotalEstimate int          `json:"total_estimate"`
}

type StateCatalogHealth struct {
	Enabled   bool      `json:"enabled"`
	Ready     bool      `json:"ready"`
	Entries   int       `json:"entries"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
	Catalog   string    `json:"catalog,omitempty"`
	EventDir  string    `json:"event_dir,omitempty"`
	Schema    int       `json:"schema"`
	Degraded  bool      `json:"degraded"`
}

type StateCatalog struct {
	mu         sync.Mutex
	enabled    bool
	catalogDir string
	eventDir   string
	catalog    string
	meta       string
	entries    map[string]StateEntry
	lastError  string
	updatedAt  time.Time
}

type stateCatalogDisk struct {
	SchemaVersion int          `json:"schema_version"`
	UpdatedAt     time.Time    `json:"updated_at"`
	Entries       []StateEntry `json:"entries"`
}

type stateCatalogMeta struct {
	SchemaVersion int       `json:"schema_version"`
	UpdatedAt     time.Time `json:"updated_at"`
	Entries       int       `json:"entries"`
	LastError     string    `json:"last_error,omitempty"`
	Degraded      bool      `json:"degraded"`
}

type stateCatalogState struct {
	catalog *StateCatalog
}

func StateCatalogEnabledFromEnv() bool {
	value := strings.TrimSpace(os.Getenv(DisableStateCatalogEnv))
	if value == "" {
		return true
	}
	value = strings.ToLower(value)
	return value != "1" && value != "true" && value != "yes" && value != "on"
}

func NewStateCatalog(catalogDir, eventDir string, enabled bool) (*StateCatalog, error) {
	catalogDir = strings.TrimSpace(catalogDir)
	eventDir = strings.TrimSpace(eventDir)
	if catalogDir == "" {
		enabled = false
	}
	if eventDir == "" && catalogDir != "" {
		eventDir = filepath.Join(catalogDir, "events")
	}
	c := &StateCatalog{
		enabled:    enabled,
		catalogDir: catalogDir,
		eventDir:   eventDir,
		catalog:    filepath.Join(catalogDir, "catalog.json"),
		meta:       filepath.Join(catalogDir, "catalog.meta.json"),
		entries:    make(map[string]StateEntry),
	}
	if !enabled {
		return c, nil
	}
	if err := os.MkdirAll(catalogDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state catalog dir: %w", err)
	}
	if err := os.MkdirAll(eventDir, 0o700); err != nil {
		return nil, fmt.Errorf("create state event dir: %w", err)
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *StateCatalog) Enabled() bool {
	return c != nil && c.enabled
}

func (c *StateCatalog) CatalogPath() string {
	if c == nil {
		return ""
	}
	return c.catalog
}

func (c *StateCatalog) EventDir() string {
	if c == nil {
		return ""
	}
	return c.eventDir
}

func (c *StateCatalog) Health() StateCatalogHealth {
	if c == nil {
		return StateCatalogHealth{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return StateCatalogHealth{
		Enabled:   c.enabled,
		Ready:     !c.enabled || c.lastError == "",
		Entries:   len(c.entries),
		LastError: c.lastError,
		UpdatedAt: c.updatedAt,
		Catalog:   c.catalog,
		EventDir:  c.eventDir,
		Schema:    stateCatalogSchemaVersion,
		Degraded:  c.lastError != "",
	}
}

func (c *StateCatalog) Upsert(entry StateEntry) error {
	if !c.Enabled() {
		return nil
	}
	if entry.Kind == "" || strings.TrimSpace(entry.RecordID) == "" {
		return fmt.Errorf("state entry kind and record id are required")
	}
	if entry.SortTime.IsZero() {
		if !entry.UpdatedAt.IsZero() {
			entry.SortTime = entry.UpdatedAt.UTC()
		} else if !entry.CreatedAt.IsZero() {
			entry.SortTime = entry.CreatedAt.UTC()
		} else {
			entry.SortTime = time.Now().UTC()
		}
	}
	entry.SortTime = entry.SortTime.UTC()
	if !entry.CreatedAt.IsZero() {
		entry.CreatedAt = entry.CreatedAt.UTC()
	}
	if !entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.UpdatedAt.UTC()
	}
	entry.SearchText = strings.ToLower(strings.TrimSpace(entry.SearchText))
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[stateEntryKey(entry.Kind, entry.RecordID)] = entry
	c.updatedAt = time.Now().UTC()
	if err := c.persistLocked(); err != nil {
		c.lastError = err.Error()
		return err
	}
	c.lastError = ""
	return nil
}

func (c *StateCatalog) BestEffortUpsert(entry StateEntry) {
	if err := c.Upsert(entry); err != nil {
		c.markError(err)
	}
}

func (c *StateCatalog) Delete(kind StateKind, recordID string) error {
	if !c.Enabled() {
		return nil
	}
	recordID = strings.TrimSpace(recordID)
	if kind == "" || recordID == "" {
		return fmt.Errorf("state delete kind and record id are required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, stateEntryKey(kind, recordID))
	c.updatedAt = time.Now().UTC()
	if err := c.persistLocked(); err != nil {
		c.lastError = err.Error()
		return err
	}
	c.lastError = ""
	return nil
}

func (c *StateCatalog) BestEffortDelete(kind StateKind, recordID string) {
	if err := c.Delete(kind, recordID); err != nil {
		c.markError(err)
	}
}

func (c *StateCatalog) Replace(entries []StateEntry) error {
	if !c.Enabled() {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]StateEntry, len(entries))
	for _, entry := range entries {
		if entry.Kind == "" || strings.TrimSpace(entry.RecordID) == "" {
			continue
		}
		if entry.SortTime.IsZero() {
			entry.SortTime = time.Now().UTC()
		}
		entry.SortTime = entry.SortTime.UTC()
		entry.SearchText = strings.ToLower(strings.TrimSpace(entry.SearchText))
		c.entries[stateEntryKey(entry.Kind, entry.RecordID)] = entry
	}
	c.updatedAt = time.Now().UTC()
	if err := c.persistLocked(); err != nil {
		c.lastError = err.Error()
		return err
	}
	c.lastError = ""
	return nil
}

func (c *StateCatalog) Query(query StateQuery) (StatePage, error) {
	if !c.Enabled() {
		return StatePage{}, fmt.Errorf("state catalog is disabled")
	}
	c.mu.Lock()
	entries := make([]StateEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}
	c.mu.Unlock()

	filtered := make([]StateEntry, 0, len(entries))
	kindSet := make(map[StateKind]struct{}, len(query.Kinds))
	for _, kind := range query.Kinds {
		kindSet[kind] = struct{}{}
	}
	text := strings.ToLower(strings.TrimSpace(query.Text))
	for _, entry := range entries {
		if len(kindSet) > 0 {
			if _, ok := kindSet[entry.Kind]; !ok {
				continue
			}
		}
		if query.SessionID != "" && entry.SessionID != strings.TrimSpace(query.SessionID) {
			continue
		}
		if query.RepoRoot != "" && entry.RepoRoot != strings.TrimSpace(query.RepoRoot) {
			continue
		}
		if query.Workspace != "" && entry.Workspace != strings.TrimSpace(query.Workspace) {
			continue
		}
		if query.Status != "" && !strings.EqualFold(entry.Status, strings.TrimSpace(query.Status)) {
			continue
		}
		if !query.Since.IsZero() && entry.SortTime.Before(query.Since.UTC()) {
			continue
		}
		if !query.Until.IsZero() && entry.SortTime.After(query.Until.UTC()) {
			continue
		}
		if text != "" && !strings.Contains(entry.SearchText, text) {
			continue
		}
		filtered = append(filtered, entry)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].SortTime.Equal(filtered[j].SortTime) {
			if filtered[i].Kind == filtered[j].Kind {
				return filtered[i].RecordID < filtered[j].RecordID
			}
			return filtered[i].Kind < filtered[j].Kind
		}
		return filtered[i].SortTime.After(filtered[j].SortTime)
	})

	start := 0
	if cursor := strings.TrimSpace(query.Cursor); cursor != "" {
		cur, err := decodeStateCursor(cursor)
		if err != nil {
			return StatePage{}, err
		}
		for i, entry := range filtered {
			if entry.RecordID == cur.RecordID && entry.Kind == cur.Kind && entry.SortTime.Equal(cur.SortTime) {
				start = i + 1
				break
			}
		}
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if start > len(filtered) {
		start = len(filtered)
	}
	end := start + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := StatePage{
		Items:         append([]StateEntry(nil), filtered[start:end]...),
		TotalEstimate: len(filtered),
	}
	if end < len(filtered) && end > start {
		page.NextCursor = encodeStateCursor(filtered[end-1])
	}
	return page, nil
}

func (c *StateCatalog) AppendExecutionEvent(event port.ExecutionEvent) error {
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

func (c *StateCatalog) markError(err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastError = err.Error()
}

func (c *StateCatalog) load() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := os.ReadFile(c.catalog)
	if err != nil {
		if os.IsNotExist(err) {
			c.updatedAt = time.Now().UTC()
			return c.persistLocked()
		}
		return fmt.Errorf("read state catalog: %w", err)
	}
	var disk stateCatalogDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("unmarshal state catalog: %w", err)
	}
	c.entries = make(map[string]StateEntry, len(disk.Entries))
	for _, entry := range disk.Entries {
		c.entries[stateEntryKey(entry.Kind, entry.RecordID)] = entry
	}
	c.updatedAt = disk.UpdatedAt
	return nil
}

func (c *StateCatalog) persistLocked() error {
	entries := make([]StateEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SortTime.Equal(entries[j].SortTime) {
			if entries[i].Kind == entries[j].Kind {
				return entries[i].RecordID < entries[j].RecordID
			}
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].SortTime.After(entries[j].SortTime)
	})
	disk := stateCatalogDisk{
		SchemaVersion: stateCatalogSchemaVersion,
		UpdatedAt:     c.updatedAt,
		Entries:       entries,
	}
	if err := writeJSONFile(c.catalog, disk); err != nil {
		return fmt.Errorf("persist state catalog: %w", err)
	}
	meta := stateCatalogMeta{
		SchemaVersion: stateCatalogSchemaVersion,
		UpdatedAt:     c.updatedAt,
		Entries:       len(entries),
		LastError:     c.lastError,
		Degraded:      c.lastError != "",
	}
	if err := writeJSONFile(c.meta, meta); err != nil {
		return fmt.Errorf("persist state catalog meta: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func appendJSONL(path string, value any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func stateEntryKey(kind StateKind, recordID string) string {
	return string(kind) + "|" + strings.TrimSpace(recordID)
}

type stateCursor struct {
	Kind     StateKind `json:"kind"`
	RecordID string    `json:"record_id"`
	SortTime time.Time `json:"sort_time"`
}

func encodeStateCursor(entry StateEntry) string {
	data, _ := json.Marshal(stateCursor{
		Kind:     entry.Kind,
		RecordID: entry.RecordID,
		SortTime: entry.SortTime.UTC(),
	})
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeStateCursor(raw string) (stateCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return stateCursor{}, fmt.Errorf("decode state cursor: %w", err)
	}
	var cursor stateCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		return stateCursor{}, fmt.Errorf("unmarshal state cursor: %w", err)
	}
	return cursor, nil
}

func normalizeStateText(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, strings.ToLower(part))
	}
	return strings.Join(filtered, " ")
}

func marshalStateMetadata(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func StateEntryFromSession(sess *session.Session) (StateEntry, bool) {
	if sess == nil || !session.VisibleInHistory(sess) {
		return StateEntry{}, false
	}
	title := strings.TrimSpace(sess.Config.Goal)
	if title == "" {
		title = sess.ID
	}
	source, parentID, preview, activityKind, archived, activityAt := session.ThreadMetadataValues(sess)
	sortTime := sessionSortTime(sess)
	if !activityAt.IsZero() {
		sortTime = activityAt.UTC()
	}
	return StateEntry{
		Kind:       StateKindSession,
		RecordID:   sess.ID,
		SessionID:  sess.ID,
		Status:     string(sess.Status),
		Title:      title,
		Summary:    firstNonEmpty(strings.TrimSpace(preview), strings.TrimSpace(sess.Config.Mode)),
		SearchText: normalizeStateText(sess.ID, sess.Config.Goal, sess.Config.Mode, string(sess.Status), source, parentID, preview, activityKind),
		SortTime:   sortTime,
		CreatedAt:  sess.CreatedAt,
		UpdatedAt:  sortTime,
		Metadata: marshalStateMetadata(map[string]any{
			"mode":        sess.Config.Mode,
			"recoverable": session.IsRecoverableStatus(sess.Status),
			"steps":       sess.Budget.UsedSteps,
			"source":      source,
			"parent_id":   parentID,
			"preview":     preview,
			"archived":    archived,
			"activity":    activityKind,
		}),
	}, true
}

func sessionSortTime(sess *session.Session) time.Time {
	if sess == nil {
		return time.Time{}
	}
	if !sess.EndedAt.IsZero() {
		return sess.EndedAt.UTC()
	}
	if !sess.CreatedAt.IsZero() {
		return sess.CreatedAt.UTC()
	}
	return time.Now().UTC()
}

func LogicalCheckpointSessionID(item *port.CheckpointRecord) string {
	if item == nil {
		return ""
	}
	sessionID := strings.TrimSpace(item.SessionID)
	for _, ref := range item.Lineage {
		if ref.Kind == port.CheckpointLineageSession && strings.TrimSpace(ref.ID) != "" {
			sessionID = strings.TrimSpace(ref.ID)
			break
		}
	}
	return sessionID
}

func StateEntryFromCheckpoint(item *port.CheckpointRecord) (StateEntry, bool) {
	if item == nil {
		return StateEntry{}, false
	}
	sessionID := LogicalCheckpointSessionID(item)
	return StateEntry{
		Kind:       StateKindCheckpoint,
		RecordID:   item.ID,
		SessionID:  sessionID,
		Status:     "created",
		Title:      firstNonEmpty(strings.TrimSpace(item.Note), item.ID),
		Summary:    fmt.Sprintf("patches=%d lineage=%d", len(item.PatchIDs), len(item.Lineage)),
		SearchText: normalizeStateText(item.ID, sessionID, item.Note),
		SortTime:   item.CreatedAt.UTC(),
		CreatedAt:  item.CreatedAt.UTC(),
		UpdatedAt:  item.CreatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"lineage_depth":        len(item.Lineage),
			"patch_count":          len(item.PatchIDs),
			"worktree_snapshot_id": item.WorktreeSnapshotID,
		}),
	}, true
}

func StateEntryFromTask(task port.TaskRecord) (StateEntry, bool) {
	if strings.TrimSpace(task.ID) == "" {
		return StateEntry{}, false
	}
	title := strings.TrimSpace(task.Goal)
	if title == "" {
		title = task.ID
	}
	sortTime := task.UpdatedAt
	if sortTime.IsZero() {
		sortTime = task.CreatedAt
	}
	return StateEntry{
		Kind:       StateKindTask,
		RecordID:   task.ID,
		Workspace:  strings.TrimSpace(task.WorkspaceID),
		SessionID:  strings.TrimSpace(task.SessionID),
		Status:     string(task.Status),
		Title:      title,
		Summary:    strings.TrimSpace(task.AgentName),
		SearchText: normalizeStateText(task.ID, task.AgentName, task.Goal, task.Result, task.Error, string(task.Status), task.SessionID, task.ParentSessionID, task.JobID, task.JobItemID),
		SortTime:   sortTime.UTC(),
		CreatedAt:  task.CreatedAt.UTC(),
		UpdatedAt:  task.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"agent_name":        task.AgentName,
			"claimed_by":        task.ClaimedBy,
			"depends_on":        append([]string(nil), task.DependsOn...),
			"result":            task.Result,
			"error":             task.Error,
			"workspace_id":      task.WorkspaceID,
			"session_id":        task.SessionID,
			"parent_session_id": task.ParentSessionID,
			"job_id":            task.JobID,
			"job_item_id":       task.JobItemID,
		}),
	}, true
}

func StateEntryFromJob(job port.AgentJob) (StateEntry, bool) {
	if strings.TrimSpace(job.ID) == "" {
		return StateEntry{}, false
	}
	title := strings.TrimSpace(job.Goal)
	if title == "" {
		title = job.ID
	}
	sortTime := job.UpdatedAt
	if sortTime.IsZero() {
		sortTime = job.CreatedAt
	}
	return StateEntry{
		Kind:       StateKindJob,
		RecordID:   job.ID,
		Status:     string(job.Status),
		Title:      title,
		Summary:    strings.TrimSpace(job.AgentName),
		SearchText: normalizeStateText(job.ID, job.AgentName, job.Goal, string(job.Status)),
		SortTime:   sortTime.UTC(),
		CreatedAt:  job.CreatedAt.UTC(),
		UpdatedAt:  job.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"agent_name": job.AgentName,
			"revision":   job.Revision,
		}),
	}, true
}

func StateEntryFromJobItem(item port.AgentJobItem) (StateEntry, bool) {
	if strings.TrimSpace(item.JobID) == "" || strings.TrimSpace(item.ItemID) == "" {
		return StateEntry{}, false
	}
	sortTime := item.UpdatedAt
	if sortTime.IsZero() {
		sortTime = item.CreatedAt
	}
	recordID := strings.TrimSpace(item.JobID) + ":" + strings.TrimSpace(item.ItemID)
	return StateEntry{
		Kind:       StateKindJobItem,
		RecordID:   recordID,
		Status:     string(item.Status),
		Title:      firstNonEmpty(item.ItemID, recordID),
		Summary:    strings.TrimSpace(item.Executor),
		SearchText: normalizeStateText(item.JobID, item.ItemID, item.Executor, item.Result, item.Error, string(item.Status)),
		SortTime:   sortTime.UTC(),
		CreatedAt:  item.CreatedAt.UTC(),
		UpdatedAt:  item.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"job_id":   item.JobID,
			"item_id":  item.ItemID,
			"executor": item.Executor,
			"result":   item.Result,
			"error":    item.Error,
		}),
	}, true
}

func StateEntryFromMemory(record port.MemoryRecord) (StateEntry, bool) {
	if strings.TrimSpace(record.Path) == "" {
		return StateEntry{}, false
	}
	sortTime := record.UpdatedAt
	if sortTime.IsZero() {
		sortTime = memoryFreshness(record)
	}
	return StateEntry{
		Kind:       StateKindMemory,
		RecordID:   strings.TrimSpace(record.Path),
		Workspace:  strings.TrimSpace(record.Workspace),
		RepoRoot:   strings.TrimSpace(record.CWD),
		Status:     firstNonEmpty(string(record.Status), string(port.MemoryStatusActive)),
		Title:      firstNonEmpty(record.Path, record.Group, record.SourcePath, record.ID),
		Summary:    strings.TrimSpace(record.Summary),
		SearchText: normalizeStateText(record.Path, record.Group, record.Summary, record.Content, strings.Join(record.Tags, " "), record.SourcePath, record.CWD, record.GitBranch, record.SourceKind, string(record.Stage), string(record.Status)),
		SortTime:   sortTime.UTC(),
		CreatedAt:  record.CreatedAt.UTC(),
		UpdatedAt:  record.UpdatedAt.UTC(),
		Metadata: marshalStateMetadata(map[string]any{
			"id":                record.ID,
			"path":              record.Path,
			"group":             record.Group,
			"stage":             record.Stage,
			"status":            record.Status,
			"tags":              append([]string(nil), record.Tags...),
			"workspace":         record.Workspace,
			"cwd":               record.CWD,
			"git_branch":        record.GitBranch,
			"source_kind":       record.SourceKind,
			"source_id":         record.SourceID,
			"source_path":       record.SourcePath,
			"source_updated_at": record.SourceUpdatedAt,
			"usage_count":       record.UsageCount,
			"last_used_at":      record.LastUsedAt,
			"citation":          record.Citation,
		}),
	}, true
}

func StateEntryFromExecutionEvent(event port.ExecutionEvent) StateEntry {
	recordID := strings.TrimSpace(event.EventID)
	if recordID == "" {
		recordID = strings.TrimSpace(event.CallID)
	}
	if recordID == "" {
		recordID = fmt.Sprintf("%d-%s-%s", event.Timestamp.UTC().UnixNano(), event.Type, firstNonEmpty(strings.TrimSpace(event.ToolName), strings.TrimSpace(event.Model)))
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
		Summary:   firstNonEmpty(strings.TrimSpace(event.ReasonCode), strings.TrimSpace(event.Risk)),
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
			"data":          event.Data,
		}),
	}
}

func executionEventJournalRecord(event port.ExecutionEvent) map[string]any {
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
		"data":          event.Data,
	}
}

type indexedSessionStore struct {
	inner   session.SessionStore
	catalog *StateCatalog
}

func WrapSessionStore(store session.SessionStore, catalog *StateCatalog) session.SessionStore {
	if store == nil || catalog == nil || !catalog.Enabled() {
		return store
	}
	return &indexedSessionStore{inner: store, catalog: catalog}
}

func (s *indexedSessionStore) Save(ctx context.Context, sess *session.Session) error {
	if err := s.inner.Save(ctx, sess); err != nil {
		return err
	}
	if entry, ok := StateEntryFromSession(sess); ok {
		s.catalog.BestEffortUpsert(entry)
	} else if sess != nil {
		s.catalog.BestEffortDelete(StateKindSession, sess.ID)
	}
	return nil
}

func (s *indexedSessionStore) Load(ctx context.Context, id string) (*session.Session, error) {
	return s.inner.Load(ctx, id)
}

func (s *indexedSessionStore) List(ctx context.Context) ([]session.SessionSummary, error) {
	return s.inner.List(ctx)
}

func (s *indexedSessionStore) Delete(ctx context.Context, id string) error {
	if err := s.inner.Delete(ctx, id); err != nil {
		return err
	}
	s.catalog.BestEffortDelete(StateKindSession, id)
	return nil
}

func (s *indexedSessionStore) Watch(ctx context.Context, id string) (<-chan *session.Session, error) {
	watchable, ok := s.inner.(session.WatchableSessionStore)
	if !ok {
		return nil, session.ErrNotSupported
	}
	return watchable.Watch(ctx, id)
}

type indexedCheckpointStore struct {
	inner   port.CheckpointStore
	catalog *StateCatalog
}

func WrapCheckpointStore(store port.CheckpointStore, catalog *StateCatalog) port.CheckpointStore {
	if store == nil || catalog == nil || !catalog.Enabled() {
		return store
	}
	return &indexedCheckpointStore{inner: store, catalog: catalog}
}

func (s *indexedCheckpointStore) Create(ctx context.Context, req port.CheckpointCreateRequest) (*port.CheckpointRecord, error) {
	record, err := s.inner.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromCheckpoint(record); ok {
		s.catalog.BestEffortUpsert(entry)
	}
	return record, nil
}

func (s *indexedCheckpointStore) Load(ctx context.Context, id string) (*port.CheckpointRecord, error) {
	return s.inner.Load(ctx, id)
}

func (s *indexedCheckpointStore) List(ctx context.Context) ([]port.CheckpointRecord, error) {
	return s.inner.List(ctx)
}

func (s *indexedCheckpointStore) FindBySession(ctx context.Context, sessionID string) ([]port.CheckpointRecord, error) {
	return s.inner.FindBySession(ctx, sessionID)
}

type indexedTaskRuntime struct {
	inner   port.TaskRuntime
	catalog *StateCatalog
}

func WrapTaskRuntime(runtime port.TaskRuntime, catalog *StateCatalog) port.TaskRuntime {
	if runtime == nil || catalog == nil || !catalog.Enabled() {
		return runtime
	}
	return &indexedTaskRuntime{inner: runtime, catalog: catalog}
}

func (r *indexedTaskRuntime) UpsertTask(ctx context.Context, task port.TaskRecord) error {
	if err := r.inner.UpsertTask(ctx, task); err != nil {
		return err
	}
	if entry, ok := StateEntryFromTask(task); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return nil
}

func (r *indexedTaskRuntime) GetTask(ctx context.Context, id string) (*port.TaskRecord, error) {
	return r.inner.GetTask(ctx, id)
}

func (r *indexedTaskRuntime) ListTasks(ctx context.Context, query port.TaskQuery) ([]port.TaskRecord, error) {
	return r.inner.ListTasks(ctx, query)
}

func (r *indexedTaskRuntime) ClaimNextReady(ctx context.Context, claimer string, preferredAgent string) (*port.TaskRecord, error) {
	task, err := r.inner.ClaimNextReady(ctx, claimer, preferredAgent)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromTask(*task); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return task, nil
}

func (r *indexedTaskRuntime) UpsertJob(ctx context.Context, job port.AgentJob) error {
	jobRuntime, ok := r.inner.(port.JobRuntime)
	if !ok {
		return fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	if err := jobRuntime.UpsertJob(ctx, job); err != nil {
		return err
	}
	if entry, ok := StateEntryFromJob(job); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return nil
}

func (r *indexedTaskRuntime) GetJob(ctx context.Context, id string) (*port.AgentJob, error) {
	jobRuntime, ok := r.inner.(port.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	return jobRuntime.GetJob(ctx, id)
}

func (r *indexedTaskRuntime) ListJobs(ctx context.Context, query port.JobQuery) ([]port.AgentJob, error) {
	jobRuntime, ok := r.inner.(port.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	return jobRuntime.ListJobs(ctx, query)
}

func (r *indexedTaskRuntime) UpsertJobItem(ctx context.Context, item port.AgentJobItem) error {
	jobRuntime, ok := r.inner.(port.JobRuntime)
	if !ok {
		return fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	if err := jobRuntime.UpsertJobItem(ctx, item); err != nil {
		return err
	}
	if entry, ok := StateEntryFromJobItem(item); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return nil
}

func (r *indexedTaskRuntime) ListJobItems(ctx context.Context, query port.JobItemQuery) ([]port.AgentJobItem, error) {
	jobRuntime, ok := r.inner.(port.JobRuntime)
	if !ok {
		return nil, fmt.Errorf("job runtime is not supported by wrapped task runtime")
	}
	return jobRuntime.ListJobItems(ctx, query)
}

func (r *indexedTaskRuntime) MarkJobItemRunning(ctx context.Context, jobID, itemID, executor string) (*port.AgentJobItem, error) {
	atomicRuntime, ok := r.inner.(port.AtomicJobRuntime)
	if !ok {
		return nil, fmt.Errorf("atomic job runtime is not supported by wrapped task runtime")
	}
	item, err := atomicRuntime.MarkJobItemRunning(ctx, jobID, itemID, executor)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromJobItem(*item); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return item, nil
}

func (r *indexedTaskRuntime) ReportJobItemResult(ctx context.Context, jobID, itemID, executor string, status port.AgentJobStatus, result string, errMsg string) (*port.AgentJobItem, error) {
	atomicRuntime, ok := r.inner.(port.AtomicJobRuntime)
	if !ok {
		return nil, fmt.Errorf("atomic job runtime is not supported by wrapped task runtime")
	}
	item, err := atomicRuntime.ReportJobItemResult(ctx, jobID, itemID, executor, status, result, errMsg)
	if err != nil {
		return nil, err
	}
	if entry, ok := StateEntryFromJobItem(*item); ok {
		r.catalog.BestEffortUpsert(entry)
	}
	return item, nil
}

type stateCatalogObserver struct {
	port.NoOpObserver
	catalog *StateCatalog
}

func NewStateCatalogObserver(catalog *StateCatalog) port.Observer {
	if catalog == nil || !catalog.Enabled() {
		return nil
	}
	return &stateCatalogObserver{catalog: catalog}
}

func (o *stateCatalogObserver) OnExecutionEvent(_ context.Context, event port.ExecutionEvent) {
	if err := o.catalog.AppendExecutionEvent(event); err != nil {
		o.catalog.markError(err)
	}
}

func WithStateCatalog(catalog *StateCatalog) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureStateCatalogState(k).catalog = catalog
	}
}

func StateCatalogOf(k *kernel.Kernel) *StateCatalog {
	if k == nil {
		return nil
	}
	actual, ok := kernel.Extensions(k).State(stateCatalogStateKey)
	if !ok {
		return nil
	}
	state, _ := actual.(*stateCatalogState)
	if state == nil {
		return nil
	}
	return state.catalog
}

func ObserverForStateCatalog(k *kernel.Kernel) port.Observer {
	return NewStateCatalogObserver(StateCatalogOf(k))
}

func ensureStateCatalogState(k *kernel.Kernel) *stateCatalogState {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateCatalogStateKey, &stateCatalogState{})
	state := actual.(*stateCatalogState)
	if loaded {
		return state
	}
	return state
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
