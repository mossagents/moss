package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/bootstrap"
	appconfig "github.com/mossagents/moss/config"
	"github.com/mossagents/moss/kernel/session"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/scheduler"
	"gopkg.in/yaml.v3"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type InvestorProfile struct {
	Holdings        []Holding `json:"holdings,omitempty"`
	Watchlist       []string  `json:"watchlist,omitempty"`
	RiskTolerance   string    `json:"risk_tolerance,omitempty"`
	InvestmentStyle string    `json:"investment_style,omitempty"`
	ReviewInterval  string    `json:"review_interval,omitempty"`
	DecisionFactors []string  `json:"decision_factors,omitempty"`
	Constraints     []string  `json:"constraints,omitempty"`
	Notes           []string  `json:"notes,omitempty"`
	Warnings        []string  `json:"warnings,omitempty"`
	SourceHints     []string  `json:"source_hints,omitempty"`
	RawText         string    `json:"raw_text,omitempty"`
	LastInterpreted string    `json:"last_interpreted,omitempty"`
}

type Holding struct {
	Asset       string  `json:"asset"`
	Symbol      string  `json:"symbol,omitempty"`
	Quantity    float64 `json:"quantity,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	CostBasis   float64 `json:"cost_basis,omitempty"`
	Currency    string  `json:"currency,omitempty"`
	PriceUnit   string  `json:"price_unit,omitempty"`
	AcquiredAt  string  `json:"acquired_at,omitempty"`
	Description string  `json:"description,omitempty"`
}

type frontMatterProfile struct {
	RiskTolerance   string               `yaml:"risk_tolerance"`
	RiskProfile     string               `yaml:"risk_profile"`
	InvestmentStyle string               `yaml:"investment_style"`
	ReviewInterval  string               `yaml:"review_interval"`
	Holdings        []frontMatterHolding `yaml:"holdings"`
	Watchlist       []string             `yaml:"watchlist"`
	DecisionFactors []string             `yaml:"decision_factors"`
	Factors         []string             `yaml:"factors"`
	Constraints     []string             `yaml:"constraints"`
	Notes           []string             `yaml:"notes"`
}

type frontMatterHolding struct {
	Asset      string  `yaml:"asset"`
	Symbol     string  `yaml:"symbol"`
	Quantity   float64 `yaml:"quantity"`
	Unit       string  `yaml:"unit"`
	CostBasis  float64 `yaml:"cost_basis"`
	Currency   string  `yaml:"currency"`
	PriceUnit  string  `yaml:"price_unit"`
	AcquiredAt string  `yaml:"acquired_at"`
}

var (
	frontMatterPattern  = regexp.MustCompile(`(?s)\A---\s*\n(.*?)\n---\s*(.*)\z`)
	dateAtPricePattern  = regexp.MustCompile(`(?i)(?P<date>\d{4}[年/-]\d{1,2}[月/-]\d{1,2}日?)\s*(?:以|at)\s*(?P<price>\d+(?:\.\d+)?)\s*(?P<currency>元|人民币|美元|usd|cny|rmb)?\s*(?:每|/|per)\s*(?P<price_unit>克|g|gram|grams|股|share|shares|盎司|oz|ounce|ounces|枚|coin)\s*(?:的价格)?\s*(?:购入|买入|买了|购买|bought)\s*(?P<asset>[\p{Han}A-Za-z./+\-]{1,16}?)\s*(?P<qty>\d+(?:\.\d+)?)\s*(?P<qty_unit>克|g|gram|grams|股|share|shares|盎司|oz|ounce|ounces|枚|coin)`)
	boughtOnPattern     = regexp.MustCompile(`(?i)(?:在|on)\s*(?P<date>\d{4}[年/-]\d{1,2}[月/-]\d{1,2}日?)\s*(?:以|at)\s*(?P<price>\d+(?:\.\d+)?)\s*(?P<currency>元|人民币|美元|usd|cny|rmb)?\s*(?:每|/|per)\s*(?P<price_unit>克|g|gram|grams|股|share|shares|盎司|oz|ounce|ounces|枚|coin).*?(?:购入|买入|买了|购买|bought)?\s*(?P<asset>[\p{Han}A-Za-z./+\-]{1,16}?)\s*(?P<qty>\d+(?:\.\d+)?)\s*(?P<qty_unit>克|g|gram|grams|股|share|shares|盎司|oz|ounce|ounces|枚|coin)`)
	plainHoldingPattern = regexp.MustCompile(`(?i)(?:持有|关注持仓|holding|hold)\s*(?P<asset>[\p{Han}A-Za-z./+\-]+?)\s*(?P<qty>\d+(?:\.\d+)?)\s*(?P<qty_unit>克|g|gram|grams|股|share|shares|盎司|oz|ounce|ounces|枚|coin)`)
)

func loadInvestorProfile(workspace string) (*InvestorProfile, error) {
	ctx := bootstrap.LoadWithAppName(workspace, "mossquant")
	return parseInvestorProfile(ctx)
}

func parseInvestorProfile(ctx *bootstrap.Context) (*InvestorProfile, error) {
	profile := &InvestorProfile{
		LastInterpreted: time.Now().Format(time.RFC3339),
	}
	if ctx == nil || ctx.Empty() {
		profile.Warnings = append(profile.Warnings, "No AGENTS.md or USER.md content was found; advisory runs will have limited personalization.")
		return profile, nil
	}

	if strings.TrimSpace(ctx.Agents) != "" {
		parsed, err := parseInvestorProfileText(ctx.Agents)
		if err != nil {
			return nil, fmt.Errorf("parse AGENTS.md: %w", err)
		}
		mergeProfile(profile, parsed)
		profile.SourceHints = append(profile.SourceHints, "AGENTS.md")
	}
	if strings.TrimSpace(ctx.User) != "" {
		parsed, err := parseInvestorProfileText(ctx.User)
		if err != nil {
			return nil, fmt.Errorf("parse USER.md: %w", err)
		}
		mergeProfile(profile, parsed)
		profile.SourceHints = append(profile.SourceHints, "USER.md")
	}

	profile.SourceHints = dedupeStrings(profile.SourceHints)
	profile.Holdings = dedupeHoldings(profile.Holdings)
	profile.Watchlist = dedupeStrings(profile.Watchlist)
	profile.DecisionFactors = dedupeStrings(profile.DecisionFactors)
	profile.Constraints = dedupeStrings(profile.Constraints)
	profile.Notes = dedupeStrings(profile.Notes)
	profile.Warnings = dedupeStrings(profile.Warnings)
	if profile.ReviewInterval == "" {
		profile.ReviewInterval = "10m"
	}
	if profile.Empty() {
		profile.Warnings = append(profile.Warnings, "Profile content was loaded, but no structured holdings/watchlist/risk settings could be extracted.")
	}
	return profile, nil
}

func parseInvestorProfileText(text string) (*InvestorProfile, error) {
	profile := &InvestorProfile{
		RawText: strings.TrimSpace(text),
	}
	if strings.TrimSpace(text) == "" {
		return profile, nil
	}

	body := strings.TrimSpace(text)
	if front, rest, ok := splitFrontMatter(text); ok {
		var meta frontMatterProfile
		if err := yaml.Unmarshal([]byte(front), &meta); err != nil {
			return nil, fmt.Errorf("parse front matter: %w", err)
		}
		applyFrontMatter(profile, meta)
		body = strings.TrimSpace(rest)
	}

	applyInlineAssignments(profile, body)
	applyMarkdownSections(profile, body)
	applyFreeformHoldings(profile, body)
	profile.Holdings = dedupeHoldings(profile.Holdings)
	profile.Watchlist = dedupeStrings(profile.Watchlist)
	profile.DecisionFactors = dedupeStrings(profile.DecisionFactors)
	profile.Constraints = dedupeStrings(profile.Constraints)
	profile.Notes = dedupeStrings(profile.Notes)
	return profile, nil
}

func splitFrontMatter(text string) (string, string, bool) {
	m := frontMatterPattern.FindStringSubmatch(text)
	if len(m) != 3 {
		return "", "", false
	}
	return m[1], m[2], true
}

func applyFrontMatter(profile *InvestorProfile, meta frontMatterProfile) {
	if profile.RiskTolerance == "" {
		profile.RiskTolerance = firstNonEmpty(meta.RiskTolerance, meta.RiskProfile)
	}
	if profile.InvestmentStyle == "" {
		profile.InvestmentStyle = meta.InvestmentStyle
	}
	if profile.ReviewInterval == "" {
		profile.ReviewInterval = meta.ReviewInterval
	}
	for _, h := range meta.Holdings {
		profile.Holdings = append(profile.Holdings, Holding{
			Asset:      normalizeAssetName(h.Asset),
			Symbol:     normalizeSymbol(firstNonEmpty(h.Symbol, inferSymbol(h.Asset))),
			Quantity:   h.Quantity,
			Unit:       normalizeUnit(h.Unit),
			CostBasis:  h.CostBasis,
			Currency:   normalizeCurrency(h.Currency),
			PriceUnit:  normalizeUnit(h.PriceUnit),
			AcquiredAt: normalizeDateString(h.AcquiredAt),
		})
	}
	profile.Watchlist = append(profile.Watchlist, meta.Watchlist...)
	profile.DecisionFactors = append(profile.DecisionFactors, meta.DecisionFactors...)
	profile.DecisionFactors = append(profile.DecisionFactors, meta.Factors...)
	profile.Constraints = append(profile.Constraints, meta.Constraints...)
	profile.Notes = append(profile.Notes, meta.Notes...)
}

func applyInlineAssignments(profile *InvestorProfile, body string) {
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rawLine, "-"), "*"))
		if line == "" {
			continue
		}
		switch {
		case hasPrefixedLabel(line, "风险承受能力", "风险偏好", "risk tolerance", "risk profile"):
			if profile.RiskTolerance == "" {
				profile.RiskTolerance = strings.TrimSpace(valueAfterLabel(line, "风险承受能力", "风险偏好", "risk tolerance", "risk profile"))
			}
		case hasPrefixedLabel(line, "投资倾向", "投资风格", "investment style"):
			if profile.InvestmentStyle == "" {
				profile.InvestmentStyle = strings.TrimSpace(valueAfterLabel(line, "投资倾向", "投资风格", "investment style"))
			}
		case hasPrefixedLabel(line, "复盘频率", "review interval", "cadence", "schedule"):
			if profile.ReviewInterval == "" {
				profile.ReviewInterval = strings.TrimSpace(valueAfterLabel(line, "复盘频率", "review interval", "cadence", "schedule"))
			}
		case hasPrefixedLabel(line, "关注标的", "watchlist", "关注资产"):
			profile.Watchlist = append(profile.Watchlist, splitCSVLike(valueAfterLabel(line, "关注标的", "watchlist", "关注资产"))...)
		case hasPrefixedLabel(line, "决策因素", "影响因素", "factors", "decision factors"):
			profile.DecisionFactors = append(profile.DecisionFactors, splitCSVLike(valueAfterLabel(line, "决策因素", "影响因素", "factors", "decision factors"))...)
		case hasPrefixedLabel(line, "约束", "constraints"):
			profile.Constraints = append(profile.Constraints, splitCSVLike(valueAfterLabel(line, "约束", "constraints"))...)
		}
	}
}

func applyMarkdownSections(profile *InvestorProfile, body string) {
	sections := map[string][]string{}
	current := ""
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			current = classifyHeading(strings.TrimSpace(strings.TrimLeft(line, "#")))
			continue
		}
		if current == "" || line == "" {
			continue
		}
		sections[current] = append(sections[current], line)
	}

	for key, lines := range sections {
		switch key {
		case "holdings":
			for _, line := range lines {
				if holding, ok := parseHoldingStatement(line); ok {
					profile.Holdings = append(profile.Holdings, holding)
					continue
				}
				profile.Notes = append(profile.Notes, normalizeListItem(line))
			}
		case "watchlist":
			for _, line := range lines {
				profile.Watchlist = append(profile.Watchlist, splitCSVLike(normalizeListItem(line))...)
			}
		case "risk":
			if profile.RiskTolerance == "" {
				profile.RiskTolerance = strings.Join(cleanLines(lines), " ")
			}
		case "factors":
			for _, line := range lines {
				profile.DecisionFactors = append(profile.DecisionFactors, splitCSVLike(normalizeListItem(line))...)
			}
		case "constraints":
			for _, line := range lines {
				profile.Constraints = append(profile.Constraints, splitCSVLike(normalizeListItem(line))...)
			}
		case "notes":
			for _, line := range lines {
				profile.Notes = append(profile.Notes, normalizeListItem(line))
			}
		case "interval":
			if profile.ReviewInterval == "" {
				profile.ReviewInterval = strings.Join(cleanLines(lines), " ")
			}
		}
	}
}

func applyFreeformHoldings(profile *InvestorProfile, body string) {
	lines := strings.FieldsFunc(body, func(r rune) bool {
		return r == '\n' || r == '。' || r == ';'
	})
	for _, line := range lines {
		if holding, ok := parseHoldingStatement(strings.TrimSpace(line)); ok {
			profile.Holdings = append(profile.Holdings, holding)
		}
	}
}

func parseHoldingStatement(line string) (Holding, bool) {
	line = normalizeListItem(line)
	for _, pattern := range []*regexp.Regexp{dateAtPricePattern, boughtOnPattern, plainHoldingPattern} {
		matches := captureGroups(pattern, line)
		if len(matches) == 0 {
			continue
		}
		qty, _ := strconv.ParseFloat(matches["qty"], 64)
		cost, _ := strconv.ParseFloat(matches["price"], 64)
		asset := normalizeAssetName(matches["asset"])
		asset = cleanExtractedAsset(asset)
		if asset == "" {
			return Holding{}, false
		}
		return Holding{
			Asset:       asset,
			Symbol:      normalizeSymbol(inferSymbol(asset)),
			Quantity:    qty,
			Unit:        normalizeUnit(firstNonEmpty(matches["qty_unit"], matches["unit"])),
			CostBasis:   cost,
			Currency:    normalizeCurrency(matches["currency"]),
			PriceUnit:   normalizeUnit(matches["price_unit"]),
			AcquiredAt:  normalizeDateString(matches["date"]),
			Description: line,
		}, true
	}
	return Holding{}, false
}

func registerProfileTools(reg tool.Registry, workspace string, fallback *InvestorProfile) error {
	spec := tool.ToolSpec{
		Name:        "get_investor_profile",
		Description: "Return the structured investor profile parsed from AGENTS.md and USER.md, including holdings, watchlist, risk tolerance, review cadence, and decision factors.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Risk:        tool.RiskLow,
		Capabilities: []string{
			"profile",
			"advisory",
		},
	}
	handler := func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		profile, err := loadInvestorProfile(workspace)
		if err != nil {
			if fallback == nil {
				return nil, err
			}
			profile = fallback
		}
		return json.Marshal(profile)
	}
	return reg.Register(spec, handler)
}

func mergeProfile(dst, src *InvestorProfile) {
	if dst == nil || src == nil {
		return
	}
	if dst.RiskTolerance == "" {
		dst.RiskTolerance = src.RiskTolerance
	}
	if dst.InvestmentStyle == "" {
		dst.InvestmentStyle = src.InvestmentStyle
	}
	if dst.ReviewInterval == "" {
		dst.ReviewInterval = src.ReviewInterval
	}
	dst.Holdings = append(dst.Holdings, src.Holdings...)
	dst.Watchlist = append(dst.Watchlist, src.Watchlist...)
	dst.DecisionFactors = append(dst.DecisionFactors, src.DecisionFactors...)
	dst.Constraints = append(dst.Constraints, src.Constraints...)
	dst.Notes = append(dst.Notes, src.Notes...)
	dst.Warnings = append(dst.Warnings, src.Warnings...)
	dst.RawText = strings.TrimSpace(strings.Join([]string{dst.RawText, src.RawText}, "\n\n"))
}

func (p *InvestorProfile) Empty() bool {
	if p == nil {
		return true
	}
	return len(p.Holdings) == 0 &&
		len(p.Watchlist) == 0 &&
		strings.TrimSpace(p.RiskTolerance) == "" &&
		strings.TrimSpace(p.InvestmentStyle) == "" &&
		len(p.DecisionFactors) == 0 &&
		len(p.Constraints) == 0
}

func (p *InvestorProfile) DisplayRiskTolerance() string {
	if p == nil || strings.TrimSpace(p.RiskTolerance) == "" {
		return "unspecified"
	}
	return p.RiskTolerance
}

func (p *InvestorProfile) TrackedAssets() []string {
	if p == nil {
		return nil
	}
	var assets []string
	for _, holding := range p.Holdings {
		label := holding.Asset
		if label == "" {
			label = holding.Symbol
		}
		if label != "" {
			assets = append(assets, label)
		}
	}
	assets = append(assets, p.Watchlist...)
	return dedupeStrings(assets)
}

func (p *InvestorProfile) SummaryMarkdown() string {
	if p == nil || p.Empty() {
		return "- No structured investor profile has been parsed yet."
	}
	lines := []string{
		fmt.Sprintf("- Risk tolerance: %s", p.DisplayRiskTolerance()),
	}
	if p.InvestmentStyle != "" {
		lines = append(lines, fmt.Sprintf("- Investment style: %s", p.InvestmentStyle))
	}
	if p.ReviewInterval != "" {
		lines = append(lines, fmt.Sprintf("- Preferred review interval: %s", p.ReviewInterval))
	}
	if len(p.Holdings) > 0 {
		var holdings []string
		for _, h := range p.Holdings {
			holdings = append(holdings, holdingSummary(h))
		}
		lines = append(lines, fmt.Sprintf("- Holdings: %s", strings.Join(holdings, "; ")))
	}
	if len(p.Watchlist) > 0 {
		lines = append(lines, fmt.Sprintf("- Watchlist: %s", strings.Join(p.Watchlist, ", ")))
	}
	if len(p.DecisionFactors) > 0 {
		lines = append(lines, fmt.Sprintf("- Decision factors: %s", strings.Join(p.DecisionFactors, ", ")))
	}
	if len(p.Constraints) > 0 {
		lines = append(lines, fmt.Sprintf("- Constraints: %s", strings.Join(p.Constraints, ", ")))
	}
	return strings.Join(lines, "\n")
}

func effectiveReviewInterval(profile *InvestorProfile, fallback string) string {
	if profile != nil && strings.TrimSpace(profile.ReviewInterval) != "" {
		return strings.TrimSpace(profile.ReviewInterval)
	}
	if strings.TrimSpace(fallback) == "" {
		return "10m"
	}
	return strings.TrimSpace(fallback)
}

func ensureDefaultReviewJob(sched *scheduler.Scheduler, profile *InvestorProfile, fallbackInterval, trust string) (string, bool, error) {
	schedule := normalizeSchedule(effectiveReviewInterval(profile, fallbackInterval))
	if profile == nil || profile.Empty() {
		return schedule, false, nil
	}
	goal := buildDefaultReviewGoal(profile)
	existing, hasExisting := findJob(sched, "investment-review")
	if hasExisting && existing.Schedule == schedule && strings.TrimSpace(existing.Goal) == strings.TrimSpace(goal) {
		return schedule, false, nil
	}
	job := scheduler.Job{
		ID:       "investment-review",
		Schedule: schedule,
		Goal:     goal,
		Config: session.SessionConfig{
			Goal:       goal,
			Mode:       "scheduled",
			TrustLevel: firstNonEmpty(trust, "restricted"),
			MaxSteps:   40,
		},
	}
	if err := sched.AddJob(job); err != nil {
		return schedule, false, err
	}
	return schedule, true, nil
}

func buildDefaultReviewGoal(profile *InvestorProfile) string {
	assets := profile.TrackedAssets()
	factors := "market, policy, macro, and geopolitical developments"
	if len(profile.DecisionFactors) > 0 {
		factors = strings.Join(profile.DecisionFactors, ", ")
	}
	return fmt.Sprintf(`Run a periodic investment review.

Start by calling get_investor_profile.
Then gather up-to-date information for these tracked assets: %s.
Also research these relevant factors: %s.

Use web_search and web_fetch to find current price drivers, official policy updates, and important global events. Keep the external research compact: shortlist sources with web_search, read only the highest-value pages with web_fetch, do not treat words like 今日/最新 as proof of recency, and for every material source you rely on call assess_source_credibility while preferring medium/high credibility evidence.

Before finalizing the recommendation, delegate a final audit to investment-reviewer so the thesis is checked for risk fit, source quality, and missing caveats.

Return a structured advisory report with:
1. portfolio/watchlist context
2. major developments
3. source credibility assessment
4. recommendation (hold / watch / reduce / accumulate / avoid)
5. risks and uncertainties
6. clear reasoning aligned with the investor's risk tolerance

This is advisory-only. Do not place trades unless the user explicitly asks.`,
		strings.Join(assets, ", "),
		factors,
	)
}

func classifyHeading(heading string) string {
	h := strings.ToLower(strings.TrimSpace(heading))
	switch {
	case strings.Contains(h, "持仓"), strings.Contains(h, "holding"):
		return "holdings"
	case strings.Contains(h, "关注"), strings.Contains(h, "watchlist"), strings.Contains(h, "观察"):
		return "watchlist"
	case strings.Contains(h, "风险"), strings.Contains(h, "risk"):
		return "risk"
	case strings.Contains(h, "因素"), strings.Contains(h, "factor"):
		return "factors"
	case strings.Contains(h, "约束"), strings.Contains(h, "constraint"):
		return "constraints"
	case strings.Contains(h, "频率"), strings.Contains(h, "interval"), strings.Contains(h, "cadence"):
		return "interval"
	default:
		return "notes"
	}
}

func hasPrefixedLabel(line string, labels ...string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, label := range labels {
		label = strings.ToLower(strings.TrimSpace(label))
		if strings.HasPrefix(lower, label+":") || strings.HasPrefix(lower, label+"：") || strings.HasPrefix(lower, label+" ") {
			return true
		}
	}
	return false
}

func valueAfterColon(line string) string {
	if idx := strings.IndexAny(line, ":："); idx >= 0 {
		return strings.TrimSpace(line[idx+1:])
	}
	return ""
}

func valueAfterLabel(line string, labels ...string) string {
	if value := valueAfterColon(line); value != "" {
		return value
	}
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	for _, label := range labels {
		label = strings.TrimSpace(label)
		labelLower := strings.ToLower(label)
		if strings.HasPrefix(lower, labelLower+" ") {
			return strings.TrimSpace(trimmed[len(label):])
		}
	}
	return ""
}

func splitCSVLike(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}
	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；'
	})
	var out []string
	for _, part := range parts {
		part = normalizeListItem(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 && input != "" {
		out = append(out, input)
	}
	return out
}

func normalizeListItem(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "-")
	line = strings.TrimPrefix(line, "*")
	line = strings.TrimSpace(line)
	return line
}

func normalizeAssetName(asset string) string {
	asset = strings.TrimSpace(asset)
	lower := strings.ToLower(asset)
	switch lower {
	case "gold", "xau", "xauusd":
		return "黄金"
	case "silver", "xag", "xagusd":
		return "白银"
	case "bitcoin", "btc":
		return "比特币"
	case "ethereum", "eth":
		return "以太坊"
	default:
		return asset
	}
}

func cleanExtractedAsset(asset string) string {
	asset = strings.TrimSpace(asset)
	for _, prefix := range []string{"的价格", "价格", "购买", "买入", "购入", "买了", "持有"} {
		asset = strings.TrimPrefix(asset, prefix)
	}
	asset = strings.Trim(asset, " ：:，,。；;")
	return normalizeAssetName(asset)
}

func inferSymbol(asset string) string {
	switch strings.ToLower(strings.TrimSpace(asset)) {
	case "黄金", "gold", "xau", "xauusd":
		return "XAUUSD"
	case "白银", "silver", "xag", "xagusd":
		return "XAGUSD"
	case "比特币", "btc", "bitcoin":
		return "BTC"
	case "以太坊", "eth", "ethereum":
		return "ETH"
	default:
		upper := strings.ToUpper(strings.TrimSpace(asset))
		if len(upper) <= 8 && upper == strings.ToUpper(asset) {
			return upper
		}
		return ""
	}
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func normalizeCurrency(currency string) string {
	switch strings.ToLower(strings.TrimSpace(currency)) {
	case "人民币", "rmb", "cny", "元":
		return "CNY"
	case "usd", "美元":
		return "USD"
	default:
		return strings.ToUpper(strings.TrimSpace(currency))
	}
}

func normalizeUnit(unit string) string {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "gram", "grams", "克":
		return "gram"
	case "share", "shares", "股":
		return "share"
	case "oz", "ounce", "ounces", "盎司":
		return "ounce"
	case "coin", "枚":
		return "coin"
	default:
		return strings.TrimSpace(unit)
	}
}

func normalizeDateString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("年", "-", "月", "-", "日", "")
	candidate := replacer.Replace(value)
	candidate = strings.ReplaceAll(candidate, "/", "-")
	for _, layout := range []string{"2006-1-2", "2006-01-02"} {
		if t, err := time.Parse(layout, candidate); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return value
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func dedupeHoldings(values []Holding) []Holding {
	type key struct {
		asset string
		date  string
		qty   string
		cost  string
	}
	seen := map[key]bool{}
	var out []Holding
	for _, holding := range values {
		if strings.TrimSpace(holding.Asset) == "" {
			continue
		}
		holding.Asset = normalizeAssetName(holding.Asset)
		if holding.Symbol == "" {
			holding.Symbol = inferSymbol(holding.Asset)
		}
		k := key{
			asset: strings.ToLower(holding.Asset),
			date:  holding.AcquiredAt,
			qty:   fmt.Sprintf("%.4f", holding.Quantity),
			cost:  fmt.Sprintf("%.4f", holding.CostBasis),
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, holding)
	}
	return out
}

func holdingSummary(h Holding) string {
	var parts []string
	label := h.Asset
	if h.Symbol != "" && !strings.EqualFold(h.Symbol, h.Asset) {
		label = fmt.Sprintf("%s (%s)", label, h.Symbol)
	}
	if h.Quantity > 0 {
		if h.Unit != "" {
			label = fmt.Sprintf("%s %.4g %s", label, h.Quantity, h.Unit)
		} else {
			label = fmt.Sprintf("%s %.4g", label, h.Quantity)
		}
	}
	parts = append(parts, label)
	if h.CostBasis > 0 {
		currency := firstNonEmpty(h.Currency, "CNY")
		perUnit := ""
		if h.PriceUnit != "" {
			perUnit = "/" + h.PriceUnit
		}
		parts = append(parts, fmt.Sprintf("cost %.2f %s%s", h.CostBasis, currency, perUnit))
	}
	if h.AcquiredAt != "" {
		parts = append(parts, "acquired "+h.AcquiredAt)
	}
	return strings.Join(parts, ", ")
}

func captureGroups(pattern *regexp.Regexp, input string) map[string]string {
	match := pattern.FindStringSubmatch(input)
	if len(match) == 0 {
		return nil
	}
	result := map[string]string{}
	for i, name := range pattern.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		result[name] = strings.TrimSpace(match[i])
	}
	return result
}

func cleanLines(lines []string) []string {
	var out []string
	for _, line := range lines {
		line = normalizeListItem(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func normalizeSchedule(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "@every 10m"
	}
	if strings.HasPrefix(value, "@") {
		return value
	}
	replacer := strings.NewReplacer(
		"分钟", "m",
		"分", "m",
		"小时", "h",
		"时", "h",
		"天", "24h",
		"周", "168h",
		"月", "720h",
		"个", "",
		" ", "",
	)
	normalized := replacer.Replace(strings.ToLower(value))
	normalized = strings.TrimSpace(normalized)
	if normalized != "" {
		if _, err := time.ParseDuration(normalized); err == nil {
			return "@every " + normalized
		}
	}
	return "@every " + value
}

func hasJob(sched *scheduler.Scheduler, id string) bool {
	for _, job := range sched.ListJobs() {
		if job.ID == id {
			return true
		}
	}
	return false
}

func findJob(sched *scheduler.Scheduler, id string) (scheduler.Job, bool) {
	for _, job := range sched.ListJobs() {
		if job.ID == id {
			return job, true
		}
	}
	return scheduler.Job{}, false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (p *InvestorProfile) ReportLabel() string {
	assets := p.TrackedAssets()
	if len(assets) == 0 {
		return "general"
	}
	if len(assets) == 1 {
		return sanitizeSlug(assets[0])
	}
	return sanitizeSlug(strings.Join(slices.Clone(assets[:min(3, len(assets))]), "-"))
}

func sanitizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "_", "-")
	re := regexp.MustCompile(`[^a-z0-9\p{Han}-]+`)
	value = re.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "report"
	}
	return value
}

func reportWorkspaceDir(workspace string) string {
	if filepath.Clean(workspace) == filepath.Clean(appconfig.AppDir()) {
		return workspace
	}
	return filepath.Join(workspace, ".mossquant")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
