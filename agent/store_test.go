package agent

import (
	"context"
	"github.com/mossagents/moss/kernel/model"
	"os"
	"path/filepath"
	"testing"
)

// --- MemoryTaskStore tests ---

func TestMemoryTaskStore_SaveLoad(t *testing.T) {
	store := NewMemoryTaskStore()
	ctx := context.Background()

	task := &Task{
		ID:        "t-1",
		AgentName: "coder",
		Goal:      "write tests",
		Status:    TaskRunning,
	}

	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(ctx, "t-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.ID != "t-1" || loaded.Goal != "write tests" {
		t.Fatalf("unexpected loaded task: %+v", loaded)
	}

	// Mutating the original must not affect the store.
	task.Goal = "modified"
	loaded2, _ := store.Load(ctx, "t-1")
	if loaded2.Goal != "write tests" {
		t.Fatal("store should return copies")
	}
}

func TestMemoryTaskStore_LoadNotFound(t *testing.T) {
	store := NewMemoryTaskStore()
	ctx := context.Background()

	task, err := store.Load(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if task != nil {
		t.Fatal("expected nil for nonexistent task")
	}
}

func TestMemoryTaskStore_List(t *testing.T) {
	store := NewMemoryTaskStore()
	ctx := context.Background()

	tasks := []*Task{
		{ID: "t-1", AgentName: "coder", Status: TaskRunning},
		{ID: "t-2", AgentName: "coder", Status: TaskCompleted},
		{ID: "t-3", AgentName: "reviewer", Status: TaskRunning},
	}
	for _, task := range tasks {
		if err := store.Save(ctx, task); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
}

func TestMemoryTaskStore_Delete(t *testing.T) {
	store := NewMemoryTaskStore()
	ctx := context.Background()

	_ = store.Save(ctx, &Task{ID: "t-1", Goal: "a"})
	_ = store.Save(ctx, &Task{ID: "t-2", Goal: "b"})

	if err := store.Delete(ctx, "t-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	task, _ := store.Load(ctx, "t-1")
	if task != nil {
		t.Fatal("expected nil after delete")
	}

	all, _ := store.List(ctx)
	if len(all) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(all))
	}
}

// --- FileTaskStore tests ---

func TestFileTaskStore_CRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	store := NewFileTaskStore(path)
	ctx := context.Background()

	// Save
	task := &Task{ID: "f-1", AgentName: "coder", Goal: "build", Status: TaskRunning}
	if err := store.Save(ctx, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Load
	loaded, err := store.Load(ctx, "f-1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil || loaded.Goal != "build" {
		t.Fatalf("unexpected: %+v", loaded)
	}

	// Load not found
	missing, err := store.Load(ctx, "no-such")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing task")
	}

	// Save second task + List
	_ = store.Save(ctx, &Task{ID: "f-2", Goal: "test"})
	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}

	// Update existing
	task.Goal = "updated-build"
	_ = store.Save(ctx, task)
	loaded2, _ := store.Load(ctx, "f-1")
	if loaded2.Goal != "updated-build" {
		t.Fatalf("expected updated goal, got %q", loaded2.Goal)
	}
	all2, _ := store.List(ctx)
	if len(all2) != 2 {
		t.Fatalf("update should not add duplicates, got %d", len(all2))
	}

	// Delete
	if err := store.Delete(ctx, "f-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted, _ := store.Load(ctx, "f-1")
	if deleted != nil {
		t.Fatal("expected nil after delete")
	}
	remaining, _ := store.List(ctx)
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(remaining))
	}

	// Delete non-existent is not an error
	if err := store.Delete(ctx, "no-such"); err != nil {
		t.Fatalf("Delete non-existent: %v", err)
	}
}

func TestFileTaskStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	store := NewFileTaskStore(path)
	ctx := context.Background()

	// List on non-existent file returns empty
	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected 0, got %d", len(all))
	}
}

func TestFileTaskStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	ctx := context.Background()

	// Write with one store instance
	s1 := NewFileTaskStore(path)
	_ = s1.Save(ctx, &Task{ID: "p-1", Goal: "persist"})

	// Read with a new store instance (simulates restart)
	s2 := NewFileTaskStore(path)
	loaded, err := s2.Load(ctx, "p-1")
	if err != nil {
		t.Fatalf("Load from new instance: %v", err)
	}
	if loaded == nil || loaded.Goal != "persist" {
		t.Fatalf("persistence failed: %+v", loaded)
	}
}

func TestFileTaskStore_ReturnsCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	store := NewFileTaskStore(path)
	ctx := context.Background()

	_ = store.Save(ctx, &Task{ID: "c-1", Goal: "original"})

	loaded, _ := store.Load(ctx, "c-1")
	loaded.Goal = "mutated"

	loaded2, _ := store.Load(ctx, "c-1")
	if loaded2.Goal != "original" {
		t.Fatal("Load should return independent copies")
	}
}

// --- TaskTracker with FileTaskStore ---

func TestTaskTracker_FileTaskStore_PersistAcrossReconstruction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tracker-tasks.json")

	// Create tracker with FileTaskStore, start a task.
	store1 := NewFileTaskStore(path)
	tt1 := NewTaskTracker(WithTaskStore(store1))

	task := &Task{ID: "tt-1", AgentName: "builder", Goal: "compile", Status: TaskRunning}
	tt1.Start(task, nil)

	// Verify file exists on disk
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("task store file was not created")
	}

	// Create a new tracker with a fresh FileTaskStore pointing to the same file.
	store2 := NewFileTaskStore(path)
	tt2 := NewTaskTracker(WithTaskStore(store2))

	got, ok := tt2.Get("tt-1")
	if !ok {
		t.Fatal("task not restored from file store")
	}
	if got.Goal != "compile" || got.Status != TaskRunning {
		t.Fatalf("unexpected restored task: %+v", got)
	}

	// Complete the task in tt2 and verify persistence.
	tt2.Complete("tt-1", "done", model.TokenUsage{})

	store3 := NewFileTaskStore(path)
	tt3 := NewTaskTracker(WithTaskStore(store3))

	got2, _ := tt3.Get("tt-1")
	if got2 == nil || got2.Status != TaskCompleted {
		t.Fatalf("completed status not persisted: %+v", got2)
	}
}

func TestTaskTracker_DefaultsToMemoryStore(t *testing.T) {
	tt := NewTaskTracker()
	task := &Task{ID: "m-1", AgentName: "a", Goal: "test", Status: TaskRunning}
	tt.Start(task, nil)

	got, ok := tt.Get("m-1")
	if !ok || got.Goal != "test" {
		t.Fatalf("memory store default broken: %+v", got)
	}
}
