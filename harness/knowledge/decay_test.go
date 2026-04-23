package knowledge_test

import (
	"context"
	"github.com/mossagents/moss/harness/knowledge"
	"testing"
	"time"
)

func TestDecayImportanceValue(t *testing.T) {
	// After one half-life, importance should be halved
	orig := 1.0
	halfLife := time.Hour
	elapsed := time.Hour
	got := knowledge.DecayImportanceValue(orig, elapsed, halfLife)
	if got < 0.49 || got > 0.51 {
		t.Errorf("expected ~0.5 after one half-life, got %f", got)
	}

	// Zero halfLife returns original
	got = knowledge.DecayImportanceValue(orig, elapsed, 0)
	if got != orig {
		t.Errorf("expected original importance with zero halfLife, got %f", got)
	}
}

func TestMemoryEpisodicStore_Prune(t *testing.T) {
	ctx := context.Background()
	store := knowledge.NewMemoryEpisodicStore()

	now := time.Now()
	eps := []knowledge.Episode{
		{ID: "1", Timestamp: now.Add(-3 * time.Hour), Importance: 0.1},
		{ID: "2", Timestamp: now.Add(-1 * time.Hour), Importance: 0.9},
		{ID: "3", Timestamp: now.Add(-30 * time.Minute), Importance: 0.5},
	}
	for _, ep := range eps {
		_ = store.Append(ctx, ep)
	}

	// Prune by min importance
	removed, err := store.Prune(ctx, knowledge.PruneConfig{MinImportance: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed (importance 0.1), got %d", removed)
	}

	remaining, _ := store.Recent(ctx, "", 10)
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(remaining))
	}
}

func TestMemoryEpisodicStore_PruneMaxAge(t *testing.T) {
	ctx := context.Background()
	store := knowledge.NewMemoryEpisodicStore()

	now := time.Now()
	_ = store.Append(ctx, knowledge.Episode{ID: "old", Timestamp: now.Add(-5 * time.Hour)})
	_ = store.Append(ctx, knowledge.Episode{ID: "new", Timestamp: now.Add(-1 * time.Minute)})

	removed, err := store.Prune(ctx, knowledge.PruneConfig{MaxAge: 2 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
}

func TestMemoryEpisodicStore_PruneMaxCount(t *testing.T) {
	ctx := context.Background()
	store := knowledge.NewMemoryEpisodicStore()

	now := time.Now()
	for i := 0; i < 5; i++ {
		_ = store.Append(ctx, knowledge.Episode{
			ID:        string(rune('a' + i)),
			Timestamp: now.Add(time.Duration(i) * time.Minute),
		})
	}

	removed, err := store.Prune(ctx, knowledge.PruneConfig{MaxCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	remaining, _ := store.Recent(ctx, "", 10)
	if len(remaining) != 3 {
		t.Errorf("expected 3 remaining, got %d", len(remaining))
	}
}

func TestMemoryEpisodicStore_DecayImportance(t *testing.T) {
	ctx := context.Background()
	store := knowledge.NewMemoryEpisodicStore()

	past := time.Now().Add(-2 * time.Hour) // 2 half-lives ago
	_ = store.Append(ctx, knowledge.Episode{ID: "e1", Timestamp: past, Importance: 1.0})

	err := store.DecayImportance(ctx, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	recent, _ := store.Recent(ctx, "", 1)
	if len(recent) != 1 {
		t.Fatal("expected 1 episode")
	}
	// After 2 half-lives: I = 1.0 * 0.5^2 = 0.25
	if recent[0].Importance > 0.27 || recent[0].Importance < 0.23 {
		t.Errorf("expected ~0.25 after decay, got %f", recent[0].Importance)
	}
}

func TestEpisodeExpiresAtFilter(t *testing.T) {
	ctx := context.Background()
	store := knowledge.NewMemoryEpisodicStore()

	past := time.Now().Add(-1 * time.Hour)
	future := time.Now().Add(1 * time.Hour)

	_ = store.Append(ctx, knowledge.Episode{ID: "expired", Timestamp: time.Now(), ExpiresAt: past})
	_ = store.Append(ctx, knowledge.Episode{ID: "valid", Timestamp: time.Now(), ExpiresAt: future})
	_ = store.Append(ctx, knowledge.Episode{ID: "nolimit", Timestamp: time.Now()})

	results, err := store.Search(ctx, "", knowledge.EpisodeFilter{ExcludeExpired: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 non-expired episodes, got %d", len(results))
	}
}
