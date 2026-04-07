package port

import (
	"context"
	"time"
)

// VectorDoc 是向量存储中的一条文档记录。
type VectorDoc struct {
	ID        string         `json:"id"`
	Namespace string         `json:"namespace,omitempty"`
	Text      string         `json:"text"`
	Embedding []float64      `json:"embedding,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	TTL       time.Duration  `json:"ttl,omitempty"`
	Score     float64        `json:"score,omitempty"`
}

// VectorQuery 是向量检索请求。
type VectorQuery struct {
	Text      string         `json:"text"`
	Namespace string         `json:"namespace,omitempty"`
	Limit     int            `json:"limit"`
	Threshold float64        `json:"threshold,omitempty"`
	Filter    map[string]any `json:"filter,omitempty"`
}

// VectorResult 是单条检索结果。
type VectorResult struct {
	Doc   VectorDoc `json:"doc"`
	Score float64   `json:"score"`
}

// VectorStore 提供向量文档的 upsert、检索和删除能力。
// 实现可以是 in-memory、pgvector、Qdrant 等。
type VectorStore interface {
	// Upsert 添加或更新一批文档；Embedding 为 nil 时由实现自行生成（需 Embedder）。
	Upsert(ctx context.Context, docs []VectorDoc) error

	// Search 语义检索，返回相似度最高的 Limit 条结果。
	Search(ctx context.Context, embedder Embedder, query VectorQuery) ([]VectorResult, error)

	// Delete 按 ID 删除文档。
	Delete(ctx context.Context, ids []string) error

	// Count 返回指定 namespace 下的文档数（namespace 为空则统计全部）。
	Count(ctx context.Context, namespace string) (int, error)
}
