package skillsx

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
	kt "github.com/mossagents/moss/testing"
	"github.com/mossagents/moss/skill"
)

func TestRegisterProgressiveTools_ListAndActivate(t *testing.T) {
	mock := &kt.MockLLM{}
	io := kt.NewRecorderIO()
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(io),
	)

	ctx := context.Background()
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".agent", "skills", "demo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	skillFile := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(`---
name: demo
description: demo skill
---
Use demo instructions.
`), 0644); err != nil {
		t.Fatal(err)
	}

	SetManifests(k, skill.DiscoverSkillManifests(workspace))
	EnableProgressive(k)
	if err := RegisterProgressiveTools(k); err != nil {
		t.Fatalf("RegisterProgressiveTools: %v", err)
	}

	_, listHandler, ok := k.ToolRegistry().Get("list_skills")
	if !ok {
		t.Fatal("list_skills not registered")
	}
	listRaw, err := listHandler(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list_skills call failed: %v", err)
	}
	var listed []map[string]any
	if err := json.Unmarshal(listRaw, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0]["name"] != "demo" {
		t.Fatalf("unexpected listed skills: %+v", listed)
	}
	if listed[0]["loaded"] != false {
		t.Fatalf("expected unloaded before activation: %+v", listed[0])
	}

	_, activateHandler, ok := k.ToolRegistry().Get("activate_skill")
	if !ok {
		t.Fatal("activate_skill not registered")
	}
	actInput, _ := json.Marshal(map[string]string{"name": "demo"})
	actRaw, err := activateHandler(ctx, actInput)
	if err != nil {
		t.Fatalf("activate_skill call failed: %v", err)
	}
	var actResp map[string]string
	if err := json.Unmarshal(actRaw, &actResp); err != nil {
		t.Fatal(err)
	}
	if actResp["status"] != "loaded" {
		t.Fatalf("unexpected activate response: %+v", actResp)
	}
	if _, loaded := Manager(k).Get("demo"); !loaded {
		t.Fatal("skill should be loaded after activation")
	}
}

func TestProgressivePromptHint(t *testing.T) {
	mock := &kt.MockLLM{}
	io := kt.NewRecorderIO()
	k := kernel.New(
		kernel.WithLLM(mock),
		kernel.WithUserIO(io),
	)
	EnableProgressive(k)
	SetManifests(k, []skill.Manifest{
		{Name: "a"},
		{Name: "b"},
	})

	// use extension hook result via session creation
	sess, err := k.NewSession(context.Background(), session.SessionConfig{Goal: "goal"})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	if len(sess.Messages) == 0 {
		t.Fatal("expected system prompt message")
	}
	content := sess.Messages[0].Content
	if !strings.Contains(content, "list_skills") || !strings.Contains(content, "activate_skill") {
		t.Fatalf("expected progressive hint in system prompt, got: %q", content)
	}
}

