package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/mossagents/moss/config"
)

func TestParseSkillMDContent_Valid(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
---
# Test Skill

This is the body of the skill.

## Steps
1. Do something
2. Do something else
`
	s, err := ParseSkillMDContent(content, "test.md")
	if err != nil {
		t.Fatal(err)
	}
	if s.name != "test-skill" {
		t.Errorf("name = %q, want test-skill", s.name)
	}
	if s.description != "A test skill" {
		t.Errorf("description = %q, want 'A test skill'", s.description)
	}
	if s.body == "" {
		t.Error("body should not be empty")
	}
	if s.body != "# Test Skill\n\nThis is the body of the skill.\n\n## Steps\n1. Do something\n2. Do something else" {
		t.Errorf("unexpected body: %q", s.body)
	}
}
func TestParseSkillMDContent_MissingName(t *testing.T) {
	content := `---
description: no name
---
body
`
	_, err := ParseSkillMDContent(content, "bad.md")
	if err == nil {
		t.Error("expected error when name is missing")
	}
}

func TestParseSkillMDContent_NoFrontmatter(t *testing.T) {
	content := `# Just a Markdown File
No frontmatter here.
`
	// body is returned as-is, no frontmatter → empty YAML → no name → error
	_, err := ParseSkillMDContent(content, "nofm.md")
	if err == nil {
		t.Error("expected error for missing name in non-frontmatter file")
	}
}

func TestParseSkillMDContent_UnterminatedFrontmatter(t *testing.T) {
	content := `---
name: broken
description: missing closing
`
	_, err := ParseSkillMDContent(content, "broken.md")
	if err == nil {
		t.Error("expected error for unterminated frontmatter")
	}
}

func TestSkill_Metadata(t *testing.T) {
	s := &Skill{
		name:        "my-skill",
		description: "test desc",
		body:        "some instructions",
	}
	meta := s.Metadata()
	if meta.Name != "my-skill" {
		t.Errorf("Name = %q", meta.Name)
	}
	if len(meta.Prompts) != 1 || meta.Prompts[0] != "some instructions" {
		t.Errorf("Prompts = %v", meta.Prompts)
	}
	if len(meta.Tools) != 0 {
		t.Errorf("Skill should have no tools, got %v", meta.Tools)
	}
}

func TestSkill_InitShutdown(t *testing.T) {
	s := &Skill{name: "noop"}
	ctx := context.Background()
	if err := s.Init(ctx, Deps{}); err != nil {
		t.Errorf("Init: %v", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantFM  string
		wantErr bool
	}{
		{
			name:   "standard",
			input:  "---\nname: a\n---\nbody",
			wantFM: "name: a",
		},
		{
			name:   "no frontmatter",
			input:  "# just markdown",
			wantFM: "",
		},
		{
			name:    "unterminated",
			input:   "---\nname: a\nno closing",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, _, err := splitFrontmatter(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if fm != tt.wantFM {
				t.Errorf("frontmatter = %q, want %q", fm, tt.wantFM)
			}
		})
	}
}

func TestDiscoverSkills(t *testing.T) {
	// 创建临时工作区
	workspace := t.TempDir()

	// 创建 .agents/skills/frontend-design/SKILL.md
	dir1 := filepath.Join(workspace, ".agents", "skills", "frontend-design")
	if err := os.MkdirAll(dir1, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir1, "SKILL.md"), []byte(`---
name: frontend-design
description: Frontend design guidelines
---
Use semantic HTML and modern CSS.
`), 0644); err != nil {
		t.Fatal(err)
	}

	// 创建 .moss/skills/my-skill/SKILL.md
	dir2 := filepath.Join(workspace, ".moss", "skills", "my-skill")
	if err := os.MkdirAll(dir2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "SKILL.md"), []byte(`---
name: my-skill
description: Custom moss skill
---
Custom instructions here.
`), 0644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverSkills(workspace)
	if len(skills) < 2 {
		t.Fatalf("expected at least 2 skills, got %d", len(skills))
	}

	names := make(map[string]bool)
	for _, s := range skills {
		names[s.name] = true
	}
	if !names["frontend-design"] {
		t.Error("missing frontend-design skill")
	}
	if !names["my-skill"] {
		t.Error("missing my-skill skill")
	}
}

func TestDiscoverSkills_ProjectAgentDir(t *testing.T) {
	workspace := t.TempDir()

	dir := filepath.Join(workspace, ".agent", "skills", "local-agent-skill")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: local-agent-skill
description: Loaded from .agent
---
Project-local .agent skill.
`), 0644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverSkills(workspace)
	for _, s := range skills {
		if s.name == "local-agent-skill" {
			return
		}
	}

	t.Fatal("missing local-agent-skill from .agent/skills")
}

func TestDiscoverSkills_GlobalAgentDir(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".agent", "skills", "global-agent-skill")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: global-agent-skill
description: Loaded from ~/.agent
---
Global .agent skill.
`), 0644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverSkills(workspace)
	for _, s := range skills {
		if s.name == "global-agent-skill" {
			return
		}
	}

	t.Fatal("missing global-agent-skill from ~/.agent/skills")
}

func TestDiscoverSkills_GlobalAgentsDir(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".agents", "skills", "global-agents-skill")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: global-agents-skill
description: Loaded from ~/.agents
---
Global .agents skill.
`), 0644); err != nil {
		t.Fatal(err)
	}

	skills := DiscoverSkills(workspace)
	for _, s := range skills {
		if s.name == "global-agents-skill" {
			return
		}
	}

	t.Fatal("missing global-agents-skill from ~/.agents/skills")
}

