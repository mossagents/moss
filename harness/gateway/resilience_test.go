package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRetryBudget(t *testing.T) {
	b := NewRetryBudget(2)
	if err := b.Consume("llm"); err != nil {
		t.Fatalf("consume1: %v", err)
	}
	if err := b.Consume("tool"); err != nil {
		t.Fatalf("consume2: %v", err)
	}
	if err := b.Consume("delivery"); !errors.Is(err, ErrRetryBudgetExceeded) {
		t.Fatalf("consume3 err=%v want ErrRetryBudgetExceeded", err)
	}
	if b.Remaining() != 0 {
		t.Fatalf("remaining=%d want 0", b.Remaining())
	}
}

func TestProfileRotator(t *testing.T) {
	r := NewProfileRotator([]ModelProfile{
		{Name: "p1", Provider: "openai", Model: "gpt-4o"},
		{Name: "p2", Provider: "openai", Model: "gpt-4.1"},
	})
	if got := r.Current().Name; got != "p1" {
		t.Fatalf("current=%s want p1", got)
	}
	if ok := r.MarkFailureAndRotate(); !ok {
		t.Fatal("expected rotate true")
	}
	if got := r.Current().Name; got != "p2" {
		t.Fatalf("current=%s want p2", got)
	}
	if ok := r.MarkFailureAndRotate(); ok {
		t.Fatal("expected rotate false at end")
	}
}

func TestValidateRuntimeAssets_StrictAndBestEffort(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "HEARTBEAT.md"), []byte("hb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "CRON.json"), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "skills", "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", "a", "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := ValidateRuntimeAssets(ws, AssetModeBestEffort)
	if err != nil {
		t.Fatalf("best-effort err: %v", err)
	}
	if len(report.Invalid) == 0 {
		t.Fatal("expected invalid assets due to bad CRON.json")
	}
	if _, err := ValidateRuntimeAssets(ws, AssetModeStrict); err == nil {
		t.Fatal("strict mode should fail on invalid CRON.json")
	}
}
