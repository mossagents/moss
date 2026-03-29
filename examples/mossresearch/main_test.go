package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteResearchRequestCreatesArtifact(t *testing.T) {
	workspace := t.TempDir()

	if err := writeResearchRequest(workspace, "Compare agent frameworks"); err != nil {
		t.Fatalf("writeResearchRequest: %v", err)
	}

	data, err := os.ReadFile(researchRequestPath(workspace))
	if err != nil {
		t.Fatalf("read research request: %v", err)
	}
	if got := string(data); got != "Compare agent frameworks\n" {
		t.Fatalf("research request = %q, want exact goal with trailing newline", got)
	}
}

func TestEnsureFinalReportWritesFallbackWhenMissing(t *testing.T) {
	workspace := t.TempDir()

	if err := ensureFinalReport(workspace, ""); err != nil {
		t.Fatalf("ensureFinalReport: %v", err)
	}

	data, err := os.ReadFile(finalReportPath(workspace))
	if err != nil {
		t.Fatalf("read final report: %v", err)
	}
	if !strings.Contains(string(data), "no final textual report was returned") {
		t.Fatalf("final report fallback missing, got %q", string(data))
	}
}

func TestEnsureFinalReportPreservesExistingContent(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(researchOutputDir(workspace), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	reportPath := finalReportPath(workspace)
	if err := os.WriteFile(reportPath, []byte("existing report\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := ensureFinalReport(workspace, "new report"); err != nil {
		t.Fatalf("ensureFinalReport: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read final report: %v", err)
	}
	if got := string(data); got != "existing report\n" {
		t.Fatalf("final report overwritten: %q", got)
	}
}

func TestResearchArtifactPathsStayInWorkspace(t *testing.T) {
	workspace := t.TempDir()

	if got := researchOutputDir(workspace); got != filepath.Join(workspace, outputDirName) {
		t.Fatalf("researchOutputDir = %q", got)
	}
	if got := researchRequestPath(workspace); got != filepath.Join(workspace, outputDirName, "research_request.md") {
		t.Fatalf("researchRequestPath = %q", got)
	}
	if got := finalReportPath(workspace); got != filepath.Join(workspace, outputDirName, "final_report.md") {
		t.Fatalf("finalReportPath = %q", got)
	}
}
