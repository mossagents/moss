package product

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mossagents/moss/appkit"
	appconfig "github.com/mossagents/moss/config"
)

type MCPConfigSource string

const (
	MCPConfigSourceGlobal  MCPConfigSource = "global"
	MCPConfigSourceProject MCPConfigSource = "project"
)

type MCPServerConfigView struct {
	Name       string          `json:"name"`
	Source     MCPConfigSource `json:"source"`
	Path       string          `json:"path"`
	Transport  string          `json:"transport"`
	Enabled    bool            `json:"enabled"`
	Effective  bool            `json:"effective"`
	Status     string          `json:"status"`
	Trust      string          `json:"trust"`
	Command    string          `json:"command,omitempty"`
	Args       []string        `json:"args,omitempty"`
	URL        string          `json:"url,omitempty"`
	Target     string          `json:"target,omitempty"`
	HasEnv     bool            `json:"has_env"`
	EnvKeys    []string        `json:"env_keys,omitempty"`
	ToolPrefix string          `json:"tool_prefix"`
}

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
	fmt.Fprintf(&b, "  provider: %s\n", firstNonEmpty(cfg.EffectiveAPIType(), "(not set)"))
	fmt.Fprintf(&b, "  name:     %s\n", firstNonEmpty(cfg.DisplayProviderName(), "(not set)"))
	fmt.Fprintf(&b, "  model:    %s\n", firstNonEmpty(cfg.Model, "(not set)"))
	fmt.Fprintf(&b, "  base_url: %s\n", firstNonEmpty(cfg.BaseURL, "(not set)"))
	fmt.Fprintf(&b, "  tui.theme: %s\n", firstNonEmpty(cfg.TUI.Theme, "default"))
	fmt.Fprintf(&b, "  tui.personality: %s\n", firstNonEmpty(cfg.TUI.Personality, PersonalityFriendly))
	fastMode := "false"
	if cfg.TUI.FastMode != nil && *cfg.TUI.FastMode {
		fastMode = "true"
	}
	fmt.Fprintf(&b, "  tui.fast_mode: %s\n", fastMode)
	if len(cfg.TUI.StatusLine) > 0 {
		fmt.Fprintf(&b, "  tui.status_line: %s\n", strings.Join(cfg.TUI.StatusLine, ", "))
	}
	if showSensitive {
		apiKeyDisplay := "(not set)"
		if strings.TrimSpace(cfg.APIKey) != "" {
			apiKeyDisplay = maskKey(cfg.APIKey)
		}
		fmt.Fprintf(&b, "  api_key:  %s\n", apiKeyDisplay)
	}
	if flags != nil {
		fmt.Fprintf(&b, "\nEffective runtime:\n")
		fmt.Fprintf(&b, "  provider: %s\n", firstNonEmpty(flags.EffectiveAPIType(), "(not set)"))
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

func ListMCPServers(workspace, trust string) ([]MCPServerConfigView, error) {
	trust = appconfig.NormalizeTrustLevel(trust)
	globalCfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("load global config: %w", err)
	}
	projectCfg, err := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspace))
	if err != nil {
		return nil, fmt.Errorf("load project config: %w", err)
	}
	return buildMCPServerViews(workspace, trust, globalCfg, projectCfg), nil
}

func GetMCPServer(workspace, trust, name string) ([]MCPServerConfigView, error) {
	servers, err := ListMCPServers(workspace, trust)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("mcp server name is required")
	}
	var matches []MCPServerConfigView
	for _, server := range servers {
		if strings.EqualFold(server.Name, name) {
			matches = append(matches, server)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("mcp server %q not found", name)
	}
	return matches, nil
}

