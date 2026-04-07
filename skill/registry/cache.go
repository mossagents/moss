package registry

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/skill"
)

// InstalledRecord tracks a skill that has been installed into the local cache.
type InstalledRecord struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
	Dir         string    `json:"dir"` // absolute path to extracted skill dir
}

// LocalCache manages skills installed to ~/.moss/skills/.
type LocalCache struct {
	root string // e.g. ~/.moss/skills
}

// NewLocalCache creates (or opens) the cache rooted at dir.
// If dir is empty, it defaults to ~/.moss/skills.
func NewLocalCache(dir string) (*LocalCache, error) {
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("registry: cannot find home dir: %w", err)
		}
		dir = filepath.Join(home, ".moss", "skills")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("registry: cannot create cache dir %q: %w", dir, err)
	}
	return &LocalCache{root: dir}, nil
}

// installedPath returns the path to installed.json.
func (c *LocalCache) installedPath() string {
	return filepath.Join(c.root, "installed.json")
}

// ListInstalled returns all currently installed skills.
func (c *LocalCache) ListInstalled() ([]InstalledRecord, error) {
	data, err := os.ReadFile(c.installedPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("registry: read installed.json: %w", err)
	}
	var recs []InstalledRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("registry: parse installed.json: %w", err)
	}
	return recs, nil
}

// IsInstalled returns whether a skill with the given name is installed
// (optionally at a specific version; empty version = any version).
func (c *LocalCache) IsInstalled(name, version string) (bool, error) {
	recs, err := c.ListInstalled()
	if err != nil {
		return false, err
	}
	for _, r := range recs {
		if r.Name == name && (version == "" || r.Version == version) {
			return true, nil
		}
	}
	return false, nil
}

// Install downloads and unpacks the skill from entry.ArchiveURL
// and records the installation in installed.json.
func (c *LocalCache) Install(ctx context.Context, entry RegistryEntry) (*InstalledRecord, error) {
	if entry.ArchiveURL == "" {
		return nil, fmt.Errorf("registry: entry %q has no archive_url", entry.Name)
	}

	// Fetch archive
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.ArchiveURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry: download %q: %w", entry.ArchiveURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("registry: download status %d", resp.StatusCode)
	}
	archiveData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("registry: read archive: %w", err)
	}

	// Extract zip to <root>/<name>@<version>/
	destDir := filepath.Join(c.root, fmt.Sprintf("%s@%s", entry.Name, entry.Version))
	if err := extractZip(archiveData, destDir); err != nil {
		return nil, fmt.Errorf("registry: extract: %w", err)
	}

	rec := InstalledRecord{
		Name:        entry.Name,
		Version:     entry.Version,
		InstalledAt: time.Now(),
		Dir:         destDir,
	}
	if err := c.saveRecord(rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Remove uninstalls a skill by name (and optionally version).
// If version is empty, removes all versions.
func (c *LocalCache) Remove(name, version string) error {
	recs, err := c.ListInstalled()
	if err != nil {
		return err
	}

	remaining := recs[:0]
	removed := false
	for _, r := range recs {
		if r.Name == name && (version == "" || r.Version == version) {
			if err := os.RemoveAll(r.Dir); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("registry: remove dir %q: %w", r.Dir, err)
			}
			removed = true
		} else {
			remaining = append(remaining, r)
		}
	}
	if !removed {
		return fmt.Errorf("registry: skill %q not installed", name)
	}
	return c.writeInstalled(remaining)
}

// LoadMetadata reads skill.Metadata from an installed skill's directory.
// It looks for a skill.json file at the root of the installed directory.
func (c *LocalCache) LoadMetadata(rec InstalledRecord) (*skill.Metadata, error) {
	path := filepath.Join(rec.Dir, "skill.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("registry: read skill.json from %q: %w", rec.Dir, err)
	}
	var meta skill.Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("registry: parse skill.json: %w", err)
	}
	return &meta, nil
}

// saveRecord adds or updates a record in installed.json.
func (c *LocalCache) saveRecord(rec InstalledRecord) error {
	recs, err := c.ListInstalled()
	if err != nil {
		return err
	}
	updated := false
	for i, r := range recs {
		if r.Name == rec.Name && r.Version == rec.Version {
			recs[i] = rec
			updated = true
			break
		}
	}
	if !updated {
		recs = append(recs, rec)
	}
	return c.writeInstalled(recs)
}

func (c *LocalCache) writeInstalled(recs []InstalledRecord) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("registry: marshal installed.json: %w", err)
	}
	tmp := c.installedPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("registry: write installed.json: %w", err)
	}
	return os.Rename(tmp, c.installedPath())
}

// extractZip extracts a zip archive (in-memory) to destDir.
func extractZip(data []byte, destDir string) error {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		// strip leading directory component if present (common in GitHub archives)
		name := f.Name
		if idx := strings.Index(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		if name == "" {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(name))
		// security: prevent path traversal
		if !strings.HasPrefix(target, destDir+string(os.PathSeparator)) && target != destDir {
			return fmt.Errorf("invalid path in archive: %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeZipEntry(f, target); err != nil {
			return err
		}
	}
	return nil
}

func writeZipEntry(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
