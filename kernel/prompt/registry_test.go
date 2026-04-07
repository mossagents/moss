package prompt_test

import (
	"testing"
	"testing/fstest"

	"github.com/mossagents/moss/kernel/prompt"
)

func TestMemoryRegistry_RegisterAndGet(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	err := reg.Register(prompt.PromptTemplate{
		ID:       "test.hello",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
		Defaults: map[string]any{"Name": "World"},
	})
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := reg.Get("test.hello")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.ID != "test.hello" {
		t.Fatalf("expected ID test.hello, got %s", tmpl.ID)
	}
}

func TestMemoryRegistry_Render_Defaults(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_ = reg.Register(prompt.PromptTemplate{
		ID:       "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
		Defaults: map[string]any{"Name": "World"},
	})

	result, err := reg.Render("greeting", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Hello, World!" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
	if result.Tokens == 0 {
		t.Error("expected non-zero token estimate")
	}
}

func TestMemoryRegistry_Render_VarsOverrideDefaults(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_ = reg.Register(prompt.PromptTemplate{
		ID:       "greeting",
		Version:  "1.0.0",
		Template: "Hello, {{.Name}}!",
		Defaults: map[string]any{"Name": "World"},
	})

	result, err := reg.Render("greeting", map[string]any{"Name": "Moss"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "Hello, Moss!" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestMemoryRegistry_Render_Conditional(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_ = reg.Register(prompt.PromptTemplate{
		ID:       "system",
		Version:  "1.0.0",
		Template: "You are Moss.{{if .HasCodeExec}} You can run code.{{end}}",
		Defaults: map[string]any{"HasCodeExec": false},
	})

	// Without code exec
	r1, _ := reg.Render("system", nil)
	if r1.Content != "You are Moss." {
		t.Fatalf("unexpected: %q", r1.Content)
	}

	// With code exec
	r2, _ := reg.Render("system", map[string]any{"HasCodeExec": true})
	if r2.Content != "You are Moss. You can run code." {
		t.Fatalf("unexpected: %q", r2.Content)
	}
}

func TestMemoryRegistry_Partials(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_ = reg.Register(prompt.PromptTemplate{
		ID:       "partial.safety",
		Version:  "1.0.0",
		Template: "Do not do harm.",
	})
	_ = reg.Register(prompt.PromptTemplate{
		ID:       "system.base",
		Version:  "1.0.0",
		Template: "You are Moss.\n{{template \"partial.safety\" .}}",
		Partials: []string{"partial.safety"},
	})

	result, err := reg.Render("system.base", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "You are Moss.\nDo not do harm." {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestMemoryRegistry_List(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_ = reg.Register(prompt.PromptTemplate{ID: "a", Version: "1", Template: "a", Tags: []string{"system"}})
	_ = reg.Register(prompt.PromptTemplate{ID: "b", Version: "1", Template: "b", Tags: []string{"tool"}})
	_ = reg.Register(prompt.PromptTemplate{ID: "c", Version: "1", Template: "c", Tags: []string{"system"}})

	all := reg.List()
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	filtered := reg.List("system")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 system templates, got %d", len(filtered))
	}
}

func TestMemoryRegistry_GetVersion(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_ = reg.Register(prompt.PromptTemplate{ID: "x", Version: "1.0.0", Template: "v1"})
	_ = reg.Register(prompt.PromptTemplate{ID: "x", Version: "2.0.0", Template: "v2"})

	v1, err := reg.GetVersion("x", "1.0.0")
	if err != nil || v1.Template != "v1" {
		t.Fatalf("expected v1, err=%v", err)
	}
	latest, _ := reg.Get("x")
	if latest.Template != "v2" {
		t.Fatal("expected latest to be v2")
	}
}

func TestFSLoader_Build(t *testing.T) {
	mockFS := fstest.MapFS{
		"system/base.yaml": &fstest.MapFile{
			Data: []byte(`id: system.base
version: "1.0.0"
template: "You are {{.Name}}."
defaults:
  Name: Moss
`),
		},
		"tools/not_yaml.txt": &fstest.MapFile{Data: []byte("ignored")},
	}

	loader := &prompt.FSLoader{Root: mockFS}
	reg, err := loader.Build()
	if err != nil {
		t.Fatal(err)
	}

	result, err := reg.Render("system.base", map[string]any{"Name": "Claude"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "You are Claude." {
		t.Fatalf("unexpected: %q", result.Content)
	}
}

func TestRegistry_NotFound(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	_, err := reg.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent template")
	}
}

func TestRegistry_EmptyIDError(t *testing.T) {
	reg := prompt.NewMemoryRegistry()
	err := reg.Register(prompt.PromptTemplate{Template: "hello"})
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}
