package knowledge

import (
	"fmt"
	"sort"
)

func anyToString(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", v)
}

func sortStrings(ss []string) {
	sort.Strings(ss)
}
