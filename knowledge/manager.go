// Package knowledge 提供 Agent 的多层记忆体系。
//
// 三层架构：
//
//   - WorkingMemory：当前 session 激活状态（临时变量、目标、对话摘要）
//   - EpisodicStore：历史事件序列（工具调用、决策、错误）
//   - VectorStore：持久知识库（文档、代码片段，通过向量检索）
//
// 通过 MemoryManager 统一访问，RAGMiddleware 在每轮 LLM 调用前自动注入。
package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

// MemoryManager 聚合三层记忆，提供统一访问和 context 注入接口。
type MemoryManager struct {
	Working  *WorkingMemory
	Episodic EpisodicStore
	Semantic port.VectorStore
	Embedder port.Embedder
}

// MemoryManagerConfig 配置 MemoryManager。
type MemoryManagerConfig struct {
	// Working 可选，不提供则使用空的 WorkingMemory。
	Working *WorkingMemory
	// Episodic 可选，不提供则使用内存实现。
	Episodic EpisodicStore
	// Semantic 可选，不提供则禁用语义搜索。
	Semantic port.VectorStore
	// Embedder 当 Semantic 不为 nil 时必须提供。
	Embedder port.Embedder
}

// NewMemoryManager 创建 MemoryManager。
func NewMemoryManager(cfg MemoryManagerConfig) *MemoryManager {
	if cfg.Working == nil {
		cfg.Working = NewWorkingMemory()
	}
	if cfg.Episodic == nil {
		cfg.Episodic = NewMemoryEpisodicStore()
	}
	return &MemoryManager{
		Working:  cfg.Working,
		Episodic: cfg.Episodic,
		Semantic: cfg.Semantic,
		Embedder: cfg.Embedder,
	}
}

// InjectConfig 控制 context 注入的参数。
type InjectConfig struct {
	SessionID    string
	Query        string        // 语义检索的查询文本（通常是最后一条 user message）
	EpisodicN    int           // 最近事件数，默认 10
	SemanticK    int           // 语义检索结果数，默认 5
	Threshold    float64       // 相似度阈值，默认 0.7
	MaxChars     int           // 注入内容最大字符数，默认 4000
}

func (c *InjectConfig) episodicN() int {
	if c.EpisodicN <= 0 {
		return 10
	}
	return c.EpisodicN
}

func (c *InjectConfig) semanticK() int {
	if c.SemanticK <= 0 {
		return 5
	}
	return c.SemanticK
}

func (c *InjectConfig) threshold() float64 {
	if c.Threshold <= 0 {
		return 0.7
	}
	return c.Threshold
}

func (c *InjectConfig) maxChars() int {
	if c.MaxChars <= 0 {
		return 4000
	}
	return c.MaxChars
}

// Inject 生成注入到 LLM context 的记忆块文本。
// 返回的字符串可以作为 system message 的附加内容。
func (m *MemoryManager) Inject(ctx context.Context, cfg InjectConfig) (string, error) {
	var sections []string

	// 1. Working Memory
	if wSummary := m.Working.Summary(ctx); wSummary != "" {
		sections = append(sections, "<working_memory>\n"+wSummary+"</working_memory>")
	}

	// 2. Episodic Memory
	if m.Episodic != nil && cfg.SessionID != "" {
		episodes, err := m.Episodic.Recent(ctx, cfg.SessionID, cfg.episodicN())
		if err == nil && len(episodes) > 0 {
			sections = append(sections, "<recent_events>\n"+formatEpisodes(episodes)+"</recent_events>")
		}
	}

	// 3. Semantic Memory
	if m.Semantic != nil && m.Embedder != nil && cfg.Query != "" {
		results, err := m.Semantic.Search(ctx, m.Embedder, port.VectorQuery{
			Text:      cfg.Query,
			Limit:     cfg.semanticK(),
			Threshold: cfg.threshold(),
		})
		if err == nil && len(results) > 0 {
			sections = append(sections, "<relevant_knowledge>\n"+formatVectorResults(results)+"</relevant_knowledge>")
		}
	}

	if len(sections) == 0 {
		return "", nil
	}

	full := "<memory_context>\n" + strings.Join(sections, "\n") + "\n</memory_context>"
	maxChars := cfg.maxChars()
	if len(full) > maxChars {
		full = full[:maxChars] + "\n...[truncated]"
	}
	return full, nil
}

// Record 记录一条事件到 Episodic Memory。
func (m *MemoryManager) Record(ctx context.Context, ep Episode) error {
	if m.Episodic == nil {
		return nil
	}
	return m.Episodic.Append(ctx, ep)
}

// RecordToolCall 便捷方法：记录工具调用事件。
func (m *MemoryManager) RecordToolCall(ctx context.Context, sessionID, toolName, summary string) error {
	return m.Record(ctx, Episode{
		SessionID:  sessionID,
		Kind:       EpisodeToolCall,
		Summary:    fmt.Sprintf("%s: %s", toolName, summary),
		Importance: 0.5,
		Timestamp:  time.Now(),
	})
}

// RecordError 便捷方法：记录错误事件。
func (m *MemoryManager) RecordError(ctx context.Context, sessionID, errMsg string) error {
	return m.Record(ctx, Episode{
		SessionID:  sessionID,
		Kind:       EpisodeError,
		Summary:    errMsg,
		Importance: 0.8,
		Timestamp:  time.Now(),
	})
}

// Learn 将文档加入 Semantic Memory（自动嵌入）。
func (m *MemoryManager) Learn(ctx context.Context, docs []port.VectorDoc) error {
	if m.Semantic == nil {
		return fmt.Errorf("memory manager: semantic store not configured")
	}
	if m.Embedder == nil {
		return fmt.Errorf("memory manager: embedder not configured")
	}
	// 为缺少 Embedding 的文档生成向量
	toEmbed := make([]string, 0, len(docs))
	needEmbed := make([]int, 0, len(docs))
	for i, d := range docs {
		if len(d.Embedding) == 0 {
			toEmbed = append(toEmbed, d.Text)
			needEmbed = append(needEmbed, i)
		}
	}
	if len(toEmbed) > 0 {
		embeddings, err := m.Embedder.EmbedBatch(ctx, toEmbed)
		if err != nil {
			return fmt.Errorf("memory manager: embed: %w", err)
		}
		for j, idx := range needEmbed {
			docs[idx].Embedding = embeddings[j]
		}
	}
	return m.Semantic.Upsert(ctx, docs)
}

// ---- 格式化辅助 ----------------------------------------------------------

func formatEpisodes(eps []Episode) string {
	var sb strings.Builder
	for _, ep := range eps {
		sb.WriteString(fmt.Sprintf("- [%s][%s] %s\n",
			ep.Timestamp.Format("15:04"), ep.Kind, ep.Summary))
	}
	return sb.String()
}

func formatVectorResults(results []port.VectorResult) string {
	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("[相关度: %.2f] %s\n", r.Score, r.Doc.Text))
	}
	return sb.String()
}
