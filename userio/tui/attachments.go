package tui

import (
	"encoding/base64"
	"fmt"
	mdl "github.com/mossagents/moss/kernel/model"
	"os"
	"path/filepath"
	"strings"
)

type composerAttachment struct {
	Key     string
	Label   string
	Path    string
	Kind    string
	Summary string
	Part    mdl.ContentPart
}

type resolvedMention struct {
	Token string
	Path  string
	Part  mdl.ContentPart
}

func buildComposerSubmission(input, workspace string, pending []composerAttachment) (string, string, []mdl.ContentPart, error) {
	runText := strings.TrimSpace(input)
	mentions, err := resolveMentionParts(runText, workspace)
	if err != nil {
		return "", "", nil, err
	}
	attachments := make([]composerAttachment, 0, len(pending)+len(mentions))
	seen := make(map[string]struct{}, len(pending)+len(mentions))
	appendAttachment := func(item composerAttachment) {
		if strings.TrimSpace(item.Key) == "" {
			item.Key = strings.ToLower(strings.TrimSpace(item.Path) + "\x00" + strings.TrimSpace(item.Kind))
		}
		if _, ok := seen[item.Key]; ok {
			return
		}
		seen[item.Key] = struct{}{}
		attachments = append(attachments, item)
	}
	for _, item := range pending {
		appendAttachment(item)
	}
	for _, mention := range mentions {
		appendAttachment(composerAttachmentFromPart(mention.Path, mention.Part))
	}
	parts := make([]mdl.ContentPart, 0, len(attachments)+1)
	if runText != "" {
		parts = append(parts, mdl.TextPart(runText))
	}
	for _, item := range attachments {
		parts = append(parts, item.Part)
	}
	displayText := strings.TrimSpace(runText)
	if len(attachments) > 0 {
		lines := make([]string, 0, len(attachments))
		for _, item := range attachments {
			lines = append(lines, fmt.Sprintf("- [%s] %s", item.Kind, item.Label))
		}
		if displayText != "" {
			displayText += "\n\n"
		}
		displayText += "Attachments:\n" + strings.Join(lines, "\n")
	}
	return displayText, runText, parts, nil
}

func resolveMentionParts(input, workspace string) ([]resolvedMention, error) {
	if !strings.Contains(input, "@") {
		return nil, nil
	}
	tokens := strings.Fields(input)
	mentions := make([]resolvedMention, 0, 4)
	seen := make(map[string]struct{})
	for _, token := range tokens {
		if !strings.HasPrefix(token, "@") || len(token) == 1 {
			continue
		}
		path, ok := resolveMentionPath(workspace, strings.TrimPrefix(token, "@"))
		if !ok {
			return nil, fmt.Errorf("mentioned file %s not found", token)
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		part, err := buildAttachmentPart(path, &mdl.MentionBinding{
			Trigger: "@",
			Value:   token,
			Target:  path,
			Label:   filepath.Base(path),
			Start:   strings.Index(input, token),
			End:     strings.Index(input, token) + len(token),
		})
		if err != nil {
			return nil, err
		}
		mentions = append(mentions, resolvedMention{Token: token, Path: path, Part: part})
	}
	return mentions, nil
}

func buildAttachmentDraft(workspace, raw string) (composerAttachment, error) {
	path, ok := resolveMentionPath(workspace, raw)
	if !ok {
		return composerAttachment{}, fmt.Errorf("mentioned path %q was not found", strings.TrimSpace(raw))
	}
	part, err := buildAttachmentPart(path, nil)
	if err != nil {
		return composerAttachment{}, err
	}
	return composerAttachmentFromPart(path, part), nil
}

func buildAttachmentPart(path string, mention *mdl.MentionBinding) (mdl.ContentPart, error) {
	if !isMediaPath(path) {
		info, err := os.Stat(path)
		if err != nil {
			return mdl.ContentPart{}, fmt.Errorf("stat attachment %s: %w", path, err)
		}
		return mdl.FileRefPart(mdl.AttachmentRef{
			Name:      filepath.Base(path),
			Path:      path,
			SizeBytes: info.Size(),
			Source:    "composer",
		}, mention), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return mdl.ContentPart{}, fmt.Errorf("read media attachment %s: %w", path, err)
	}
	partType, mimeType, err := detectMediaPart(path, data)
	if err != nil {
		return mdl.ContentPart{}, err
	}
	part := mdl.MediaInlinePart(partType, mimeType, base64.StdEncoding.EncodeToString(data), path)
	if mention != nil {
		part.Mention = mention
	}
	return part, nil
}

func composerAttachmentFromPart(path string, part mdl.ContentPart) composerAttachment {
	label := filepath.Base(path)
	if strings.TrimSpace(label) == "" {
		label = path
	}
	kind := "file"
	switch part.Type {
	case mdl.ContentPartInputImage:
		kind = "image"
	case mdl.ContentPartInputAudio:
		kind = "audio"
	case mdl.ContentPartInputVideo:
		kind = "video"
	}
	summary := valueOrDefaultString(path, label)
	return composerAttachment{
		Key:     strings.ToLower(strings.TrimSpace(path) + "\x00" + kind),
		Label:   label,
		Path:    path,
		Kind:    kind,
		Summary: summary,
		Part:    part,
	}
}
