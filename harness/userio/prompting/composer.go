package prompting

import (
	_ "embed"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/mossagents/moss/harness/bootstrap"
	config "github.com/mossagents/moss/harness/config"
	"github.com/mossagents/moss/harness/extensions/capability"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/session"
)

//go:embed templates/system_prompt.tmpl
var defaultSystemPromptTemplate string

type ComposeInput struct {
	Workspace           string
	Trust               string
	ConfigInstructions  string
	SessionInstructions string
	ModelInstructions   string
	ProfileName         string
	TaskMode            string
	CollaborationMode   string
	PermissionProfile   string
	PromptPack          string
	Preset              string
	Kernel              *kernel.Kernel
	SkillPrompts        []string
	RuntimeNotices      []string
}

type ComposeOutput struct {
	Prompt    string
	Envelope  PromptEnvelope
	Graph     InstructionGraph
	DebugMeta ComposeDebugMeta
}

type PromptEnvelope struct {
	Prompt          string
	EnabledLayerIDs []string
}

type InstructionProfile struct {
	ID                string `json:"id,omitempty"`
	ProfileName       string `json:"profile_name,omitempty"`
	TaskMode          string `json:"task_mode,omitempty"`
	CollaborationMode string `json:"collaboration_mode,omitempty"`
}

type SessionPromptMode struct {
	ProfileName       string `json:"profile_name,omitempty"`
	TaskMode          string `json:"task_mode,omitempty"`
	CollaborationMode string `json:"collaboration_mode,omitempty"`
	PermissionProfile string `json:"permission_profile,omitempty"`
	PromptPack        string `json:"prompt_pack,omitempty"`
	Preset            string `json:"preset,omitempty"`
}

type InstructionLayer struct {
	ID                string `json:"id"`
	Source            string `json:"source"`
	Scope             string `json:"scope,omitempty"`
	Priority          int    `json:"priority,omitempty"`
	Activation        string `json:"activation,omitempty"`
	Content           string `json:"content,omitempty"`
	Enabled           bool   `json:"enabled"`
	SuppressionReason string `json:"suppression_reason,omitempty"`
	TokenEstimate     int    `json:"token_estimate,omitempty"`
}

type InstructionGraph struct {
	BaseSource  string             `json:"base_source,omitempty"`
	Profile     InstructionProfile `json:"profile"`
	Layers      []InstructionLayer `json:"layers,omitempty"`
	SourceChain []string           `json:"source_chain,omitempty"`
}

type ComposeDebugMeta struct {
	BaseSource          string
	DynamicSectionID    []string
	EnabledLayers       []string
	SuppressedLayers    []string
	SuppressionReasons  map[string]string
	LayerTokenEstimates map[string]int
	SourceChain         []string
	InstructionProfile  string
	PromptAssembly      string
	PromptVersion       string
}

const (
	MetadataSessionInstructionsKey = "prompt.session_instructions"
	MetadataBaseSourceKey          = "prompt.debug.base_source"
	MetadataDynamicSectionsKey     = "prompt.debug.dynamic_sections"
	MetadataSourceChainKey         = "prompt.debug.source_chain"
	MetadataProfileNameKey         = "profile"
	MetadataEnabledLayersKey       = "prompt.debug.enabled_layers"
	MetadataSuppressedLayersKey    = "prompt.debug.suppressed_layers"
	MetadataSuppressionReasonsKey  = "prompt.debug.suppression_reasons"
	MetadataLayerTokensKey         = "prompt.debug.layer_tokens"
	MetadataInstructionProfileKey  = "prompt.debug.instruction_profile"
	MetadataPromptAssemblyKey      = "prompt.assembly"
	MetadataPromptVersionKey       = "prompt.version"
)

func Compose(in ComposeInput) (ComposeOutput, error) {
	graph, err := buildInstructionGraph(in)
	if err != nil {
		return ComposeOutput{}, err
	}
	envelope := renderPromptEnvelope(graph)
	debug := buildComposeDebugMeta(graph, envelope)
	return ComposeOutput{
		Prompt:    envelope.Prompt,
		Envelope:  envelope,
		Graph:     graph,
		DebugMeta: debug,
	}, nil
}