func TestDiscoverSkills_ProjectAgentPrecedenceOverAppDir(t *testing.T) {
	workspace := t.TempDir()

	for dir, body := range map[string]string{
		filepath.Join(workspace, ".agent", "skills", "shared-skill"): "loaded from .agent",
		filepath.Join(workspace, ".moss", "skills", "shared-skill"):  "loaded from .moss",
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: shared-skill
description: precedence test
---
`+body+`
`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	skills := DiscoverSkills(workspace)
	count := 0
	for _, s := range skills {
		if s.name != "shared-skill" {
			continue
		}
		count++
		if s.body != "loaded from .agent" {
			t.Fatalf("shared-skill body = %q, want %q", s.body, "loaded from .agent")
		}
		if !strings.Contains(s.source, filepath.Join(".agent", "skills", "shared-skill", "SKILL.md")) {
			t.Fatalf("shared-skill source = %q, want .agent path", s.source)
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 shared-skill, got %d", count)
	}
}

func TestDiscoverSkills_Dedup(t *testing.T) {
	workspace := t.TempDir()

	// 同名 skill 在两个目录中
	for _, dir := range []string{
		filepath.Join(workspace, ".agents", "skills", "dup-skill"),
		filepath.Join(workspace, ".moss", "skills", "dup-skill"),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: dup-skill
description: duplicate
---
Content
`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	skills := DiscoverSkills(workspace)
	count := 0
	for _, s := range skills {
		if s.name == "dup-skill" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 dup-skill (dedup), got %d", count)
	}
}

func TestDiscoverSkillManifests(t *testing.T) {
	workspace := t.TempDir()

	dir := filepath.Join(workspace, ".agent", "skills", "manifest-skill")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: manifest-skill
description: listed without loading full prompt
---
# Big Prompt

Body content.
`), 0644); err != nil {
		t.Fatal(err)
	}

	manifests := DiscoverSkillManifests(workspace)
	if len(manifests) == 0 {
		t.Fatal("expected at least one discovered manifest")
	}
	var got *Manifest
	for i := range manifests {
		if manifests[i].Name == "manifest-skill" {
			got = &manifests[i]
			break
		}
	}
	if got == nil {
		t.Fatal("missing manifest-skill manifest")
	}
	if got.Description != "listed without loading full prompt" {
		t.Fatalf("description = %q", got.Description)
	}
	if !strings.Contains(got.Source, filepath.Join(".agent", "skills", "manifest-skill", "SKILL.md")) {
		t.Fatalf("unexpected source path: %q", got.Source)
	}
}

func TestDiscoverSkillManifests_AppDirIncludesLegacyMoss(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	appconfig.SetAppName("mosscode")
	t.Cleanup(func() { appconfig.SetAppName("moss") })

	projectLegacyDir := filepath.Join(workspace, ".moss", "skills", "legacy-project")
	if err := os.MkdirAll(projectLegacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectLegacyDir, "SKILL.md"), []byte(`---
name: legacy-project
description: legacy project path
---
legacy project body
`), 0644); err != nil {
		t.Fatal(err)
	}

	globalLegacyDir := filepath.Join(home, ".moss", "skills", "legacy-global")
	if err := os.MkdirAll(globalLegacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalLegacyDir, "SKILL.md"), []byte(`---
name: legacy-global
description: legacy global path
---
legacy global body
`), 0644); err != nil {
		t.Fatal(err)
	}

	manifests := DiscoverSkillManifests(workspace)
	byName := map[string]Manifest{}
	for _, mf := range manifests {
		byName[mf.Name] = mf
	}

	if _, ok := byName["legacy-project"]; !ok {
		t.Fatal("missing legacy-project from .moss/skills")
	}
	if _, ok := byName["legacy-global"]; !ok {
		t.Fatal("missing legacy-global from ~/.moss/skills")
	}
}

func TestDiscoverSkillManifests_GlobalInstalledPluginsSkills(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	pluginSkillDir := filepath.Join(home, ".copilot", "installed-plugins", "awesome-copilot", "project-planning", "skills", "create-implementation-plan")
	if err := os.MkdirAll(pluginSkillDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginSkillDir, "SKILL.md"), []byte(`---
name: create-implementation-plan
description: create implementation plans
---
plan body
`), 0644); err != nil {
		t.Fatal(err)
	}

	manifests := DiscoverSkillManifests(workspace)
	for _, mf := range manifests {
		if mf.Name == "create-implementation-plan" {
			if !strings.Contains(mf.Source, filepath.Join("installed-plugins", "awesome-copilot", "project-planning", "skills", "create-implementation-plan", "SKILL.md")) {
				t.Fatalf("unexpected source: %q", mf.Source)
			}
			return
		}
	}
	t.Fatal("missing create-implementation-plan from ~/.copilot/installed-plugins/**/skills")
}

func TestParseSkillMD_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(`---
name: file-test
description: Test from file
---
File body.
`), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := ParseSkillMD(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.name != "file-test" {
		t.Errorf("name = %q", s.name)
	}
	if s.body != "File body." {
		t.Errorf("body = %q", s.body)
	}
}

func TestParseSkillMD_NotExist(t *testing.T) {
	_, err := ParseSkillMD("/nonexistent/SKILL.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
