package main

import (
	"testing"
)

func TestAssessSourceCredibilityOfficial(t *testing.T) {
	result := assessSourceCredibility(credibilitySource{
		URL:         "https://www.federalreserve.gov/newsevents/pressreleases/monetary20260312a.htm",
		Title:       "Federal Reserve issues statement",
		SourceType:  "official",
		PublishedAt: "2026-03-12",
	})
	if result.Level != "high" {
		t.Fatalf("expected high credibility, got %s (%d)", result.Level, result.Score)
	}
}

func TestAssessSourceCredibilityOpinion(t *testing.T) {
	result := assessSourceCredibility(credibilitySource{
		URL:        "https://somewriter.substack.com/p/my-hot-take",
		Title:      "Opinion: why gold will moon",
		SourceType: "opinion",
	})
	if result.Level == "high" {
		t.Fatalf("expected non-high credibility, got %s (%d)", result.Level, result.Score)
	}
}
