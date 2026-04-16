package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mossagents/moss/harness/appkit/product"
	configpkg "github.com/mossagents/moss/harness/config"
	"strings"
)

type modelPickerOption struct {
	title        string
	detail       string
	provider     string
	providerName string
	model        string
	auto         bool
}

type modelPickerState struct {
	options []modelPickerOption
	list    *selectionListState
}

func persistedModelOverride() (string, string, string, bool) {
	prefs, err := product.LoadTUIConfig()
	if err != nil {
		return "", "", "", false
	}
	provider := strings.TrimSpace(prefs.SelectedProvider)
	providerName := strings.TrimSpace(prefs.SelectedProviderName)
	model := strings.TrimSpace(prefs.SelectedModel)
	return provider, providerName, model, provider != "" || providerName != "" || model != ""
}

func hasPersistedModelOverride() bool {
	_, _, _, ok := persistedModelOverride()
	return ok
}

func persistModelOverride(selection switchModelMsg) error {
	_, err := product.UpdateTUIConfig(func(cfg *configpkg.TUIConfig) error {
		if selection.auto {
			cfg.SelectedProvider = ""
			cfg.SelectedProviderName = ""
			cfg.SelectedModel = ""
			return nil
		}
		cfg.SelectedProvider = strings.TrimSpace(selection.provider)
		cfg.SelectedProviderName = strings.TrimSpace(selection.providerName)
		cfg.SelectedModel = strings.TrimSpace(selection.model)
		return nil
	})
	return err
}

func resolveModelPickerConfig(workspace, trust string) (*configpkg.Config, error) {
	globalCfg, err := configpkg.LoadGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("load global config: %w", err)
	}
	if !configpkg.ProjectAssetsAllowed(trust) {
		return globalCfg, nil
	}
	projectCfg, err := configpkg.LoadProjectConfig(workspace)
	if err != nil {
		return nil, fmt.Errorf("load project config: %w", err)
	}
	if projectCfg != nil && len(projectCfg.Models) > 0 {
		return projectCfg, nil
	}
	return globalCfg, nil
}

func newModelPickerState(workspace, trust, currentProvider, currentProviderName, currentModel string, currentAuto bool) (*modelPickerState, error) {
	cfg, err := resolveModelPickerConfig(workspace, trust)
	if err != nil {
		return nil, err
	}
	options := buildModelPickerOptions(cfg)
	items := make([]selectionListItem, 0, len(options))
	for _, option := range options {
		items = append(items, selectionListItem{
			Title:  option.title,
			Detail: option.detail,
		})
	}
	state := &modelPickerState{
		options: options,
		list: &selectionListState{
			Title:        "Models",
			Footer:       "↑↓ choose • Enter apply • Esc close",
			EmptyMessage: "No configured models.",
			Items:        items,
		},
	}
	if currentAuto {
		return state, nil
	}
	for i, option := range options {
		if option.auto {
			continue
		}
		if option.matches(currentProvider, currentProviderName, currentModel) {
			state.list.Cursor = i
			return state, nil
		}
	}
	return state, nil
}

func buildModelPickerOptions(cfg *configpkg.Config) []modelPickerOption {
	if cfg == nil {
		cfg = &configpkg.Config{}
	}
	identity := cfg.ProviderIdentity()
	autoDetail := "Use the configured default model selection."
	if label := identity.Label(); label != "" {
		autoDetail = label
		if model := strings.TrimSpace(cfg.Model); model != "" {
			autoDetail += " · " + model
		}
	}
	options := []modelPickerOption{{
		title:        "Auto",
		detail:       autoDetail,
		provider:     identity.Provider,
		providerName: identity.Name,
		model:        strings.TrimSpace(cfg.Model),
		auto:         true,
	}}

	seen := map[string]int{}
	for _, candidate := range cfg.Models {
		id := configpkg.NormalizeProviderIdentity(candidate.Provider, candidate.Name)
		model := strings.TrimSpace(candidate.Model)
		title := model
		if title == "" {
			title = valueOrDefaultString(id.DisplayName(), "(default)")
		}
		detailParts := []string{}
		if label := id.Label(); label != "" && !strings.EqualFold(label, title) {
			detailParts = append(detailParts, label)
		}
		if candidate.Default {
			detailParts = append(detailParts, "configured default")
		}
		key := strings.ToLower(strings.TrimSpace(id.Provider) + "\x00" + strings.TrimSpace(id.Name) + "\x00" + model)
		if idx, ok := seen[key]; ok {
			if options[idx].detail == "" && len(detailParts) > 0 {
				options[idx].detail = strings.Join(detailParts, " · ")
			}
			continue
		}
		seen[key] = len(options)
		options = append(options, modelPickerOption{
			title:        title,
			detail:       strings.Join(detailParts, " · "),
			provider:     id.Provider,
			providerName: id.Name,
			model:        model,
		})
	}
	return options
}

