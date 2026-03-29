package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func saveAdvisoryReport(workspace, jobID string, profile *InvestorProfile, output string) (string, error) {
	root := reportWorkspaceDir(workspace)
	reportsDir := filepath.Join(root, "reports")
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return "", fmt.Errorf("create reports dir: %w", err)
	}

	now := time.Now()
	body := strings.TrimSpace(output)
	if body == "" {
		body = "The advisory run completed, but no final textual report was returned. Review the session transcript for details."
	}

	content := fmt.Sprintf(`# mossquant advisory report

- Generated at: %s
- Job ID: %s
- Tracked assets: %s
- Risk tolerance: %s

## Investor profile snapshot

%s

## Advisory output

%s
`, now.Format(time.RFC3339), jobID, strings.Join(profile.TrackedAssets(), ", "), profile.DisplayRiskTolerance(), profile.SummaryMarkdown(), body)

	filename := fmt.Sprintf("%s-%s-%s.md", now.Format("20060102-150405"), sanitizeSlug(jobID), profile.ReportLabel())
	reportPath := filepath.Join(reportsDir, filename)
	if err := os.WriteFile(reportPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}

	latestPath := filepath.Join(root, "latest_report.md")
	if err := os.WriteFile(latestPath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write latest report: %w", err)
	}
	return reportPath, nil
}
