package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/port"
)

func TestFileStore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// 空列表
	summaries, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries, got %d", len(summaries))
	}

	// Save
	sess := &Session{
		ID:     "test-1",
		Status: StatusCompleted,
		Config: SessionConfig{
			Goal: "test goal",
			Mode: "test",
		},
		Messages: []port.Message{
			{Role: port.RoleUser, Content: "hello"},
			{Role: port.RoleAssistant, Content: "world"},
		},
		Budget:    Budget{MaxSteps: 10, UsedSteps: 3},
		CreatedAt: time.Now(),
		EndedAt:   time.Now(),
	}

	if err := store.Save(ctx, sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load
	loaded, err := store.Load(ctx, "test-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}
	if loaded.ID != "test-1" {
		t.Fatalf("expected ID test-1, got %s", loaded.ID)
	}
	if loaded.Config.Goal != "test goal" {
		t.Fatalf("expected goal 'test goal', got %s", loaded.Config.Goal)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loaded.Messages))
	}

	// Load non-existent
	missing, err := store.Load(ctx, "no-such-id")
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing session")
	}

	// List
	summaries, err = store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Goal != "test goal" {
		t.Fatalf("expected goal 'test goal', got %s", summaries[0].Goal)
	}

	// Delete
	if err := store.Delete(ctx, "test-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify deleted
	loaded, err = store.Load(ctx, "test-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestFileStoreSanitizeID(t *testing.T) {
	// 确保路径遍历攻击被阻止
	id := sanitizeID("../../etc/passwd")
	if id == "../../etc/passwd" {
		t.Fatal("sanitizeID should prevent path traversal")
	}

	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 即使传入恶意 ID，文件也应该在 store.dir 内
	path := store.path("../../etc/passwd")
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	// 应该在 dir 内（不以 .. 开头）
	if filepath.IsAbs(rel) || rel[:2] == ".." {
		t.Fatalf("path traversal not prevented: %s", path)
	}
}

func TestFileStoreOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	sess := &Session{
		ID:     "ow-1",
		Status: StatusRunning,
		Config: SessionConfig{Goal: "v1"},
	}
	if err := store.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// Update and save again
	sess.Status = StatusCompleted
	sess.Config.Goal = "v2"
	if err := store.Save(ctx, sess); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(ctx, "ow-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", loaded.Status)
	}
	if loaded.Config.Goal != "v2" {
		t.Fatalf("expected v2, got %s", loaded.Config.Goal)
	}

	// 确认只有一个文件
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}
}
