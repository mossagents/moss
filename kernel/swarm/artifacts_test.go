package swarm

import (
	"testing"
	"time"

	"github.com/mossagents/moss/kernel/artifact"
)

func TestStampArtifactAndArtifactRefFromArtifact(t *testing.T) {
	createdAt := time.Now().UTC()
	src := &artifact.Artifact{
		ID:        "artifact-1",
		Name:      "draft",
		MIMEType:  "text/plain",
		Version:   2,
		Metadata:  map[string]any{"custom": "value"},
		CreatedAt: createdAt,
	}
	ref := ArtifactRef{
		RunID:    "swarm-1",
		ThreadID: "thread-1",
		TaskID:   "task-1",
		Name:     "research-draft",
		Kind:     ArtifactSynthesisDraft,
		Version:  3,
		MIMEType: "text/markdown",
		Summary:  "latest synthesis draft",
	}

	StampArtifact(src, ref)
	got, err := ArtifactRefFromArtifact("sess-1", src)
	if err != nil {
		t.Fatalf("ArtifactRefFromArtifact: %v", err)
	}
	if got.RunID != "swarm-1" || got.ThreadID != "thread-1" || got.TaskID != "task-1" {
		t.Fatalf("unexpected swarm ids: %+v", got)
	}
	if got.Name != "research-draft" || got.Kind != ArtifactSynthesisDraft || got.Version != 3 || got.MIMEType != "text/markdown" {
		t.Fatalf("unexpected artifact projection: %+v", got)
	}
	if got.Summary != "latest synthesis draft" {
		t.Fatalf("unexpected summary: %+v", got)
	}
	if got.Metadata["custom"] != "value" {
		t.Fatalf("expected custom metadata to survive, got %+v", got.Metadata)
	}
	if _, ok := got.Metadata[ArtifactMetadataSwarmRunID]; ok {
		t.Fatalf("swarm metadata should be stripped from ref metadata: %+v", got.Metadata)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("created_at mismatch: got %v want %v", got.CreatedAt, createdAt)
	}
}