func buildInstructionGraph(in ComposeInput) (InstructionGraph, error) {
	graph := InstructionGraph{
		Profile: resolveInstructionProfile(in.CollaborationMode, in.ProfileName, in.TaskMode),
	}
	baseLayers, baseText, baseSource, err := buildBaseLayers(in)
	if err != nil {
		return InstructionGraph{}, err
	}
	graph.BaseSource = baseSource
	graph.Layers = append(graph.Layers, baseLayers...)
	graph.Layers = append(graph.Layers, buildDynamicLayers(in, baseText)...)
	graph.SourceChain = buildSourceChain(graph.Layers)
	return graph, nil
}

func containsHeading(base, heading string) bool {
	if strings.TrimSpace(base) == "" {
		return false
	}
	needle := "## " + strings.ToLower(strings.TrimSpace(heading))
	for _, line := range strings.Split(strings.ToLower(base), "\n") {
		if strings.TrimSpace(line) == needle {
			return true
		}
	}
	return false
}

func DefaultTemplate() string {
	return defaultSystemPromptTemplate
}

func ResolveBaseTemplate(workspace, trust string) string {
	ctx := config.DefaultTemplateContext(workspace)
	return config.RenderSystemPromptForTrust(workspace, trust, defaultSystemPromptTemplate, ctx)
}

func resolveBaseInstructions(in ComposeInput) (text, source string, err error) {
	cfg := strings.TrimSpace(in.ConfigInstructions)
	if cfg != "" {
		return cfg, "config", nil
	}
	session := strings.TrimSpace(in.SessionInstructions)
	if session != "" {
		return session, "session", nil
	}
	model := strings.TrimSpace(in.ModelInstructions)
	if model != "" {
		return model, "model", nil
	}
	text = strings.TrimSpace(ResolveBaseTemplate(in.Workspace, in.Trust))
	if text == "" {
		return "", "", fmt.Errorf("resolved empty base instructions")
	}
	return text, "template", nil
}

func buildBaseLayers(in ComposeInput) ([]InstructionLayer, string, string, error) {
	type candidate struct {
		id       string
		source   string
		scope    string
		priority int
		content  string
	}
	candidates := []candidate{
		{id: "base_config", source: "config", scope: "base", priority: 400, content: strings.TrimSpace(in.ConfigInstructions)},
		{id: "base_session", source: "session", scope: "base", priority: 300, content: strings.TrimSpace(in.SessionInstructions)},
		{id: "base_model", source: "model", scope: "base", priority: 200, content: strings.TrimSpace(in.ModelInstructions)},
		{id: "base_template", source: "template", scope: "base", priority: 100, content: strings.TrimSpace(ResolveBaseTemplate(in.Workspace, in.Trust))},
	}
	resolvedText, resolvedSource, err := resolveBaseInstructions(in)
	if err != nil {
		return nil, "", "", err
	}
	layers := make([]InstructionLayer, 0, len(candidates))
	for _, item := range candidates {
		layer := InstructionLayer{
			ID:            item.id,
			Source:        item.source,
			Scope:         item.scope,
			Priority:      item.priority,
			Activation:    "priority-first-non-empty",
			Content:       item.content,
			TokenEstimate: session.EstimateTextTokens(item.content),
		}
		switch {
		case item.source == resolvedSource:
			layer.Enabled = true
		case item.content == "":
			layer.SuppressionReason = "empty_content"
		default:
			layer.SuppressionReason = "lower_priority_source"
		}
		layers = append(layers, layer)
	}
	return layers, resolvedText, resolvedSource, nil
}

