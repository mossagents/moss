package presets

import (
	"fmt"
	"sort"
	"strings"
)

type Preset struct {
	ID                string
	PromptPack        string
	CollaborationMode string
	PermissionProfile string
	SessionPolicy     string
	ModelProfile      string
}

func (p Preset) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return fmt.Errorf("preset id is required")
	}
	if strings.TrimSpace(p.PromptPack) == "" {
		return fmt.Errorf("preset %q prompt_pack is required", p.ID)
	}
	if strings.TrimSpace(p.CollaborationMode) == "" {
		return fmt.Errorf("preset %q collaboration_mode is required", p.ID)
	}
	if strings.TrimSpace(p.PermissionProfile) == "" {
		return fmt.Errorf("preset %q permission_profile is required", p.ID)
	}
	if strings.TrimSpace(p.SessionPolicy) == "" {
		return fmt.Errorf("preset %q session_policy is required", p.ID)
	}
	if strings.TrimSpace(p.ModelProfile) == "" {
		return fmt.Errorf("preset %q model_profile is required", p.ID)
	}
	return nil
}

func Resolve(registry map[string]Preset, id string) (Preset, error) {
	id = strings.TrimSpace(id)
	preset, ok := registry[id]
	if !ok {
		return Preset{}, fmt.Errorf("unknown preset %q", id)
	}
	if err := preset.Validate(); err != nil {
		return Preset{}, err
	}
	return preset, nil
}

func Names(registry map[string]Preset) []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
