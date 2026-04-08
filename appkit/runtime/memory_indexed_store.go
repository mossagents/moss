package runtime

import (
	"context"
	memstore "github.com/mossagents/moss/kernel/memory"
	"io"
	"time"
)

type indexedMemoryStore struct {
	base    memstore.MemoryStore
	catalog *StateCatalog
}

func newIndexedMemoryStore(base memstore.MemoryStore, catalog *StateCatalog) memstore.MemoryStore {
	if base == nil || catalog == nil || !catalog.Enabled() {
		return base
	}
	return &indexedMemoryStore{base: base, catalog: catalog}
}

func (s *indexedMemoryStore) Upsert(ctx context.Context, record memstore.MemoryRecord) (*memstore.MemoryRecord, error) {
	out, err := s.base.Upsert(ctx, record)
	if err != nil {
		return nil, err
	}
	s.syncRecord(*out)
	return out, nil
}

func (s *indexedMemoryStore) GetByPath(ctx context.Context, path string) (*memstore.MemoryRecord, error) {
	return s.base.GetByPath(ctx, path)
}

func (s *indexedMemoryStore) DeleteByPath(ctx context.Context, path string) error {
	if err := s.base.DeleteByPath(ctx, path); err != nil {
		return err
	}
	return s.catalog.Delete(StateKindMemory, normalizeMemoryPath(path))
}

func (s *indexedMemoryStore) List(ctx context.Context, limit int) ([]memstore.MemoryRecord, error) {
	return s.base.List(ctx, limit)
}

func (s *indexedMemoryStore) Search(ctx context.Context, query memstore.MemoryQuery) ([]memstore.MemoryRecord, error) {
	return s.base.Search(ctx, query)
}

func (s *indexedMemoryStore) RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error {
	if err := s.base.RecordUsage(ctx, paths, usedAt); err != nil {
		return err
	}
	for _, path := range dedupeStrings(paths) {
		record, err := s.base.GetByPath(ctx, path)
		if err != nil {
			continue
		}
		s.syncRecord(*record)
	}
	return nil
}

func (s *indexedMemoryStore) Close() error {
	closer, ok := s.base.(io.Closer)
	if !ok || closer == nil {
		return nil
	}
	return closer.Close()
}

func (s *indexedMemoryStore) syncRecord(record memstore.MemoryRecord) {
	if entry, ok := StateEntryFromMemory(record); ok {
		if err := s.catalog.Upsert(entry); err != nil {
			s.catalog.markError(err)
		}
	}
}
