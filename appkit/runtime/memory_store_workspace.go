package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mossagents/moss/kernel/port"
)

const memoryIndexPath = ".moss/memory_index.json"

type workspaceMemoryStore struct {
	ws port.Workspace
}

func NewWorkspaceMemoryStore(ws port.Workspace) port.MemoryStore {
	return &workspaceMemoryStore{ws: ws}
}

func (s *workspaceMemoryStore) Upsert(ctx context.Context, record port.MemoryRecord) (*port.MemoryRecord, error) {
	if strings.TrimSpace(record.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	now := time.Now().UTC()
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	key := normalizeMemoryPath(record.Path)
	var existing *port.MemoryRecord
	if current, ok := records[key]; ok {
		cp := current
		existing = &cp
	}
	record = normalizeMemoryRecord(record, existing, now)
	if existing == nil {
		record.ID = uuid.New().String()
	}
	records[key] = record
	if err := s.persistIndex(ctx, records); err != nil {
		return nil, err
	}
	out := record
	return &out, nil
}

func (s *workspaceMemoryStore) GetByPath(ctx context.Context, path string) (*port.MemoryRecord, error) {
	key := normalizeMemoryPath(path)
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	record, ok := records[key]
	if !ok {
		return nil, fmt.Errorf("memory %q not found", key)
	}
	out := record
	return &out, nil
}

func (s *workspaceMemoryStore) DeleteByPath(ctx context.Context, path string) error {
	key := normalizeMemoryPath(path)
	records, err := s.loadIndex(ctx)
	if err != nil {
		return err
	}
	delete(records, key)
	return s.persistIndex(ctx, records)
}

func (s *workspaceMemoryStore) List(ctx context.Context, limit int) ([]port.MemoryRecord, error) {
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]port.MemoryRecord, 0, len(records))
	for _, record := range records {
		out = append(out, record)
	}
	sortMemoryRecords(out, port.MemoryQuery{})
	return trimMemoryRecords(out, limit), nil
}

func (s *workspaceMemoryStore) Search(ctx context.Context, query port.MemoryQuery) ([]port.MemoryRecord, error) {
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]port.MemoryRecord, 0, len(records))
	for _, record := range records {
		if !memoryMatchesQuery(record, query) {
			continue
		}
		out = append(out, record)
	}
	sortMemoryRecords(out, query)
	return trimMemoryRecords(out, query.Limit), nil
}

func (s *workspaceMemoryStore) RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error {
	records, err := s.loadIndex(ctx)
	if err != nil {
		return err
	}
	changed := false
	for _, path := range dedupeStrings(paths) {
		key := normalizeMemoryPath(path)
		record, ok := records[key]
		if !ok {
			continue
		}
		records[key] = bumpMemoryUsage(record, usedAt)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistIndex(ctx, records)
}

func (s *workspaceMemoryStore) loadIndex(ctx context.Context) (map[string]port.MemoryRecord, error) {
	raw, err := s.ws.ReadFile(ctx, memoryIndexPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return make(map[string]port.MemoryRecord), nil
		}
		return nil, err
	}
	var records map[string]port.MemoryRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("decode memory index: %w", err)
	}
	if records == nil {
		records = make(map[string]port.MemoryRecord)
	}
	for key, record := range records {
		record.Path = normalizeMemoryPath(firstNonEmpty(record.Path, key))
		record.Tags = normalizeMemoryTags(record.Tags)
		record.Citation = normalizeMemoryCitation(record.Citation)
		if record.Stage == "" {
			record.Stage = port.MemoryStageManual
		}
		if record.Status == "" {
			record.Status = port.MemoryStatusActive
		}
		records[key] = record
	}
	return records, nil
}

func (s *workspaceMemoryStore) persistIndex(ctx context.Context, records map[string]port.MemoryRecord) error {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]port.MemoryRecord, len(records))
	for _, key := range keys {
		ordered[key] = records[key]
	}
	raw, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory index: %w", err)
	}
	return s.ws.WriteFile(ctx, memoryIndexPath, raw)
}
