package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/knowledge"
)

// NewMemoryStore 返回官方提供的内存知识库实现。
func NewMemoryKnowledgeStore() *knowledge.MemoryStore {
	return knowledge.NewMemoryStore()
}

// RegisterTools 将 knowledge 能力作为标准扩展工具集接入 Kernel。
func RegisterKnowledgeTools(k *kernel.Kernel, store knowledge.Store, embedder model.Embedder) error {
	return register(k.ToolRegistry(), store, embedder)
}

func register(reg tool.Registry, store knowledge.Store, embedder model.Embedder) error {
	tools := []struct {
		spec    tool.ToolSpec
		handler tool.ToolHandler
	}{
		{ingestDocSpec, ingestDocHandler(store, embedder)},
		{knowledgeSearchSpec, knowledgeSearchHandler(store, embedder)},
		{knowledgeListSpec, knowledgeListHandler(store)},
	}
	for _, t := range tools {
		if err := reg.Register(tool.NewRawTool(t.spec, t.handler)); err != nil {
			return err
		}
	}
	return nil
}

var ingestDocSpec = tool.ToolSpec{
	Name: "ingest_document",
	Description: `Ingest a document into the knowledge base for later semantic search.
The text will be automatically chunked and embedded. Use this after reading or
crawling a page to store it for future reference.

Example: ingest_document(id="readme", source="README.md", text="...", chunk_size=800)`,
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
	Risk:         tool.RiskLow,
	Capabilities: []string{"knowledge"},
}

func ingestDocHandler(store knowledge.Store, embedder model.Embedder) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			ID        string `json:"id"`
			Source    string `json:"source"`
			Text      string `json:"text"`
			ChunkSize int    `json:"chunk_size"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		chunks := knowledge.ChunkText(params.Text, params.ChunkSize, 0)
		if err := store.Add(ctx, embedder, params.ID, params.Source, chunks, nil); err != nil {
			return nil, fmt.Errorf("ingest document: %w", err)
		}

		docs, totalChunks := store.Count()
		return json.Marshal(map[string]any{
			"status":       "ingested",
			"id":           params.ID,
			"source":       params.Source,
			"chunks":       len(chunks),
			"total_docs":   docs,
			"total_chunks": totalChunks,
		})
	}
}

var knowledgeSearchSpec = tool.ToolSpec{
	Name: "knowledge_search",
	Description: `Search the knowledge base for text relevant to a query.
Returns the most relevant text chunks ranked by semantic similarity.

Example: knowledge_search(query="how to deploy", limit=5)`,
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Search query in natural language"},
			"limit": {"type": "integer", "description": "Maximum number of results (default: 5)"}
		},
		"required": ["query"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"knowledge"},
}

func knowledgeSearchHandler(store knowledge.Store, embedder model.Embedder) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}

		limit := params.Limit
		if limit <= 0 {
			limit = 5
		}

		results, err := store.Search(ctx, embedder, params.Query, limit)
		if err != nil {
			return nil, fmt.Errorf("knowledge search: %w", err)
		}

		return json.Marshal(map[string]any{
			"query":   params.Query,
			"count":   len(results),
			"results": results,
		})
	}
}

var knowledgeListSpec = tool.ToolSpec{
	Name:         "knowledge_list",
	Description:  "List all documents currently stored in the knowledge base with chunk counts.",
	InputSchema:  json.RawMessage(`{"type": "object", "properties": {}}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"knowledge"},
}

func knowledgeListHandler(store knowledge.Store) tool.ToolHandler {
	return func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		summaries := store.List()
		docs, chunks := store.Count()
		return json.Marshal(map[string]any{
			"documents":    summaries,
			"total_docs":   docs,
			"total_chunks": chunks,
		})
	}
}
