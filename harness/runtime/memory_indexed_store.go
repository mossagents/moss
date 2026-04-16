package runtime

import (
	"context"
	"io"
	"time"

	memstore "github.com/mossagents/moss/harness/runtime/memory"
	rstate "github.com/mossagents/moss/harness/runtime/state"
)

type indexedMemoryStore struct {
	base    memstore.ExtendedMemoryStore
	catalog *rstate.StateCatalog
}

func newIndexedMemoryStore(base memstore.ExtendedMemoryStore, catalog *rstate.StateCatalog) memstore.ExtendedMemoryStore {
	if base == nil || catalog == nil || !catalog.Enabled() {
		return base
	}
	return &indexedMemoryStore{base: base, catalog: catalog}
}

func (s *indexedMemoryStore) UpsertExtended(ctx context.Context, record memstore.ExtendedMemoryRecord) (*memstore.ExtendedMemoryRecord, error) {
	out, err := s.base.UpsertExtended(ctx, record)
	if err != nil {
		return nil, err
	}
	s.syncRecord(*out)
	return out, nil
}

func (s *indexedMemoryStore) GetByPathExtended(ctx context.Context, path string) (*memstore.ExtendedMemoryRecord, error) {
	return s.base.GetByPathExtended(ctx, path)
}

func (s *indexedMemoryStore) DeleteByPath(ctx context.Context, path string) error {
	if err := s.base.DeleteByPath(ctx, path); err != nil {
		return err
	}
	return s.catalog.Delete(rstate.StateKindMemory, memstore.NormalizePath(path))
}

func (s *indexedMemoryStore) ListExtended(ctx context.Context, limit int) ([]memstore.ExtendedMemoryRecord, error) {
	return s.base.ListExtended(ctx, limit)
}

func (s *indexedMemoryStore) SearchExtended(ctx context.Context, query memstore.ExtendedMemoryQuery) ([]memstore.ExtendedMemoryRecord, error) {
	return s.base.SearchExtended(ctx, query)
}

func (s *indexedMemoryStore) RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error {
	if err := s.base.RecordUsage(ctx, paths, usedAt); err != nil {
		return err
	}
	for _, path := range memstore.DedupeStrings(paths) {
		record, err := s.base.GetByPathExtended(ctx, path)
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

func (s *indexedMemoryStore) syncRecord(record memstore.ExtendedMemoryRecord) {
	if entry, ok := rstate.StateEntryFromMemory(record); ok {
		if err := s.catalog.Upsert(entry); err != nil {
			s.catalog.MarkError(err)
		}
	}
}
