package skill

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	appconfig "github.com/mossagents/moss/kernel/config"
	"gopkg.in/yaml.v3"
)

// Skill 代表一个 skills.sh 兼容的 SKILL.md 文件。
// 它将 SKILL.md 的正文注入到 system prompt 中，不注册任何工具。
type Skill struct {
	name        string
	description string
	body        string // markdown 正文（去除 frontmatter 后的内容）
	source      string // 文件来源路径
}

var _ Provider = (*Skill)(nil)

// skillFrontmatter 是 SKILL.md 的 YAML frontmatter 结构。
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
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
		description: meta.Description,
		body:        strings.TrimSpace(body),
		source:      source,
	}, nil
}

func (s *Skill) Metadata() Metadata {
	return Metadata{
		Name:        s.name,
		Version:     "0.0.0",
		Description: s.description,
		Prompts:     []string{s.body},
	}
}

func (s *Skill) Init(_ context.Context, _ Deps) error {
	return nil // 纯提示词 skill，无需初始化
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

// DiscoverSkills 扫描标准目录，发现所有 SKILL.md 文件。
// 按以下优先级扫描（project → global）：
//
//	Project: .agents/skills/, .moss/skills/
//	Global:  ~/.copilot/skills/, ~/.config/agents/skills/, ~/.moss/skills/
func DiscoverSkills(workspace string) []*Skill {
	var skills []*Skill

	// Project-level 目录
	projectDirs := []string{
		filepath.Join(workspace, ".agents", "skills"),
		filepath.Join(workspace, "."+appconfig.AppName(), "skills"),
	}

	// Global-level 目录
	home, _ := os.UserHomeDir()
	var globalDirs []string
	if home != "" {
		globalDirs = append(globalDirs,
			filepath.Join(home, ".copilot", "skills"),
			filepath.Join(home, "."+appconfig.AppName(), "skills"),
		)
		if runtime.GOOS != "windows" {
			globalDirs = append(globalDirs, filepath.Join(home, ".config", "agents", "skills"))
		} else {
			globalDirs = append(globalDirs, filepath.Join(home, ".config", "agents", "skills"))
		}
	}

	seen := make(map[string]bool) // 去重：skill name → loaded

	// 扫描 project 目录（优先级高）
	for _, dir := range projectDirs {
		for _, s := range scanSkillDir(dir) {
			if !seen[s.name] {
				seen[s.name] = true
				skills = append(skills, s)
			}
		}
	}

	// 扫描 global 目录
	for _, dir := range globalDirs {
		for _, s := range scanSkillDir(dir) {
			if !seen[s.name] {
				seen[s.name] = true
				skills = append(skills, s)
			}
		}
	}

	return skills
}

// scanSkillDir 扫描目录中的 SKILL.md 文件。
// 支持两种结构：
//
//	skills/SKILL.md          （根目录直接有 SKILL.md）
//	skills/<name>/SKILL.md   （子目录中有 SKILL.md）
func scanSkillDir(dir string) []*Skill {
	var skills []*Skill

	// 检查根目录的 SKILL.md
	rootSkill := filepath.Join(dir, "SKILL.md")
	if s, err := ParseSkillMD(rootSkill); err == nil {
		skills = append(skills, s)
	}

	// 扫描一级子目录
	entries, err := os.ReadDir(dir)
	if err != nil {
		return skills
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, entry.Name(), "SKILL.md")
		if s, err := ParseSkillMD(skillFile); err == nil {
			skills = append(skills, s)
		}
	}

	return skills
}
