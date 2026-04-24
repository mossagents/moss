package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseRunCommandDetailAndAsOf(t *testing.T) {
	setupTestAppHome(t)
	cfg, err := parseRunCommand([]string{
		"--topic", "Current AI IDE landscape",
		"--model", "gpt-4o",
		"--detail", "brief",
		"--as-of", "2026-04-23T12:34:56Z",
	})
	if err != nil {
		t.Fatalf("parse run command: %v", err)
	}
	if cfg.Detail != detailBrief {
		t.Fatalf("expected detail %q, got %q", detailBrief, cfg.Detail)
	}
	want := time.Date(2026, 4, 23, 12, 34, 56, 0, time.UTC)
	if !cfg.AsOf.Equal(want) {
		t.Fatalf("expected as-of %s, got %s", want.Format(time.RFC3339), cfg.AsOf.Format(time.RFC3339))
	}
}

func TestParseRunCommandWorkers(t *testing.T) {
	setupTestAppHome(t)
	cfg, err := parseRunCommand([]string{"--topic", "test", "--model", "gpt-4o"})
	if err != nil {
		t.Fatalf("parse default workers: %v", err)
	}
	if cfg.Workers != 3 {
		t.Fatalf("expected default 3 workers, got %d", cfg.Workers)
	}
	cfg, err = parseRunCommand([]string{"--topic", "test", "--model", "gpt-4o", "--workers", "5"})
	if err != nil {
		t.Fatalf("parse explicit workers: %v", err)
	}
	if cfg.Workers != 5 {
		t.Fatalf("expected 5 workers, got %d", cfg.Workers)
	}
	_, err = parseRunCommand([]string{"--topic", "test", "--model", "gpt-4o", "--workers", "0"})
	if err == nil {
		t.Fatal("expected error for --workers 0")
	}
}

func TestParseRunCommandRequiresTopicAndModel(t *testing.T) {
	setupTestAppHome(t)
	_, err := parseRunCommand([]string{"--model", "gpt-4o"})
	if err == nil {
		t.Fatal("expected error when --topic is missing")
	}
	_, err = parseRunCommand([]string{"--topic", "test"})
	if err == nil {
		t.Fatal("expected error when --model is missing")
	}
}

func TestParseRunCommandLang(t *testing.T) {
	setupTestAppHome(t)
	cfg, err := parseRunCommand([]string{"--topic", "test", "--model", "gpt-4o", "--lang", "zh"})
	if err != nil {
		t.Fatalf("parse --lang: %v", err)
	}
	if cfg.Lang != "zh" {
		t.Fatalf("expected lang %q, got %q", "zh", cfg.Lang)
	}
	cfg, err = parseRunCommand([]string{"--topic", "test", "--model", "gpt-4o"})
	if err != nil {
		t.Fatalf("parse no lang: %v", err)
	}
	if cfg.Lang != "" {
		t.Fatalf("expected empty lang, got %q", cfg.Lang)
	}
}

func setupTestAppHome(t *testing.T) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("create test home: %v", err)
	}
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}
