package domain

import "time"

type ArtifactKind string

const (
	ArtifactKindFileRef       ArtifactKind = "file_ref"
	ArtifactKindFileWrite     ArtifactKind = "file_write"
	ArtifactKindPatch         ArtifactKind = "patch"
	ArtifactKindCommandOutput ArtifactKind = "command_output"
	ArtifactKindFinalAnswer   ArtifactKind = "final_answer"
)

type Artifact struct {
	ArtifactID string
	RunID      string
	TaskID     string
	Kind       ArtifactKind
	Path       string
	Content    string
	Summary    string
	CreatedAt  time.Time
}
