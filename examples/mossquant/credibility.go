package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/tool"
	"net/url"
	"strings"
	"time"
)

type credibilitySource struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	SourceType  string `json:"source_type,omitempty"`
}

type credibilityResult struct {
	URL         string   `json:"url,omitempty"`
	Domain      string   `json:"domain,omitempty"`
	Title       string   `json:"title,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	Score       int      `json:"score"`
	Level       string   `json:"level"`
	Reliable    bool     `json:"reliable"`
	Reasons     []string `json:"reasons,omitempty"`
	PublishedAt string   `json:"published_at,omitempty"`
}

func registerCredibilityTools(reg tool.Registry) error {
	spec := tool.ToolSpec{
		Name:        "assess_source_credibility",
		Description: "Assess the credibility of one or more information sources and return a scored rationale. Use this before relying on policy, market, or geopolitical sources in an advisory report.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"sources":{
					"type":"array",
					"items":{
						"type":"object",
						"properties":{
							"url":{"type":"string"},
							"title":{"type":"string"},
							"publisher":{"type":"string"},
							"published_at":{"type":"string"},
							"source_type":{"type":"string"}
						},
						"required":["url"]
					}
				}
			},
			"required":["sources"]
		}`),
		Risk:         tool.RiskLow,
		Capabilities: []string{"analysis", "credibility", "advisory"},
	}
	handler := func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Sources []credibilitySource `json:"sources"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if len(params.Sources) == 0 {
			return nil, fmt.Errorf("sources are required")
		}
		results := make([]credibilityResult, 0, len(params.Sources))
		total := 0
		for _, source := range params.Sources {
			result := assessSourceCredibility(source)
			results = append(results, result)
			total += result.Score
		}
		payload := map[string]any{
			"count":         len(results),
			"average_score": float64(total) / float64(len(results)),
			"sources":       results,
		}
		return json.Marshal(payload)
	}
	return reg.Register(spec, handler)
}

func assessSourceCredibility(source credibilitySource) credibilityResult {
	result := credibilityResult{
		URL:         source.URL,
		Title:       source.Title,
		Publisher:   source.Publisher,
		PublishedAt: source.PublishedAt,
		Score:       50,
	}

	parsed, err := url.Parse(source.URL)
	if err == nil {
		result.Domain = strings.ToLower(parsed.Hostname())
	}

	add := func(delta int, reason string) {
		result.Score += delta
		if reason != "" {
			result.Reasons = append(result.Reasons, reason)
		}
	}

	domain := result.Domain
	switch {
	case strings.HasSuffix(domain, ".gov"), strings.HasSuffix(domain, ".gov.cn"), strings.HasSuffix(domain, ".edu"), strings.HasSuffix(domain, ".org.cn"):
		add(30, "Official or institutional domain")
	case domainIn(domain, "reuters.com", "apnews.com", "bloomberg.com", "ft.com", "wsj.com", "worldgoldcouncil.org", "imf.org", "worldbank.org", "federalreserve.gov", "ecb.europa.eu", "gov.cn", "state.gov", "who.int", "oecd.org"):
		add(22, "Recognized primary or top-tier source")
	}

	sourceType := strings.ToLower(strings.TrimSpace(source.SourceType))
	switch sourceType {
	case "official", "government", "regulator", "central-bank", "exchange", "filing":
		add(18, "Declared source type is primary/official")
	case "major-wire", "major-news", "research":
		add(10, "Declared source type is generally reputable")
	case "blog", "forum", "social", "opinion", "influencer":
		add(-20, "Declared source type is weak or subjective")
	}

	titleLower := strings.ToLower(source.Title)
	if containsAny(titleLower, "opinion", "commentary", "rumor", "论坛", "传闻", "观点") {
		add(-15, "Title suggests commentary or rumor rather than primary reporting")
	}

	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source.URL)), "https://") {
		add(3, "HTTPS source")
	}

	if source.PublishedAt != "" {
		if published, ok := parseAnyTime(source.PublishedAt); ok {
			age := time.Since(published)
			switch {
			case age <= 72*time.Hour:
				add(6, "Freshly published")
			case age > 180*24*time.Hour:
				add(-10, "Source may be stale")
			}
		}
	}

	if containsAny(domain, "substack.com", "medium.com", "x.com", "twitter.com", "youtube.com", "reddit.com", "weibo.com") {
		add(-18, "Platform domain is user-generated and should be treated cautiously")
	}

	if result.Score < 0 {
		result.Score = 0
	}
	if result.Score > 100 {
		result.Score = 100
	}

	switch {
	case result.Score >= 75:
		result.Level = "high"
	case result.Score >= 55:
		result.Level = "medium"
	default:
		result.Level = "low"
	}
	result.Reliable = result.Level != "low"
	return result
}

func domainIn(domain string, domains ...string) bool {
	for _, candidate := range domains {
		if domain == candidate || strings.HasSuffix(domain, "."+candidate) {
			return true
		}
	}
	return false
}

func containsAny(value string, keys ...string) bool {
	for _, key := range keys {
		if strings.Contains(value, key) {
			return true
		}
	}
	return false
}

func parseAnyTime(value string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02",
		"2006-1-2",
		"2006/01/02",
		"2006/1/2",
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
