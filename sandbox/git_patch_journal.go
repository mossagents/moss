package sandbox

import (
	"encoding/json"
	"fmt"
	"github.com/mossagents/moss/kernel/workspace"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type patchJournalEntry struct {
	Patch       string         `json:"patch"`
	TargetFiles []string       `json:"target_files,omitempty"`
	ThreeWay    bool           `json:"three_way,omitempty"`
	Cached      bool           `json:"cached,omitempty"`
	Source      workspace.PatchSource `json:"source,omitempty"`
	AppliedAt   time.Time      `json:"applied_at"`
}

type gitPatchJournal struct {
	mu   sync.Mutex
	path string
}

func newGitPatchJournal(gitDir string) *gitPatchJournal {
	return &gitPatchJournal{
		path: filepath.Join(gitDir, "moss-patches.json"),
	}
}

func (j *gitPatchJournal) Save(patchID string, entry patchJournalEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadLocked()
	if err != nil {
		return err
	}
	state[patchID] = entry
	return j.persistLocked(state)
}

func (j *gitPatchJournal) Load(patchID string) (*patchJournalEntry, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadLocked()
	if err != nil {
		return nil, err
	}
	entry, ok := state[patchID]
	if !ok {
		return nil, workspace.ErrPatchNotFound
	}
	cp := entry
	cp.TargetFiles = append([]string(nil), entry.TargetFiles...)
	return &cp, nil
}

func (j *gitPatchJournal) Delete(patchID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadLocked()
	if err != nil {
		return err
	}
	delete(state, patchID)
	return j.persistLocked(state)
}

func (j *gitPatchJournal) List() ([]workspace.PatchSnapshotRef, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	state, err := j.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]workspace.PatchSnapshotRef, 0, len(state))
	for patchID, entry := range state {
		out = append(out, workspace.PatchSnapshotRef{
			PatchID:     patchID,
			TargetFiles: append([]string(nil), entry.TargetFiles...),
			Source:      entry.Source,
			AppliedAt:   entry.AppliedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AppliedAt.Equal(out[j].AppliedAt) {
			return out[i].PatchID < out[j].PatchID
		}
		return out[i].AppliedAt.Before(out[j].AppliedAt)
	})
	return out, nil
}

func (j *gitPatchJournal) loadLocked() (map[string]patchJournalEntry, error) {
	data, err := os.ReadFile(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]patchJournalEntry{}, nil
		}
		return nil, fmt.Errorf("read patch journal: %w", err)
	}
	var state map[string]patchJournalEntry
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal patch journal: %w", err)
	}
	if state == nil {
		state = map[string]patchJournalEntry{}
	}
	return state, nil
}

func (j *gitPatchJournal) persistLocked(state map[string]patchJournalEntry) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal patch journal: %w", err)
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write patch journal tmp: %w", err)
	}
	if err := os.Rename(tmp, j.path); err != nil {
		return fmt.Errorf("replace patch journal: %w", err)
	}
	return nil
}
