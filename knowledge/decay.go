package knowledge

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PruneConfig controls which episodes are removed during a Prune call.
// Zero values for each field disable that criterion.
type PruneConfig struct {
	Now           time.Time     // reference time; zero = time.Now()
	MaxAge        time.Duration // remove episodes older than this (0 = no limit)
	MinImportance float64       // remove episodes below this importance (0 = no limit)
	MaxCount      int           // keep only the most recent N episodes (0 = no limit)
}

func (c PruneConfig) now() time.Time {
	if c.Now.IsZero() {
		return time.Now()
	}
	return c.Now
}

// Prunable extends EpisodicStore with maintenance operations.
type Prunable interface {
	EpisodicStore
	// Prune removes episodes matching the prune criteria and returns the count removed.
	Prune(ctx context.Context, cfg PruneConfig) (int, error)
	// DecayImportance applies exponential decay to all episode importances.
	DecayImportance(ctx context.Context, halfLife time.Duration) error
}

// DecayImportanceValue computes I(t) = I₀ × 0.5^(elapsed/halfLife).
// Returns the original importance if halfLife <= 0.
func DecayImportanceValue(importance float64, elapsed, halfLife time.Duration) float64 {
	if halfLife <= 0 {
		return importance
	}
	ratio := float64(elapsed) / float64(halfLife)
	return importance * math.Pow(0.5, ratio)
}

// applyPruneConfig filters a slice by PruneConfig and returns (kept, removedCount).
func applyPruneConfig(episodes []Episode, cfg PruneConfig) ([]Episode, int) {
	now := cfg.now()
	var kept []Episode

	for _, ep := range episodes {
		if cfg.MaxAge > 0 && now.Sub(ep.Timestamp) > cfg.MaxAge {
			continue
		}
		if cfg.MinImportance > 0 && ep.Importance < cfg.MinImportance {
			continue
		}
		kept = append(kept, ep)
	}

	if cfg.MaxCount > 0 && len(kept) > cfg.MaxCount {
		sort.Slice(kept, func(i, j int) bool {
			return kept[i].Timestamp.After(kept[j].Timestamp)
		})
		kept = kept[:cfg.MaxCount]
	}

	return kept, len(episodes) - len(kept)
}

// ---- MemoryEpisodicStore Prunable implementation -------------------------

// Prune removes matching episodes from the in-memory store.
func (s *MemoryEpisodicStore) Prune(_ context.Context, cfg PruneConfig) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept, removed := applyPruneConfig(s.episodes, cfg)
	s.episodes = kept
	return removed, nil
}

// DecayImportance applies exponential decay to all in-memory episode importances.
func (s *MemoryEpisodicStore) DecayImportance(_ context.Context, halfLife time.Duration) error {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.episodes {
		elapsed := now.Sub(s.episodes[i].Timestamp)
		s.episodes[i].Importance = DecayImportanceValue(s.episodes[i].Importance, elapsed, halfLife)
	}
	return nil
}

// ---- FileEpisodicStore Prunable implementation ---------------------------

// Prune removes matching episodes (load → filter → atomic replace).
func (s *FileEpisodicStore) Prune(_ context.Context, cfg PruneConfig) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readLocked()
	if err != nil {
		return 0, err
	}
	kept, removed := applyPruneConfig(all, cfg)
	if removed == 0 {
		return 0, nil
	}
	return removed, s.writeAllLocked(kept)
}

// DecayImportance updates importance for all episodes in the JSONL file.
func (s *FileEpisodicStore) DecayImportance(_ context.Context, halfLife time.Duration) error {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readLocked()
	if err != nil {
		return err
	}
	for i := range all {
		elapsed := now.Sub(all[i].Timestamp)
		all[i].Importance = DecayImportanceValue(all[i].Importance, elapsed, halfLife)
	}
	return s.writeAllLocked(all)
}

// writeAllLocked atomically replaces the JSONL file with the given episodes.
// Caller must hold s.mu.
func (s *FileEpisodicStore) writeAllLocked(episodes []Episode) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "episodic-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	enc := json.NewEncoder(tmp)
	for _, ep := range episodes {
		if err := enc.Encode(ep); err != nil {
			tmp.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, s.path)
}
