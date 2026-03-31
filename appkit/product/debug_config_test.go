package product

import (
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/config"
)

func TestBuildDebugConfigReportIncludesCommandDirs(t *testing.T) {
	appconfig.SetAppName("mosscode")
	report := BuildDebugConfigReport("mosscode", t.TempDir(), "openai", "gpt-5", "trusted", "confirm", "coding", "default")
	if len(report.CommandDirs) != 3 {
		t.Fatalf("expected 3 command dirs, got %d", len(report.CommandDirs))
	}
}

func TestRenderDebugConfigReportIncludesThemeAndPaths(t *testing.T) {
	appconfig.SetAppName("mosscode")
	report := BuildDebugConfigReport("mosscode", t.TempDir(), "openai", "gpt-5", "trusted", "confirm", "coding", "plain")
	rendered := RenderDebugConfigReport(report)
	if !strings.Contains(rendered, "theme=plain") || !strings.Contains(rendered, "Command dir:") {
		t.Fatalf("unexpected debug config report: %s", rendered)
	}
}
