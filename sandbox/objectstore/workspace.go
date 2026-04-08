package objectstore

import (
	"context"
	"fmt"
	kws "github.com/mossagents/moss/kernel/workspace"
	"path/filepath"
	"strings"
	"time"
)

// ObjectStoreWorkspace implements kws.Workspace over a BlobClient.
//
// All paths are relative to rootPrefix inside the bucket. For example:
//
//	workspace root = "workspaces/sess-001/"
//	ReadFile("src/main.go") → blob key "workspaces/sess-001/src/main.go"
type ObjectStoreWorkspace struct {
	client     BlobClient
	rootPrefix string // always ends with "/"
}

// NewObjectStoreWorkspace creates a workspace backed by the given BlobClient.
// rootPrefix is a path prefix for all keys (e.g. "workspaces/session-123/").
func NewObjectStoreWorkspace(client BlobClient, rootPrefix string) *ObjectStoreWorkspace {
	if rootPrefix != "" && !strings.HasSuffix(rootPrefix, "/") {
		rootPrefix += "/"
	}
	return &ObjectStoreWorkspace{client: client, rootPrefix: rootPrefix}
}

// NewS3Workspace is a convenience constructor that creates an S3-backed workspace.
func NewS3Workspace(cfg S3Config, rootPrefix string) (*ObjectStoreWorkspace, error) {
	client, err := NewS3BlobClient(cfg, nil)
	if err != nil {
		return nil, err
	}
	return NewObjectStoreWorkspace(client, rootPrefix), nil
}

// ReadFile downloads and returns a file from the workspace.
func (w *ObjectStoreWorkspace) ReadFile(ctx context.Context, path string) ([]byte, error) {
	data, err := w.client.Get(ctx, w.key(path))
	if err != nil {
		return nil, fmt.Errorf("workspace read %q: %w", path, err)
	}
	return data, nil
}

// WriteFile uploads a file to the workspace.
func (w *ObjectStoreWorkspace) WriteFile(ctx context.Context, path string, content []byte) error {
	if err := w.client.Put(ctx, w.key(path), content); err != nil {
		return fmt.Errorf("workspace write %q: %w", path, err)
	}
	return nil
}

// ListFiles lists workspace paths matching a glob pattern.
// Because object stores don't support server-side glob, the list is fetched
// from the storage backend and filtered client-side.
func (w *ObjectStoreWorkspace) ListFiles(ctx context.Context, pattern string) ([]string, error) {
	// Determine the longest non-glob prefix to narrow the server-side list.
	prefix := globPrefix(pattern)
	keys, err := w.client.List(ctx, w.key(prefix))
	if err != nil {
		return nil, fmt.Errorf("workspace list %q: %w", pattern, err)
	}

	var matched []string
	for _, k := range keys {
		rel := w.relPath(k)
		if rel == "" {
			continue
		}
		ok, err := filepath.Match(pattern, rel)
		if err != nil {
			return nil, fmt.Errorf("workspace list: invalid pattern %q: %w", pattern, err)
		}
		if ok {
			matched = append(matched, rel)
		}
	}
	return matched, nil
}

// Stat returns metadata for the given path.
func (w *ObjectStoreWorkspace) Stat(ctx context.Context, path string) (kws.FileInfo, error) {
	meta, err := w.client.Head(ctx, w.key(path))
	if err != nil {
		return kws.FileInfo{}, fmt.Errorf("workspace stat %q: %w", path, err)
	}
	return kws.FileInfo{
		Name:    filepath.Base(path),
		Size:    meta.Size,
		IsDir:   false,
		ModTime: meta.LastModified,
	}, nil
}

// DeleteFile removes a file from the workspace.
func (w *ObjectStoreWorkspace) DeleteFile(ctx context.Context, path string) error {
	if err := w.client.Delete(ctx, w.key(path)); err != nil {
		return fmt.Errorf("workspace delete %q: %w", path, err)
	}
	return nil
}

// key converts a workspace-relative path into a storage key.
func (w *ObjectStoreWorkspace) key(path string) string {
	path = strings.TrimLeft(filepath.ToSlash(path), "/")
	if path == "" {
		return strings.TrimRight(w.rootPrefix, "/")
	}
	return w.rootPrefix + path
}

// relPath strips the root prefix from a storage key.
func (w *ObjectStoreWorkspace) relPath(key string) string {
	return strings.TrimPrefix(key, w.rootPrefix)
}

// globPrefix returns the longest literal prefix before any wildcard.
func globPrefix(pattern string) string {
	for i, ch := range pattern {
		if ch == '*' || ch == '?' || ch == '[' {
			return pattern[:i]
		}
	}
	return pattern
}

// ensure ObjectStoreWorkspace satisfies kws.Workspace at compile time
var _ kws.Workspace = (*ObjectStoreWorkspace)(nil)

// StaticFileInfo is a convenience FileInfo used for stub implementations.
var StaticFileInfo = kws.FileInfo{ModTime: time.Time{}}
