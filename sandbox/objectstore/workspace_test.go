package objectstore_test

import (
	"context"
	"testing"

	"github.com/mossagents/moss/sandbox/objectstore"
)

func TestMockBlobClient_PutGet(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()

	if err := c.Put(ctx, "foo/bar.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	data, err := c.Get(ctx, "foo/bar.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", data)
	}
}

func TestMockBlobClient_Delete(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()
	_ = c.Put(ctx, "k", []byte("v"))
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	_, err := c.Get(ctx, "k")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestMockBlobClient_List(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()
	_ = c.Put(ctx, "ws/a/1.txt", []byte("a"))
	_ = c.Put(ctx, "ws/a/2.txt", []byte("b"))
	_ = c.Put(ctx, "ws/b/3.txt", []byte("c"))

	keys, err := c.List(ctx, "ws/a/")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys under ws/a/, got %d", len(keys))
	}
}

func TestObjectStoreWorkspace_ReadWrite(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "workspaces/test/")

	if err := ws.WriteFile(ctx, "hello.txt", []byte("world")); err != nil {
		t.Fatal(err)
	}
	data, err := ws.ReadFile(ctx, "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got %q", data)
	}
}

func TestObjectStoreWorkspace_ListFiles(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "workspaces/test/")

	_ = ws.WriteFile(ctx, "src/main.go", []byte(""))
	_ = ws.WriteFile(ctx, "src/util.go", []byte(""))
	_ = ws.WriteFile(ctx, "README.md", []byte(""))

	files, err := ws.ListFiles(ctx, "src/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expected 2 .go files, got %d: %v", len(files), files)
	}
}

func TestObjectStoreWorkspace_Stat(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/")

	_ = ws.WriteFile(ctx, "data.bin", []byte("12345"))
	info, err := ws.Stat(ctx, "data.bin")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 5 {
		t.Errorf("expected size=5, got %d", info.Size)
	}
	if info.Name != "data.bin" {
		t.Errorf("expected name='data.bin', got %q", info.Name)
	}
}

func TestObjectStoreWorkspace_DeleteFile(t *testing.T) {
	ctx := context.Background()
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/")

	_ = ws.WriteFile(ctx, "tmp.txt", []byte("x"))
	if err := ws.DeleteFile(ctx, "tmp.txt"); err != nil {
		t.Fatal(err)
	}
	_, err := ws.ReadFile(ctx, "tmp.txt")
	if err == nil {
		t.Error("expected error after delete")
	}
}
