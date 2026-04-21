package product

import (
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/harness/config"
)

func TestBuildDebugConfigReportIncludesCommandDirs(t *testing.T) {
	appconfig.SetAppName("mosscode")
	report := BuildDebugConfigReport("mosscode", t.TempDir(), "openai", "gpt-5", "trusted", "confirm", SessionSelectorReport{}, "default", "", "", "")
	if len(report.CommandDirs) != 3 {
		t.Fatalf("expected 3 command dirs, got %d", len(report.CommandDirs))
	}
}

func TestRenderDebugConfigReportIncludesThemeAndPaths(t *testing.T) {
	appconfig.SetAppName("mosscode")
	report := BuildDebugConfigReport("mosscode", t.TempDir(), "openai", "gpt-5", "trusted", "confirm", SessionSelectorReport{RunMode: "interactive", Preset: "code", CollaborationMode: "execute", PermissionProfile: "workspace-write"}, "plain", "config", "environment,skills", "base:config -> dynamic:environment -> dynamic:skills")
	rendered := RenderDebugConfigReport(report)
	if !strings.Contains(rendered, "theme=plain") ||
		!strings.Contains(rendered, "Session selectors: run=interactive | preset=code | mode=execute | permissions=workspace-write") ||
		!strings.Contains(rendered, "Command dir:") ||
		!strings.Contains(rendered, "Prompt base source: config") ||
		!strings.Contains(rendered, "Prompt source chain: base:config -> dynamic:environment -> dynamic:skills") {
		t.Fatalf("unexpected debug config report: %s", rendered)
	}
}
