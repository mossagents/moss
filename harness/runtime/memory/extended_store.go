package memstore

import (
	"context"
	"time"

	"github.com/mossagents/moss/kernel/memory"
)

// ExtendedMemoryStore 是 harness 层的完整 memory 存储接口，在 kernel MemoryStore
// 基础上增加 RecordUsage 和扩展查询能力。
type ExtendedMemoryStore interface {
	UpsertExtended(ctx context.Context, record ExtendedMemoryRecord) (*ExtendedMemoryRecord, error)
	GetByPathExtended(ctx context.Context, path string) (*ExtendedMemoryRecord, error)
	DeleteByPath(ctx context.Context, path string) error
	ListExtended(ctx context.Context, limit int) ([]ExtendedMemoryRecord, error)
	SearchExtended(ctx context.Context, query ExtendedMemoryQuery) ([]ExtendedMemoryRecord, error)
	RecordUsage(ctx context.Context, paths []string, usedAt time.Time) error
}

// ToKernelRecord 将 ExtendedMemoryRecord 转换为 kernel memory.MemoryRecord。
func ToKernelRecord(ext ExtendedMemoryRecord) memory.MemoryRecord {
	return memory.MemoryRecord{
		ID:        ext.ID,
		Path:      ext.Path,
		Content:   ext.Content,
		Summary:   ext.Summary,
		Tags:      ext.Tags,
		CreatedAt: ext.CreatedAt,
		UpdatedAt: ext.UpdatedAt,
		Metadata:  ext.Metadata,
	}
}

// FromKernelRecord 将 kernel memory.MemoryRecord 转换为 ExtendedMemoryRecord（扩展字段为零值）。
func FromKernelRecord(r memory.MemoryRecord) ExtendedMemoryRecord {
	return ExtendedMemoryRecord{
		ID:        r.ID,
		Path:      r.Path,
		Content:   r.Content,
		Summary:   r.Summary,
		Tags:      r.Tags,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		Metadata:  r.Metadata,
	}
}

// ToKernelQuery 将 ExtendedMemoryQuery 转换为 kernel memory.MemoryQuery。
func ToKernelQuery(ext ExtendedMemoryQuery) memory.MemoryQuery {
	return memory.MemoryQuery{
		Query: ext.Query,
		Tags:  ext.Tags,
		Limit: ext.Limit,
	}
}

// KernelStoreAdapter 将 ExtendedMemoryStore 适配为 kernel memory.MemoryStore 接口。
type KernelStoreAdapter struct {
	Ext ExtendedMemoryStore
}

var _ memory.MemoryStore = (*KernelStoreAdapter)(nil)

func (a *KernelStoreAdapter) Upsert(ctx context.Context, record memory.MemoryRecord) (*memory.MemoryRecord, error) {
	result, err := a.Ext.UpsertExtended(ctx, FromKernelRecord(record))
	if err != nil {
		return nil, err
	}
	kr := ToKernelRecord(*result)
	return &kr, nil
}

func (a *KernelStoreAdapter) GetByPath(ctx context.Context, path string) (*memory.MemoryRecord, error) {
	result, err := a.Ext.GetByPathExtended(ctx, path)
	if err != nil {
		return nil, err
	}
	kr := ToKernelRecord(*result)
	return &kr, nil
}

func (a *KernelStoreAdapter) DeleteByPath(ctx context.Context, path string) error {
	return a.Ext.DeleteByPath(ctx, path)
}

func (a *KernelStoreAdapter) List(ctx context.Context, limit int) ([]memory.MemoryRecord, error) {
	records, err := a.Ext.ListExtended(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]memory.MemoryRecord, len(records))
	for i, r := range records {
		out[i] = ToKernelRecord(r)
	}
	return out, nil
}

func (a *KernelStoreAdapter) Search(ctx context.Context, query memory.MemoryQuery) ([]memory.MemoryRecord, error) {
	records, err := a.Ext.SearchExtended(ctx, ExtendedMemoryQuery{
		Query: query.Query,
		Tags:  query.Tags,
		Limit: query.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]memory.MemoryRecord, len(records))
	for i, r := range records {
		out[i] = ToKernelRecord(r)
	}
	return out, nil
}
