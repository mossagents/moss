package skillsx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
	"github.com/mossagents/moss/skill"
)

const stateKey kernel.ExtensionStateKey = "skillsx.state"

type state struct {
	manager                *skill.Manager
	manifests              []skill.Manifest
	progressive            bool
	progressiveToolsLoaded bool
}

// WithManager 替换当前 Skill Manager。
func WithManager(m *skill.Manager) kernel.Option {
	return func(k *kernel.Kernel) {
		ensureState(k).manager = m
	}
}

// Manager 返回当前 Kernel 绑定的 Skill Manager。
func Manager(k *kernel.Kernel) *skill.Manager {
	return ensureState(k).manager
}

// SetManifests 设置可按需激活的技能清单。
func SetManifests(k *kernel.Kernel, manifests []skill.Manifest) {
	st := ensureState(k)
	st.manifests = append([]skill.Manifest(nil), manifests...)
}

// Manifests 返回当前可按需激活的技能清单。
func Manifests(k *kernel.Kernel) []skill.Manifest {
	st := ensureState(k)
	return append([]skill.Manifest(nil), st.manifests...)
}

// EnableProgressive 启用按需技能提示模式。
func EnableProgressive(k *kernel.Kernel) {
	ensureState(k).progressive = true
}

// RegisterProgressiveTools 注册按需技能加载工具。
func RegisterProgressiveTools(k *kernel.Kernel) error {
	st := ensureState(k)
	if st.progressiveToolsLoaded {
		return nil
	}
	if err := k.ToolRegistry().Register(tool.ToolSpec{
		Name:        "list_skills",
		Description: "List discovered SKILL.md skills and whether each one has been activated.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Risk:        tool.RiskLow,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		manifests := append([]skill.Manifest(nil), st.manifests...)
		resp := make([]map[string]any, 0, len(manifests))
		for _, mf := range manifests {
			_, loaded := st.manager.Get(mf.Name)
			resp = append(resp, map[string]any{
				"name":        mf.Name,
				"description": mf.Description,
				"source":      mf.Source,
				"loaded":      loaded,
			})
		}
		return json.Marshal(resp)
	}); err != nil {
		return err
	}
	if err := k.ToolRegistry().Register(tool.ToolSpec{
		Name:        "activate_skill",
		Description: "Load one discovered SKILL.md into the active prompt context by skill name.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Skill name to activate"}},"required":["name"]}`),
		Risk:        tool.RiskLow,
	}, func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if _, ok := st.manager.Get(name); ok {
			return json.Marshal(map[string]string{
				"status": "already_loaded",
				"name":   name,
			})
		}
		var found *skill.Manifest
		for i := range st.manifests {
			if st.manifests[i].Name == name {
				found = &st.manifests[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("skill %q not found in discovered manifests", name)
		}
		ps, err := skill.ParseSkillMD(found.Source)
		if err != nil {
			return nil, fmt.Errorf("load skill %q: %w", name, err)
		}
		if err := st.manager.Register(ctx, ps, Deps(k)); err != nil {
			return nil, fmt.Errorf("activate skill %q: %w", name, err)
		}
		return json.Marshal(map[string]string{
			"status": "loaded",
			"name":   name,
		})
	}); err != nil {
		return err
	}
	st.progressiveToolsLoaded = true
	return nil
}

// Deps 返回 Skill 注册所需的依赖集合。
func Deps(k *kernel.Kernel) skill.Deps {
	return skill.Deps{
		ToolRegistry: k.ToolRegistry(),
		Middleware:   k.Middleware(),
		Sandbox:      k.Sandbox(),
		UserIO:       k.UserIO(),
		Workspace:    k.Workspace(),
		Executor:     k.Executor(),
	}
}

func ensureState(k *kernel.Kernel) *state {
	bridge := kernel.Extensions(k)
	actual, loaded := bridge.LoadOrStoreState(stateKey, &state{
		manager: skill.NewManager(),
	})
	st := actual.(*state)
	if loaded {
		return st
	}
	bridge.OnShutdown(300, func(ctx context.Context, _ *kernel.Kernel) error {
		if st.manager == nil {
			return nil
		}
		return st.manager.ShutdownAll(ctx)
	})
	bridge.OnSystemPrompt(200, func(_ *kernel.Kernel) string {
		if st.manager == nil {
			return ""
		}
		additions := st.manager.SystemPromptAdditions()
		if !st.progressive || len(st.manifests) == 0 {
			return additions
		}
		names := make([]string, 0, len(st.manifests))
		for _, mf := range st.manifests {
			if _, loaded := st.manager.Get(mf.Name); loaded {
				continue
			}
			names = append(names, mf.Name)
		}
		if len(names) == 0 {
			return additions
		}
		hint := "Discovered skills are available on demand. Use `list_skills` to browse and `activate_skill` to load one when needed: " + strings.Join(names, ", ")
		if additions == "" {
			return hint
		}
		return additions + "\n\n" + hint
	})
	return st
}
