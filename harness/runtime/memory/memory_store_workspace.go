package memstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/ids"
	"github.com/mossagents/moss/kernel/workspace"
	"github.com/mossagents/moss/x/stringutil"
)

const memoryIndexPath = ".moss/memory_index.json"

type workspaceMemoryStore struct {
	ws workspace.Workspace
}

func NewWorkspaceMemoryStore(ws workspace.Workspace) ExtendedMemoryStore {
	return &workspaceMemoryStore{ws: ws}
}

func (s *workspaceMemoryStore) UpsertExtended(ctx context.Context, record ExtendedMemoryRecord) (*ExtendedMemoryRecord, error) {
	if strings.TrimSpace(record.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	now := time.Now().UTC()
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	key := NormalizePath(record.Path)
	var existing *ExtendedMemoryRecord
	if current, ok := records[key]; ok {
		cp := current
		existing = &cp
	}
	record = normalizeMemoryRecord(record, existing, now)
	if existing == nil {
		record.ID = ids.New()
	}
	records[key] = record
	if err := s.persistIndex(ctx, records); err != nil {
		return nil, err
	}
	out := record
	return &out, nil
}

func (s *workspaceMemoryStore) GetByPathExtended(ctx context.Context, path string) (*ExtendedMemoryRecord, error) {
	key := NormalizePath(path)
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
	key := NormalizePath(path)
	records, err := s.loadIndex(ctx)
	if err != nil {
		return err
	}
	delete(records, key)
	return s.persistIndex(ctx, records)
}

func (s *workspaceMemoryStore) ListExtended(ctx context.Context, limit int) ([]ExtendedMemoryRecord, error) {
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ExtendedMemoryRecord, 0, len(records))
	for _, record := range records {
		out = append(out, record)
	}
	sortMemoryRecords(out, ExtendedMemoryQuery{})
	return trimMemoryRecords(out, limit), nil
}

func (s *workspaceMemoryStore) SearchExtended(ctx context.Context, query ExtendedMemoryQuery) ([]ExtendedMemoryRecord, error) {
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ExtendedMemoryRecord, 0, len(records))
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
	for _, path := range DedupeStrings(paths) {
		key := NormalizePath(path)
		record, ok := records[key]
		if !ok {
			continue
		}
		records[key] = BumpMemoryUsage(record, usedAt)
		changed = true
	}
	if !changed {
		return nil
	}
	return s.persistIndex(ctx, records)
}

func (s *workspaceMemoryStore) loadIndex(ctx context.Context) (map[string]ExtendedMemoryRecord, error) {
	raw, err := s.ws.ReadFile(ctx, memoryIndexPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return make(map[string]ExtendedMemoryRecord), nil
		}
		return nil, err
	}
	var records map[string]ExtendedMemoryRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("decode memory index: %w", err)
	}
	if records == nil {
		records = make(map[string]ExtendedMemoryRecord)
	}
	for key, record := range records {
		record.Path = NormalizePath(stringutil.FirstNonEmpty(record.Path, key))
		record.Tags = normalizeMemoryTags(record.Tags)
		record.Citation = normalizeMemoryCitation(record.Citation)
		if record.Stage == "" {
			record.Stage = MemoryStageManual
		}
		if record.Status == "" {
			record.Status = MemoryStatusActive
		}
		records[key] = record
	}
	return records, nil
}

func (s *workspaceMemoryStore) persistIndex(ctx context.Context, records map[string]ExtendedMemoryRecord) error {
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]ExtendedMemoryRecord, len(records))
	for _, key := range keys {
		ordered[key] = records[key]
	}
	raw, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory index: %w", err)
	}
	return s.ws.WriteFile(ctx, memoryIndexPath, raw)
}
