package sandbox

import (
	"context"
	"testing"
)

func TestScopedWorkspace_Isolation(t *testing.T) {
	shared := NewMemoryWorkspace()
	ws1 := NewScopedWorkspace("room_1", shared)
	ws2 := NewScopedWorkspace("room_2", shared)
	ctx := context.Background()

	if err := ws1.WriteFile(ctx, "notes.txt", []byte("room1 notes")); err != nil {
		t.Fatalf("ws1 WriteFile: %v", err)
	}
	if err := ws2.WriteFile(ctx, "notes.txt", []byte("room2 notes")); err != nil {
		t.Fatalf("ws2 WriteFile: %v", err)
	}

	d1, err := ws1.ReadFile(ctx, "notes.txt")
	if err != nil {
		t.Fatalf("ws1 read: %v", err)
	}
	d2, err := ws2.ReadFile(ctx, "notes.txt")
	if err != nil {
		t.Fatalf("ws2 read: %v", err)
	}

	if string(d1) != "room1 notes" {
		t.Errorf("ws1 expected 'room1 notes', got %q", string(d1))
	}
	if string(d2) != "room2 notes" {
		t.Errorf("ws2 expected 'room2 notes', got %q", string(d2))
	}

	// ws1 看不到 ws2 的文件
	_, err = ws1.ReadFile(ctx, "other.txt")
	if err == nil {
		t.Error("ws1 should not see ws2's files")
	}
}

func TestScopedWorkspace_PathTraversalPrevention(t *testing.T) {
	shared := NewMemoryWorkspace()
	kws := NewScopedWorkspace("tenant_a", shared)
	ctx := context.Background()

	_, err := kws.ReadFile(ctx, "../tenant_b/secret.txt")
	if err == nil {
		t.Error("should prevent path traversal with '..'")
	}
}

func TestScopedWorkspace_ListFiles(t *testing.T) {
	shared := NewMemoryWorkspace()
	kws := NewScopedWorkspace("project", shared)
	ctx := context.Background()

	if err := kws.WriteFile(ctx, "a.go", []byte("a")); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := kws.WriteFile(ctx, "b.go", []byte("b")); err != nil {
		t.Fatalf("write b.go: %v", err)
	}

	// 在 shared 中直接写入不属于 project scope 的文件
	if err := shared.WriteFile(ctx, "other/c.go", []byte("c")); err != nil {
		t.Fatalf("shared WriteFile: %v", err)
	}

	files, err := kws.ListFiles(ctx, "*.go")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 files, got %d: %v", len(files), files)
	}
	// 结果中不应包含前缀
	for _, f := range files {
		if f != "a.go" && f != "b.go" {
			t.Errorf("unexpected file in results: %s", f)
		}
	}
}

func TestScopedWorkspace_DeleteAndStat(t *testing.T) {
	shared := NewMemoryWorkspace()
	kws := NewScopedWorkspace("ns", shared)
	ctx := context.Background()

	if err := kws.WriteFile(ctx, "file.txt", []byte("data")); err != nil {
		t.Fatalf("WriteFile file.txt: %v", err)
	}

	info, err := kws.Stat(ctx, "file.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size != 4 {
		t.Errorf("expected size 4, got %d", info.Size)
	}

	if err := kws.DeleteFile(ctx, "file.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	_, err = kws.Stat(ctx, "file.txt")
	if err == nil {
		t.Error("expected error after delete")
	}
}
