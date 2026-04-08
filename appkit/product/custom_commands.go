package product

import (
	"errors"
	"fmt"
	appconfig "github.com/mossagents/moss/config"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CustomCommand struct {
	Name    string
	Summary string
	Prompt  string
	Path    string
	Scope   string
}

type customCommandRoot struct {
	Path  string
	Scope string
}

func DiscoverCustomCommands(workspace, appName, trust string) ([]CustomCommand, error) {
	roots := customCommandRoots(workspace, appName, trust)
	commands := make(map[string]CustomCommand)
	for _, root := range roots {
		entries, err := os.ReadDir(root.Path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read custom command dir %s: %w", root.Path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.ToLower(filepath.Ext(entry.Name())) != ".md" {
				continue
			}
			path := filepath.Join(root.Path, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read custom command %s: %w", path, err)
			}
			prompt := strings.TrimSpace(string(data))
			if prompt == "" {
				return nil, fmt.Errorf("custom command %s is empty", path)
			}
			name := normalizeCustomCommandName(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
			if name == "" {
				return nil, fmt.Errorf("custom command %s has no usable command name", path)
			}
			commands[name] = CustomCommand{
				Name:    name,
				Summary: summarizeCustomCommand(prompt),
				Prompt:  prompt,
				Path:    path,
				Scope:   root.Scope,
			}
		}
	}
	out := make([]CustomCommand, 0, len(commands))
	for _, cmd := range commands {
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func RenderCustomCommandPrompt(cmd CustomCommand, args, workspace string) string {
	prompt := strings.TrimSpace(cmd.Prompt)
	args = strings.TrimSpace(args)
	prompt = strings.ReplaceAll(prompt, "{{args}}", args)
	prompt = strings.ReplaceAll(prompt, "{{workspace}}", strings.TrimSpace(workspace))
	if args != "" && !strings.Contains(cmd.Prompt, "{{args}}") {
		prompt += "\n\nArguments:\n" + args
	}
	return strings.TrimSpace(prompt)
}

func InitWorkspaceBootstrap(workspace, appName string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	if err := os.MkdirAll(absWorkspace, 0o755); err != nil {
		return "", fmt.Errorf("create workspace: %w", err)
	}
	agentsPath := filepath.Join(absWorkspace, "AGENTS.md")
	commandsDir := filepath.Join(absWorkspace, "."+appName, "commands")
	lines := []string{fmt.Sprintf("Workspace initialized for %s.", appName)}

	if _, err := os.Stat(agentsPath); err == nil {
		lines = append(lines, fmt.Sprintf("Kept existing %s.", agentsPath))
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(agentsPath, []byte(defaultAgentsTemplate(appName)), 0o600); err != nil {
			return "", fmt.Errorf("write %s: %w", agentsPath, err)
		}
		lines = append(lines, fmt.Sprintf("Created %s.", agentsPath))
	} else {
		return "", fmt.Errorf("inspect %s: %w", agentsPath, err)
	}

	if err := os.MkdirAll(commandsDir, 0o755); err != nil {
		return "", fmt.Errorf("create custom commands dir %s: %w", commandsDir, err)
	}
	lines = append(lines, fmt.Sprintf("Custom commands directory ready: %s", commandsDir))
	lines = append(lines, "Workspace custom commands are loaded only in trusted posture.")
	return strings.Join(lines, "\n"), nil
}

func customCommandRoots(workspace, appName, trust string) []customCommandRoot {
	roots := make([]customCommandRoot, 0, 3)
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(appName) != "" {
		roots = append(roots, customCommandRoot{
			Path:  filepath.Join(home, "."+appName, "commands"),
			Scope: "user",
		})
	}
	if appconfig.ProjectAssetsAllowed(trust) && strings.TrimSpace(workspace) != "" {
		roots = append(roots,
			customCommandRoot{
				Path:  filepath.Join(workspace, "."+appName, "commands"),
				Scope: "project",
			},
			customCommandRoot{
				Path:  filepath.Join(workspace, ".agents", "commands"),
				Scope: "project",
			},
		)
	}
	return roots
}

func normalizeCustomCommandName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func summarizeCustomCommand(prompt string) string {
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line == "" {
			continue
		}
		if len(line) > 72 {
			return line[:69] + "..."
		}
		return line
	}
	return "Run custom workflow"
}

func defaultAgentsTemplate(appName string) string {
	return fmt.Sprintf(`# AGENTS.md

## Mission

- Describe what this repository is for.
- Describe what "%s" should optimize for when changing code here.

## Working agreements

- Note preferred build/test commands.
- Note any safety, review, or deployment rules.
- Note repo-specific architectural boundaries.

## Custom workflows

- Put reusable prompt commands in .%s/commands/*.md
- Use {{args}} inside a command file when the workflow needs caller arguments.
`, appName, appName)
}