func (o modelPickerOption) matches(provider, providerName, model string) bool {
	return strings.EqualFold(strings.TrimSpace(o.provider), strings.TrimSpace(provider)) &&
		strings.EqualFold(strings.TrimSpace(o.providerName), strings.TrimSpace(providerName)) &&
		strings.EqualFold(strings.TrimSpace(o.model), strings.TrimSpace(model))
}

func (o modelPickerOption) switchLabel() string {
	if o.auto {
		return "Auto"
	}
	return o.title
}

func (m chatModel) openModelPicker() (chatModel, tea.Cmd) {
	state, err := newModelPickerState(m.workspace, m.trust, m.providerID, m.providerName, m.model, m.modelAuto)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to load configured models: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	m.modelPicker = state
	m.openModelOverlay()
	m.refreshViewport()
	return m, nil
}

func (m chatModel) switchModelByQuery(query string) (chatModel, tea.Cmd) {
	state, err := newModelPickerState(m.workspace, m.trust, m.providerID, m.providerName, m.model, m.modelAuto)
	if err != nil {
		m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("failed to load configured models: %v", err)})
		m.refreshViewport()
		return m, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return m.openModelPicker()
	}
	for _, option := range state.options {
		if strings.EqualFold(query, option.title) || strings.EqualFold(query, option.model) || (option.auto && strings.EqualFold(query, "auto")) {
			return m.applyModelPickerSelection(option)
		}
	}
	m.messages = append(m.messages, chatMessage{kind: msgError, content: fmt.Sprintf("unknown model %q (use /model to choose from configured models)", query)})
	m.refreshViewport()
	return m, nil
}

func (m chatModel) handleModelPickerKey(msg tea.KeyMsg) (chatModel, tea.Cmd) {
	if m.modelPicker == nil || len(m.modelPicker.options) == 0 {
		return m.closeModelOverlay(), nil
	}
	switch msg.String() {
	case "up":
		m.modelPicker.list.Move(-1)
	case "down":
		m.modelPicker.list.Move(1)
	case "enter":
		idx := m.modelPicker.list.SelectedIndex()
		if idx >= 0 {
			return m.applyModelPickerSelection(m.modelPicker.options[idx])
		}
	}
	m.refreshViewport()
	return m, nil
}

func (m chatModel) applyModelPickerSelection(option modelPickerOption) (chatModel, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{
		kind:    msgSystem,
		content: fmt.Sprintf("Switching model to %s...", option.switchLabel()),
	})
	m.streaming = true
	m.modelAuto = option.auto
	m.modelPicker = nil
	if m.overlays != nil {
		m.overlays.Close(overlayModel)
	}
	m.refreshViewport()
	return m, func() tea.Msg {
		return switchModelMsg{
			provider:     option.provider,
			providerName: option.providerName,
			model:        option.model,
			auto:         option.auto,
		}
	}
}

func (m chatModel) renderModelPicker(width int) string {
	if m.modelPicker == nil || m.modelPicker.list == nil {
		return ""
	}
	return renderSelectionListDialog(width, m.selectionDialogMaxHeight(), m.modelPicker.list)
}
