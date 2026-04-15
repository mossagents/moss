package sandbox

import (
	"context"
	"testing"
)

func TestMemoryWorkspace_ReadWriteFile(t *testing.T) {
	kws := NewMemoryWorkspace()
	ctx := context.Background()

	// 写入
	if err := kws.WriteFile(ctx, "hello.txt", []byte("world")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 读取
	data, err := kws.ReadFile(ctx, "hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got %q", string(data))
	}

	// 覆盖写入
	if err := kws.WriteFile(ctx, "hello.txt", []byte("updated")); err != nil {
		t.Fatalf("WriteFile overwrite: %v", err)
	}
	data, err = kws.ReadFile(ctx, "hello.txt")
	if err != nil {
		t.Fatalf("ReadFile after overwrite: %v", err)
	}
	if string(data) != "updated" {
		t.Errorf("expected 'updated', got %q", string(data))
	}
}

func TestMemoryWorkspace_NotFound(t *testing.T) {
	kws := NewMemoryWorkspace()
	ctx := context.Background()

	_, err := kws.ReadFile(ctx, "nonexistent.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestMemoryWorkspace_ListFiles(t *testing.T) {
	kws := NewMemoryWorkspace()
	ctx := context.Background()

	if err := kws.WriteFile(ctx, "src/a.go", []byte("a")); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}
	if err := kws.WriteFile(ctx, "src/b.go", []byte("b")); err != nil {
		t.Fatalf("WriteFile b: %v", err)
	}
	if err := kws.WriteFile(ctx, "readme.md", []byte("r")); err != nil {
		t.Fatalf("WriteFile readme: %v", err)
	}

	matches, err := kws.ListFiles(ctx, "src/*.go")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}

func TestMemoryWorkspace_Stat(t *testing.T) {
	kws := NewMemoryWorkspace()
	ctx := context.Background()

	if err := kws.WriteFile(ctx, "data.bin", []byte("12345")); err != nil {
		t.Fatalf("WriteFile data.bin: %v", err)
	}
	info, err := kws.Stat(ctx, "data.bin")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size != 5 {
		t.Errorf("expected size 5, got %d", info.Size)
	}
	if info.Name != "data.bin" {
		t.Errorf("expected name 'data.bin', got %q", info.Name)
	}
}

func TestMemoryWorkspace_DeleteFile(t *testing.T) {
	kws := NewMemoryWorkspace()
	ctx := context.Background()

	if err := kws.WriteFile(ctx, "temp.txt", []byte("tmp")); err != nil {
		t.Fatalf("WriteFile temp.txt: %v", err)
	}
	if err := kws.DeleteFile(ctx, "temp.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	_, err := kws.ReadFile(ctx, "temp.txt")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMemoryWorkspace_CapacityLimit(t *testing.T) {
	kws := NewMemoryWorkspace(WithMaxTotalSize(10))
	ctx := context.Background()

	if err := kws.WriteFile(ctx, "a.txt", []byte("12345")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := kws.WriteFile(ctx, "b.txt", []byte("12345")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	// 超过容量
	err := kws.WriteFile(ctx, "c.txt", []byte("1"))
	if err == nil {
		t.Error("expected capacity exceeded error")
	}
}

func TestMemoryWorkspace_PathNormalization(t *testing.T) {
	kws := NewMemoryWorkspace()
	ctx := context.Background()

	// 使用反斜杠写入
	if err := kws.WriteFile(ctx, "src\\main.go", []byte("main")); err != nil {
		t.Fatalf("WriteFile normalized path: %v", err)
	}
	// 使用正斜杠读取
	data, err := kws.ReadFile(ctx, "src/main.go")
	if err != nil {
		t.Fatalf("path normalization failed: %v", err)
	}
	if string(data) != "main" {
		t.Errorf("expected 'main', got %q", string(data))
	}
}

func TestMemoryWorkspace_IsolationBetweenInstances(t *testing.T) {
	ws1 := NewMemoryWorkspace()
	ws2 := NewMemoryWorkspace()
	ctx := context.Background()

	if err := ws1.WriteFile(ctx, "shared.txt", []byte("room1")); err != nil {
		t.Fatalf("ws1 WriteFile: %v", err)
	}
	if err := ws2.WriteFile(ctx, "shared.txt", []byte("room2")); err != nil {
		t.Fatalf("ws2 WriteFile: %v", err)
	}

	d1, _ := ws1.ReadFile(ctx, "shared.txt")
	d2, _ := ws2.ReadFile(ctx, "shared.txt")

	if string(d1) != "room1" {
		t.Errorf("ws1 should have 'room1', got %q", string(d1))
	}
	if string(d2) != "room2" {
		t.Errorf("ws2 should have 'room2', got %q", string(d2))
	}
}
