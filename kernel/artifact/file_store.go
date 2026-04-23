package artifact

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/ids"
)

// FileStore is a filesystem-backed Store implementation.
type FileStore struct {
	mu  sync.RWMutex
	dir string
}

// NewFileStore creates a new filesystem-backed artifact store rooted at dir.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create artifact store dir: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

var _ Store = (*FileStore)(nil)

func (s *FileStore) Save(_ context.Context, sessionID string, a *Artifact) error {
	if a == nil {
		return fmt.Errorf("artifact is nil")
	}
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("artifact name is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.artifactDir(sessionID, a.Name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create artifact dir: %w", err)
	}

	nextVersion, err := s.nextVersionLocked(dir)
	if err != nil {
		return err
	}
	saved := &Artifact{
		ID:        ids.New(),
		Name:      a.Name,
		MIMEType:  a.MIMEType,
		Data:      append([]byte(nil), a.Data...),
		Version:   nextVersion,
		Metadata:  cloneMetadata(a.Metadata),
		CreatedAt: time.Now().UTC(),
	}
	if err := s.writeArtifactLocked(s.versionPath(sessionID, a.Name, saved.Version), saved); err != nil {
		return err
	}

	a.ID = saved.ID
	a.Version = saved.Version
	a.CreatedAt = saved.CreatedAt
	return nil
}

func (s *FileStore) Load(_ context.Context, sessionID, name string, version int) (*Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if version <= 0 {
		latest, err := s.latestVersionLocked(sessionID, name)
		if err != nil {
			return nil, err
		}
		version = latest
	}
	return s.readArtifactLocked(s.versionPath(sessionID, name, version))
}

func (s *FileStore) List(_ context.Context, sessionID string) ([]*Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessionDir := s.sessionDir(sessionID)
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list session artifacts: %w", err)
	}
	out := make([]*Artifact, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name, err := decodePathComponent(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("decode artifact name: %w", err)
		}
		latest, err := s.latestVersionLocked(sessionID, name)
		if err != nil {
			return nil, err
		}
		item, err := s.readArtifactLocked(s.versionPath(sessionID, name, latest))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].Version < out[j].Version
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *FileStore) Versions(_ context.Context, sessionID, name string) ([]*Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions, err := s.versionNumbersLocked(sessionID, name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]*Artifact, 0, len(versions))
	for _, version := range versions {
		item, err := s.readArtifactLocked(s.versionPath(sessionID, name, version))
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *FileStore) Delete(_ context.Context, sessionID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.RemoveAll(s.artifactDir(sessionID, name)); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

func (s *FileStore) nextVersionLocked(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("list artifact versions: %w", err)
	}
	maxVersion := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, ok := artifactVersionFromFile(entry.Name())
		if ok && version > maxVersion {
			maxVersion = version
		}
	}
	return maxVersion + 1, nil
}

func (s *FileStore) latestVersionLocked(sessionID, name string) (int, error) {
	versions, err := s.versionNumbersLocked(sessionID, name)
	if err != nil {
		return 0, err
	}
	if len(versions) == 0 {
		return 0, fmt.Errorf("artifact %q not found in session %q", name, sessionID)
	}
	return versions[len(versions)-1], nil
}

func (s *FileStore) versionNumbersLocked(sessionID, name string) ([]int, error) {
	dir := s.artifactDir(sessionID, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: artifact %q not found in session %q", os.ErrNotExist, name, sessionID)
		}
		return nil, fmt.Errorf("list artifact versions: %w", err)
	}
	out := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, ok := artifactVersionFromFile(entry.Name())
		if !ok {
			continue
		}
		out = append(out, version)
	}
	sort.Ints(out)
	return out, nil
}

func (s *FileStore) writeArtifactLocked(path string, a *Artifact) error {
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("marshal artifact: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write artifact: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename artifact: %w", err)
	}
	return nil
}

func (s *FileStore) readArtifactLocked(path string) (*Artifact, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			base := filepath.Base(path)
			if version, ok := artifactVersionFromFile(base); ok {
				dir := filepath.Base(filepath.Dir(path))
				name, _ := decodePathComponent(dir)
				return nil, fmt.Errorf("%w: artifact %q version %d not found", os.ErrNotExist, name, version)
			}
		}
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	var stored Artifact
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("unmarshal artifact: %w", err)
	}
	return cloneArtifact(&stored), nil
}

func (s *FileStore) sessionDir(sessionID string) string {
	return filepath.Join(s.dir, encodePathComponent(sessionID))
}

func (s *FileStore) artifactDir(sessionID, name string) string {
	return filepath.Join(s.sessionDir(sessionID), encodePathComponent(name))
}

func (s *FileStore) versionPath(sessionID, name string, version int) string {
	return filepath.Join(s.artifactDir(sessionID, name), fmt.Sprintf("%06d.json", version))
}

func artifactVersionFromFile(name string) (int, bool) {
	if !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	version, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
	if err != nil || version <= 0 {
		return 0, false
	}
	return version, true
}

func encodePathComponent(value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func decodePathComponent(value string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
