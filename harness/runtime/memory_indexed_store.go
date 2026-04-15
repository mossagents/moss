package runtime

import (
	"context"
	"github.com/mossagents/moss/kernel/memory"
	memstore "github.com/mossagents/moss/harness/runtime/memory"
	rstate "github.com/mossagents/moss/harness/runtime/state"
	"io"
	"time"
)

type indexedMemoryStore struct {
	base    memory.MemoryStore
	catalog *rstate.StateCatalog
}

func newIndexedMemoryStore(base memory.MemoryStore, catalog *rstate.StateCatalog) memory.MemoryStore {
	if base == nil || catalog == nil || !catalog.Enabled() {
		return base
	}
	return &indexedMemoryStore{base: base, catalog: catalog}
}

func (s *indexedMemoryStore) Upsert(ctx context.Context, record memory.MemoryRecord) (*memory.MemoryRecord, error) {
	out, err := s.base.Upsert(ctx, record)
	if err != nil {
		return nil, err
	}
	s.syncRecord(*out)
	return out, nil
}

func (s *indexedMemoryStore) GetByPath(ctx context.Context, path string) (*memory.MemoryRecord, error) {
	return s.base.GetByPath(ctx, path)
}

func (s *indexedMemoryStore) DeleteByPath(ctx context.Context, path string) error {
	if err := s.base.DeleteByPath(ctx, path); err != nil {
		return err
	}
	return s.catalog.Delete(rstate.StateKindMemory, memstore.NormalizePath(path))
}

func (s *indexedMemoryStore) List(ctx context.Context, limit int) ([]memory.MemoryRecord, error) {
	return s.base.List(ctx, limit)
}

func (s *indexedMemoryStore) Search(ctx context.Context, query memory.MemoryQuery) ([]memory.MemoryRecord, error) {
	return s.base.Search(ctx, query)
}

func (s *indexedMemoryStore) RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error {
	if err := s.base.RecordUsage(ctx, paths, usedAt); err != nil {
		return err
	}
	for _, path := range memstore.DedupeStrings(paths) {
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

func (s *indexedMemoryStore) syncRecord(record memory.MemoryRecord) {
	if entry, ok := rstate.StateEntryFromMemory(record); ok {
		if err := s.catalog.Upsert(entry); err != nil {
			s.catalog.MarkError(err)
		}
	}
}

