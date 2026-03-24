// Package embedding 提供文本嵌入模型的适配器。
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/mossagi/moss/kernel/port"
)

// 确保实现 port.Embedder 接口。
var _ port.Embedder = (*OpenAIEmbedder)(nil)

const (
	DefaultModel     = "text-embedding-3-small"
	DefaultDimension = 1536
	DefaultBaseURL   = "https://api.openai.com/v1"
)

// OpenAIEmbedder 是兼容 OpenAI Embeddings API 的适配器。
// 支持 OpenAI、Azure OpenAI 和任何兼容的本地模型 API。
type OpenAIEmbedder struct {
	apiKey  string
	baseURL string
	model   string
	dim     int
	client  *http.Client
}

// Option 是 OpenAIEmbedder 的配置选项。
type Option func(*OpenAIEmbedder)

// WithModel 设置嵌入模型名称。
func WithModel(model string) Option {
	return func(e *OpenAIEmbedder) { e.model = model }
}

// WithDimension 设置输出向量维度。
func WithDimension(dim int) Option {
	return func(e *OpenAIEmbedder) { e.dim = dim }
}

// New 创建 OpenAI 兼容的嵌入适配器。
// apiKey 为空时从 OPENAI_API_KEY 环境变量读取。
func New(apiKey string, opts ...Option) *OpenAIEmbedder {
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	e := &OpenAIEmbedder{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		model:   DefaultModel,
		dim:     DefaultDimension,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// NewWithBaseURL 创建指定 baseURL 的嵌入适配器。
func NewWithBaseURL(apiKey, baseURL string, opts ...Option) *OpenAIEmbedder {
	e := New(apiKey, opts...)
	if baseURL != "" {
		e.baseURL = baseURL
	}
	return e
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	results, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body := map[string]any{
		"model": e.model,
		"input": texts,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embedding API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	var result embeddingResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	// 按 index 排序（API 不保证顺序）
	embeddings := make([][]float64, len(texts))
	for _, item := range result.Data {
		if item.Index < len(embeddings) {
			embeddings[item.Index] = item.Embedding
		}
	}

	return embeddings, nil
}

func (e *OpenAIEmbedder) Dimension() int {
	return e.dim
}

// ─── API types ──────────────────────────────────────

type embeddingResponse struct {
	Data  []embeddingData `json:"data"`
	Model string          `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

type embeddingData struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}
