package stringutil

import "testing"

func TestFirstNonEmpty_ReturnsFirst(t *testing.T) {
	if got := FirstNonEmpty("a", "b", "c"); got != "a" {
		t.Fatalf("got %q, want %q", got, "a")
	}
}

func TestFirstNonEmpty_SkipsBlanks(t *testing.T) {
	if got := FirstNonEmpty("", "  ", "hello", "world"); got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestFirstNonEmpty_TrimsWhitespace(t *testing.T) {
	if got := FirstNonEmpty("  trimmed  "); got != "trimmed" {
		t.Fatalf("got %q, want %q", got, "trimmed")
	}
}

func TestFirstNonEmpty_AllBlank(t *testing.T) {
	if got := FirstNonEmpty("", "  ", "\t"); got != "" {
		t.Fatalf("got %q, want %q", got, "")
	}
}

func TestFirstNonEmpty_NoArgs(t *testing.T) {
	if got := FirstNonEmpty(); got != "" {
		t.Fatalf("got %q, want %q", got, "")
	}
}
