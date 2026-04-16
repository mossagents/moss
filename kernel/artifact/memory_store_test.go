package artifact

import (
	"context"
	"testing"
)

func TestMemoryStore_SaveAndLoad(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	a := &Artifact{Name: "output.txt", MIMEType: "text/plain", Data: []byte("hello")}
	if err := store.Save(ctx, "s1", a); err != nil {
		t.Fatalf("save: %v", err)
	}
	if a.Version != 1 || a.ID == "" {
		t.Fatalf("after save: version=%d, id=%q", a.Version, a.ID)
	}

	loaded, err := store.Load(ctx, "s1", "output.txt", 0)
	if err != nil {
		t.Fatalf("load latest: %v", err)
	}
	if string(loaded.Data) != "hello" || loaded.Version != 1 {
		t.Fatalf("loaded: data=%q version=%d", loaded.Data, loaded.Version)
	}
}

func TestMemoryStore_Versioning(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	store.Save(ctx, "s1", &Artifact{Name: "doc", Data: []byte("v1")})
	store.Save(ctx, "s1", &Artifact{Name: "doc", Data: []byte("v2")})
	store.Save(ctx, "s1", &Artifact{Name: "doc", Data: []byte("v3")})

	// Latest version.
	latest, _ := store.Load(ctx, "s1", "doc", 0)
	if string(latest.Data) != "v3" || latest.Version != 3 {
		t.Fatalf("latest: data=%q version=%d", latest.Data, latest.Version)
	}

	// Specific version.
	v1, _ := store.Load(ctx, "s1", "doc", 1)
	if string(v1.Data) != "v1" {
		t.Fatalf("v1: data=%q", v1.Data)
	}

	// All versions.
	versions, _ := store.Versions(ctx, "s1", "doc")
	if len(versions) != 3 {
		t.Fatalf("versions count: %d", len(versions))
	}

	// Version out of range.
	_, err := store.Load(ctx, "s1", "doc", 5)
	if err == nil {
		t.Fatal("expected error for out-of-range version")
	}
}

func TestMemoryStore_List(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	store.Save(ctx, "s1", &Artifact{Name: "a.txt", Data: []byte("aaa")})
	store.Save(ctx, "s1", &Artifact{Name: "b.txt", Data: []byte("bbb")})
	store.Save(ctx, "s1", &Artifact{Name: "a.txt", Data: []byte("aaa-v2")})

	items, _ := store.List(ctx, "s1")
	if len(items) != 2 {
		t.Fatalf("list count: %d", len(items))
	}

	// Empty session.
	empty, _ := store.List(ctx, "s-none")
	if len(empty) != 0 {
		t.Fatalf("expected empty list, got %d", len(empty))
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	store.Save(ctx, "s1", &Artifact{Name: "temp", Data: []byte("data")})
	if err := store.Delete(ctx, "s1", "temp"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := store.Load(ctx, "s1", "temp", 0)
	if err == nil {
		t.Fatal("expected not-found after delete")
	}
}

func TestMemoryStore_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	_, err := store.Load(ctx, "s1", "missing", 0)
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
}

func TestMemoryStore_NilArtifact(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	if err := store.Save(ctx, "s1", nil); err == nil {
		t.Fatal("expected error for nil artifact")
	}
}

func TestMemoryStore_EmptyName(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	if err := store.Save(ctx, "s1", &Artifact{Data: []byte("data")}); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestMemoryStore_DataIsolation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	original := []byte("original")
	store.Save(ctx, "s1", &Artifact{Name: "test", Data: original})

	// Mutate original.
	original[0] = 'X'

	loaded, _ := store.Load(ctx, "s1", "test", 0)
	if loaded.Data[0] == 'X' {
		t.Fatal("stored data should be independent of original slice")
	}

	// Mutate loaded.
	loaded.Data[0] = 'Y'
	loaded2, _ := store.Load(ctx, "s1", "test", 0)
	if loaded2.Data[0] == 'Y' {
		t.Fatal("loaded data should be independent copy")
	}
}
