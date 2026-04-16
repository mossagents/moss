// Package declarative provides declarative hook configuration for the harness.
// Hooks can be declared in YAML/JSON config and compiled into kernel hook
// plugins without writing Go code.
package declarative

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mossagents/moss/kernel/hooks"
	kplugin "github.com/mossagents/moss/kernel/plugin"
)

// HookEvent represents when a hook fires.
type HookEvent string

const (
	EventPreToolUse  HookEvent = "pre_tool_use"
	EventPostToolUse HookEvent = "post_tool_use"
)

// HookType identifies the execution mechanism for a declarative hook.
type HookType string

const (
	HookTypeCommand HookType = "command"
	HookTypeHTTP    HookType = "http"
	HookTypePrompt  HookType = "prompt"
)

// HookConfig is the YAML/JSON-friendly hook declaration.
type HookConfig struct {
	Name           string        `yaml:"name" json:"name"`
	Type           HookType      `yaml:"type" json:"type"`
	Event          HookEvent     `yaml:"event" json:"event"`
	Match          string        `yaml:"match,omitempty" json:"match,omitempty"` // glob pattern for tool name
	Command        string        `yaml:"command,omitempty" json:"command,omitempty"`
	URL            string        `yaml:"url,omitempty" json:"url,omitempty"`
	Method         string        `yaml:"method,omitempty" json:"method,omitempty"`
	Prompt         string        `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Timeout        time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	BlockOnFailure bool          `yaml:"block_on_failure,omitempty" json:"block_on_failure,omitempty"`
}

// CompileHooks converts declarative hook configs into kernel plugins.
func CompileHooks(configs []HookConfig) ([]*kplugin.Group, error) {
	var plugins []*kplugin.Group
	for _, cfg := range configs {
		p, err := compileHook(cfg)
		if err != nil {
			return nil, fmt.Errorf("compile hook %q: %w", cfg.Name, err)
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

func compileHook(cfg HookConfig) (*kplugin.Group, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("hook name is required")
	}

	var hookFn hooks.Hook[hooks.ToolEvent]
	switch cfg.Type {
	case HookTypeCommand:
		if cfg.Command == "" {
			return nil, fmt.Errorf("command is required for command hook")
		}
		hookFn = commandHook(cfg)
	case HookTypeHTTP:
		if cfg.URL == "" {
			return nil, fmt.Errorf("url is required for http hook")
		}
		hookFn = httpHook(cfg)
	case HookTypePrompt:
		if cfg.Prompt == "" {
			return nil, fmt.Errorf("prompt is required for prompt hook")
		}
		hookFn = promptHook(cfg)
	default:
		return nil, fmt.Errorf("unknown hook type %q", cfg.Type)
	}

	wrappedFn := wrapWithMatch(hookFn, cfg.Match, cfg.Event)

	return kplugin.ToolLifecycleHook(cfg.Name, 0, wrappedFn), nil
}

// wrapWithMatch wraps a hook function with tool name glob matching and event
// stage filtering.
func wrapWithMatch(fn hooks.Hook[hooks.ToolEvent], pattern string, event HookEvent) hooks.Hook[hooks.ToolEvent] {
	return func(ctx context.Context, ev *hooks.ToolEvent) error {
		if ev == nil || ev.Tool == nil {
			return nil
		}
		// Filter by event stage.
		switch event {
		case EventPreToolUse:
			if ev.Stage != hooks.ToolLifecycleBefore {
				return nil
			}
		case EventPostToolUse:
			if ev.Stage != hooks.ToolLifecycleAfter {
				return nil
			}
		}
		// Match tool name if pattern specified.
		if pattern != "" {
			matched, _ := filepath.Match(pattern, ev.Tool.Name)
			if !matched {
				return nil
			}
		}
		return fn(ctx, ev)
	}
}

// hookError returns an error if blockOnFailure is true, otherwise logs and returns nil.
func hookError(hookName, reason string, blockOnFailure bool) error {
	if blockOnFailure {
		return fmt.Errorf("declarative hook %q blocked: %s", hookName, reason)
	}
	return nil
}

// hookTimeout returns the effective timeout, defaulting to 10s.
func hookTimeout(cfg HookConfig) time.Duration {
	if cfg.Timeout > 0 {
		return cfg.Timeout
	}
	return 10 * time.Second
}

// toolEnv builds environment variables for a command hook.
func toolEnv(ev *hooks.ToolEvent) []string {
	if ev == nil || ev.Tool == nil {
		return nil
	}
	env := []string{
		"HOOK_TOOL_NAME=" + ev.Tool.Name,
		"HOOK_TOOL_RISK=" + string(ev.Tool.Risk),
	}
	if ev.Session != nil {
		env = append(env, "HOOK_SESSION_ID="+ev.Session.ID)
	}
	if input := strings.TrimSpace(string(ev.Input)); input != "" && len(input) < 4096 {
		env = append(env, "HOOK_TOOL_INPUT="+input)
	}
	return env
}
