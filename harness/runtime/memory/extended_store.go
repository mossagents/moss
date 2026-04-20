package memstore

import (
	"context"
	"time"

	"github.com/mossagents/moss/kernel/memory"
)

const (
	metadataKeyMemoryScope       = "memory_scope"
	metadataKeyMemorySessionID   = "memory_session_id"
	metadataKeyMemoryRepoID      = "memory_repo_id"
	metadataKeyMemoryUserID      = "memory_user_id"
	metadataKeyMemoryKind        = "memory_kind"
	metadataKeyMemoryFingerprint = "memory_fingerprint"
	metadataKeyMemoryConfidence  = "memory_confidence"
	metadataKeyMemoryExpiresAt   = "memory_expires_at"
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
	metadata := cloneMemoryMetadata(ext.Metadata)
	if ext.Scope != "" {
		metadata[metadataKeyMemoryScope] = string(ext.Scope)
	}
	if ext.SessionID != "" {
		metadata[metadataKeyMemorySessionID] = ext.SessionID
	}
	if ext.RepoID != "" {
		metadata[metadataKeyMemoryRepoID] = ext.RepoID
	}
	if ext.UserID != "" {
		metadata[metadataKeyMemoryUserID] = ext.UserID
	}
	if ext.Kind != "" {
		metadata[metadataKeyMemoryKind] = ext.Kind
	}
	if ext.Fingerprint != "" {
		metadata[metadataKeyMemoryFingerprint] = ext.Fingerprint
	}
	if ext.Confidence > 0 {
		metadata[metadataKeyMemoryConfidence] = ext.Confidence
	}
	if !ext.ExpiresAt.IsZero() {
		metadata[metadataKeyMemoryExpiresAt] = FormatMemoryTime(ext.ExpiresAt)
	}
	return memory.MemoryRecord{
		ID:        ext.ID,
		Path:      ext.Path,
		Content:   ext.Content,
		Summary:   ext.Summary,
		Tags:      ext.Tags,
		CreatedAt: ext.CreatedAt,
		UpdatedAt: ext.UpdatedAt,
		Metadata:  metadata,
	}
}

// FromKernelRecord 将 kernel memory.MemoryRecord 转换为 ExtendedMemoryRecord（扩展字段为零值）。
func FromKernelRecord(r memory.MemoryRecord) ExtendedMemoryRecord {
	record := ExtendedMemoryRecord{
		ID:        r.ID,
		Path:      r.Path,
		Content:   r.Content,
		Summary:   r.Summary,
		Tags:      r.Tags,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		Metadata:  cloneMemoryMetadata(r.Metadata),
	}
	if record.Metadata != nil {
		if raw, ok := record.Metadata[metadataKeyMemoryScope].(string); ok {
			record.Scope = MemoryScope(raw)
		}
		if raw, ok := record.Metadata[metadataKeyMemorySessionID].(string); ok {
			record.SessionID = raw
		}
		if raw, ok := record.Metadata[metadataKeyMemoryRepoID].(string); ok {
			record.RepoID = raw
		}
		if raw, ok := record.Metadata[metadataKeyMemoryUserID].(string); ok {
			record.UserID = raw
		}
		if raw, ok := record.Metadata[metadataKeyMemoryKind].(string); ok {
			record.Kind = raw
		}
		if raw, ok := record.Metadata[metadataKeyMemoryFingerprint].(string); ok {
			record.Fingerprint = raw
		}
		record.Confidence = metadataFloat64(record.Metadata, metadataKeyMemoryConfidence)
		if raw, ok := record.Metadata[metadataKeyMemoryExpiresAt].(string); ok {
			record.ExpiresAt = ParseMemoryTime(raw)
		}
	}
	return record
}

// ToKernelQuery 将 ExtendedMemoryQuery 转换为 kernel memory.MemoryQuery。
func ToKernelQuery(ext ExtendedMemoryQuery) memory.MemoryQuery {
	return memory.MemoryQuery{
		Query: ext.Query,
		Tags:  ext.Tags,
		Limit: ext.Limit,
	}
}

func cloneMemoryMetadata(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
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
