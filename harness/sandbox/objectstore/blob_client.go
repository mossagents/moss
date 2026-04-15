// Package objectstore provides a kws.Workspace implementation backed by
// S3-compatible object storage (AWS S3, GCS XML API, MinIO, Cloudflare R2).
// No AWS SDK is required; all requests are signed via pure net/http.
package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BlobMeta describes object metadata returned by Head.
type BlobMeta struct {
	Size         int64
	ContentType  string
	LastModified time.Time
	ETag         string
}

// BlobClient is the storage abstraction used by ObjectStoreWorkspace.
type BlobClient interface {
	// Get downloads the object at key.
	Get(ctx context.Context, key string) ([]byte, error)
	// Put uploads body to key, overwriting any existing object.
	Put(ctx context.Context, key string, body []byte) error
	// Delete removes the object at key.
	Delete(ctx context.Context, key string) error
	// List returns all keys with the given prefix.
	List(ctx context.Context, prefix string) ([]string, error)
	// Head fetches metadata for key without downloading the body.
	Head(ctx context.Context, key string) (BlobMeta, error)
}

// MockBlobClient is an in-memory BlobClient for testing.
type MockBlobClient struct {
	objects map[string][]byte
}

// NewMockBlobClient creates a new in-memory BlobClient.
func NewMockBlobClient() *MockBlobClient {
	return &MockBlobClient{objects: make(map[string][]byte)}
}

func (m *MockBlobClient) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.objects[key]
	if !ok {
		return nil, fmt.Errorf("objectstore: key not found: %s", key)
	}
	cp := make([]byte, len(v))
	copy(cp, v)
	return cp, nil
}

func (m *MockBlobClient) Put(_ context.Context, key string, body []byte) error {
	cp := make([]byte, len(body))
	copy(cp, body)
	m.objects[key] = cp
	return nil
}

func (m *MockBlobClient) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func (m *MockBlobClient) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *MockBlobClient) Head(_ context.Context, key string) (BlobMeta, error) {
	v, ok := m.objects[key]
	if !ok {
		return BlobMeta{}, fmt.Errorf("objectstore: key not found: %s", key)
	}
	return BlobMeta{Size: int64(len(v)), ContentType: "application/octet-stream"}, nil
}

// httpDoer abstracts http.Client for testability.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func readBody(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(resp.Body)
}