func SetMCPEnabled(workspace, name, scope string, enabled bool) (MCPServerConfigView, error) {
	targetScope, err := resolveMCPScope(scope)
	if err != nil {
		return MCPServerConfigView{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return MCPServerConfigView{}, fmt.Errorf("mcp server name is required")
	}
	if targetScope == "" {
		targetScope, err = inferMCPScope(workspace, name)
		if err != nil {
			return MCPServerConfigView{}, err
		}
	}
	cfg, path, err := loadConfigForScope(workspace, targetScope)
	if err != nil {
		return MCPServerConfigView{}, err
	}
	index := -1
	for i, skillCfg := range cfg.Skills {
		if !skillCfg.IsMCP() || !strings.EqualFold(skillCfg.Name, name) {
			continue
		}
		index = i
		break
	}
	if index < 0 {
		return MCPServerConfigView{}, fmt.Errorf("mcp server %q not found in %s config", name, targetScope)
	}
	cfg.Skills[index].Enabled = boolPtr(enabled)
	if err := appconfig.SaveConfig(path, cfg); err != nil {
		return MCPServerConfigView{}, fmt.Errorf("save %s config: %w", targetScope, err)
	}
	servers, err := ListMCPServers(workspace, appconfig.TrustTrusted)
	if err != nil {
		return MCPServerConfigView{}, err
	}
	for _, server := range servers {
		if strings.EqualFold(server.Name, name) && server.Source == targetScope {
			return server, nil
		}
	}
	return MCPServerConfigView{}, fmt.Errorf("mcp server %q updated but could not be reloaded", name)
}

func RenderMCPServerList(servers []MCPServerConfigView) string {
	if len(servers) == 0 {
		return "No MCP servers configured.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Configured MCP servers:\n")
	for _, server := range servers {
		fmt.Fprintf(&b, "  - %s [%s] transport=%s enabled=%t effective=%t status=%s",
			server.Name, server.Source, firstNonEmpty(server.Transport, "-"), server.Enabled, server.Effective, server.Status)
		if server.Target != "" {
			fmt.Fprintf(&b, " target=%s", server.Target)
		}
		if server.HasEnv {
			fmt.Fprintf(&b, " env=%d", len(server.EnvKeys))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func RenderMCPServerDetail(servers []MCPServerConfigView) string {
	if len(servers) == 0 {
		return "No MCP server details available.\n"
	}
	var b strings.Builder
	for i, server := range servers {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "MCP server: %s\n", server.Name)
		fmt.Fprintf(&b, "  source:     %s\n", server.Source)
		fmt.Fprintf(&b, "  path:       %s\n", server.Path)
		fmt.Fprintf(&b, "  transport:  %s\n", firstNonEmpty(server.Transport, "-"))
		fmt.Fprintf(&b, "  enabled:    %t\n", server.Enabled)
		fmt.Fprintf(&b, "  effective:  %t\n", server.Effective)
		fmt.Fprintf(&b, "  status:     %s\n", server.Status)
		fmt.Fprintf(&b, "  trust:      %s\n", server.Trust)
		if server.Command != "" {
			fmt.Fprintf(&b, "  command:    %s\n", server.Command)
		}
		if len(server.Args) > 0 {
			fmt.Fprintf(&b, "  args:       %s\n", strings.Join(server.Args, " "))
		}
		if server.URL != "" {
			fmt.Fprintf(&b, "  url:        %s\n", server.URL)
		}
		fmt.Fprintf(&b, "  tool_prefix:%s\n", server.ToolPrefix)
		if server.HasEnv {
			fmt.Fprintf(&b, "  env_keys:   %s\n", strings.Join(server.EnvKeys, ", "))
		}
	}
	return b.String()
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
		cfg.Provider = identity.Provider
		if strings.TrimSpace(cfg.Name) == "" {
			cfg.Name = identity.Name
		}
		value = identity.Provider
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
	case "api_type", "apitype", "provider":
		cfg.Provider = ""
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
			return fmt.Errorf("unknown config key %q (supported: provider, name, model, base_url, api_key)", key)
		}
		return fmt.Errorf("unknown config key %q (supported: provider, name, model, base_url)", key)
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

func buildMCPServerViews(workspace, trust string, globalCfg, projectCfg *appconfig.Config) []MCPServerConfigView {
	trust = appconfig.NormalizeTrustLevel(trust)
	projectAllowed := appconfig.ProjectAssetsAllowed(trust)
	globalPath := appconfig.DefaultGlobalConfigPath()
	projectPath := appconfig.DefaultProjectConfigPath(workspace)
	activeSource := make(map[string]MCPConfigSource)
	activeIsMCP := make(map[string]bool)

	if globalCfg != nil {
		for _, skillCfg := range globalCfg.Skills {
			activeSource[skillCfg.Name] = MCPConfigSourceGlobal
			activeIsMCP[skillCfg.Name] = skillCfg.IsMCP()
		}
	}
	if projectAllowed && projectCfg != nil {
		for _, skillCfg := range projectCfg.Skills {
			activeSource[skillCfg.Name] = MCPConfigSourceProject
			activeIsMCP[skillCfg.Name] = skillCfg.IsMCP()
		}
	}

	var views []MCPServerConfigView
	appendViews := func(source MCPConfigSource, path string, trustLabel string, cfg *appconfig.Config) {
		if cfg == nil {
			return
		}
		for _, skillCfg := range cfg.Skills {
			if !skillCfg.IsMCP() {
				continue
			}
			status := "enabled"
			effective := true
			switch {
			case !skillCfg.IsEnabled():
				status = "disabled"
				effective = false
			case source == MCPConfigSourceProject && !projectAllowed:
				status = "suppressed_by_trust"
				effective = false
			case activeSource[skillCfg.Name] != source || !activeIsMCP[skillCfg.Name]:
				status = "shadowed"
				effective = false
			}
			envKeys := sortedMapKeys(skillCfg.Env)
			views = append(views, MCPServerConfigView{
				Name:       skillCfg.Name,
				Source:     source,
				Path:       path,
				Transport:  strings.TrimSpace(skillCfg.Transport),
				Enabled:    skillCfg.IsEnabled(),
				Effective:  effective,
				Status:     status,
				Trust:      trustLabel,
				Command:    strings.TrimSpace(skillCfg.Command),
				Args:       append([]string(nil), skillCfg.Args...),
				URL:        strings.TrimSpace(skillCfg.URL),
				Target:     describeMCPTarget(skillCfg),
				HasEnv:     len(skillCfg.Env) > 0,
				EnvKeys:    envKeys,
				ToolPrefix: skillCfg.Name + "_",
			})
		}
	}

	appendViews(MCPConfigSourceGlobal, globalPath, "always", globalCfg)
	appendViews(MCPConfigSourceProject, projectPath, ternary(projectAllowed, "allowed", "suppressed"), projectCfg)
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].Source < views[j].Source
	})
	return views
}

