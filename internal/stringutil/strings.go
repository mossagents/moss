// Package stringutil provides lightweight string helpers shared across the module.
package stringutil

import "strings"

// FirstNonEmpty returns the first non-blank string after trimming whitespace.
// Returns "" if all values are blank.
func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
