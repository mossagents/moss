package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/mossagents/moss/kernel/tool"
)

var JinaSearchSpec = tool.ToolSpec{
	Name:        "jina_search",
	Description: "使用 Jina Search API 进行 Web 搜索，返回搜索结果列表（标题、URL、摘要）。需要设置环境变量 JINA_API_KEY",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query":  {"type": "string",  "description": "搜索关键词"},
			"count":  {"type": "integer", "description": "返回结果数量，1-20，默认 5"},
			"gl":     {"type": "string",  "description": "国家/地区代码，例如 cn、us"},
			"hl":     {"type": "string",  "description": "语言代码，例如 zh、en"}
		},
		"required": ["query"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"search", "web"},
}

func JinaSearchHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			Query string `json:"query"`
			Count int    `json:"count"`
			GL    string `json:"gl"`
			HL    string `json:"hl"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if args.Count <= 0 || args.Count > 20 {
			args.Count = 5
		}

		apiURL := fmt.Sprintf("https://s.jina.ai/%s", url.PathEscape(args.Query))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Retain-Images", "none")
		if key := jinaAPIKey(); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		if args.GL != "" {
			q := req.URL.Query()
			q.Set("gl", args.GL)
			req.URL.RawQuery = q.Encode()
		}
		if args.HL != "" {
			q := req.URL.Query()
			q.Set("hl", args.HL)
			req.URL.RawQuery = q.Encode()
		}
		{
			q := req.URL.Query()
			q.Set("count", fmt.Sprintf("%d", args.Count))
			req.URL.RawQuery = q.Encode()
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

		// Jina 返回 { "code": 200, "status": 20000, "data": ... }
		var envelope struct {
			Code   int             `json:"code"`
			Status int             `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			// 如果无法解析 envelope，直接返回原始响应
			return body, nil
		}
		if envelope.Data != nil {
			return envelope.Data, nil
		}
		return body, nil
	}
}

// RegisterJinaSearch 便捷注册方法。
func RegisterJinaSearch(reg tool.Registry) {
	_ = reg.Register(JinaSearchSpec, JinaSearchHandler())
}
