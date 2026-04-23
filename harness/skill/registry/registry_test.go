package registry

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mossagents/moss/harness/capability"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTempCache(t *testing.T) *LocalCache {
	t.Helper()
	dir := t.TempDir()
	c, err := NewLocalCache(dir)
	if err != nil {
		t.Fatalf("NewLocalCache: %v", err)
	}
	return c
}

// makeZip creates a minimal in-memory zip containing the given files.
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip.Write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	return buf.Bytes()
}

// ─── NewLocalCache ────────────────────────────────────────────────────────────

func TestNewLocalCache_ExplicitDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "myskills")
	c, err := NewLocalCache(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.root != sub {
		t.Fatalf("root = %q, want %q", c.root, sub)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("dir should be created: %v", err)
	}
}

// ─── ListInstalled ────────────────────────────────────────────────────────────

func TestListInstalled_EmptyReturnsNil(t *testing.T) {
	c := newTempCache(t)
	recs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("expected empty, got %v", recs)
	}
}

func TestListInstalled_ReturnsRecords(t *testing.T) {
	c := newTempCache(t)
	expected := []InstalledRecord{
		{Name: "my-skill", Version: "1.0.0", InstalledAt: time.Now().Truncate(time.Second), Dir: "/some/dir"},
	}
	data, _ := json.MarshalIndent(expected, "", "  ")
	if err := os.WriteFile(c.installedPath(), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	recs, err := c.ListInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 || recs[0].Name != "my-skill" {
		t.Fatalf("unexpected records: %v", recs)
	}
}

func TestListInstalled_CorruptFileReturnsError(t *testing.T) {
	c := newTempCache(t)
	if err := os.WriteFile(c.installedPath(), []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := c.ListInstalled()
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
}

// ─── IsInstalled ─────────────────────────────────────────────────────────────

func TestIsInstalled_NotInstalled(t *testing.T) {
	c := newTempCache(t)
	ok, err := c.IsInstalled("missing", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected false for missing skill")
	}
}

func TestIsInstalled_Found_AnyVersion(t *testing.T) {
	c := newTempCache(t)
	_ = c.saveRecord(InstalledRecord{Name: "skill-a", Version: "2.0"})
	ok, err := c.IsInstalled("skill-a", "")
	if err != nil || !ok {
		t.Fatalf("expected true, got ok=%v err=%v", ok, err)
	}
}

func TestIsInstalled_VersionMatch(t *testing.T) {
	c := newTempCache(t)
	_ = c.saveRecord(InstalledRecord{Name: "skill-b", Version: "1.0"})
	ok, _ := c.IsInstalled("skill-b", "1.0")
	if !ok {
		t.Fatal("version 1.0 should be found")
	}
	ok, _ = c.IsInstalled("skill-b", "2.0")
	if ok {
		t.Fatal("version 2.0 should not be found")
	}
}

// ─── Remove ──────────────────────────────────────────────────────────────────

func TestRemove_ExistingSkill(t *testing.T) {
	c := newTempCache(t)
	dir := t.TempDir()
	_ = c.saveRecord(InstalledRecord{Name: "rm-skill", Version: "1.0", Dir: dir})
	if err := c.Remove("rm-skill", ""); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	ok, _ := c.IsInstalled("rm-skill", "")
	if ok {
		t.Fatal("skill should be removed")
	}
}

func TestRemove_NotInstalled_ReturnsError(t *testing.T) {
	c := newTempCache(t)
	if err := c.Remove("nonexistent", ""); err == nil {
		t.Fatal("expected error removing nonexistent skill")
	}
}

func TestRemove_ByVersion_LeavesOthers(t *testing.T) {
	c := newTempCache(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	_ = c.saveRecord(InstalledRecord{Name: "multi", Version: "1.0", Dir: dir1})
	_ = c.saveRecord(InstalledRecord{Name: "multi", Version: "2.0", Dir: dir2})
	if err := c.Remove("multi", "1.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	ok, _ := c.IsInstalled("multi", "2.0")
	if !ok {
		t.Fatal("version 2.0 should still be installed")
	}
}

// ─── LoadMetadata ─────────────────────────────────────────────────────────────

func TestLoadMetadata_Valid(t *testing.T) {
	c := newTempCache(t)
	dir := t.TempDir()
	meta := capability.Metadata{Name: "my-skill", Description: "does stuff"}
	data, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "skill.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := c.LoadMetadata(InstalledRecord{Dir: dir})
	if err != nil {
		t.Fatalf("LoadMetadata: %v", err)
	}
	if got.Name != "my-skill" {
		t.Fatalf("name = %q, want my-skill", got.Name)
	}
}

func TestLoadMetadata_MissingFile(t *testing.T) {
	c := newTempCache(t)
	_, err := c.LoadMetadata(InstalledRecord{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error for missing skill.json")
	}
}

// ─── extractZip ──────────────────────────────────────────────────────────────

func TestExtractZip_BasicFiles(t *testing.T) {
	// extractZip strips the first path component (GitHub-style archive prefix).
	// Use "prefix/" as the top-level dir; after stripping: flat files are preserved.
	zipData := makeZip(t, map[string]string{
		"prefix/file.txt":       "hello",
		"prefix/sub/nested.txt": "world",
	})
	dest := t.TempDir()
	if err := extractZip(zipData, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "file.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("file.txt: err=%v content=%q", err, data)
	}
	data, err = os.ReadFile(filepath.Join(dest, "sub", "nested.txt"))
	if err != nil || string(data) != "world" {
		t.Fatalf("sub/nested.txt: err=%v content=%q", err, data)
	}
}

func TestExtractZip_StripLeadingDir(t *testing.T) {
	// GitHub-style archives have a top-level dir: "repo-main/file.txt"
	zipData := makeZip(t, map[string]string{
		"repo-main/README.md": "# README",
		"repo-main/skill.json": `{"name":"x"}`,
	})
	dest := t.TempDir()
	if err := extractZip(zipData, dest); err != nil {
		t.Fatalf("extractZip: %v", err)
	}
	// Leading component stripped: file should be at dest/README.md
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("README.md should exist after strip: %v", err)
	}
}

func TestExtractZip_InvalidData(t *testing.T) {
	if err := extractZip([]byte("not a zip"), t.TempDir()); err == nil {
		t.Fatal("expected error for invalid zip data")
	}
}

// ─── Install (with fake HTTP server) ─────────────────────────────────────────

func TestInstall_Success(t *testing.T) {
	zipData := makeZip(t, map[string]string{
		"skill.json": `{"name":"hello","version":"1.0"}`,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipData)
	}))
	defer srv.Close()

	c := newTempCache(t)
	entry := RegistryEntry{
		Metadata:   capability.Metadata{Name: "hello", Version: "1.0"},
		ArchiveURL: srv.URL + "/hello.zip",
	}
	rec, err := c.Install(context.Background(), entry)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if rec.Name != "hello" {
		t.Fatalf("Name = %q, want hello", rec.Name)
	}
	// Check persisted
	ok, _ := c.IsInstalled("hello", "1.0")
	if !ok {
		t.Fatal("skill should be persisted in installed.json after Install")
	}
}

func TestInstall_NoArchiveURL(t *testing.T) {
	c := newTempCache(t)
	_, err := c.Install(context.Background(), RegistryEntry{})
	if err == nil {
		t.Fatal("expected error for empty ArchiveURL")
	}
}

func TestInstall_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := newTempCache(t)
	_, err := c.Install(context.Background(), RegistryEntry{
		Metadata:   capability.Metadata{Name: "x", Version: "1"},
		ArchiveURL: srv.URL + "/x.zip",
	})
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

// ─── HTTPRegistryClient ───────────────────────────────────────────────────────

func serveJSON(t *testing.T, v any) *httptest.Server {
	t.Helper()
	data, _ := json.Marshal(v)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
}

func TestHTTPRegistryClient_List(t *testing.T) {
	entries := []RegistryEntry{{Metadata: capability.Metadata{Name: "skill-x", Version: "1.0"}}}
	srv := serveJSON(t, entries)
	defer srv.Close()

	client := NewHTTPRegistryClient(srv.URL)
	got, err := client.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "skill-x" {
		t.Fatalf("unexpected entries: %v", got)
	}
}

func TestHTTPRegistryClient_Search(t *testing.T) {
	entries := []RegistryEntry{{Metadata: capability.Metadata{Name: "found", Version: "1.0"}}}
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		data, _ := json.Marshal(entries)
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	}))
	defer srv.Close()

	client := NewHTTPRegistryClient(srv.URL)
	got, err := client.Search(context.Background(), SearchOptions{Query: "test", Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if capturedURL == "" {
		t.Fatal("URL not captured")
	}
}

func TestHTTPRegistryClient_Get(t *testing.T) {
	entry := RegistryEntry{Metadata: capability.Metadata{Name: "my-skill", Version: "2.0"}}
	srv := serveJSON(t, entry)
	defer srv.Close()

	client := NewHTTPRegistryClient(srv.URL)
	got, err := client.Get(context.Background(), "my-skill", "2.0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "my-skill" {
		t.Fatalf("Name = %q, want my-skill", got.Name)
	}
}

func TestHTTPRegistryClient_Get_NoVersion(t *testing.T) {
	entry := RegistryEntry{Metadata: capability.Metadata{Name: "latest-skill", Version: "3.0"}}
	srv := serveJSON(t, entry)
	defer srv.Close()

	client := NewHTTPRegistryClient(srv.URL)
	got, err := client.Get(context.Background(), "latest-skill", "")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "latest-skill" {
		t.Fatalf("Name = %q, want latest-skill", got.Name)
	}
}

func TestHTTPRegistryClient_Get_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := NewHTTPRegistryClient(srv.URL)
	_, err := client.Get(context.Background(), "missing", "1.0")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestHTTPRegistryClient_List_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()
	client := NewHTTPRegistryClient(srv.URL)
	_, err := client.List(context.Background())
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestHTTPRegistryClient_Get_500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := NewHTTPRegistryClient(srv.URL)
	_, err := client.Get(context.Background(), "x", "1.0")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
