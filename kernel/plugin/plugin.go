package plugin

import (
	"fmt"
	"strings"

	"github.com/mossagents/moss/kernel/hooks"
)

// Plugin 是生命周期扩展的核心接口。
// 复杂或多阶段的插件直接实现此接口；单阶段插件使用 Group 便捷构造。
type Plugin interface {
	// Name 返回插件唯一标识。
	Name() string
	// Order 返回执行优先级（值越小越先执行）。
	Order() int
	// Install 将 hook/interceptor 注册到 Registry。
	Install(reg *hooks.Registry)
}

// Validate 校验插件合法性。
func Validate(p Plugin) error {
	if strings.TrimSpace(p.Name()) == "" {
		return fmt.Errorf("plugin name is required")
	}
	if g, ok := p.(*Group); ok && g.Empty() {
		return fmt.Errorf("plugin %q must define at least one hook or interceptor", p.Name())
	}
	return nil
}

// Install 校验并安装插件到 Registry。
func Install(reg *hooks.Registry, p Plugin) error {
	if err := Validate(p); err != nil {
		return err
	}
	p.Install(reg)
	return nil
}
