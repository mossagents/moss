package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

func TestPromptSkill_Metadata(t *testing.T) {
	s := &PromptSkill{
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
		t.Errorf("PromptSkill should have no tools, got %v", meta.Tools)
	}
}

func TestPromptSkill_InitShutdown(t *testing.T) {
	s := &PromptSkill{name: "noop"}
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

func TestDiscoverPromptSkills(t *testing.T) {
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

	skills := DiscoverPromptSkills(workspace)
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

func TestDiscoverPromptSkills_Dedup(t *testing.T) {
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

	skills := DiscoverPromptSkills(workspace)
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
