package artifact

import (
	"context"
	"testing"
)

func TestFileStore_SaveLoadAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	original := []byte("hello")
	item := &Artifact{Name: "output.txt", MIMEType: "text/plain", Data: original}
	if err := store.Save(ctx, "s1", item); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if item.Version != 1 || item.ID == "" {
		t.Fatalf("saved artifact = %+v", item)
	}
	original[0] = 'X'

	reopened, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore reopen: %v", err)
	}
	loaded, err := reopened.Load(ctx, "s1", "output.txt", 0)
	if err != nil {
		t.Fatalf("Load latest: %v", err)
	}
	if string(loaded.Data) != "hello" {
		t.Fatalf("loaded data = %q, want hello", loaded.Data)
	}
}

func TestFileStore_VersioningAndList(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	for _, value := range []string{"v1", "v2", "v3"} {
		if err := store.Save(ctx, "s1", &Artifact{Name: "doc", Data: []byte(value)}); err != nil {
			t.Fatalf("Save %s: %v", value, err)
		}
	}
	if err := store.Save(ctx, "s1", &Artifact{Name: "other", Data: []byte("x")}); err != nil {
		t.Fatalf("Save other: %v", err)
	}

	latest, err := store.Load(ctx, "s1", "doc", 0)
	if err != nil {
		t.Fatalf("Load latest: %v", err)
	}
	if latest.Version != 3 || string(latest.Data) != "v3" {
		t.Fatalf("latest = %+v", latest)
	}

	v1, err := store.Load(ctx, "s1", "doc", 1)
	if err != nil {
		t.Fatalf("Load v1: %v", err)
	}
	if string(v1.Data) != "v1" {
		t.Fatalf("v1 data = %q", v1.Data)
	}

	versions, err := store.Versions(ctx, "s1", "doc")
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("versions len = %d, want 3", len(versions))
	}

	list, err := store.List(ctx, "s1")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
}

func TestFileStore_DeleteAndMissing(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if err := store.Save(ctx, "s1", &Artifact{Name: "temp", Data: []byte("x")}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := store.Delete(ctx, "s1", "temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Load(ctx, "s1", "temp", 0); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestFileStore_SaveValidation(t *testing.T) {
	ctx := context.Background()
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if err := store.Save(ctx, "s1", nil); err == nil {
		t.Fatal("expected nil artifact error")
	}
	if err := store.Save(ctx, "s1", &Artifact{Data: []byte("x")}); err == nil {
		t.Fatal("expected empty name error")
	}
}
