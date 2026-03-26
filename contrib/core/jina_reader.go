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

var JinaReaderSpec = tool.ToolSpec{
	Name:        "jina_reader",
	Description: "使用 Jina Reader API 读取网页内容，将网页转换为干净的 Markdown 文本，便于 LLM 处理。需要设置环境变量 JINA_API_KEY",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"url":             {"type": "string", "description": "要读取的网页 URL"},
			"target_selector": {"type": "string", "description": "CSS 选择器，仅提取页面的指定部分"},
			"remove_selector": {"type": "string", "description": "CSS 选择器，删除页面中的指定元素"},
			"token_budget":    {"type": "integer", "description": "Token 预算限制，超出则拒绝请求"}
		},
		"required": ["url"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"reader", "web"},
}

func JinaReaderHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var args struct {
			URL            string `json:"url"`
			TargetSelector string `json:"target_selector"`
			RemoveSelector string `json:"remove_selector"`
			TokenBudget    int    `json:"token_budget"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if args.URL == "" {
			return nil, fmt.Errorf("url is required")
		}

		apiURL := fmt.Sprintf("https://r.jina.ai/%s", url.PathEscape(args.URL))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("X-Retain-Images", "none")
		if key := jinaAPIKey(); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		if args.TargetSelector != "" {
			req.Header.Set("X-Target-Selector", args.TargetSelector)
		}
		if args.RemoveSelector != "" {
			req.Header.Set("X-Remove-Selector", args.RemoveSelector)
		}
		if args.TokenBudget > 0 {
			req.Header.Set("X-Token-Budget", fmt.Sprintf("%d", args.TokenBudget))
		}

		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("jina reader request failed: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("jina reader returned %d: %s", resp.StatusCode, string(body))
		}

		// Jina 返回 { "code": 200, "status": 20000, "data": ... }
		var envelope struct {
			Code   int             `json:"code"`
			Status int             `json:"status"`
			Data   json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return body, nil
		}
		if envelope.Data != nil {
			return envelope.Data, nil
		}
		return body, nil
	}
}

// RegisterJinaReader 便捷注册方法。
func RegisterJinaReader(reg tool.Registry) {
	_ = reg.Register(JinaReaderSpec, JinaReaderHandler())
}
