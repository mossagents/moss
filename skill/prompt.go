package skill

import (
	"bufio"
	"context"
	"fmt"
	"github.com/mossagents/moss/capability"
	appconfig "github.com/mossagents/moss/config"
	"gopkg.in/yaml.v3"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Skill 代表一个 skills.sh 兼容的 SKILL.md prompt skill。
// 它只将 SKILL.md 的正文注入到 system prompt 中，不注册任何工具，
// 也不会替代 runtime builtin tools 或 MCP providers。
type Skill struct {
	name        string
	version     string
	description string
	dependsOn   []string
	requires    []capability.Dependency
	requiredEnv []string
	body        string // markdown 正文（去除 frontmatter 后的内容）
	source      string // 文件来源路径
}

// Manifest 描述一个可发现的 SKILL.md（不包含正文内容）。
// 用于按需激活场景，避免在发现阶段注入全部提示词。
type Manifest struct {
	Name        string                  `json:"name"`
	Version     string                  `json:"version,omitempty"`
	Description string                  `json:"description"`
	DependsOn   []string                `json:"depends_on,omitempty"`
	Requires    []capability.Dependency `json:"requires,omitempty"`
	RequiredEnv []string                `json:"required_env,omitempty"`
	Source      string                  `json:"source"`
}

var _ capability.Provider = (*Skill)(nil)

// skillFrontmatter 是 SKILL.md 的 YAML frontmatter 结构。
type skillFrontmatter struct {
	Name        string                  `yaml:"name"`
	Version     string                  `yaml:"version"`
	Description string                  `yaml:"description"`
	DependsOn   []string                `yaml:"depends_on"`
	Requires    []capability.Dependency `yaml:"requires"`
	RequiredEnv []string                `yaml:"required_env"`
}

// ParseSkillMD 从指定路径解析 SKILL.md 文件。
func ParseSkillMD(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	return ParseSkillMDContent(string(data), path)
}

// ParseSkillMDContent 从内容字符串解析 SKILL.md。
func ParseSkillMDContent(content, source string) (*Skill, error) {
	fm, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter in %s: %w", source, err)
	}

	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return nil, fmt.Errorf("parse YAML frontmatter in %s: %w", source, err)
	}

	if meta.Name == "" {
		return nil, fmt.Errorf("SKILL.md %s: 'name' field is required in frontmatter", source)
	}

	return &Skill{
		name:        meta.Name,
		version:     meta.Version,
		description: meta.Description,
		dependsOn:   append([]string(nil), meta.DependsOn...),
		requires:    append([]capability.Dependency(nil), meta.Requires...),
		requiredEnv: append([]string(nil), meta.RequiredEnv...),
		body:        strings.TrimSpace(body),
		source:      source,
	}, nil
}

func (s *Skill) Metadata() capability.Metadata {
	version := s.version
	if version == "" {
		version = "0.0.0"
	}
	return capability.Metadata{
		Name:        s.name,
		Version:     version,
		Description: s.description,
		Prompts:     []string{s.body},
		DependsOn:   append([]string(nil), s.dependsOn...),
		Requires:    append([]capability.Dependency(nil), s.requires...),
		RequiredEnv: append([]string(nil), s.requiredEnv...),
	}
}

func (s *Skill) Init(_ context.Context, _ capability.Deps) error {
	// 校验 SKILL.md frontmatter 中声明的必要环境变量。
	for _, env := range s.requiredEnv {
		if strings.TrimSpace(os.Getenv(env)) == "" {
			return fmt.Errorf("skill %q requires environment variable %q which is not set", s.name, env)
		}
	}
	return nil
}

func (s *Skill) Shutdown(_ context.Context) error {
	return nil
}

// splitFrontmatter 分离 YAML frontmatter 和 markdown 正文。
// 支持 --- 分隔的 frontmatter 格式。
func splitFrontmatter(content string) (frontmatter, body string, err error) {
	scanner := bufio.NewScanner(strings.NewReader(content))

	// 跳过前导空行
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		if line != "" {
			// 没有 frontmatter
			return "", content, nil
		}
	}

	// 读取 frontmatter 直到下一个 ---
	var fmLines []string
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			found = true
			break
		}
		fmLines = append(fmLines, line)
	}

	if !found {
		return "", content, fmt.Errorf("unterminated frontmatter (missing closing ---)")
	}

	// 剩余内容为 body
	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}

	return strings.Join(fmLines, "\n"), strings.Join(bodyLines, "\n"), nil
}

// DiscoverSkills 扫描标准目录并加载 SKILL.md 内容。
// 按以下优先级扫描（project → global）：
//
//	Project: .agents/skills/, .agent/skills/, .<app>/skills/
//	Global:  ~/.copilot/skills/, ~/.copilot/installed-plugins/**/skills/, ~/.agents/skills/, ~/.agent/skills/, ~/.config/agents/skills/, ~/.<app>/skills/
func DiscoverSkills(workspace string) []*Skill {
	manifests := DiscoverSkillManifests(workspace)
	return discoverSkillsFromManifests(manifests)
}

func DiscoverSkillsForTrust(workspace, trust string) []*Skill {
	manifests := DiscoverSkillManifestsForTrust(workspace, trust)
	return discoverSkillsFromManifests(manifests)
}

