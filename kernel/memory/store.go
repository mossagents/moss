package memory

import (
	"context"
	"time"
)

// MemoryRecord 是结构化 memory 的核心记录。
// 扩展字段（Citation、Stage、Status、源信息等）由上层（如 harness）通过 Metadata 或
// 扩展类型提供。
type MemoryRecord struct {
	ID        string         `json:"id"`
	Path      string         `json:"path"`
	Content   string         `json:"content"`
	Summary   string         `json:"summary,omitempty"`
	Tags      []string       `json:"tags,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// MemoryQuery 是 memory 查询参数。
type MemoryQuery struct {
	Query string   `json:"query,omitempty"`
	Tags  []string `json:"tags,omitempty"`
	Limit int      `json:"limit,omitempty"`
}

// MemoryStore 提供结构化 memory 的持久化与检索能力。
type MemoryStore interface {
	Upsert(ctx context.Context, record MemoryRecord) (*MemoryRecord, error)
	GetByPath(ctx context.Context, path string) (*MemoryRecord, error)
	DeleteByPath(ctx context.Context, path string) error
	List(ctx context.Context, limit int) ([]MemoryRecord, error)
	Search(ctx context.Context, query MemoryQuery) ([]MemoryRecord, error)
}
