package promptpacks

import (
	"fmt"
	"sort"
	"strings"
)

type Pack struct {
	ID     string
	Source string
}

func (p Pack) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("prompt pack id is required")
	}
	if strings.TrimSpace(p.Source) == "" {
		return fmt.Errorf("prompt pack %q source is required", p.ID)
	}
	return nil
}

func Resolve(registry map[string]Pack, id string) (Pack, error) {
	id = strings.TrimSpace(id)
	pack, ok := registry[id]
	if !ok {
		return Pack{}, fmt.Errorf("unknown prompt pack %q", id)
	}
	if err := pack.Validate(); err != nil {
		return Pack{}, err
	}
	return pack, nil
}

func Names(registry map[string]Pack) []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
