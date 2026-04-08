package model

import (
	"context"
)

// Embedder 将文本转换为向量嵌入。
// 适配 OpenAI text-embedding-3-small 或兼容 API。
type Embedder interface {
	// Embed 返回文本的向量嵌入。
	Embed(ctx context.Context, text string) ([]float64, error)

	// EmbedBatch 批量嵌入多段文本（减少 API 调用）。
	EmbedBatch(ctx context.Context, texts []string) ([][]float64, error)

	// Dimension 返回嵌入向量的维度。
	Dimension() int
}
