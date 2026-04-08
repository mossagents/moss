package adapters

import (
	"context"
	mdl "github.com/mossagents/moss/kernel/model"
	"testing"
)

// stubEmbedder 用于测试，返回固定维度的随机向量（基于文本哈希）。
type stubEmbedder struct{}

func (e *stubEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	return hashToVec(text, 4), nil
}

func (e *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = hashToVec(t, 4)
	}
	return out, nil
}

func (e *stubEmbedder) Dimension() int { return 4 }

// hashToVec converts a string into a small deterministic float64 vector.
func hashToVec(s string, dim int) []float64 {
	v := make([]float64, dim)
	for i, c := range s {
		v[i%dim] += float64(c)
	}
	// normalize
	var norm float64
	for _, x := range v {
		norm += x * x
	}
	if norm > 0 {
		for i := range v {
			v[i] /= norm
		}
	}
	return v
}

func TestMemoryVectorStore_UpsertAndSearch(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()
	emb := &stubEmbedder{}

	docs := []mdl.VectorDoc{
		{ID: "d1", Text: "golang programming language"},
		{ID: "d2", Text: "python machine learning"},
		{ID: "d3", Text: "go concurrency patterns"},
	}
	if err := store.UpsertWithEmbedder(ctx, emb, docs); err != nil {
		t.Fatal(err)
	}

	n, err := store.Count(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 docs, got %d", n)
	}

	results, err := store.Search(ctx, emb, mdl.VectorQuery{
		Text:  "golang",
		Limit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestMemoryVectorStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()
	emb := &stubEmbedder{}

	docs := []mdl.VectorDoc{
		{ID: "a", Text: "alpha", Embedding: emb.hashToVec("alpha", 4)},
		{ID: "b", Text: "beta", Embedding: emb.hashToVec("beta", 4)},
	}
	if err := store.Upsert(ctx, docs); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	n, _ := store.Count(ctx, "")
	if n != 1 {
		t.Fatalf("expected 1 doc after delete, got %d", n)
	}
}

func TestMemoryVectorStore_Namespace(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryVectorStore()
	emb := &stubEmbedder{}

	docs := []mdl.VectorDoc{
		{ID: "x1", Namespace: "ns1", Text: "hello world"},
		{ID: "x2", Namespace: "ns2", Text: "hello world"},
	}
	if err := store.UpsertWithEmbedder(ctx, emb, docs); err != nil {
		t.Fatal(err)
	}
	n1, _ := store.Count(ctx, "ns1")
	n2, _ := store.Count(ctx, "ns2")
	if n1 != 1 || n2 != 1 {
		t.Fatalf("namespace counts wrong: ns1=%d ns2=%d", n1, n2)
	}
}

// helper to expose private hashToVec for test
func (e *stubEmbedder) hashToVec(s string, dim int) []float64 {
	return hashToVec(s, dim)
}
