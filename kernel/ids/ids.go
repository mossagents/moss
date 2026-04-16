package ids

import (
	"strings"

	"github.com/oklog/ulid/v2"
)

// New returns a lexicographically sortable ULID string for internal records.
func New() string {
	return ulid.Make().String()
}

// NewPrefixed returns a ULID string with a stable semantic prefix when provided.
func NewPrefixed(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return New()
	}
	return prefix + "-" + New()
}