func inferMCPScope(workspace, name string) (MCPConfigSource, error) {
	globalCfg, err := appconfig.LoadGlobalConfig()
	if err != nil {
		return "", fmt.Errorf("load global config: %w", err)
	}
	projectCfg, err := appconfig.LoadConfig(appconfig.DefaultProjectConfigPath(workspace))
	if err != nil {
		return "", fmt.Errorf("load project config: %w", err)
	}
	name = strings.TrimSpace(name)
	var matches []MCPConfigSource
	for _, skillCfg := range globalCfg.Skills {
		if skillCfg.IsMCP() && strings.EqualFold(skillCfg.Name, name) {
			matches = append(matches, MCPConfigSourceGlobal)
			break
		}
	}
	for _, skillCfg := range projectCfg.Skills {
		if skillCfg.IsMCP() && strings.EqualFold(skillCfg.Name, name) {
			matches = append(matches, MCPConfigSourceProject)
			break
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("mcp server %q not found", name)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("mcp server %q exists in multiple config scopes; specify global or project", name)
	}
}

func resolveMCPScope(scope string) (MCPConfigSource, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "":
		return "", nil
	case string(MCPConfigSourceGlobal):
		return MCPConfigSourceGlobal, nil
	case string(MCPConfigSourceProject):
		return MCPConfigSourceProject, nil
	default:
		return "", fmt.Errorf("unknown mcp config scope %q (supported: global, project)", scope)
	}
}

func loadConfigForScope(workspace string, scope MCPConfigSource) (*appconfig.Config, string, error) {
	switch scope {
	case MCPConfigSourceGlobal:
		cfg, err := appconfig.LoadGlobalConfig()
		if err != nil {
			return nil, "", fmt.Errorf("load global config: %w", err)
		}
		return cfg, appconfig.DefaultGlobalConfigPath(), nil
	case MCPConfigSourceProject:
		path := appconfig.DefaultProjectConfigPath(workspace)
		cfg, err := appconfig.LoadConfig(path)
		if err != nil {
			return nil, "", fmt.Errorf("load project config: %w", err)
		}
		return cfg, path, nil
	default:
		return nil, "", fmt.Errorf("unsupported mcp config scope %q", scope)
	}
}

func describeMCPTarget(skillCfg appconfig.SkillConfig) string {
	switch strings.TrimSpace(skillCfg.Transport) {
	case "stdio":
		target := strings.TrimSpace(skillCfg.Command)
		if len(skillCfg.Args) == 0 {
			return target
		}
		if target == "" {
			return strings.Join(skillCfg.Args, " ")
		}
		return strings.TrimSpace(target + " " + strings.Join(skillCfg.Args, " "))
	case "sse":
		return strings.TrimSpace(skillCfg.URL)
	default:
		return ""
	}
}

func sortedMapKeys(items map[string]string) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func boolPtr(v bool) *bool {
	value := v
	return &value
}

func ternary(ok bool, a, b string) string {
	if ok {
		return a
	}
	return b
}
