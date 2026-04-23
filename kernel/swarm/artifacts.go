package swarm

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/artifact"
)

const (
	ArtifactMetadataSwarmRunID = "swarm_run_id"
	ArtifactMetadataThreadID   = "thread_id"
	ArtifactMetadataTaskID     = "task_id"
	ArtifactMetadataKind       = "swarm_artifact_kind"
	ArtifactMetadataSummary    = "swarm_artifact_summary"
)

// StampArtifact annotates an artifact with swarm metadata so the artifact store
// can act as a durable fact carrier for artifact refs.
func StampArtifact(a *artifact.Artifact, ref ArtifactRef) {
	if a == nil {
		return
	}
	if a.Metadata == nil {
		a.Metadata = make(map[string]any)
	}
	if ref.RunID != "" {
		a.Metadata[ArtifactMetadataSwarmRunID] = ref.RunID
	}
	if ref.ThreadID != "" {
		a.Metadata[ArtifactMetadataThreadID] = ref.ThreadID
	}
	if ref.TaskID != "" {
		a.Metadata[ArtifactMetadataTaskID] = ref.TaskID
	}
	if ref.Kind != "" {
		a.Metadata[ArtifactMetadataKind] = string(ref.Kind)
	}
	if ref.Summary != "" {
		a.Metadata[ArtifactMetadataSummary] = ref.Summary
	}
	if ref.Name != "" {
		a.Name = ref.Name
	}
	if ref.MIMEType != "" {
		a.MIMEType = ref.MIMEType
	}
	if ref.Version > 0 {
		a.Version = ref.Version
	}
}

// ArtifactRefFromArtifact projects a stored artifact back into the swarm domain.
func ArtifactRefFromArtifact(sessionID string, a *artifact.Artifact) (ArtifactRef, error) {
	if a == nil {
		return ArtifactRef{}, fmt.Errorf("artifact must not be nil")
	}
	ref := ArtifactRef{
		ID:        strings.TrimSpace(a.ID),
		SessionID: strings.TrimSpace(sessionID),
		Name:      strings.TrimSpace(a.Name),
		Version:   a.Version,
		MIMEType:  strings.TrimSpace(a.MIMEType),
		CreatedAt: a.CreatedAt,
	}
	if a.Metadata != nil {
		ref.RunID = metadataString(a.Metadata, ArtifactMetadataSwarmRunID)
		ref.ThreadID = metadataString(a.Metadata, ArtifactMetadataThreadID)
		ref.TaskID = metadataString(a.Metadata, ArtifactMetadataTaskID)
		ref.Kind = ArtifactKind(metadataString(a.Metadata, ArtifactMetadataKind))
		ref.Summary = metadataString(a.Metadata, ArtifactMetadataSummary)
		ref.Metadata = cloneMap(a.Metadata)
		delete(ref.Metadata, ArtifactMetadataSwarmRunID)
		delete(ref.Metadata, ArtifactMetadataThreadID)
		delete(ref.Metadata, ArtifactMetadataTaskID)
		delete(ref.Metadata, ArtifactMetadataKind)
		delete(ref.Metadata, ArtifactMetadataSummary)
		if len(ref.Metadata) == 0 {
			ref.Metadata = nil
		}
	}
	return ref, nil
}

func metadataString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	if raw, ok := meta[key]; ok {
		if value, ok := raw.(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
