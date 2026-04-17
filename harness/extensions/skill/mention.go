package skill

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_-]+)`)

// CollectSkillMentions scans user input text for @skillname references and
// returns DynamicFragments for each matched skill whose full content should
// be injected into the prompt context.
func CollectSkillMentions(input string, manifests []Manifest) []session.PromptContextFragment {
	if input == "" || len(manifests) == 0 {
		return nil
	}

	byName := make(map[string]Manifest, len(manifests))
	for _, m := range manifests {
		byName[strings.ToLower(m.Name)] = m
	}

	matches := mentionRe.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var fragments []session.PromptContextFragment

	for _, match := range matches {
		name := strings.ToLower(match[1])
		if seen[name] {
			continue
		}
		seen[name] = true

		mf, ok := byName[name]
		if !ok {
			continue
		}

		skill, err := ParseSkillMD(mf.Source)
		if err != nil {
			continue
		}
		if strings.TrimSpace(skill.body) == "" {
			continue
		}

		text := fmt.Sprintf("<skill><name>%s</name>\n%s\n</skill>", skill.name, skill.body)
		fragments = append(fragments, session.NewPromptContextFragment(
			"skill:"+skill.name,
			"skill",
			model.RoleSystem,
			skill.name,
			text,
		))
	}

	return fragments
}

// BuildSkillCatalogFragment creates a lightweight startup fragment listing all
// available skills. This replaces full skill body injection in the system prompt.
func BuildSkillCatalogFragment(manifests []Manifest) session.PromptContextFragment {
	if len(manifests) == 0 {
		return session.PromptContextFragment{}
	}

	var lines []string
	for _, m := range manifests {
		desc := strings.TrimSpace(m.Description)
		if desc == "" {
			desc = "no description"
		}
		lines = append(lines, fmt.Sprintf("- @%s: %s", m.Name, desc))
	}

	text := "<available_skills>\nUse @skillname in your message to activate a skill.\n" +
		strings.Join(lines, "\n") + "\n</available_skills>"

	return session.NewPromptContextFragment(
		"skill_catalog",
		"skill",
		model.RoleSystem,
		"available skills",
		text,
	)
}
