package prompting

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/mossagents/moss/appkit/runtime"
	"github.com/mossagents/moss/bootstrap"
	config "github.com/mossagents/moss/config"
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
	Kernel              *kernel.Kernel
	SkillPrompts        []string
	RuntimeNotices      []string
}

type ComposeOutput struct {
	Prompt    string
	DebugMeta ComposeDebugMeta
}

type ComposeDebugMeta struct {
	BaseSource       string
	DynamicSectionID []string
}

const (
	MetadataSessionInstructionsKey = "prompt.session_instructions"
	MetadataBaseSourceKey          = "prompt.debug.base_source"
	MetadataDynamicSectionsKey     = "prompt.debug.dynamic_sections"
	MetadataSourceChainKey         = "prompt.debug.source_chain"
	MetadataProfileNameKey         = "profile"
)

func Compose(in ComposeInput) (ComposeOutput, error) {
	base, baseSource, err := resolveBaseInstructions(in)
	if err != nil {
		return ComposeOutput{}, err
	}
	base = strings.TrimSpace(base)

	sections := make([]section, 0, 8)
	if !containsHeading(base, "environment") {
		ctx := config.DefaultTemplateContext(in.Workspace)
		sections = append(sections, section{
			ID:      "environment",
			Content: renderEnvironmentSection(ctx),
		})
	}

	if bctx := bootstrap.LoadWithAppNameAndTrust(in.Workspace, config.AppName(), in.Trust); bctx != nil {
		content := strings.TrimSpace(bctx.SystemPromptSection())
		if content != "" {
			content = "## Bootstrap Context\n" + content
		}
		sections = append(sections, section{
			ID:      "bootstrap",
			Content: content,
		})
	}

	caps := renderCapabilitiesSection(in.Kernel)
	sections = append(sections, section{ID: "capabilities", Content: caps})
	sections = append(sections, section{ID: "profile_mode", Content: renderProfileModeSection(in.ProfileName, in.TaskMode)})
	sections = append(sections, section{ID: "skills", Content: renderSkillsSection(in.Kernel, in.SkillPrompts)})
	sections = append(sections, section{ID: "runtime_notices", Content: renderRuntimeNoticesSection(in.RuntimeNotices)})

	var parts []string
	if strings.TrimSpace(base) != "" {
		parts = append(parts, base)
	}
	debug := ComposeDebugMeta{BaseSource: baseSource}
	for _, s := range dedupeSections(sections) {
		if strings.TrimSpace(s.Content) == "" {
			continue
		}
		parts = append(parts, s.Content)
		debug.DynamicSectionID = append(debug.DynamicSectionID, s.ID)
	}

	return ComposeOutput{
		Prompt:    strings.Join(parts, "\n\n"),
		DebugMeta: debug,
	}, nil
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

type section struct {
	ID      string
	Content string
}

func dedupeSections(in []section) []section {
	seen := map[string]struct{}{}
	out := make([]section, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s.ID) == "" {
			continue
		}
		if _, ok := seen[s.ID]; ok {
			continue
		}
		seen[s.ID] = struct{}{}
		out = append(out, s)
	}
	return out
}

func renderEnvironmentSection(ctx map[string]any) string {
	return strings.TrimSpace(fmt.Sprintf("## Environment\n- Operating system: %v\n- Default shell: %v\n- Workspace root: %v", ctx["OS"], ctx["Shell"], ctx["Workspace"]))
}

func renderCapabilitiesSection(k *kernel.Kernel) string {
	if k == nil {
		return ""
	}
	specs := k.ToolRegistry().List()
	if len(specs) == 0 {
		return ""
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	var b strings.Builder
	b.WriteString("## Runtime Capabilities\n")
	for _, spec := range specs {
		desc := strings.TrimSpace(spec.Description)
		if desc == "" {
			desc = "No description."
		}
		fmt.Fprintf(&b, "- **%s** (%s): %s\n", spec.Name, spec.Risk, desc)
	}
	return strings.TrimSpace(b.String())
}

func renderSkillsSection(k *kernel.Kernel, skillPrompts []string) string {
	var parts []string
	for _, p := range skillPrompts {
		if t := strings.TrimSpace(p); t != "" {
			parts = append(parts, t)
		}
	}
	if k != nil {
		if add := strings.TrimSpace(runtime.SkillsManager(k).SystemPromptAdditions()); add != "" {
			parts = append(parts, add)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "## Skills\n" + strings.Join(parts, "\n\n")
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

func renderProfileModeSection(profileName, taskMode string) string {
	profileName = strings.TrimSpace(profileName)
	taskMode = strings.ToLower(strings.TrimSpace(taskMode))
	if profileName == "" && taskMode == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Operating Mode\n")
	if profileName != "" {
		fmt.Fprintf(&b, "- Active profile: %s\n", profileName)
	}
	if taskMode != "" {
		fmt.Fprintf(&b, "- Task mode: %s\n", taskMode)
	}
	switch taskMode {
	case "research":
		b.WriteString("- Prefer broad reading, explicit evidence, and source-backed conclusions.\n")
	case "planning":
		b.WriteString("- Prioritize decomposition, sequencing, and risk-aware implementation planning.\n")
	case "readonly":
		b.WriteString("- Avoid mutating operations unless explicitly approved by the user.\n")
	default:
		if taskMode != "" {
			b.WriteString("- Prioritize direct implementation with concise verification.\n")
		}
	}
	return strings.TrimSpace(b.String())
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

func AttachComposeDebugMeta(metadata map[string]any, debug ComposeDebugMeta) map[string]any {
	if metadata == nil {
		metadata = map[string]any{}
	}
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
	chain := []string{}
	if base != "" {
		chain = append(chain, "base:"+base)
	}
	for _, section := range sections {
		chain = append(chain, "dynamic:"+section)
	}
	if len(chain) > 0 {
		metadata[MetadataSourceChainKey] = strings.Join(chain, " -> ")
	}
	return metadata
}
