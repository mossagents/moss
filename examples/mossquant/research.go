package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mossagents/moss/agent"
	"github.com/mossagents/moss/appkit"
	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
)

type jinaSearchParams struct {
	Query string `json:"query"`
	Count int    `json:"count"`
	GL    string `json:"gl"`
	HL    string `json:"hl"`
}

type jinaReaderParams struct {
	URL            string `json:"url"`
	TargetSelector string `json:"target_selector"`
	RemoveSelector string `json:"remove_selector"`
	TokenBudget    int    `json:"token_budget"`
}

func registerResearchTools(reg tool.Registry) error {
	if err := reg.Register(jinaSearchSpec, jinaSearchHandler()); err != nil {
		return fmt.Errorf("register jina_search: %w", err)
	}
	if err := reg.Register(jinaReaderSpec, jinaReaderHandler()); err != nil {
		return fmt.Errorf("register jina_reader: %w", err)
	}
	return nil
}

var jinaSearchSpec = tool.ToolSpec{
	Name:        "jina_search",
	Description: "Search the web via Jina Search and return extracted result content for current market, policy, and geopolitical research.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"query":{"type":"string","description":"search query"},
			"count":{"type":"integer","description":"result count (1-20)"},
			"gl":{"type":"string","description":"country code, e.g. us/cn"},
			"hl":{"type":"string","description":"language code, e.g. en/zh-CN"}
		},
		"required":["query"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"search", "web", "research"},
}

func jinaSearchHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params jinaSearchParams
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(params.Query) == "" {
			return nil, fmt.Errorf("query is required")
		}
		if params.Count <= 0 || params.Count > 20 {
			params.Count = 5
		}

		req, err := newJinaSearchRequest(ctx, params)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("jina search %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return unwrapJinaPayload(body)
	}
}

var jinaReaderSpec = tool.ToolSpec{
	Name:        "jina_reader",
	Description: "Read a webpage via Jina Reader and return extracted content for analysis.",
	InputSchema: json.RawMessage(`{
		"type":"object",
		"properties":{
			"url":{"type":"string","description":"target page url"},
			"target_selector":{"type":"string","description":"optional CSS selector to focus on"},
			"remove_selector":{"type":"string","description":"optional CSS selector to remove"},
			"token_budget":{"type":"integer","description":"optional max token budget"}
		},
		"required":["url"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"reader", "web", "research"},
}

func jinaReaderHandler() tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params jinaReaderParams
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		if strings.TrimSpace(params.URL) == "" {
			return nil, fmt.Errorf("url is required")
		}

		req, err := newJinaReaderRequest(ctx, params)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("jina reader %s: %s", resp.Status, strings.TrimSpace(string(body)))
		}
		return unwrapJinaPayload(body)
	}
}

func unwrapJinaPayload(body []byte) (json.RawMessage, error) {
	var envelope struct {
		Code   int             `json:"code"`
		Status int             `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		return envelope.Data, nil
	}
	return body, nil
}

func newJinaSearchRequest(ctx context.Context, params jinaSearchParams) (*http.Request, error) {
	endpoint := "https://s.jina.ai/" + url.QueryEscape(params.Query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Retain-Images", "none")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	q := req.URL.Query()
	if params.GL != "" {
		q.Set("gl", params.GL)
	}
	if params.HL != "" {
		q.Set("hl", params.HL)
	}
	q.Set("count", fmt.Sprintf("%d", params.Count))
	req.URL.RawQuery = q.Encode()
	return req, nil
}

func newJinaReaderRequest(ctx context.Context, params jinaReaderParams) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://r.jina.ai/"+url.PathEscape(params.URL), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Retain-Images", "none")
	if key := os.Getenv("JINA_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if params.TargetSelector != "" {
		req.Header.Set("X-Target-Selector", params.TargetSelector)
	}
	if params.RemoveSelector != "" {
		req.Header.Set("X-Remove-Selector", params.RemoveSelector)
	}
	if params.TokenBudget > 0 {
		req.Header.Set("X-Token-Budget", fmt.Sprintf("%d", params.TokenBudget))
	}
	return req, nil
}

func registerResearchAgents(k *kernel.Kernel, flags *appkit.AppFlags) error {
	researchPrompt := strings.TrimSpace(fmt.Sprintf(`
You are an investment research sub-agent. Today's date is %s.

Your job is to gather evidence for the main mossquant advisor, not to make the final decision alone.

Available tools:
- get_investor_profile
- jina_search
- jina_reader
- assess_source_credibility

Rules:
1. Start from the investor profile and tracked assets.
2. Prefer official, regulatory, exchange, central-bank, and top-tier news sources.
3. Before trusting any important source, assess it with assess_source_credibility.
4. Focus on current price drivers, policy changes, and geopolitical or macro events that materially affect the tracked assets.
5. Return concise findings with citations and explicit source credibility notes.
6. If evidence is mixed or weak, say so clearly.
`, time.Now().Format("2006-01-02")))

	reg := runtime.AgentRegistry(k)
	if err := reg.Register(agent.AgentConfig{
		Name:         "market-researcher",
		Description:  "Focused web researcher for asset news, policy updates, and macro/geopolitical drivers.",
		SystemPrompt: researchPrompt,
		Tools:        []string{"get_investor_profile", "jina_search", "jina_reader", "assess_source_credibility"},
		MaxSteps:     20,
		TrustLevel:   flags.Trust,
	}); err != nil {
		return err
	}

	reviewerPrompt := strings.TrimSpace(fmt.Sprintf(`
You are an investment review sub-agent. Today's date is %s.

Your role is to challenge and audit a draft recommendation before it is shown to the user.

Available tools:
- get_investor_profile
- jina_search
- jina_reader
- assess_source_credibility
- get_market_data
- analyze_market
- read_file
- read_memory

Review checklist:
1. Does the recommendation fit the investor's stated risk tolerance and constraints?
2. Are the cited sources credible enough, current enough, and relevant enough?
3. Is the reasoning balanced, or is it overconfident / one-sided?
4. Are downside scenarios, uncertainty, and invalidation conditions explained?
5. Is there any unsupported leap from evidence to conclusion?

Return:
- verdict: approve / revise
- major issues
- missing evidence or caveats
- a corrected concise recommendation if needed
`, time.Now().Format("2006-01-02")))

	return reg.Register(agent.AgentConfig{
		Name:         "investment-reviewer",
		Description:  "Risk and evidence reviewer for draft investment recommendations.",
		SystemPrompt: reviewerPrompt,
		Tools:        []string{"get_investor_profile", "jina_search", "jina_reader", "assess_source_credibility", "get_market_data", "analyze_market", "read_file", "read_memory"},
		MaxSteps:     16,
		TrustLevel:   flags.Trust,
	})
}
