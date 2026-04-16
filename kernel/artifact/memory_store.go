package artifact

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mossagents/moss/kernel/ids"
)

// MemoryStore is an in-memory Store implementation for testing and lightweight use.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]map[string][]*Artifact // sessionID → name → versions
}

// NewMemoryStore creates a new in-memory artifact store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[string]map[string][]*Artifact),
	}
}

var _ Store = (*MemoryStore)(nil)

func (s *MemoryStore) Save(_ context.Context, sessionID string, a *Artifact) error {
	if a == nil {
		return fmt.Errorf("artifact is nil")
	}
	if a.Name == "" {
		return fmt.Errorf("artifact name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[sessionID] == nil {
		s.data[sessionID] = make(map[string][]*Artifact)
	}
	versions := s.data[sessionID][a.Name]

	saved := &Artifact{
		ID:        ids.New(),
		Name:      a.Name,
		MIMEType:  a.MIMEType,
		Data:      append([]byte(nil), a.Data...),
		Version:   len(versions) + 1,
		Metadata:  cloneMetadata(a.Metadata),
		CreatedAt: time.Now().UTC(),
	}

	s.data[sessionID][a.Name] = append(versions, saved)
	a.ID = saved.ID
	a.Version = saved.Version
	a.CreatedAt = saved.CreatedAt
	return nil
}

func (s *MemoryStore) Load(_ context.Context, sessionID, name string, version int) (*Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions := s.data[sessionID][name]
	if len(versions) == 0 {
		return nil, fmt.Errorf("artifact %q not found in session %q", name, sessionID)
	}
	if version <= 0 {
		return cloneArtifact(versions[len(versions)-1]), nil
	}
	if version > len(versions) {
		return nil, fmt.Errorf("artifact %q version %d not found (latest: %d)", name, version, len(versions))
	}
	return cloneArtifact(versions[version-1]), nil
}

func (s *MemoryStore) List(_ context.Context, sessionID string) ([]*Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := s.data[sessionID]
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]*Artifact, 0, len(names))
	for _, versions := range names {
		if len(versions) > 0 {
			out = append(out, cloneArtifact(versions[len(versions)-1]))
		}
	}
	return out, nil
}

func (s *MemoryStore) Versions(_ context.Context, sessionID, name string) ([]*Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	versions := s.data[sessionID][name]
	if len(versions) == 0 {
		return nil, nil
	}
	out := make([]*Artifact, len(versions))
	for i, v := range versions {
		out[i] = cloneArtifact(v)
	}
	return out, nil
}

func (s *MemoryStore) Delete(_ context.Context, sessionID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[sessionID] != nil {
		delete(s.data[sessionID], name)
	}
	return nil
}

func cloneArtifact(a *Artifact) *Artifact {
	if a == nil {
		return nil
	}
	return &Artifact{
		ID:        a.ID,
		Name:      a.Name,
		MIMEType:  a.MIMEType,
		Data:      append([]byte(nil), a.Data...),
		Version:   a.Version,
		Metadata:  cloneMetadata(a.Metadata),
		CreatedAt: a.CreatedAt,
	}
}

func cloneMetadata(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
