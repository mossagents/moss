package knowledge

import (
	"context"
	"math"
	"sort"
	"sync"

	"github.com/mossagents/moss/kernel/port"
)

// MemoryStore 是基于内存的知识库实现，使用余弦相似度进行搜索。
type MemoryStore struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

// NewMemoryStore 创建内存知识库。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{docs: make(map[string]*Document)}
}

func (m *MemoryStore) Add(ctx context.Context, embedder port.Embedder, id, source string, texts []string, metadata map[string]any) error {
	if len(texts) == 0 {
		return nil
	}

	// 批量嵌入
	embeddings, err := embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return err
	}

	chunks := make([]Chunk, len(texts))
	for i, text := range texts {
		chunks[i] = Chunk{
			Text:      text,
			Index:     i,
			Embedding: embeddings[i],
		}
	}

	doc := &Document{
		ID:       id,
		Source:   source,
		Chunks:   chunks,
		Metadata: metadata,
	}

	m.mu.Lock()
	m.docs[id] = doc
	m.mu.Unlock()

	return nil
}

func (m *MemoryStore) Search(ctx context.Context, embedder port.Embedder, query string, limit int) ([]SearchResult, error) {
	// 嵌入查询
	queryVec, err := embedder.Embed(ctx, query)
	if err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// 计算所有 chunk 的余弦相似度
	var results []SearchResult
	for _, doc := range m.docs {
		for _, chunk := range doc.Chunks {
			score := cosineSimilarity(queryVec, chunk.Embedding)
			results = append(results, SearchResult{
				DocID:    doc.ID,
				Source:   doc.Source,
				Text:     chunk.Text,
				Score:    score,
				ChunkIdx: chunk.Index,
			})
		}
	}

	// 按分数降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func (m *MemoryStore) Remove(id string) error {
	m.mu.Lock()
	delete(m.docs, id)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) List() []DocumentSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]DocumentSummary, 0, len(m.docs))
	for _, doc := range m.docs {
		summaries = append(summaries, DocumentSummary{
			ID:     doc.ID,
			Source: doc.Source,
			Chunks: len(doc.Chunks),
		})
	}
	return summaries
}

func (m *MemoryStore) Count() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	chunks := 0
	for _, doc := range m.docs {
		chunks += len(doc.Chunks)
	}
	return len(m.docs), chunks
}

// cosineSimilarity 计算两个向量的余弦相似度。
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
