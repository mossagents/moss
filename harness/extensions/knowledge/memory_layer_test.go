package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkingMemory_SetGet(t *testing.T) {
	ctx := context.Background()
	wm := NewWorkingMemory()

	if err := wm.Set(ctx, "goal", "fix bug"); err != nil {
		t.Fatal(err)
	}
	v, ok := wm.Get(ctx, "goal")
	if !ok || v != "fix bug" {
		t.Fatalf("expected 'fix bug', got %v", v)
	}

	summary := wm.Summary(ctx)
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestWorkingMemory_Clear(t *testing.T) {
	ctx := context.Background()
	wm := NewWorkingMemory()
	_ = wm.Set(ctx, "k", "v")
	_ = wm.Clear(ctx)
	if _, ok := wm.Get(ctx, "k"); ok {
		t.Fatal("expected key to be cleared")
	}
}

func TestMemoryEpisodicStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEpisodicStore()

	ep1 := Episode{SessionID: "s1", Kind: EpisodeToolCall, Summary: "read_file", Timestamp: time.Now()}
	ep2 := Episode{SessionID: "s1", Kind: EpisodeError, Summary: "permission denied", Timestamp: time.Now().Add(time.Second)}

	_ = store.Append(ctx, ep1)
	_ = store.Append(ctx, ep2)

	recent, err := store.Recent(ctx, "s1", 5)
	if err != nil || len(recent) != 2 {
		t.Fatalf("expected 2 episodes, got %d err=%v", len(recent), err)
	}

	results, err := store.Search(ctx, "permission", EpisodeFilter{SessionID: "s1"})
	if err != nil || len(results) != 1 {
		t.Fatalf("expected 1 search result, got %d err=%v", len(results), err)
	}
}

func TestFileEpisodicStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "episodes.jsonl")

	store, err := NewFileEpisodicStore(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	ep := Episode{SessionID: "sess", Kind: EpisodeDecision, Summary: "decided to refactor", Importance: 0.9}
	if err := store.Append(ctx, ep); err != nil {
		t.Fatal(err)
	}

	// Verify file was written
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Fatal("expected file to have content")
	}

	recent, err := store.Recent(ctx, "sess", 10)
	if err != nil || len(recent) != 1 {
		t.Fatalf("expected 1 episode, got %d err=%v", len(recent), err)
	}
	if recent[0].Summary != "decided to refactor" {
		t.Fatalf("unexpected summary: %s", recent[0].Summary)
	}
}

func TestMemoryManager_Inject(t *testing.T) {
	ctx := context.Background()
	wm := NewWorkingMemory()
	_ = wm.Set(ctx, "current_goal", "fix auth bug")

	store := NewMemoryEpisodicStore()
	_ = store.Append(ctx, Episode{
		SessionID: "s1",
		Kind:      EpisodeToolCall,
		Summary:   "read_file: auth.go",
		Timestamp: time.Now(),
	})

	mgr := NewMemoryManager(MemoryManagerConfig{
		Working:  wm,
		Episodic: store,
	})

	result, err := mgr.Inject(ctx, InjectConfig{SessionID: "s1", Query: "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("expected non-empty injection")
	}
	if len(result) > 5000 {
		t.Fatalf("injection too large: %d chars", len(result))
	}

	// Should contain working memory and episodic sections
	if !contains(result, "current_goal") {
		t.Error("expected working memory in injection")
	}
	if !contains(result, "read_file") {
		t.Error("expected episodic events in injection")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsInsensitive(s, sub)
}
