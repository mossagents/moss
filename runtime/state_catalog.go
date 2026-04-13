package runtime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const stateCatalogSchemaVersion = 1

type StateKind string

const (
	StateKindSession        StateKind = "session"
	StateKindCheckpoint     StateKind = "checkpoint"
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
	defer func() { _ = f.Close() }()
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
