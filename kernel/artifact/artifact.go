// Package artifact defines a lightweight Artifact management interface for
// storing and versioning binary/text artifacts produced during agent sessions.
package artifact

import (
	"context"
	"time"
)

// Artifact represents a named, versioned piece of content produced or consumed
// by an agent during a session.
type Artifact struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	MIMEType  string         `json:"mime_type,omitempty"`
	Data      []byte         `json:"data,omitempty"`
	Version   int            `json:"version"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at,omitempty"`
}

// Store provides CRUD operations for session-scoped artifacts.
// Implementations may store artifacts in memory, on disk, or in a remote backend.
type Store interface {
	// Save persists an artifact for a session. If an artifact with the same
	// name already exists, a new version is created.
	Save(ctx context.Context, sessionID string, a *Artifact) error

	// Load retrieves a specific version of a named artifact. Version 0 loads
	// the latest version.
	Load(ctx context.Context, sessionID, name string, version int) (*Artifact, error)

	// List returns all latest-version artifacts for a session.
	List(ctx context.Context, sessionID string) ([]*Artifact, error)

	// Versions returns all versions of a named artifact, ordered by version ascending.
	Versions(ctx context.Context, sessionID, name string) ([]*Artifact, error)

	// Delete removes all versions of a named artifact.
	Delete(ctx context.Context, sessionID, name string) error
}