func buildDynamicLayers(in ComposeInput, baseText string) []InstructionLayer {
	ctx := config.DefaultTemplateContext(in.Workspace)
	layers := []InstructionLayer{
		{
			ID:         "environment",
			Source:     "runtime",
			Scope:      "dynamic",
			Priority:   210,
			Activation: "if_missing_heading:environment",
			Content:    renderEnvironmentSection(ctx),
		},
		{
			ID:         "bootstrap",
			Source:     "bootstrap",
			Scope:      "dynamic",
			Priority:   200,
			Activation: "if_bootstrap_context_present",
			Content:    renderBootstrapSection(in.Workspace, in.Trust),
		},
		{
			ID:         "capabilities",
			Source:     "runtime",
			Scope:      "dynamic",
			Priority:   190,
			Activation: "if_runtime_capabilities_present",
			Content:    renderCapabilitiesSection(in.Kernel),
		},
		{
			ID:         "collaboration_mode",
			Source:     "collaboration",
			Scope:      "dynamic",
			Priority:   180,
			Activation: "if_collaboration_mode_present",
			Content:    renderCollaborationModeSection(in.CollaborationMode, in.TaskMode, in.ProfileName),
		},
		{
			ID:         "permissions_summary",
			Source:     "runtime",
			Scope:      "dynamic",
			Priority:   175,
			Activation: "if_permission_profile_present",
			Content:    renderPermissionSummarySection(in.PermissionProfile),
		},
		{
			ID:         "skills",
			Source:     "skills",
			Scope:      "dynamic",
			Priority:   170,
			Activation: "if_skill_prompts_present",
			Content:    renderCapabilityGuidanceSection(in.Kernel, in.SkillPrompts),
		},
		{
			ID:         "runtime_notices",
			Source:     "runtime",
			Scope:      "dynamic",
			Priority:   160,
			Activation: "if_runtime_notices_present",
			Content:    renderRuntimeNoticesSection(in.RuntimeNotices),
		},
	}
	for i := range layers {
		layers[i].Content = strings.TrimSpace(layers[i].Content)
		layers[i].TokenEstimate = session.EstimateTextTokens(layers[i].Content)
		switch {
		case layers[i].ID == "environment" && containsHeading(baseText, "environment"):
			layers[i].SuppressionReason = "duplicate_heading"
		case layers[i].Content == "":
			layers[i].SuppressionReason = "empty_content"
		default:
			layers[i].Enabled = true
		}
	}
	return layers
}

func renderBootstrapSection(workspace, trust string) string {
	if bctx := bootstrap.LoadWithAppNameAndTrust(workspace, config.AppName(), trust); bctx != nil {
		content := strings.TrimSpace(bctx.SystemPromptSection())
		if content != "" {
			return "## Bootstrap Context\n" + content
		}
	}
	return ""
}

func renderPromptEnvelope(graph InstructionGraph) PromptEnvelope {
	parts := make([]string, 0, len(graph.Layers))
	enabled := make([]string, 0, len(graph.Layers))
	for _, layer := range graph.Layers {
		if !layer.Enabled || strings.TrimSpace(layer.Content) == "" {
			continue
		}
		parts = append(parts, layer.Content)
		enabled = append(enabled, layer.ID)
	}
	return PromptEnvelope{
		Prompt:          strings.Join(parts, "\n\n"),
		EnabledLayerIDs: enabled,
	}
}

func buildSourceChain(layers []InstructionLayer) []string {
	chain := []string{}
	for _, layer := range layers {
		if !layer.Enabled {
			continue
		}
		if layer.Scope == "base" {
			chain = append(chain, "base:"+layer.Source)
			continue
		}
		chain = append(chain, "dynamic:"+layer.ID)
	}
	return chain
}

func buildComposeDebugMeta(graph InstructionGraph, envelope PromptEnvelope) ComposeDebugMeta {
	debug := ComposeDebugMeta{
		BaseSource:          strings.TrimSpace(graph.BaseSource),
		EnabledLayers:       append([]string(nil), envelope.EnabledLayerIDs...),
		SuppressionReasons:  map[string]string{},
		LayerTokenEstimates: map[string]int{},
		SourceChain:         append([]string(nil), graph.SourceChain...),
		InstructionProfile:  strings.TrimSpace(graph.Profile.ID),
		PromptAssembly:      "unified",
	}
	for _, layer := range graph.Layers {
		debug.LayerTokenEstimates[layer.ID] = layer.TokenEstimate
		if !layer.Enabled {
			debug.SuppressedLayers = append(debug.SuppressedLayers, layer.ID)
			if reason := strings.TrimSpace(layer.SuppressionReason); reason != "" {
				debug.SuppressionReasons[layer.ID] = reason
			}
			continue
		}
		if layer.Scope == "dynamic" {
			debug.DynamicSectionID = append(debug.DynamicSectionID, layer.ID)
		}
	}
	if len(debug.SuppressionReasons) == 0 {
		debug.SuppressionReasons = nil
	}
	if len(debug.LayerTokenEstimates) == 0 {
		debug.LayerTokenEstimates = nil
	}
	debug.PromptVersion = computePromptVersion(envelope.Prompt)
	return debug
}

func computePromptVersion(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "unified:empty"
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(prompt))
	return fmt.Sprintf("unified:%x", h.Sum64())
}

