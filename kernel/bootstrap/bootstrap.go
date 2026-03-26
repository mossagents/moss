// Package bootstrap 加载 OpenClaw 风格的引导上下文文件。
//
// 引导文件定义了 Agent 的身份、灵魂、工具提示和用户画像。
// 支持按优先级从项目目录和全局目录加载：
//
//	项目: <workspace>/.agents/, <workspace>/.<appName>/
//	全局: ~/.<appName>/
//
// 文件列表：
//
//	AGENTS.md  — Agent 行为指令 (对标 OpenClaw AGENTS.md)
//	SOUL.md    — 性格 / 世界观 / 沟通风格
//	TOOLS.md   — 可用工具的额外说明
//	IDENTITY.md — Agent 身份标识
//	USER.md    — 用户画像
package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
)

// Context 包含所有引导文件的内容，用于注入 system prompt。
type Context struct {
	Agents   string // AGENTS.md 内容
	Soul     string // SOUL.md 内容
	Tools    string // TOOLS.md 内容
	Identity string // IDENTITY.md 内容
	User     string // USER.md 内容
}

// Empty 返回引导上下文是否为空（没有加载到任何文件）。
func (c *Context) Empty() bool {
	return c.Agents == "" && c.Soul == "" && c.Tools == "" && c.Identity == "" && c.User == ""
}

// SystemPromptSection 将引导上下文格式化为 system prompt 片段。
// 只包含已加载的文件，每个文件用 XML 标签包裹。
func (c *Context) SystemPromptSection() string {
	var parts []string

	add := func(tag, content string) {
		if content != "" {
			parts = append(parts, "<"+tag+">\n"+content+"\n</"+tag+">")
		}
	}

	add("identity", c.Identity)
	add("soul", c.Soul)
	add("agents", c.Agents)
	add("tools", c.Tools)
	add("user", c.User)

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

var bootstrapFiles = []struct {
	field string
	name  string
}{
	{"Agents", "AGENTS.md"},
	{"Soul", "SOUL.md"},
	{"Tools", "TOOLS.md"},
	{"Identity", "IDENTITY.md"},
	{"User", "USER.md"},
}

// appName 默认应用名，可通过 SetAppName 修改。
var appName = "moss"

// SetAppName 设置应用名称，影响全局目录扫描路径。
func SetAppName(name string) { appName = name }

// Load 从工作区和全局目录加载引导上下文。
// 优先级: 项目 .agents/ > 项目 .<appName>/ > 全局 ~/.<appName>/
// 每个文件只取最高优先级的版本。
func Load(workspace string) *Context {
	return LoadWithAppName(workspace, appName)
}

// LoadWithAppName 从工作区和全局目录加载引导上下文，并显式指定应用名。
// 优先级: 项目 .agents/ > 项目 .<appName>/ > 全局 ~/.<appName>/
// 每个文件只取最高优先级的版本。
func LoadWithAppName(workspace, name string) *Context {
	ctx := &Context{}

	// 构建搜索目录列表（优先级从高到低）
	dirs := []string{
		filepath.Join(workspace, ".agents"),
		filepath.Join(workspace, "."+name),
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "."+name))
	}

	for _, bf := range bootstrapFiles {
		for _, dir := range dirs {
			path := filepath.Join(dir, bf.name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			content := strings.TrimSpace(string(data))
			if content == "" {
				continue
			}
			setField(ctx, bf.field, content)
			break // 取最高优先级
		}
	}

	return ctx
}

func setField(ctx *Context, field, value string) {
	switch field {
	case "Agents":
		ctx.Agents = value
	case "Soul":
		ctx.Soul = value
	case "Tools":
		ctx.Tools = value
	case "Identity":
		ctx.Identity = value
	case "User":
		ctx.User = value
	}
}
