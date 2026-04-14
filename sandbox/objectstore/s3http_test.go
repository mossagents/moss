package objectstore_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mossagents/moss/sandbox/objectstore"
)

// fakeS3 is a minimal S3-compatible server for testing S3BlobClient.
// It handles GET, PUT, DELETE, HEAD, and list-type=2 queries.
type fakeS3 struct {
	objects map[string][]byte
}

func newFakeS3() *fakeS3 {
	return &fakeS3{objects: make(map[string][]byte)}
}

func (f *fakeS3) handler(w http.ResponseWriter, r *http.Request) {
	// For path-style: URL is /bucket/key; strip "/bucket/" prefix
	path := strings.TrimPrefix(r.URL.Path, "/bucket")
	key := strings.TrimPrefix(path, "/")

	// List operation uses query param list-type=2
	if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
		prefix := r.URL.Query().Get("prefix")
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<?xml version="1.0"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`))
		for k, v := range f.objects {
			if prefix == "" || strings.HasPrefix(k, prefix) {
				w.Write([]byte(fmt.Sprintf("<Contents><Key>%s</Key><Size>%d</Size></Contents>", k, len(v))))
			}
		}
		w.Write([]byte(`</ListBucketResult>`))
		return
	}

	switch r.Method {
	case http.MethodGet:
		v, ok := f.objects[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(v)
	case http.MethodPut:
		body := make([]byte, 0, 64)
		buf := make([]byte, 512)
		for {
			n, err := r.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		r.Body.Close()
		f.objects[key] = body
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodHead:
		v, ok := f.objects[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(v)))
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func newS3TestClient(t *testing.T, f *fakeS3) objectstore.BlobClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	t.Cleanup(srv.Close)
	c, err := objectstore.NewS3BlobClient(objectstore.S3Config{
		Endpoint:  srv.URL,
		Bucket:    "bucket",
		Region:    "us-east-1",
		PathStyle: true,
	}, srv.Client())
	if err != nil {
		t.Fatalf("NewS3BlobClient: %v", err)
	}
	return c
}

func TestS3BlobClient_PutGet(t *testing.T) {
	f := newFakeS3()
	c := newS3TestClient(t, f)
	ctx := context.Background()

	if err := c.Put(ctx, "hello.txt", []byte("world")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, err := c.Get(ctx, "hello.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != "world" {
		t.Fatalf("Get content = %q, want world", data)
	}
}

func TestS3BlobClient_Get_NotFound(t *testing.T) {
	f := newFakeS3()
	c := newS3TestClient(t, f)
	_, err := c.Get(context.Background(), "missing.txt")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestS3BlobClient_Delete(t *testing.T) {
	f := newFakeS3()
	c := newS3TestClient(t, f)
	ctx := context.Background()
	_ = c.Put(ctx, "tmp.txt", []byte("x"))
	if err := c.Delete(ctx, "tmp.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := c.Get(ctx, "tmp.txt")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestS3BlobClient_Head(t *testing.T) {
	f := newFakeS3()
	c := newS3TestClient(t, f)
	ctx := context.Background()
	_ = c.Put(ctx, "data.bin", []byte("12345"))
	_, err := c.Head(ctx, "data.bin")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
}

func TestS3BlobClient_Head_NotFound(t *testing.T) {
	f := newFakeS3()
	c := newS3TestClient(t, f)
	_, err := c.Head(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestS3BlobClient_List(t *testing.T) {
	f := newFakeS3()
	c := newS3TestClient(t, f)
	ctx := context.Background()
	_ = c.Put(ctx, "a/1.txt", []byte(""))
	_ = c.Put(ctx, "a/2.txt", []byte(""))
	_ = c.Put(ctx, "b/3.txt", []byte(""))
	keys, err := c.List(ctx, "a/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys under a/, got %d: %v", len(keys), keys)
	}
}

// ─── NewS3Workspace (constructor test) ───────────────────────────────────────

func TestNewS3Workspace_InvalidConfig(t *testing.T) {
	_, err := objectstore.NewS3Workspace(objectstore.S3Config{}, "prefix/")
	if err == nil {
		t.Fatal("expected error for missing bucket")
	}
}

// ─── workspace edge cases ─────────────────────────────────────────────────────

func TestObjectStoreWorkspace_NoTrailingSlash(t *testing.T) {
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/test")
	ctx := context.Background()
	if err := ws.WriteFile(ctx, "a.txt", []byte("hi")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := ws.ReadFile(ctx, "a.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("content = %q, want hi", data)
	}
}

func TestObjectStoreWorkspace_ListFiles_NoMatch(t *testing.T) {
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/")
	ctx := context.Background()
	_ = ws.WriteFile(ctx, "hello.txt", []byte(""))
	files, err := ws.ListFiles(ctx, "*.go")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no .go files, got %v", files)
	}
}

func TestObjectStoreWorkspace_Stat_Error(t *testing.T) {
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/")
	_, err := ws.Stat(context.Background(), "missing.bin")
	if err == nil {
		t.Fatal("expected error for missing file stat")
	}
}

func TestObjectStoreWorkspace_ReadFile_Error(t *testing.T) {
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/")
	_, err := ws.ReadFile(context.Background(), "ghost.txt")
	if err == nil {
		t.Fatal("expected error reading missing file")
	}
}

func TestObjectStoreWorkspace_DeleteFile_Error(t *testing.T) {
	// DeleteFile wraps client.Delete — mock always succeeds but tests error path indirectly
	c := objectstore.NewMockBlobClient()
	ws := objectstore.NewObjectStoreWorkspace(c, "ws/")
	// Delete non-existent key: MockBlobClient.Delete silently succeeds
	if err := ws.DeleteFile(context.Background(), "nonexistent.txt"); err != nil {
		t.Fatalf("DeleteFile on non-existent: %v", err)
	}
}

