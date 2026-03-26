package knowledge

import (
	"context"
	"math"
	"testing"

	"github.com/mossagents/moss/kernel/port"
)

// mockEmbedder 用简单的基于词频的"嵌入"做测试（不需要真实 API）。
type mockEmbedder struct {
	dim int
}

func newMockEmbedder(dim int) *mockEmbedder {
	return &mockEmbedder{dim: dim}
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	return simpleHash(text, m.dim), nil
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, t := range texts {
		result[i] = simpleHash(t, m.dim)
	}
	return result, nil
}

func (m *mockEmbedder) Dimension() int { return m.dim }

// simpleHash 基于字符产生确定性"嵌入"向量（仅用于测试）。
func simpleHash(text string, dim int) []float64 {
	vec := make([]float64, dim)
	for i, r := range text {
		vec[i%dim] += float64(r) / 1000.0
	}
	// 归一化
	var norm float64
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

// 验证 mockEmbedder 实现了 port.Embedder 接口
var _ port.Embedder = (*mockEmbedder)(nil)

func TestMemoryStoreAddAndSearch(t *testing.T) {
	store := NewMemoryStore()
	embedder := newMockEmbedder(16)
	ctx := context.Background()

	// 添加文档
	err := store.Add(ctx, embedder, "doc1", "golang.md", []string{
		"Go is a statically typed, compiled programming language.",
		"Go was designed at Google by Robert Griesemer, Rob Pike, and Ken Thompson.",
		"Go has built-in concurrency support with goroutines and channels.",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	err = store.Add(ctx, embedder, "doc2", "python.md", []string{
		"Python is a high-level, interpreted programming language.",
		"Python emphasizes code readability with significant indentation.",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Count
	docs, chunks := store.Count()
	if docs != 2 {
		t.Fatalf("expected 2 docs, got %d", docs)
	}
	if chunks != 5 {
		t.Fatalf("expected 5 chunks, got %d", chunks)
	}

	// Search
	results, err := store.Search(ctx, embedder, "concurrency goroutine", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}
	// 最相关的结果应该包含 "concurrency" 或 "goroutines"
	t.Logf("Top result: score=%.4f doc=%s text=%q", results[0].Score, results[0].DocID, results[0].Text[:50])

	// List
	summaries := store.List()
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	// Remove
	err = store.Remove("doc1")
	if err != nil {
		t.Fatal(err)
	}
	docs, _ = store.Count()
	if docs != 1 {
		t.Fatalf("expected 1 doc after remove, got %d", docs)
	}
}

func TestCosineSimilarity(t *testing.T) {
	// 相同向量 → 1.0
	a := []float64{1, 2, 3}
	if s := cosineSimilarity(a, a); math.Abs(s-1.0) > 1e-10 {
		t.Fatalf("expected 1.0, got %f", s)
	}

	// 正交向量 → 0.0
	b := []float64{0, 0, 1}
	c := []float64{1, 0, 0}
	if s := cosineSimilarity(b, c); math.Abs(s) > 1e-10 {
		t.Fatalf("expected 0.0, got %f", s)
	}

	// 空向量
	if s := cosineSimilarity(nil, nil); s != 0 {
		t.Fatalf("expected 0 for nil, got %f", s)
	}
}

func TestChunkText(t *testing.T) {
	text := "This is a test. It has multiple sentences. Each sentence contributes to the text. The text should be chunked properly."

	chunks := ChunkText(text, 50, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
	}

	// 每个 chunk 不应超过 chunkSize
	for i, c := range chunks {
		if len([]rune(c)) > 50 {
			t.Fatalf("chunk %d exceeds 50 chars: %d", i, len([]rune(c)))
		}
	}

	// 短文本不分块
	short := "Short text."
	chunks = ChunkText(short, 100, 10)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short text, got %d", len(chunks))
	}
	if chunks[0] != short {
		t.Fatalf("expected %q, got %q", short, chunks[0])
	}
}
