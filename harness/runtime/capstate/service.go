package capstate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	rootcap "github.com/mossagents/moss/harness/extensions/capability"
	"github.com/mossagents/moss/harness/extensions/skill"
	"github.com/mossagents/moss/kernel"
	"github.com/mossagents/moss/kernel/tool"
)

type State struct {
	manager                *rootcap.Manager
	manifests              []skill.Manifest
	progressive            bool
	progressiveToolsLoaded bool
}

func Ensure(k *kernel.Kernel) *State {
	if k == nil {
		return nil
	}
	actual, loaded := k.Services().LoadOrStore(rootcap.KernelStateServiceKey, &State{
		manager: rootcap.NewManager(),
	})
	st := actual.(*State)
	if loaded {
		return st
	}
	if err := k.Stages().OnShutdown(300, func(ctx context.Context, _ *kernel.Kernel) error {
		if st.manager == nil {
			return nil
		}
		return st.manager.ShutdownAll(ctx)
	}); err != nil {
		log.Printf("capstate: register shutdown hook: %v", err)
	}
	if err := k.Prompts().Add(200, func(_ *kernel.Kernel) string {
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
	}); err != nil {
		log.Printf("capstate: register prompt hook: %v", err)
	}
	return st
}

func Lookup(k *kernel.Kernel) (*State, bool) {
	if k == nil {
		return nil, false
	}
	actual, ok := k.Services().Load(rootcap.KernelStateServiceKey)
	if !ok || actual == nil {
		return nil, false
	}
	st, ok := actual.(*State)
	if !ok || st == nil {
		return nil, false
	}
	return st, true
}

func Manager(k *kernel.Kernel) *rootcap.Manager {
	st := Ensure(k)
	if st == nil {
		return nil
	}
	if st.manager == nil {
		st.manager = rootcap.NewManager()
	}
	return st.manager
}

func (st *State) CapabilityManager() *rootcap.Manager {
	if st == nil {
		return nil
	}
	return st.manager
}

func LookupManager(k *kernel.Kernel) (*rootcap.Manager, bool) {
	st, ok := Lookup(k)
	if !ok || st.manager == nil {
		return nil, false
	}
	return st.manager, true
}

func LookupSkillManifests(k *kernel.Kernel) []skill.Manifest {
	st, ok := Lookup(k)
	if !ok {
		return nil
	}
	return append([]skill.Manifest(nil), st.manifests...)
}

func SetManager(k *kernel.Kernel, m *rootcap.Manager) {
	if st := Ensure(k); st != nil {
		st.manager = m
	}
}

func SetSkillManifests(k *kernel.Kernel, manifests []skill.Manifest) {
	if st := Ensure(k); st != nil {
		st.manifests = append([]skill.Manifest(nil), manifests...)
	}
}

func EnableProgressiveSkills(k *kernel.Kernel) {
	if st := Ensure(k); st != nil {
		st.progressive = true
	}
}

func RegisterProgressiveSkillTools(k *kernel.Kernel) error {
	st := Ensure(k)
	if st == nil {
		return fmt.Errorf("kernel is nil")
	}
	if st.progressiveToolsLoaded {
		return nil
	}
	manager := Manager(k)
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
		Name:        "list_skills",
		Description: "List discovered SKILL.md skills and whether each one has been activated.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Risk:        tool.RiskLow,
	}, func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
		manifests := append([]skill.Manifest(nil), st.manifests...)
		resp := make([]map[string]any, 0, len(manifests))
		for _, mf := range manifests {
			_, loaded := manager.Get(mf.Name)
			resp = append(resp, map[string]any{
				"name":         mf.Name,
				"description":  mf.Description,
				"depends_on":   append([]string(nil), mf.DependsOn...),
				"required_env": append([]string(nil), mf.RequiredEnv...),
				"source":       mf.Source,
				"loaded":       loaded,
			})
		}
		return json.Marshal(resp)
	})); err != nil {
		return err
	}
	if err := k.ToolRegistry().Register(tool.NewRawTool(tool.ToolSpec{
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
		if _, ok := manager.Get(name); ok {
			return json.Marshal(map[string]string{"status": "already_loaded", "name": name})
		}
		if err := activateManifestRecursive(ctx, manager, st.manifests, name, CapabilityDeps(k), nil); err != nil {
			return nil, fmt.Errorf("activate skill %q: %w", name, err)
		}
		return json.Marshal(map[string]string{"status": "loaded", "name": name})
	})); err != nil {
		return err
	}
	st.progressiveToolsLoaded = true
	return nil
}

func CapabilityDeps(k *kernel.Kernel) rootcap.Deps {
	return rootcap.Deps{
		Kernel:       k,
		ToolRegistry: k.ToolRegistry(),
		UserIO:       k.UserIO(),
		Workspace:    k.Workspace(),
		TaskRuntime:  k.TaskRuntime(),
		Mailbox:      k.Mailbox(),
		SessionStore: k.SessionStore(),
	}
}

func activateManifestRecursive(ctx context.Context, manager *rootcap.Manager, manifests []skill.Manifest, target string, deps rootcap.Deps, stack map[string]bool) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("skill name is required")
	}
	if _, ok := manager.Get(target); ok {
		return nil
	}
	if stack == nil {
		stack = make(map[string]bool)
	}
	if stack[target] {
		return fmt.Errorf("dependency cycle detected at %q", target)
	}
	var found *skill.Manifest
	for i := range manifests {
		if manifests[i].Name == target {
			found = &manifests[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("skill %q not found in discovered manifests", target)
	}
	stack[target] = true
	for _, dep := range found.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, ok := manager.Get(dep); ok {
			continue
		}
		if err := activateManifestRecursive(ctx, manager, manifests, dep, deps, stack); err != nil {
			return err
		}
	}
	delete(stack, target)
	ps, err := skill.ParseSkillMD(found.Source)
	if err != nil {
		return fmt.Errorf("load skill %q: %w", target, err)
	}
	return manager.Register(ctx, ps, deps)
}
