package plugin

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/hooks"
	"github.com/mossagents/moss/kernel/session"
)

// Plugin is the shared lifecycle extension model used by kernel-facing helpers.
type Plugin struct {
	Name  string
	Order int

	BeforeLLM          hooks.Hook[hooks.LLMEvent]
	AfterLLM           hooks.Hook[hooks.LLMEvent]
	OnSessionLifecycle hooks.Hook[session.LifecycleEvent]
	OnToolLifecycle    hooks.Hook[hooks.ToolEvent]
	OnError            hooks.Hook[hooks.ErrorEvent]

	BeforeLLMInterceptor          hooks.Interceptor[hooks.LLMEvent]
	AfterLLMInterceptor           hooks.Interceptor[hooks.LLMEvent]
	OnSessionLifecycleInterceptor hooks.Interceptor[session.LifecycleEvent]
	OnToolLifecycleInterceptor    hooks.Interceptor[hooks.ToolEvent]
	OnErrorInterceptor            hooks.Interceptor[hooks.ErrorEvent]
}

func Validate(p Plugin) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("plugin name is required")
	}
	if p.BeforeLLM == nil &&
		p.AfterLLM == nil &&
		p.OnSessionLifecycle == nil &&
		p.OnToolLifecycle == nil &&
		p.OnError == nil &&
		p.BeforeLLMInterceptor == nil &&
		p.AfterLLMInterceptor == nil &&
		p.OnSessionLifecycleInterceptor == nil &&
		p.OnToolLifecycleInterceptor == nil &&
		p.OnErrorInterceptor == nil {
		return fmt.Errorf("plugin %q must define at least one hook or interceptor", p.Name)
	}
	return nil
}

func Install(reg *hooks.Registry, p Plugin) {
	if err := Validate(p); err != nil {
		panic(err)
	}
	if p.BeforeLLMInterceptor != nil {
		reg.BeforeLLM.AddInterceptor(p.Name, p.BeforeLLMInterceptor, p.Order)
	}
	if p.BeforeLLM != nil {
		reg.BeforeLLM.AddHook(p.Name, p.BeforeLLM, p.Order)
	}
	if p.AfterLLMInterceptor != nil {
		reg.AfterLLM.AddInterceptor(p.Name, p.AfterLLMInterceptor, p.Order)
	}
	if p.AfterLLM != nil {
		reg.AfterLLM.AddHook(p.Name, p.AfterLLM, p.Order)
	}
	if p.OnSessionLifecycleInterceptor != nil {
		reg.OnSessionLifecycle.AddInterceptor(p.Name, p.OnSessionLifecycleInterceptor, p.Order)
	}
	if p.OnSessionLifecycle != nil {
		reg.OnSessionLifecycle.AddHook(p.Name, p.OnSessionLifecycle, p.Order)
	}
	if p.OnToolLifecycleInterceptor != nil {
		reg.OnToolLifecycle.AddInterceptor(p.Name, p.OnToolLifecycleInterceptor, p.Order)
	}
	if p.OnToolLifecycle != nil {
		reg.OnToolLifecycle.AddHook(p.Name, p.OnToolLifecycle, p.Order)
	}
	if p.OnErrorInterceptor != nil {
		reg.OnError.AddInterceptor(p.Name, p.OnErrorInterceptor, p.Order)
	}
	if p.OnError != nil {
		reg.OnError.AddHook(p.Name, p.OnError, p.Order)
	}
}
