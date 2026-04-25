package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/mossagents/moss/kernel/tool"
)

var JinaSearchSpec = tool.ToolSpec{
	Name:        "jina_search",
	Description: "使用 Jina AI 搜索引擎在互联网上检索信息，返回匹配网页的标题、URL 与内容摘要",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query":       {"type": "string",  "description": "搜索查询词"},
			"num_results": {"type": "integer", "description": "返回结果数量（1-10），默认 5"}
		},
		"required": ["query"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"network"},
}

type jinaSearchEnvelope struct {
	Code int                `json:"code"`
	Data []jinaSearchResult `json:"data"`
}

type jinaSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Content     string `json:"content"`
	Description string `json:"description"`
}

type jinaSearchItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func JinaSearchHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Query      string `json:"query"`
			NumResults int    `json:"num_results"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if args.Query == "" {
			return nil, fmt.Errorf("query is required")
		}
		if args.NumResults <= 0 {
			args.NumResults = 5
		}
		if args.NumResults > 10 {
			args.NumResults = 10
		}

		apiURL := "https://s.jina.ai/?q=" + url.QueryEscape(args.Query)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "moss-jina-search")
		if key := os.Getenv("JINA_API_KEY"); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("jina search request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("jina search returned %d: %s", resp.StatusCode, string(body))
		}

		var envelope jinaSearchEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("parse search response: %w", err)
		}

		results := envelope.Data
		if len(results) > args.NumResults {
			results = results[:args.NumResults]
		}

		items := make([]jinaSearchItem, 0, len(results))
		for _, r := range results {
			snippet := r.Description
			if snippet == "" {
				snippet = r.Content
			}
			if len(snippet) > 500 {
				snippet = snippet[:500] + "..."
			}
			items = append(items, jinaSearchItem{
				Title:   r.Title,
				URL:     r.URL,
				Snippet: snippet,
			})
		}
		return json.Marshal(items)
	}
}

// RegisterJinaSearch 便捷注册 jina_search 工具。
func RegisterJinaSearch(reg tool.Registry) {
	_ = reg.Register(tool.NewRawTool(JinaSearchSpec, JinaSearchHandler()))
}

// RegisterJinaTools 同时注册 jina_search 与 jina_read。
func RegisterJinaTools(reg tool.Registry) {
	RegisterJinaSearch(reg)
	RegisterJinaRead(reg)
}