func resolveInstructionProfile(collaborationMode, profileName, taskMode string) InstructionProfile {
	profileName = strings.TrimSpace(profileName)
	taskMode = strings.ToLower(strings.TrimSpace(taskMode))
	collaborationMode = effectiveCollaborationMode(collaborationMode, taskMode, profileName)
	profileID := "default"
	switch {
	case collaborationMode != "":
		profileID = collaborationMode
	case taskMode != "":
		profileID = taskMode
	case profileName != "":
		profileID = "profile:" + profileName
	}
	return InstructionProfile{
		ID:                profileID,
		ProfileName:       profileName,
		TaskMode:          taskMode,
		CollaborationMode: collaborationMode,
	}
}

func renderEnvironmentSection(ctx map[string]any) string {
	return strings.TrimSpace(fmt.Sprintf("## Environment\n- Operating system: %v\n- Default shell: %v\n- Workspace root: %v", ctx["OS"], ctx["Shell"], ctx["Workspace"]))
}

func renderCapabilitiesSection(k *kernel.Kernel) string {
	if k == nil {
		return ""
	}
	tools := k.ToolRegistry().List()
	if len(tools) == 0 {
		return ""
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name() < tools[j].Name() })
	var b strings.Builder
	b.WriteString("## Runtime Capabilities\n")
	for _, t := range tools {
		spec := t.Spec()
		desc := strings.TrimSpace(spec.Description)
		if desc == "" {
			desc = "No description."
		}
		fmt.Fprintf(&b, "- **%s** (%s): %s\n", spec.Name, spec.Risk, desc)
	}
	return strings.TrimSpace(b.String())
}

