package releasegate

import (
	"context"
	"testing"
)

func TestRun_CollectsSmokeAndReplayMetrics(t *testing.T) {
	report := Run(context.Background(), "staging")
	if !report.Passed() {
		t.Fatalf("expected release gate probe to pass:\n%s", RenderReport(report))
	}
	if len(report.Probes) != 2 {
		t.Fatalf("probe count = %d, want 2", len(report.Probes))
	}
	if report.Snapshot.RunTotal < 2 {
		t.Fatalf("run total = %v, want at least 2", report.Snapshot.RunTotal)
	}
	if report.Metrics["replay.prepared_total"] < 1 {
		t.Fatalf("replay prepared total = %v, want >=1", report.Metrics["replay.prepared_total"])
	}
}

func TestRun_NormalizesUnknownEnvironmentToProd(t *testing.T) {
	report := Run(context.Background(), "unknown")
	if report.Environment != "prod" {
		t.Fatalf("environment = %q, want prod", report.Environment)
	}
}
