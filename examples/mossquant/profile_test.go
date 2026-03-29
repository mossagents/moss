package main

import (
	"context"
	"testing"

	"github.com/mossagents/moss/scheduler"
)

func TestParseInvestorProfileTextFrontMatter(t *testing.T) {
	text := `---
risk_tolerance: medium
review_interval: 30m
holdings:
  - asset: gold
    quantity: 10
    unit: gram
    cost_basis: 1000
    currency: CNY
    price_unit: gram
    acquired_at: 2026-03-12
watchlist:
  - 比特币
factors:
  - 政府政策
  - 中东局势
---
# Notes
- 长期配置`

	profile, err := parseInvestorProfileText(text)
	if err != nil {
		t.Fatalf("parseInvestorProfileText: %v", err)
	}
	if profile.RiskTolerance != "medium" {
		t.Fatalf("unexpected risk tolerance: %q", profile.RiskTolerance)
	}
	if profile.ReviewInterval != "30m" {
		t.Fatalf("unexpected interval: %q", profile.ReviewInterval)
	}
	if len(profile.Holdings) != 1 {
		t.Fatalf("expected 1 holding, got %d", len(profile.Holdings))
	}
	if profile.Holdings[0].Asset != "黄金" {
		t.Fatalf("expected normalized asset 黄金, got %q", profile.Holdings[0].Asset)
	}
	if len(profile.DecisionFactors) != 2 {
		t.Fatalf("expected 2 factors, got %d", len(profile.DecisionFactors))
	}
}

func TestParseHoldingStatementChinese(t *testing.T) {
	line := "我在 2026年3月12日以1000元每克的价格购入黄金10克。"
	holding, ok := parseHoldingStatement(line)
	if !ok {
		t.Fatal("expected holding to be parsed")
	}
	if holding.Asset != "黄金" {
		t.Fatalf("unexpected asset: %q", holding.Asset)
	}
	if holding.Quantity != 10 {
		t.Fatalf("unexpected quantity: %v", holding.Quantity)
	}
	if holding.CostBasis != 1000 {
		t.Fatalf("unexpected cost basis: %v", holding.CostBasis)
	}
	if holding.AcquiredAt != "2026-03-12" {
		t.Fatalf("unexpected date: %q", holding.AcquiredAt)
	}
}

func TestParseHoldingStatementChineseWithPurchaseVerb(t *testing.T) {
	line := "我在 2026年3月16日以1000元每克的价格购买黄金10克。"
	holding, ok := parseHoldingStatement(line)
	if !ok {
		t.Fatal("expected holding to be parsed")
	}
	if holding.Asset != "黄金" {
		t.Fatalf("unexpected asset: %q", holding.Asset)
	}
	if holding.Quantity != 10 {
		t.Fatalf("unexpected quantity: %v", holding.Quantity)
	}
}

func TestNormalizeScheduleChineseMinutes(t *testing.T) {
	if got := normalizeSchedule("60分钟"); got != "@every 60m" {
		t.Fatalf("unexpected schedule: %q", got)
	}
}

func TestParseInvestorProfileTextInlineIntervalWithoutColon(t *testing.T) {
	profile, err := parseInvestorProfileText("复盘频率 60分钟")
	if err != nil {
		t.Fatalf("parseInvestorProfileText: %v", err)
	}
	if profile.ReviewInterval != "60分钟" {
		t.Fatalf("unexpected review interval: %q", profile.ReviewInterval)
	}
}

func TestEnsureDefaultReviewJobUpdatesExistingSchedule(t *testing.T) {
	sched := scheduler.New()
	sched.Start(context.Background(), func(context.Context, scheduler.Job) {})
	if err := sched.AddJob(scheduler.Job{
		ID:       "investment-review",
		Schedule: "@every 10m",
		Goal:     "old goal",
	}); err != nil {
		t.Fatalf("seed job: %v", err)
	}

	profile := &InvestorProfile{
		ReviewInterval: "60分钟",
		Holdings: []Holding{
			{Asset: "黄金"},
		},
	}
	schedule, changed, err := ensureDefaultReviewJob(sched, profile, "10m", "trusted")
	if err != nil {
		t.Fatalf("ensureDefaultReviewJob: %v", err)
	}
	if !changed {
		t.Fatal("expected existing job to be updated")
	}
	if schedule != "@every 60m" {
		t.Fatalf("unexpected normalized schedule: %q", schedule)
	}
	job, ok := findJob(sched, "investment-review")
	if !ok {
		t.Fatal("expected investment-review job to exist")
	}
	if job.Schedule != "@every 60m" {
		t.Fatalf("unexpected stored schedule: %q", job.Schedule)
	}
}
