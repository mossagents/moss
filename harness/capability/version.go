package capability

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseVersion parses a version string like "v1.2.3" or "1.2.3" into a [3]int.
func ParseVersion(v string) ([3]int, error) {
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return [3]int{}, nil
	}
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return result, fmt.Errorf("invalid version segment %q in %q", p, v)
		}
		result[i] = n
	}
	return result, nil
}

// CompareVersion returns -1, 0, or 1 if a < b, a == b, or a > b.
func CompareVersion(a, b string) int {
	av, _ := ParseVersion(a)
	bv, _ := ParseVersion(b)
	for i := 0; i < 3; i++ {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

// IsVersionInRange reports whether v satisfies min <= v <= max.
func IsVersionInRange(v, min, max string) bool {
	if min != "" && CompareVersion(v, min) < 0 {
		return false
	}
	if max != "" && CompareVersion(v, max) > 0 {
		return false
	}
	return true
}
