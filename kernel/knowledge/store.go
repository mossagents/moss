// Package knowledge 提供文档知识库的存储和检索能力。
//
// 支持文档摄入（自动分块+嵌入）和语义搜索：
//
//	store := knowledge.NewMemoryStore()
//	store.Add(ctx, embedder, "doc1", "source.md", chunks, nil)
//	results := store.Search(ctx, embedder, "query", 5)
package knowledge

import (
	"context"

	"github.com/mossagi/moss/kernel/port"
)

// Document 表示已摄入知识库的一个文档。
type Document struct {
	ID       string         `json:"id"`
	Source   string         `json:"source"`
	Chunks   []Chunk        `json:"chunks"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Chunk 是文档的一个分块，包含文本和对应的嵌入向量。
type Chunk struct {
	Text      string    `json:"text"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"-"` // 不序列化，运行时保存
}

// SearchResult 是知识搜索的返回结果。
type SearchResult struct {
	DocID    string  `json:"doc_id"`
	Source   string  `json:"source"`
	Text     string  `json:"text"`
	Score    float64 `json:"score"`
	ChunkIdx int     `json:"chunk_index"`
}

// Store 定义知识库的存储接口。
type Store interface {
	// Add 添加一个文档到知识库，自动嵌入所有文本块。
	Add(ctx context.Context, embedder port.Embedder, id, source string, texts []string, metadata map[string]any) error

	// Search 语义搜索，返回 top-k 最相关的文本块。
	Search(ctx context.Context, embedder port.Embedder, query string, limit int) ([]SearchResult, error)

	// Remove 删除指定 ID 的文档。
	Remove(id string) error

	// List 列出所有已摄入的文档摘要。
	List() []DocumentSummary

	// Count 返回文档总数和分块总数。
	Count() (docs int, chunks int)
}

// DocumentSummary 是文档的摘要信息。
type DocumentSummary struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Chunks int    `json:"chunks"`
}
