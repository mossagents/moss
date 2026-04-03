package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
	key := normalizeMemoryPath(record.Path)
	now := time.Now().UTC()
	records, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	existing, ok := records[key]
	if ok {
		record.ID = existing.ID
		record.CreatedAt = existing.CreatedAt
	} else {
		record.ID = uuid.New().String()
		record.CreatedAt = now
	}
	if record.Summary == "" {
		record.Summary = summarizeMemoryContent(record.Content)
	}
	record.Path = key
	record.UpdatedAt = now
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
	for _, r := range records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].Path < out[j].Path
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *workspaceMemoryStore) Search(ctx context.Context, query port.MemoryQuery) ([]port.MemoryRecord, error) {
	items, err := s.List(ctx, query.Limit)
	if err != nil {
		return nil, err
	}
	needle := strings.ToLower(strings.TrimSpace(query.Query))
	tagSet := make(map[string]struct{}, len(query.Tags))
	for _, tag := range query.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag != "" {
			tagSet[tag] = struct{}{}
		}
	}
	if needle == "" && len(tagSet) == 0 {
		return items, nil
	}
	out := make([]port.MemoryRecord, 0, len(items))
	for _, item := range items {
		if !matchesMemoryQuery(item, needle, tagSet) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func matchesMemoryQuery(item port.MemoryRecord, needle string, tagSet map[string]struct{}) bool {
	if needle != "" {
		text := strings.ToLower(item.Path + "\n" + item.Summary + "\n" + item.Content)
		if !strings.Contains(text, needle) {
			return false
		}
	}
	if len(tagSet) == 0 {
		return true
	}
	for _, tag := range item.Tags {
		if _, ok := tagSet[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
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
	return records, nil
}

func (s *workspaceMemoryStore) persistIndex(ctx context.Context, records map[string]port.MemoryRecord) error {
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode memory index: %w", err)
	}
	return s.ws.WriteFile(ctx, memoryIndexPath, raw)
}

func normalizeMemoryPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/")
	return filepath.Clean(path)
}

func summarizeMemoryContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	const maxLen = 160
	if len(content) <= maxLen {
		return content
	}
	return strings.TrimSpace(content[:maxLen]) + "..."
}
