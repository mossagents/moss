package researcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/mossagi/moss/internal/agents"
	"github.com/mossagi/moss/internal/domain"
	"github.com/mossagi/moss/internal/tools"
)

type Agent struct {
	spec    agents.AgentSpec
	catalog *tools.Catalog
}

func New(catalog *tools.Catalog) *Agent {
	return &Agent{
		spec: agents.AgentSpec{
			Name:                "researcher",
			Role:                agents.RoleResearcher,
			Instructions:        "You are the researcher agent. Use read-only tools to analyze and gather information.",
			AllowedCapabilities: []string{"read"},
			AllowedTools:        []string{"list_files", "read_file", "search_text"},
			Limits:              &agents.AgentLimits{MaxSteps: 10, MaxTokens: 2048},
		},
		catalog: catalog,
	}
}

func (a *Agent) Spec() agents.AgentSpec {
	return a.spec
}

func (a *Agent) Run(ctx context.Context, task *domain.Task, catalog *tools.Catalog) (*domain.TaskResult, error) {
	fmt.Printf("[researcher] Starting task: %s\n", task.Goal)

	var findings []string

	// List files in workspace
	if listTool, ok := catalog.Get("list_files"); ok {
		result, err := listTool.Execute(ctx, tools.ToolInput{"path": ".", "pattern": "*"})
		if err == nil && result.Success {
			if files, ok := result.Data["files"]; ok {
				findings = append(findings, fmt.Sprintf("Files found: %v", files))
			}
		}
	}

	// Try to read README
	if readTool, ok := catalog.Get("read_file"); ok {
		result, err := readTool.Execute(ctx, tools.ToolInput{"path": "README.md"})
		if err == nil && result.Success {
			if content, ok := result.Data["content"].(string); ok {
				if len(content) > 200 {
					content = content[:200] + "..."
				}
				findings = append(findings, fmt.Sprintf("README: %s", content))
			}
		}
	}

	// Search for relevant text
	if searchTool, ok := catalog.Get("search_text"); ok {
		keywords := extractKeywords(task.Goal)
		for _, kw := range keywords {
			result, err := searchTool.Execute(ctx, tools.ToolInput{"query": kw, "path": "."})
			if err == nil && result.Success {
				if matches, ok := result.Data["matches"]; ok {
					findings = append(findings, fmt.Sprintf("Search '%s': %v", kw, matches))
				}
			}
		}
	}

	summary := strings.Join(findings, "\n")
	if summary == "" {
		summary = "No findings"
	}

	fmt.Printf("[researcher] Completed with %d findings\n", len(findings))

	return &domain.TaskResult{
		Summary:   summary,
		Artifacts: []string{},
		Success:   true,
	}, nil
}

func extractKeywords(goal string) []string {
	words := strings.Fields(goal)
	var keywords []string
	for _, w := range words {
		if len(w) > 4 {
			keywords = append(keywords, w)
		}
	}
	if len(keywords) > 3 {
		keywords = keywords[:3]
	}
	return keywords
}
