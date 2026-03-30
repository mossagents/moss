package product

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
)

func ConfigPath() (string, error) {
	cfgPath := appconfig.DefaultGlobalConfigPath()
	if strings.TrimSpace(cfgPath) == "" {
		return "", fmt.Errorf("global config path is unavailable")
	}
	return cfgPath, nil
}

func ShowConfig(flags *appkit.AppFlags, showSensitive bool) (string, error) {
	cfgPath, err := ConfigPath()
	if err != nil {
		return "", err
	}
	cfg, err := appconfig.LoadConfig(cfgPath)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Config file: %s\n", cfgPath)
	fmt.Fprintf(&b, "Persisted defaults:\n")
	fmt.Fprintf(&b, "  api_type: %s\n", firstNonEmpty(cfg.EffectiveAPIType(), "(not set)"))
	fmt.Fprintf(&b, "  name:     %s\n", firstNonEmpty(cfg.DisplayProviderName(), "(not set)"))
	fmt.Fprintf(&b, "  model:    %s\n", firstNonEmpty(cfg.Model, "(not set)"))
	fmt.Fprintf(&b, "  base_url: %s\n", firstNonEmpty(cfg.BaseURL, "(not set)"))
	if showSensitive {
		apiKeyDisplay := "(not set)"
		if strings.TrimSpace(cfg.APIKey) != "" {
			apiKeyDisplay = maskKey(cfg.APIKey)
		}
		fmt.Fprintf(&b, "  api_key:  %s\n", apiKeyDisplay)
	}
	if flags != nil {
		fmt.Fprintf(&b, "\nEffective runtime:\n")
		fmt.Fprintf(&b, "  api_type: %s\n", firstNonEmpty(flags.EffectiveAPIType(), "(not set)"))
		fmt.Fprintf(&b, "  name:     %s\n", firstNonEmpty(flags.DisplayProviderName(), "(not set)"))
		fmt.Fprintf(&b, "  model:    %s\n", firstNonEmpty(flags.Model, "(default)"))
		fmt.Fprintf(&b, "  base_url: %s\n", firstNonEmpty(flags.BaseURL, "(not set)"))
	}
	return b.String(), nil
}

func SetConfig(key, value string, allowSensitive bool) (string, error) {
	cfgPath, err := ConfigPath()
	if err != nil {
		return "", err
	}
	cfg, err := appconfig.LoadConfig(cfgPath)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	display, err := applyConfigSet(cfg, key, value, allowSensitive)
	if err != nil {
		return "", err
	}
	if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
		return "", fmt.Errorf("save config: %w", err)
	}
	return display, nil
}

func UnsetConfig(key string, allowSensitive bool) error {
	cfgPath, err := ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := appconfig.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if err := applyConfigUnset(cfg, key, allowSensitive); err != nil {
		return err
	}
	if err := appconfig.SaveConfig(cfgPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

func applyConfigSet(cfg *appconfig.Config, key, value string, allowSensitive bool) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("config is required")
	}
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("value is required")
	}
	switch key {
	case "api_type", "apitype", "provider":
		identity := appconfig.NormalizeProviderIdentity(value, value, cfg.Name)
		cfg.APIType = identity.APIType
		cfg.Provider = identity.Provider
		if strings.TrimSpace(cfg.Name) == "" {
			cfg.Name = identity.Name
		}
	case "name":
		cfg.Name = value
	case "model":
		cfg.Model = value
	case "base_url", "baseurl":
		cfg.BaseURL = value
	case "api_key", "apikey":
		if !allowSensitive {
			return "", fmt.Errorf("api_key is not managed by this product surface")
		}
		cfg.APIKey = value
	default:
		if allowSensitive {
			return "", fmt.Errorf("unknown config key %q (supported: provider, name, model, base_url, api_key)", key)
		}
		return "", fmt.Errorf("unknown config key %q (supported: provider, name, model, base_url)", key)
	}
	display := value
	if key == "api_key" || key == "apikey" {
		display = maskKey(value)
	}
	return display, nil
}

func applyConfigUnset(cfg *appconfig.Config, key string, allowSensitive bool) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "name":
		cfg.Name = ""
	case "model":
		cfg.Model = ""
	case "base_url", "baseurl":
		cfg.BaseURL = ""
	case "api_key", "apikey":
		if !allowSensitive {
			return fmt.Errorf("api_key is not managed by this product surface")
		}
		cfg.APIKey = ""
	default:
		if allowSensitive {
			return fmt.Errorf("unknown config key %q (supported: name, model, base_url, api_key)", key)
		}
		return fmt.Errorf("unknown config key %q (supported: name, model, base_url)", key)
	}
	return nil
}

func maskKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "********"
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
