package appkit

import "testing"

func TestResolveRuntimeFeatureFlags_DefaultDisabled(t *testing.T) {
	got := resolveRuntimeFeatureFlags(&AppFlags{})
	if got.EnableSummarize {
		t.Fatal("expected summarize disabled by default")
	}
	if got.EnableRAG {
		t.Fatal("expected rag disabled by default")
	}
}

func TestResolveRuntimeFeatureFlags_UsesFlags(t *testing.T) {
	got := resolveRuntimeFeatureFlags(&AppFlags{EnableSummarize: true, EnableRAG: true})
	if !got.EnableSummarize {
		t.Fatal("expected summarize enabled")
	}
	if !got.EnableRAG {
		t.Fatal("expected rag enabled")
	}
}