func renderCapabilityGuidanceSection(k *kernel.Kernel, skillPrompts []string) string {
	var parts []string
	for _, p := range skillPrompts {
		if t := strings.TrimSpace(p); t != "" {
			parts = append(parts, t)
		}
	}
	if k != nil {
		if manager, ok := capability.LookupManager(k); ok && manager != nil {
			if add := strings.TrimSpace(manager.SystemPromptAdditions()); add != "" {
				parts = append(parts, add)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Capability Guidance\n" + strings.Join(parts, "\n\n")
}

func renderRuntimeNoticesSection(notices []string) string {
	var parts []string
	for _, n := range notices {
		if t := strings.TrimSpace(n); t != "" {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Runtime Notices\n" + strings.Join(parts, "\n")
}

func renderCollaborationModeSection(collaborationMode, taskMode, profileName string) string {
	collaborationMode = effectiveCollaborationMode(collaborationMode, taskMode, profileName)
	if collaborationMode == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Operating Mode\n")
	fmt.Fprintf(&b, "- Collaboration mode: %s\n", collaborationMode)
	switch collaborationMode {
	case "investigate":
		b.WriteString("- Prefer broad reading, explicit evidence, provenance, and source-backed conclusions.\n")
	case "plan":
		b.WriteString("- Prioritize decomposition, sequencing, and risk-aware implementation planning.\n")
	default:
		b.WriteString("- Prioritize direct implementation with concise verification.\n")
	}
	return strings.TrimSpace(b.String())
}

func renderPermissionSummarySection(permissionProfile string) string {
	permissionProfile = strings.TrimSpace(permissionProfile)
	if permissionProfile == "" {
		return ""
	}
	return strings.TrimSpace("## Permissions Summary\n- Permission profile: " + permissionProfile)
}

func SessionInstructionsFromMetadata(metadata map[string]any) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	raw, ok := metadata[MetadataSessionInstructionsKey]
	if !ok || raw == nil {
		return "", nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("metadata %q must be string", MetadataSessionInstructionsKey)
	}
	return strings.TrimSpace(text), nil
}

func ProfileModeFromMetadata(metadata map[string]any) (profileName, taskMode string, err error) {
	if len(metadata) == 0 {
		return "", "", nil
	}
	if raw, ok := metadata[session.MetadataTaskMode]; ok && raw != nil {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("metadata %q must be string", session.MetadataTaskMode)
		}
		taskMode = strings.TrimSpace(value)
	}
	if raw, ok := metadata[MetadataProfileNameKey]; ok && raw != nil {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("metadata %q must be string", MetadataProfileNameKey)
		}
		profileName = strings.TrimSpace(value)
	}
	return profileName, taskMode, nil
}

func SessionPromptModeFromConfig(cfg session.SessionConfig) (SessionPromptMode, error) {
	profileName, taskMode, err := ProfileModeFromMetadata(cfg.Metadata)
	if err != nil {
		return SessionPromptMode{}, err
	}
	_, preset, _, collaborationMode, promptPack, permissionProfile, _, _ := session.SessionFacetValues(&session.Session{Config: cfg})
	profileName = firstNonEmptyTrimmed(profileName, strings.TrimSpace(cfg.Profile), preset, permissionProfile)
	return SessionPromptMode{
		ProfileName:       profileName,
		TaskMode:          strings.ToLower(strings.TrimSpace(taskMode)),
		CollaborationMode: effectiveCollaborationMode(collaborationMode, taskMode, profileName),
		PermissionProfile: strings.TrimSpace(permissionProfile),
		PromptPack:        strings.TrimSpace(promptPack),
		Preset:            strings.TrimSpace(preset),
	}, nil
}

func effectiveCollaborationMode(collaborationMode, taskMode, profileName string) string {
	if mode := normalizePromptCollaborationMode(collaborationMode); mode != "" {
		return mode
	}
	if mode := legacyTaskModeToCollaborationMode(taskMode); mode != "" {
		return mode
	}
	return legacyTaskModeToCollaborationMode(profileName)
}

func normalizePromptCollaborationMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "plan", "planning":
		return "plan"
	case "investigate", "research":
		return "investigate"
	case "execute", "coding":
		return "execute"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func legacyTaskModeToCollaborationMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "planning", "plan":
		return "plan"
	case "research", "investigate":
		return "investigate"
	case "coding", "execute", "readonly":
		return "execute"
	default:
		return ""
	}
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func AttachComposeDebugMeta(metadata map[string]any, debug ComposeDebugMeta) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
	delete(metadata, MetadataBaseSourceKey)
	delete(metadata, MetadataDynamicSectionsKey)
	delete(metadata, MetadataEnabledLayersKey)
	delete(metadata, MetadataSuppressedLayersKey)
	delete(metadata, MetadataSuppressionReasonsKey)
	delete(metadata, MetadataLayerTokensKey)
	delete(metadata, MetadataSourceChainKey)
	delete(metadata, MetadataInstructionProfileKey)
	delete(metadata, MetadataPromptAssemblyKey)
	delete(metadata, MetadataPromptVersionKey)
	delete(metadata, session.MetadataPromptAssembly)
	delete(metadata, session.MetadataPromptVersion)
	base := strings.TrimSpace(debug.BaseSource)
	if base != "" {
		metadata[MetadataBaseSourceKey] = base
	}
	sections := make([]string, 0, len(debug.DynamicSectionID))
	for _, id := range debug.DynamicSectionID {
		if t := strings.TrimSpace(id); t != "" {
			sections = append(sections, t)
		}
	}
	if len(sections) > 0 {
		metadata[MetadataDynamicSectionsKey] = strings.Join(sections, ",")
	}
	enabled := make([]string, 0, len(debug.EnabledLayers))
	for _, id := range debug.EnabledLayers {
		if t := strings.TrimSpace(id); t != "" {
			enabled = append(enabled, t)
		}
	}
	if len(enabled) > 0 {
		metadata[MetadataEnabledLayersKey] = enabled
	}
	suppressed := make([]string, 0, len(debug.SuppressedLayers))
	for _, id := range debug.SuppressedLayers {
		if t := strings.TrimSpace(id); t != "" {
			suppressed = append(suppressed, t)
		}
	}
	if len(suppressed) > 0 {
		metadata[MetadataSuppressedLayersKey] = suppressed
	}
	if len(debug.SuppressionReasons) > 0 {
		reasons := make(map[string]string, len(debug.SuppressionReasons))
		for id, reason := range debug.SuppressionReasons {
			id = strings.TrimSpace(id)
			reason = strings.TrimSpace(reason)
			if id == "" || reason == "" {
				continue
			}
			reasons[id] = reason
		}
		if len(reasons) > 0 {
			metadata[MetadataSuppressionReasonsKey] = reasons
		}
	}
	if len(debug.LayerTokenEstimates) > 0 {
		tokens := make(map[string]int, len(debug.LayerTokenEstimates))
		for id, count := range debug.LayerTokenEstimates {
			id = strings.TrimSpace(id)
			if id == "" || count <= 0 {
				continue
			}
			tokens[id] = count
		}
		if len(tokens) > 0 {
			metadata[MetadataLayerTokensKey] = tokens
		}
	}
	chain := make([]string, 0, len(debug.SourceChain))
	for _, item := range debug.SourceChain {
		if t := strings.TrimSpace(item); t != "" {
			chain = append(chain, t)
		}
	}
	if len(chain) == 0 {
		if base != "" {
			chain = append(chain, "base:"+base)
		}
		for _, section := range sections {
			chain = append(chain, "dynamic:"+section)
		}
	}
	if len(chain) > 0 {
		metadata[MetadataSourceChainKey] = strings.Join(chain, " -> ")
	}
	if profile := strings.TrimSpace(debug.InstructionProfile); profile != "" {
		metadata[MetadataInstructionProfileKey] = profile
	}
	if assembly := strings.TrimSpace(debug.PromptAssembly); assembly != "" {
		metadata[MetadataPromptAssemblyKey] = assembly
		metadata[session.MetadataPromptAssembly] = assembly
	}
	if version := strings.TrimSpace(debug.PromptVersion); version != "" {
		metadata[MetadataPromptVersionKey] = version
		metadata[session.MetadataPromptVersion] = version
	}
	return metadata
}

