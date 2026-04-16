package ids

import (
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestNewReturnsParseableULID(t *testing.T) {
	value := New()
	if len(value) != 26 {
		t.Fatalf("id length = %d, want 26", len(value))
	}
	if _, err := ulid.ParseStrict(value); err != nil {
		t.Fatalf("parse ulid %q: %v", value, err)
	}
}

func TestNewPrefixedReturnsParseableSuffix(t *testing.T) {
	value := NewPrefixed("sess")
	if !strings.HasPrefix(value, "sess-") {
		t.Fatalf("prefixed id = %q, want sess-*", value)
	}
	suffix := strings.TrimPrefix(value, "sess-")
	if _, err := ulid.ParseStrict(suffix); err != nil {
		t.Fatalf("parse prefixed ulid %q: %v", value, err)
	}
}