func discoverSkillsFromManifests(manifests []Manifest) []*Skill {
	var skills []*Skill
	for _, mf := range manifests {
		s, err := ParseSkillMD(mf.Source)
		if err != nil {
			continue
		}
		skills = append(skills, s)
	}
	return skills
}

// DiscoverSkillManifests 扫描标准目录，返回可按需激活的技能清单（不加载正文）。
func DiscoverSkillManifests(workspace string) []Manifest {
	return DiscoverSkillManifestsWithOptions(workspace, DiscoverOptions{
		IncludeProject:          true,
		IncludeGlobal:           true,
		IncludeInstalledPlugins: true,
	})
}

func DiscoverSkillManifestsForTrust(workspace, trust string) []Manifest {
	return DiscoverSkillManifestsWithOptions(workspace, DiscoverOptions{
		IncludeProject:          appconfig.ProjectAssetsAllowed(trust),
		IncludeGlobal:           true,
		IncludeInstalledPlugins: true,
	})
}

type DiscoverOptions struct {
	IncludeProject          bool
	IncludeGlobal           bool
	IncludeInstalledPlugins bool
}

func DiscoverSkillManifestsWithOptions(workspace string, opts DiscoverOptions) []Manifest {
	var manifests []Manifest

	appDir := "." + appconfig.AppName()
	// Project-level 目录
	projectDirs := []string{
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(workspace, ".agent", "skills"),
		filepath.Join(workspace, appDir, "skills"),
	}

	// Global-level 目录
	home, _ := os.UserHomeDir()
	var globalDirs []string
	if home != "" {
		globalDirs = append(globalDirs,
			filepath.Join(home, ".copilot", "skills"),
			filepath.Join(home, ".agents", "skills"),
			filepath.Join(home, ".agent", "skills"),
			filepath.Join(home, appDir, "skills"),
		)
		globalDirs = append(globalDirs, filepath.Join(home, ".config", "agents", "skills"))
	}

	seen := make(map[string]bool) // 去重：skill name → loaded

	// 扫描 project 目录（优先级高）
	if opts.IncludeProject {
		for _, dir := range projectDirs {
			for _, m := range scanSkillManifestDir(dir) {
				if !seen[m.Name] {
					seen[m.Name] = true
					manifests = append(manifests, m)
				}
			}
		}
	}

	projectNames := make(map[string]bool, len(seen))
	for k := range seen {
		projectNames[k] = true
	}

	// 扫描 global 目录
	if opts.IncludeGlobal {
		for _, dir := range globalDirs {
			for _, m := range scanSkillManifestDir(dir) {
				if seen[m.Name] {
					if projectNames[m.Name] {
						slog.Default().Debug("global skill shadowed by project skill",
							"name", m.Name,
							"global_source", m.Source,
						)
					}
					continue
				}
				seen[m.Name] = true
				manifests = append(manifests, m)
			}
		}
	}
	if opts.IncludeInstalledPlugins && home != "" {
		for _, m := range scanInstalledPluginSkillManifestDirs(filepath.Join(home, ".copilot", "installed-plugins")) {
			if seen[m.Name] {
				if projectNames[m.Name] {
					slog.Default().Debug("installed-plugin skill shadowed by project skill",
						"name", m.Name,
						"plugin_source", m.Source,
					)
				}
				continue
			}
			seen[m.Name] = true
			manifests = append(manifests, m)
		}
	}

	return manifests
}

// scanInstalledPluginSkillManifestDirs 扫描 ~/.copilot/installed-plugins 下所有 */skills 目录。
func scanInstalledPluginSkillManifestDirs(root string) []Manifest {
	var manifests []Manifest
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || !d.IsDir() {
			return nil
		}
		if !strings.EqualFold(d.Name(), "skills") {
			return nil
		}
		manifests = append(manifests, scanSkillManifestDir(path)...)
		return filepath.SkipDir
	})
	return manifests
}

// scanSkillManifestDir 扫描目录中的 SKILL.md 文件。
// 支持两种结构：
//
//	skills/SKILL.md          （根目录直接有 SKILL.md）
//	skills/<name>/SKILL.md   （子目录中有 SKILL.md）
func scanSkillManifestDir(dir string) []Manifest {
	var manifests []Manifest

	// 检查根目录的 SKILL.md
	rootSkill := filepath.Join(dir, "SKILL.md")
	if m, err := parseSkillManifestFile(rootSkill); err == nil {
		manifests = append(manifests, m)
	}

	// 扫描一级子目录
	entries, err := os.ReadDir(dir)
	if err != nil {
		return manifests
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		if m, err := parseSkillManifestFile(skillFile); err == nil {
			manifests = append(manifests, m)
		}
	}

	return manifests
}

func parseSkillManifestFile(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	fm, _, err := splitFrontmatter(string(data))
	if err != nil {
		return Manifest{}, err
	}
	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(fm), &meta); err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(meta.Name) == "" {
		return Manifest{}, fmt.Errorf("missing name")
	}
	return Manifest{
		Name:        strings.TrimSpace(meta.Name),
		Version:     strings.TrimSpace(meta.Version),
		Description: strings.TrimSpace(meta.Description),
		DependsOn:   append([]string(nil), meta.DependsOn...),
		Requires:    append([]capability.Dependency(nil), meta.Requires...),
		RequiredEnv: append([]string(nil), meta.RequiredEnv...),
		Source:      path,
	}, nil
}
