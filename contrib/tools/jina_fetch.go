package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/mossagents/moss/kernel/tool"
)

var JinaFetchSpec = tool.ToolSpec{
	Name:        "jina_fetch",
	Description: "使用 Jina AI Reader 抓取网页并转换为 Markdown，适合读取文章、文档等任意网址的完整内容",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string", "description": "要抓取的网页 URL"}
		},
		"required": ["url"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"network"},
}

type jinaFetchEnvelope struct {
	Code int             `json:"code"`
	Data jinaFetchResult `json:"data"`
}

type jinaFetchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

func JinaFetchHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if args.URL == "" {
			return nil, fmt.Errorf("url is required")
		}

		readerURL := "https://r.jina.ai/" + args.URL
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, readerURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "moss-jina-fetch")
		if key := os.Getenv("JINA_API_KEY"); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("jina fetch request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("jina fetch returned %d: %s", resp.StatusCode, string(body))
		}

		var envelope jinaFetchEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("parse fetch response: %w", err)
		}

		return json.Marshal(jinaFetchResult{
			Title:   envelope.Data.Title,
			URL:     envelope.Data.URL,
			Content: envelope.Data.Content,
		})
	}
}

// RegisterJinaFetch 便捷注册 jina_fetch 工具。
func RegisterJinaFetch(reg tool.Registry) {
	_ = reg.Register(tool.NewRawTool(JinaFetchSpec, JinaFetchHandler()))
}
