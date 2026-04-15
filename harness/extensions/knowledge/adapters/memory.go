// Package adapters 提供 model.VectorStore 的各种后端实现。
package adapters

import (
	"context"
	"github.com/mossagents/moss/kernel/model"
	"math"
	"sort"
	"sync"
	"time"
)

// MemoryVectorStore 是基于内存的 VectorStore 实现，使用余弦相似度检索。
// 适用于开发、测试及轻量场景；进程重启后数据丢失。
type MemoryVectorStore struct {
	mu      sync.RWMutex
	entries []vectorEntry
}

type vectorEntry struct {
	doc       model.VectorDoc
	embedding []float64
	expiresAt time.Time // zero = 永不过期
}

func (e *vectorEntry) expired() bool {
	return !e.expiresAt.IsZero() && time.Now().After(e.expiresAt)
}

// NewMemoryVectorStore 创建空的内存向量存储。
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{}
}

// Upsert 添加或更新文档。若文档的 Embedding 为空，则调用 embedder 自动生成。
// 同一 ID 的文档会被覆盖。
func (s *MemoryVectorStore) Upsert(ctx context.Context, docs []model.VectorDoc) error {
	if len(docs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, doc := range docs {
		entry := vectorEntry{
			doc:       doc,
			embedding: doc.Embedding,
		}
		if doc.TTL > 0 {
			entry.expiresAt = time.Now().Add(doc.TTL)
		}

		replaced := false
		for i, e := range s.entries {
			if e.doc.ID == doc.ID {
				s.entries[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			s.entries = append(s.entries, entry)
		}
	}
	return nil
}

// Search 语义检索。若文档缺少 Embedding，需在 Upsert 时预先嵌入。
func (s *MemoryVectorStore) Search(ctx context.Context, embedder model.Embedder, query model.VectorQuery) ([]model.VectorResult, error) {
	queryVec, err := embedder.Embed(ctx, query.Text)
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	threshold := query.Threshold
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	type scored struct {
		entry vectorEntry
		score float64
	}

	var candidates []scored
	for _, e := range s.entries {
		if e.expired() {
			continue
		}
		if query.Namespace != "" && e.doc.Namespace != query.Namespace {
			continue
		}
		if len(e.embedding) == 0 {
			continue
		}
		score := cosineSimilarity(queryVec, e.embedding)
		if threshold > 0 && score < threshold {
			continue
		}
		candidates = append(candidates, scored{entry: e, score: score})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if limit > 0 && len(candidates) > limit {
		candidates = candidates[:limit]
	}

	results := make([]model.VectorResult, len(candidates))
	for i, c := range candidates {
		results[i] = model.VectorResult{
			Doc:   c.entry.doc,
			Score: c.score,
		}
	}
	return results, nil
}

// Delete 按 ID 删除文档。
func (s *MemoryVectorStore) Delete(_ context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.entries[:0]
	for _, e := range s.entries {
		if _, del := idSet[e.doc.ID]; !del {
			kept = append(kept, e)
		}
	}
	s.entries = kept
	return nil
}

// Count 返回指定 namespace 下的有效文档数。
func (s *MemoryVectorStore) Count(_ context.Context, namespace string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, e := range s.entries {
		if e.expired() {
			continue
		}
		if namespace == "" || e.doc.Namespace == namespace {
			count++
		}
	}
	return count, nil
}

// UpsertWithEmbedder 在 Upsert 前自动为缺少 Embedding 的文档生成向量。
func (s *MemoryVectorStore) UpsertWithEmbedder(ctx context.Context, embedder model.Embedder, docs []model.VectorDoc) error {
	toEmbed := make([]string, 0, len(docs))
	needEmbed := make([]int, 0, len(docs))
	for i, d := range docs {
		if len(d.Embedding) == 0 {
			toEmbed = append(toEmbed, d.Text)
			needEmbed = append(needEmbed, i)
		}
	}
	if len(toEmbed) > 0 {
		embeddings, err := embedder.EmbedBatch(ctx, toEmbed)
		if err != nil {
			return err
		}
		for j, idx := range needEmbed {
			docs[idx].Embedding = embeddings[j]
		}
	}
	return s.Upsert(ctx, docs)
}

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
