package attachments

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	mdl "github.com/mossagents/moss/kernel/model"
)

type ComposerAttachment struct {
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

func ExpandInlineFileMentions(input, workspace string) (string, error) {
	_ = workspace
	return input, nil
}

func BuildUserContentParts(input, workspace string) ([]mdl.ContentPart, error) {
	_, _, parts, err := BuildComposerSubmission(input, workspace, nil)
	return parts, err
}

func ResolveMentionPath(workspace, raw string) (string, bool) {
	raw = strings.TrimSpace(strings.Trim(raw, `"'.,;:()[]{}<>`))
	if raw == "" {
		return "", false
	}
	candidates := []string{raw}
	if !filepath.IsAbs(raw) && strings.TrimSpace(workspace) != "" {
		candidates = append([]string{filepath.Join(workspace, raw)}, candidates...)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return candidate, true
		}
		return abs, true
	}
	return "", false
}

func IsMediaPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	case ".wav", ".mp3", ".mpeg", ".m4a", ".ogg", ".flac":
		return true
	case ".mp4", ".webm", ".mov", ".avi", ".mkv":
		return true
	default:
		return false
	}
}

func DetectMediaPart(path string, data []byte) (mdl.ContentPartType, string, error) {
	extMIME := strings.ToLower(strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))))
	sniffMIME := strings.ToLower(strings.TrimSpace(http.DetectContentType(data)))
	extFamily := mediaFamily(extMIME)
	sniffFamily := mediaFamily(sniffMIME)

	if extFamily != "" && sniffFamily != "" && extFamily != sniffFamily {
		return "", "", fmt.Errorf("mentioned media %s has mime mismatch: extension=%q, content=%q", path, extMIME, sniffMIME)
	}

	family := extFamily
	if family == "" {
		family = sniffFamily
	}
	if family == "" {
		return "", "", fmt.Errorf("mentioned media %s has unsupported or ambiguous media type (extension=%q, content=%q)", path, extMIME, sniffMIME)
	}

	resolvedMIME := extMIME
	if mediaFamily(resolvedMIME) != family {
		resolvedMIME = sniffMIME
	}
	if mediaFamily(resolvedMIME) != family {
		switch family {
		case "image":
			resolvedMIME = "image/png"
		case "audio":
			resolvedMIME = "audio/wav"
		case "video":
			resolvedMIME = "video/mp4"
		}
	}

	switch family {
	case "image":
		return mdl.ContentPartInputImage, resolvedMIME, nil
	case "audio":
		return mdl.ContentPartInputAudio, resolvedMIME, nil
	case "video":
		return mdl.ContentPartInputVideo, resolvedMIME, nil
	default:
		return "", "", fmt.Errorf("mentioned media %s has unsupported media family %q", path, family)
	}
}

func mediaFamily(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return ""
	}
}

func BuildComposerSubmission(input, workspace string, pending []ComposerAttachment) (string, string, []mdl.ContentPart, error) {
	runText := strings.TrimSpace(input)
	mentions, err := resolveMentionParts(runText, workspace)
	if err != nil {
		return "", "", nil, err
	}
	attachments := make([]ComposerAttachment, 0, len(pending)+len(mentions))
	seen := make(map[string]struct{}, len(pending)+len(mentions))
	appendAttachment := func(item ComposerAttachment) {
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
		path, ok := ResolveMentionPath(workspace, strings.TrimPrefix(token, "@"))
		if !ok {
			return nil, fmt.Errorf("mentioned file %s not found", token)
		}
		key := strings.ToLower(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		start := strings.Index(input, token)
		part, err := buildAttachmentPart(path, &mdl.MentionBinding{
			Trigger: "@",
			Value:   token,
			Target:  path,
			Label:   filepath.Base(path),
			Start:   start,
			End:     start + len(token),
		})
		if err != nil {
			return nil, err
		}
		mentions = append(mentions, resolvedMention{Token: token, Path: path, Part: part})
	}
	return mentions, nil
}

func BuildAttachmentDraft(workspace, raw string) (ComposerAttachment, error) {
	path, ok := ResolveMentionPath(workspace, raw)
	if !ok {
		return ComposerAttachment{}, fmt.Errorf("mentioned path %q was not found", strings.TrimSpace(raw))
	}
	part, err := buildAttachmentPart(path, nil)
	if err != nil {
		return ComposerAttachment{}, err
	}
	return composerAttachmentFromPart(path, part), nil
}

func buildAttachmentPart(path string, mention *mdl.MentionBinding) (mdl.ContentPart, error) {
	if !IsMediaPath(path) {
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
	partType, mimeType, err := DetectMediaPart(path, data)
	if err != nil {
		return mdl.ContentPart{}, err
	}
	part := mdl.MediaInlinePart(partType, mimeType, base64.StdEncoding.EncodeToString(data), path)
	if mention != nil {
		part.Mention = mention
	}
	return part, nil
}

func composerAttachmentFromPart(path string, part mdl.ContentPart) ComposerAttachment {
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
	summary := label
	if strings.TrimSpace(path) != "" {
		summary = strings.TrimSpace(path)
	}
	return ComposerAttachment{
		Key:     strings.ToLower(strings.TrimSpace(path) + "\x00" + kind),
		Label:   label,
		Path:    path,
		Kind:    kind,
		Summary: summary,
		Part:    part,
	}
}
