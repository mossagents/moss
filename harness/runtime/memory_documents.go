package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	extknowledge "github.com/mossagents/moss/harness/extensions/knowledge"
	memstore "github.com/mossagents/moss/harness/runtime/memory"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/x/stringutil"
)

const (
	documentMemoryKind      = "knowledge.document"
	documentMemoryTag       = "knowledge"
	defaultDocumentChunkLen = 1000
)

type knowledgeSearchResult struct {
	DocID    string  `json:"doc_id"`
	Source   string  `json:"source"`
	Text     string  `json:"text"`
	Score    float64 `json:"score"`
	ChunkIdx int     `json:"chunk_index"`
	Path     string  `json:"path"`
}

type knowledgeDocumentSummary struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Chunks int    `json:"chunks"`
}

var ingestDocumentSpec = tool.ToolSpec{
	Name: "ingest_document",
	Description: `Ingest a document into the unified memory store for later retrieval.
The text will be chunked, embedded, and persisted as memory records.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"id":         {"type": "string", "description": "Unique document identifier"},
			"source":     {"type": "string", "description": "Source label (filename, URL, etc.)"},
			"text":       {"type": "string", "description": "Full document text to ingest"},
			"chunk_size": {"type": "integer", "description": "Optional chunk size in characters (default: 1000)"}
		},
		"required": ["id", "source", "text"]
	}`),
	Risk:         tool.RiskMedium,
	Capabilities: []string{"memory", "knowledge"},
}

var knowledgeSearchSpec = tool.ToolSpec{
	Name:        "knowledge_search",
	Description: `Search ingested document memories by semantic similarity and return the most relevant chunks.`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query in natural language"},
			"limit": {"type": "integer", "description": "Maximum number of results (default: 5)"}
		},
		"required": ["query"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory", "knowledge"},
}

var knowledgeListSpec = tool.ToolSpec{
	Name:         "knowledge_list",
	Description:  "List document memories currently stored in the unified memory store.",
	InputSchema:  json.RawMessage(`{"type": "object", "properties": {}}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"memory", "knowledge"},
}

func ingestDocumentHandler(store memstore.ExtendedMemoryStore, embedder model.Embedder, pipeline *memstore.PipelineManager) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if embedder == nil {
			return nil, fmt.Errorf("memory document embedder is not configured")
		}
		var params struct {
			ID        string `json:"id"`
			Source    string `json:"source"`
			Text      string `json:"text"`
			ChunkSize int    `json:"chunk_size"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		params.ID = strings.TrimSpace(params.ID)
		params.Source = strings.TrimSpace(strings.ReplaceAll(params.Source, "\\", "/"))
		if params.ID == "" {
			return nil, fmt.Errorf("id is required")
		}
		if params.Source == "" {
			return nil, fmt.Errorf("source is required")
		}
		chunks := extknowledge.ChunkText(params.Text, params.ChunkSize, 0)
		if len(chunks) == 0 {
			chunks = []string{params.Text}
		}
		embeddings, err := embedder.EmbedBatch(ctx, chunks)
		if err != nil {
			return nil, fmt.Errorf("embed document chunks: %w", err)
		}
		if len(embeddings) != len(chunks) {
			return nil, fmt.Errorf("embed document chunks: unexpected embedding count %d", len(embeddings))
		}
		group := knowledgeGroup(params.ID)
		for idx, chunk := range chunks {
			path := knowledgeChunkPath(params.ID, idx)
			record := memstore.ExtendedMemoryRecord{
				Path:        path,
				Content:     chunk,
				Summary:     summarizeDocumentChunk(chunk),
				Tags:        []string{documentMemoryTag, "document"},
				Scope:       memstore.MemoryScopeRepo,
				Kind:        documentMemoryKind,
				Fingerprint: fmt.Sprintf("%s#%04d", params.ID, idx),
				Confidence:  1.0,
				Stage:       memstore.MemoryStageManual,
				Status:      memstore.MemoryStatusActive,
				Group:       group,
				SourceKind:  documentMemoryKind,
				SourceID:    params.ID,
				SourcePath:  params.Source,
				Metadata: map[string]any{
					"memory_kind": documentMemoryKind,
					"doc_id":      params.ID,
					"doc_source":  params.Source,
					"chunk_index": idx,
					"chunk_count": len(chunks),
					"embedding":   embeddings[idx],
				},
			}
			if _, err := store.UpsertExtended(ctx, record); err != nil {
				return nil, err
			}
		}
		if err := syncMemoryArtifacts(ctx, pipeline); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"status":       "ingested",
			"id":           params.ID,
			"source":       params.Source,
			"chunks":       len(chunks),
			"memory_group": group,
		})
	}
}

