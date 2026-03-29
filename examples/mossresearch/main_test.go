package main

import (
	"context"
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

func TestNewJinaReaderRequestEscapesNestedURL(t *testing.T) {
	req, err := newJinaReaderRequest(context.Background(), jinaReaderParams{
		URL:            "https://example.com/article?id=123&lang=en",
		TargetSelector: "main",
		RemoveSelector: ".ads",
		TokenBudget:    2048,
	})
	if err != nil {
		t.Fatalf("newJinaReaderRequest: %v", err)
	}

	if got, want := req.URL.String(), "https://r.jina.ai/https:%2F%2Fexample.com%2Farticle%3Fid=123&lang=en"; got != want {
		t.Fatalf("request URL = %q, want %q", got, want)
	}
	if req.Header.Get("X-Target-Selector") != "main" {
		t.Fatalf("missing target selector header")
	}
	if req.Header.Get("X-Remove-Selector") != ".ads" {
		t.Fatalf("missing remove selector header")
	}
	if req.Header.Get("X-Token-Budget") != "2048" {
		t.Fatalf("missing token budget header")
	}
}

func TestNewJinaSearchRequestUsesContextAndQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := newJinaSearchRequest(ctx, jinaSearchParams{
		Query: "agent memory",
		Count: 5,
		GL:    "us",
		HL:    "en",
	})
	if err != nil {
		t.Fatalf("newJinaSearchRequest: %v", err)
	}

	if req.Context() != ctx {
		t.Fatalf("request did not keep provided context")
	}
	if got, want := req.URL.Query().Get("count"), "5"; got != want {
		t.Fatalf("count query = %q, want %q", got, want)
	}
	if got, want := req.URL.Query().Get("gl"), "us"; got != want {
		t.Fatalf("gl query = %q, want %q", got, want)
	}
	if got, want := req.URL.Query().Get("hl"), "en"; got != want {
		t.Fatalf("hl query = %q, want %q", got, want)
	}
}

func TestUnwrapJinaPayloadPassesThroughRawJSON(t *testing.T) {
	body := []byte(`{"items":[{"title":"example"}]}`)

	got, err := unwrapJinaPayload(body)
	if err != nil {
		t.Fatalf("unwrapJinaPayload: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("unwrapJinaPayload = %q, want raw JSON body", string(got))
	}
}
