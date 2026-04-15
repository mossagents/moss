package product

import (
	"fmt"
	appconfig "github.com/mossagents/moss/harness/config"
	"sort"
	"strings"
)

const (
	PersonalityFriendly  = "friendly"
	PersonalityPragmatic = "pragmatic"
	PersonalityNone      = "none"

	ExperimentalBackgroundPS            = "background-ps"
	ExperimentalComposerMentions        = "composer-mentions"
	ExperimentalStatuslineCustomization = "statusline-customization"
	ExperimentalApps                    = "apps"
	ExperimentalMultimodalImages        = "multimodal-images"
)

var experimentalFeatureDescriptions = map[string]string{
	ExperimentalApps:                    "Prepare the future apps/connectors surface.",
	ExperimentalBackgroundPS:            "Expose background activity through /ps.",
	ExperimentalComposerMentions:        "Insert file mentions into the composer with /mention.",
	ExperimentalMultimodalImages:        "Prepare first-class multimodal image input.",
	ExperimentalStatuslineCustomization: "Expose configurable footer items with /statusline.",
}

func NormalizePersonality(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", PersonalityFriendly:
		return PersonalityFriendly
	case PersonalityPragmatic:
		return PersonalityPragmatic
	case PersonalityNone:
		return PersonalityNone
	default:
		return ""
	}
}

func ValidatePersonality(name string) error {
	if NormalizePersonality(name) == "" {
		return fmt.Errorf("personality must be friendly, pragmatic, or none")
	}
	return nil
}

func NormalizeExperimentalFeature(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, ok := experimentalFeatureDescriptions[name]; ok {
		return name
	}
	return ""
}

func ValidateExperimentalFeature(name string) error {
	if NormalizeExperimentalFeature(name) == "" {
		return fmt.Errorf("unknown experimental feature %q", name)
	}
	return nil
}

func DefaultExperimentalFeatures() []string {
	return []string{
		ExperimentalBackgroundPS,
		ExperimentalComposerMentions,
		ExperimentalStatuslineCustomization,
	}
}

func ExperimentalFeatureEnabled(cfg appconfig.TUIConfig, name string) bool {
	name = NormalizeExperimentalFeature(name)
	if name == "" {
		return false
	}
	values := cfg.Experimental
	if len(values) == 0 {
		values = DefaultExperimentalFeatures()
	}
	for _, item := range values {
		if NormalizeExperimentalFeature(item) == name {
			return true
		}
	}
	return false
}

func SupportedExperimentalFeatures() []string {
	keys := make([]string, 0, len(experimentalFeatureDescriptions))
	for key := range experimentalFeatureDescriptions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func ExperimentalFeatureDescription(name string) string {
	return experimentalFeatureDescriptions[NormalizeExperimentalFeature(name)]
}

func LoadTUIConfig() (appconfig.TUIConfig, error) {
	cfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		return appconfig.TUIConfig{}, fmt.Errorf("load global config: %w", err)
	}
	return cfg.TUI, nil
}

func LoadProjectTUIConfig(workspace string) (appconfig.TUIConfig, error) {
	cfg, err := appconfig.LoadProjectConfig(workspace)
	if err != nil {
		return appconfig.TUIConfig{}, fmt.Errorf("load project config: %w", err)
	}
	return cfg.TUI, nil
}

func UpdateTUIConfig(mutator func(*appconfig.TUIConfig) error) (appconfig.TUIConfig, error) {
	cfgPath, err := ConfigPath()
	if err != nil {
		return appconfig.TUIConfig{}, err
	}
	cfg, err := appconfig.LoadConfig(cfgPath)
	if err != nil {
		return appconfig.TUIConfig{}, fmt.Errorf("load config: %w", err)
	}
	if err := mutator(&cfg.TUI); err != nil {
		return appconfig.TUIConfig{}, err
	}
	if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
		return appconfig.TUIConfig{}, fmt.Errorf("save config: %w", err)
	}
	return cfg.TUI, nil
}

func UpdateProjectTUIConfig(workspace string, mutator func(*appconfig.TUIConfig) error) (appconfig.TUIConfig, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return appconfig.TUIConfig{}, fmt.Errorf("workspace is required")
	}
	cfgPath := appconfig.DefaultProjectConfigPath(workspace)
	cfg, err := appconfig.LoadConfig(cfgPath)
	if err != nil {
		return appconfig.TUIConfig{}, fmt.Errorf("load project config: %w", err)
	}
	if err := mutator(&cfg.TUI); err != nil {
		return appconfig.TUIConfig{}, err
	}
	if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
		return appconfig.TUIConfig{}, fmt.Errorf("save project config: %w", err)
	}
	return cfg.TUI, nil
}

func RenderExperimentalFeatures(cfg appconfig.TUIConfig) string {
	var b strings.Builder
	b.WriteString("Experimental features:\n")
	for _, name := range SupportedExperimentalFeatures() {
		status := "disabled"
		if ExperimentalFeatureEnabled(cfg, name) {
			status = "enabled"
		}
		fmt.Fprintf(&b, "- %s [%s] — %s\n", name, status, ExperimentalFeatureDescription(name))
	}
	b.WriteString("\nUsage:\n")
	b.WriteString("  /experimental\n")
	b.WriteString("  /experimental enable <feature>\n")
	b.WriteString("  /experimental disable <feature>\n")
	return strings.TrimRight(b.String(), "\n")
}