func ComposeDebugMetaFromMetadata(metadata map[string]any) (ComposeDebugMeta, error) {
	if len(metadata) == 0 {
		return ComposeDebugMeta{}, nil
	}
	debug := ComposeDebugMeta{
		BaseSource:         strings.TrimSpace(metadataString(metadata, MetadataBaseSourceKey)),
		DynamicSectionID:   metadataStringSlice(metadata, MetadataDynamicSectionsKey),
		EnabledLayers:      metadataStringSlice(metadata, MetadataEnabledLayersKey),
		SuppressedLayers:   metadataStringSlice(metadata, MetadataSuppressedLayersKey),
		SourceChain:        metadataStringSlice(metadata, MetadataSourceChainKey),
		InstructionProfile: strings.TrimSpace(metadataString(metadata, MetadataInstructionProfileKey)),
		PromptAssembly:     strings.TrimSpace(metadataString(metadata, MetadataPromptAssemblyKey)),
		PromptVersion:      strings.TrimSpace(metadataString(metadata, MetadataPromptVersionKey)),
	}
	reasons, err := metadataStringMap(metadata, MetadataSuppressionReasonsKey)
	if err != nil {
		return ComposeDebugMeta{}, err
	}
	debug.SuppressionReasons = reasons
	tokens, err := metadataIntMap(metadata, MetadataLayerTokensKey)
	if err != nil {
		return ComposeDebugMeta{}, err
	}
	debug.LayerTokenEstimates = tokens
	return debug, nil
}

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	var items []string
	switch value := raw.(type) {
	case string:
		for _, part := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				items = append(items, trimmed)
			}
		}
	case []string:
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				items = append(items, trimmed)
			}
		}
	case []any:
		for _, item := range value {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				items = append(items, trimmed)
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func metadataStringMap(metadata map[string]any, key string) (map[string]string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil, nil
	}
	out := map[string]string{}
	switch value := raw.(type) {
	case map[string]string:
		for id, reason := range value {
			if id = strings.TrimSpace(id); id != "" {
				out[id] = strings.TrimSpace(reason)
			}
		}
	case map[string]any:
		for id, reason := range value {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			out[id] = strings.TrimSpace(fmt.Sprint(reason))
		}
	default:
		return nil, fmt.Errorf("metadata %q must be map[string]string", key)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func metadataIntMap(metadata map[string]any, key string) (map[string]int, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil, nil
	}
	out := map[string]int{}
	switch value := raw.(type) {
	case map[string]int:
		for id, count := range value {
			if id = strings.TrimSpace(id); id != "" && count > 0 {
				out[id] = count
			}
		}
	case map[string]any:
		for id, count := range value {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			switch v := count.(type) {
			case int:
				if v > 0 {
					out[id] = v
				}
			case int64:
				if v > 0 {
					out[id] = int(v)
				}
			case float64:
				if v > 0 {
					out[id] = int(v)
				}
			default:
				return nil, fmt.Errorf("metadata %q contains non-numeric token estimate for %q", key, id)
			}
		}
	default:
		return nil, fmt.Errorf("metadata %q must be map[string]int", key)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
