package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectSkillMentions_Basic(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "jina", "Extract content from URLs")

	manifests := []Manifest{
		{Name: "jina", Description: "Extract content from URLs", Source: filepath.Join(dir, "jina", "SKILL.md")},
		{Name: "brainstorm", Description: "Creative work", Source: filepath.Join(dir, "brainstorm", "SKILL.md")},
	}

	fragments := CollectSkillMentions("Please use @jina to fetch the page", manifests)
	if len(fragments) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(fragments))
	}
	if fragments[0].ID != "skill:jina" {
		t.Errorf("expected skill:jina, got %s", fragments[0].ID)
	}
}

func TestCollectSkillMentions_NoMention(t *testing.T) {
	manifests := []Manifest{
		{Name: "jina", Description: "Extract content"},
	}
	fragments := CollectSkillMentions("Hello world", manifests)
	if len(fragments) != 0 {
		t.Fatalf("expected 0 fragments, got %d", len(fragments))
	}
}

func TestCollectSkillMentions_Dedup(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "jina", "Extract content from URLs")

	manifests := []Manifest{
		{Name: "jina", Description: "Extract content", Source: filepath.Join(dir, "jina", "SKILL.md")},
	}
	fragments := CollectSkillMentions("Use @jina and then @jina again", manifests)
	if len(fragments) != 1 {
		t.Fatalf("expected 1 fragment (dedup), got %d", len(fragments))
	}
}

func TestCollectSkillMentions_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "Jina", "Extract content from URLs")

	manifests := []Manifest{
		{Name: "Jina", Description: "Extract content", Source: filepath.Join(dir, "Jina", "SKILL.md")},
	}
	fragments := CollectSkillMentions("Use @jina please", manifests)
	if len(fragments) != 1 {
		t.Fatalf("expected 1 fragment (case insensitive), got %d", len(fragments))
	}
}

func TestBuildSkillCatalogFragment(t *testing.T) {
	manifests := []Manifest{
		{Name: "jina", Description: "Extract content from URLs"},
		{Name: "brainstorm", Description: "Creative work planning"},
	}

	fragment := BuildSkillCatalogFragment(manifests)
	if fragment.ID != "skill_catalog" {
		t.Errorf("expected skill_catalog, got %s", fragment.ID)
	}
	if fragment.Text == "" {
		t.Error("expected non-empty text")
	}
}

func TestBuildSkillCatalogFragment_Empty(t *testing.T) {
	fragment := BuildSkillCatalogFragment(nil)
	if fragment.ID != "" {
		t.Errorf("expected empty fragment for nil manifests, got %s", fragment.ID)
	}
}

func writeSkillFile(t *testing.T, base, name, desc string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nThis is the skill body for " + name + ".\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