func knowledgeSearchHandler(store memstore.ExtendedMemoryStore, embedder model.Embedder) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		if embedder == nil {
			return nil, fmt.Errorf("memory document embedder is not configured")
		}
		var params struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		query := strings.TrimSpace(params.Query)
		if query == "" {
			return nil, fmt.Errorf("query is required")
		}
		limit := params.Limit
		if limit <= 0 {
			limit = 5
		}
		queryEmbedding, err := embedder.Embed(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("embed knowledge query: %w", err)
		}
		records, err := store.ListExtended(ctx, 0)
		if err != nil {
			return nil, err
		}
		results := make([]knowledgeSearchResult, 0, limit)
		usedPaths := make([]string, 0, limit)
		for _, record := range records {
			if record.Status != "" && record.Status != memstore.MemoryStatusActive {
				continue
			}
			if !isDocumentMemoryRecord(record) {
				continue
			}
			embedding, ok := metadataEmbedding(record.Metadata)
			if !ok {
				continue
			}
			score := cosineSimilarity(queryEmbedding, embedding)
			if math.IsNaN(score) || math.IsInf(score, 0) {
				continue
			}
			results = append(results, knowledgeSearchResult{
				DocID:    metadataString(record.Metadata, "doc_id", record.SourceID),
				Source:   metadataString(record.Metadata, "doc_source", record.SourcePath),
				Text:     record.Content,
				Score:    score,
				ChunkIdx: metadataInt(record.Metadata, "chunk_index"),
				Path:     record.Path,
			})
		}
		sort.Slice(results, func(i, j int) bool {
			if results[i].Score != results[j].Score {
				return results[i].Score > results[j].Score
			}
			if results[i].DocID != results[j].DocID {
				return results[i].DocID < results[j].DocID
			}
			return results[i].ChunkIdx < results[j].ChunkIdx
		})
		if len(results) > limit {
			results = results[:limit]
		}
		for _, result := range results {
			usedPaths = append(usedPaths, result.Path)
		}
		if err := recordMemoryUsages(ctx, store, usedPaths); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"query":   query,
			"count":   len(results),
			"results": results,
		})
	}
}

func knowledgeListHandler(store memstore.ExtendedMemoryStore) tool.ToolHandler {
	return func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		records, err := store.ListExtended(ctx, 0)
		if err != nil {
			return nil, err
		}
		summaries := map[string]*knowledgeDocumentSummary{}
		for _, record := range records {
			if record.Status != "" && record.Status != memstore.MemoryStatusActive {
				continue
			}
			if !isDocumentMemoryRecord(record) {
				continue
			}
			docID := metadataString(record.Metadata, "doc_id", record.SourceID)
			if docID == "" {
				continue
			}
			summary, ok := summaries[docID]
			if !ok {
				summary = &knowledgeDocumentSummary{
					ID:     docID,
					Source: metadataString(record.Metadata, "doc_source", record.SourcePath),
				}
				summaries[docID] = summary
			}
			summary.Chunks++
		}
		items := make([]knowledgeDocumentSummary, 0, len(summaries))
		for _, summary := range summaries {
			items = append(items, *summary)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].Source != items[j].Source {
				return items[i].Source < items[j].Source
			}
			return items[i].ID < items[j].ID
		})
		return json.Marshal(map[string]any{
			"documents":    items,
			"total_docs":   len(items),
			"total_chunks": countDocumentChunks(items),
		})
	}
}

func knowledgeChunkPath(docID string, idx int) string {
	return filepath.ToSlash(filepath.Join("knowledge", sanitizeKnowledgeID(docID), fmt.Sprintf("chunk-%04d.md", idx)))
}

func knowledgeGroup(docID string) string {
	return filepath.ToSlash(filepath.Join("knowledge", sanitizeKnowledgeID(docID)))
}

func sanitizeKnowledgeID(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		return "document"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(strings.ToLower(b.String()), "-")
	return stringutil.FirstNonEmpty(out, "document")
}

func isDocumentMemoryRecord(record memstore.ExtendedMemoryRecord) bool {
	return strings.EqualFold(strings.TrimSpace(record.SourceKind), documentMemoryKind) ||
		strings.EqualFold(metadataString(record.Metadata, "memory_kind"), documentMemoryKind)
}

func metadataString(metadata map[string]any, key string, fallback ...string) string {
	if metadata != nil {
		if value, ok := metadata[key]; ok {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	for _, value := range fallback {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func metadataInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func metadataEmbedding(metadata map[string]any) ([]float64, bool) {
	if metadata == nil {
		return nil, false
	}
	raw, ok := metadata["embedding"]
	if !ok {
		return nil, false
	}
	switch current := raw.(type) {
	case []float64:
		return current, len(current) > 0
	case []any:
		out := make([]float64, 0, len(current))
		for _, value := range current {
			switch num := value.(type) {
			case float64:
				out = append(out, num)
			case int:
				out = append(out, float64(num))
			case int64:
				out = append(out, float64(num))
			}
		}
		return out, len(out) > 0
	default:
		return nil, false
	}
}

func cosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func countDocumentChunks(items []knowledgeDocumentSummary) int {
	total := 0
	for _, item := range items {
		total += item.Chunks
	}
	return total
}

func summarizeDocumentChunk(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(text) <= 180 {
		return text
	}
	return strings.TrimSpace(text[:180]) + "..."
}
