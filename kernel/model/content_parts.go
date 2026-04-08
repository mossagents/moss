package model

import (
	"fmt"
	"strings"
)

func TextPart(text string) ContentPart {
	return ContentPart{Type: ContentPartText, Text: text}
}

func ReasoningPart(text string) ContentPart {
	return ContentPart{Type: ContentPartReasoning, Text: text}
}

func FileRefPart(ref AttachmentRef, mention *MentionBinding) ContentPart {
	part := ContentPart{
		Type:       ContentPartFileRef,
		SourcePath: strings.TrimSpace(ref.Path),
		Attachment: normalizeAttachmentRef(ref),
	}
	if mention != nil {
		part.Mention = normalizeMentionBinding(*mention)
	}
	return part
}

func ImageInlinePart(typ ContentPartType, mimeType, dataBase64, sourcePath string) ContentPart {
	return MediaInlinePart(typ, mimeType, dataBase64, sourcePath)
}

func MediaInlinePart(typ ContentPartType, mimeType, dataBase64, sourcePath string) ContentPart {
	return ContentPart{
		Type:       typ,
		MIMEType:   strings.TrimSpace(mimeType),
		DataBase64: strings.TrimSpace(dataBase64),
		SourcePath: strings.TrimSpace(sourcePath),
	}
}

func ImageURLPart(typ ContentPartType, url, sourcePath string) ContentPart {
	return MediaURLPart(typ, url, sourcePath)
}

func MediaURLPart(typ ContentPartType, url, sourcePath string) ContentPart {
	return ContentPart{
		Type:       typ,
		URL:        strings.TrimSpace(url),
		SourcePath: strings.TrimSpace(sourcePath),
	}
}

func ContentPartsToPlainText(parts []ContentPart) string {
	if len(parts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != ContentPartText {
			continue
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func ContentPartsToReasoningText(parts []ContentPart) string {
	if len(parts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != ContentPartReasoning {
			continue
		}
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}

func StripReasoningParts(parts []ContentPart) []ContentPart {
	if len(parts) == 0 {
		return nil
	}
	out := make([]ContentPart, 0, len(parts))
	for _, p := range parts {
		if p.Type == ContentPartReasoning {
			continue
		}
		out = append(out, p)
	}
	return out
}

func ValidateContentParts(parts []ContentPart) error {
	for i, part := range parts {
		if err := validateContentPart(part); err != nil {
			return fmt.Errorf("content part %d: %w", i, err)
		}
	}
	return nil
}

func validateContentPart(part ContentPart) error {
	switch part.Type {
	case ContentPartText, ContentPartReasoning:
		if strings.TrimSpace(part.Text) == "" {
			return fmt.Errorf("%s part requires non-empty text", part.Type)
		}
		if strings.TrimSpace(part.MIMEType) != "" || strings.TrimSpace(part.DataBase64) != "" || strings.TrimSpace(part.URL) != "" || part.Attachment != nil || part.Mention != nil {
			return fmt.Errorf("%s part forbids mime_type, data_base64, url, attachment, and mention", part.Type)
		}
		return nil
	case ContentPartFileRef:
		return validateFileReferencePart(part)
	case ContentPartInputImage, ContentPartOutputImage:
		return validateMediaContentPart(part, "image/")
	case ContentPartInputAudio, ContentPartOutputAudio, ContentPartInputVideo, ContentPartOutputVideo:
		return validateMediaContentPart(part, expectedMediaMIMEPrefix(part.Type))
	default:
		return fmt.Errorf("unsupported content part type %q", part.Type)
	}
}

func validateMediaContentPart(part ContentPart, expectedMIMEPrefix string) error {
	if strings.TrimSpace(part.Text) != "" {
		return fmt.Errorf("%s forbids text", part.Type)
	}
	mimeType := strings.TrimSpace(part.MIMEType)
	data := strings.TrimSpace(part.DataBase64)
	url := strings.TrimSpace(part.URL)

	hasInline := mimeType != "" || data != ""
	hasURL := url != ""

	switch {
	case hasInline && hasURL:
		return fmt.Errorf("%s requires exactly one source: inline or url", part.Type)
	case !hasInline && !hasURL:
		return fmt.Errorf("%s requires one source: inline or url", part.Type)
	case hasInline:
		if mimeType == "" || data == "" {
			return fmt.Errorf("%s inline source requires both mime_type and data_base64", part.Type)
		}
		if !strings.HasPrefix(strings.ToLower(mimeType), expectedMIMEPrefix) {
			return fmt.Errorf("%s mime_type must start with %s", part.Type, expectedMIMEPrefix)
		}
	case hasURL:
		if mimeType != "" || data != "" {
			return fmt.Errorf("%s url source forbids mime_type and data_base64", part.Type)
		}
	}
	return nil
}

func validateFileReferencePart(part ContentPart) error {
	if strings.TrimSpace(part.Text) != "" || strings.TrimSpace(part.MIMEType) != "" || strings.TrimSpace(part.DataBase64) != "" || strings.TrimSpace(part.URL) != "" {
		return fmt.Errorf("%s forbids text, mime_type, data_base64, and url", part.Type)
	}
	ref := normalizeAttachmentRefValue(part.Attachment)
	if ref == nil {
		return fmt.Errorf("%s requires attachment", part.Type)
	}
	if strings.TrimSpace(ref.ID) == "" && strings.TrimSpace(ref.Path) == "" && strings.TrimSpace(ref.URI) == "" {
		return fmt.Errorf("%s attachment requires id, path, or uri", part.Type)
	}
	if mention := normalizeMentionBindingValue(part.Mention); mention != nil {
		if strings.TrimSpace(mention.Value) == "" && strings.TrimSpace(mention.Target) == "" {
			return fmt.Errorf("%s mention requires value or target", part.Type)
		}
		if mention.Start < 0 || mention.End < 0 {
			return fmt.Errorf("%s mention offsets must be non-negative", part.Type)
		}
		if mention.End > 0 && mention.End < mention.Start {
			return fmt.Errorf("%s mention end must be >= start", part.Type)
		}
	}
	return nil
}

func expectedMediaMIMEPrefix(typ ContentPartType) string {
	switch typ {
	case ContentPartFileRef:
		return ""
	case ContentPartInputImage, ContentPartOutputImage:
		return "image/"
	case ContentPartInputAudio, ContentPartOutputAudio:
		return "audio/"
	case ContentPartInputVideo, ContentPartOutputVideo:
		return "video/"
	default:
		return ""
	}
}

func normalizeAttachmentRef(ref AttachmentRef) *AttachmentRef {
	normalized := AttachmentRef{
		ID:        strings.TrimSpace(ref.ID),
		Name:      strings.TrimSpace(ref.Name),
		Path:      strings.TrimSpace(ref.Path),
		URI:       strings.TrimSpace(ref.URI),
		MIMEType:  strings.TrimSpace(ref.MIMEType),
		SizeBytes: ref.SizeBytes,
		Digest:    strings.TrimSpace(ref.Digest),
		Source:    strings.TrimSpace(ref.Source),
	}
	if normalized == (AttachmentRef{}) {
		return nil
	}
	return &normalized
}

func normalizeAttachmentRefValue(ref *AttachmentRef) *AttachmentRef {
	if ref == nil {
		return nil
	}
	return normalizeAttachmentRef(*ref)
}

func normalizeMentionBinding(binding MentionBinding) *MentionBinding {
	normalized := MentionBinding{
		Trigger: strings.TrimSpace(binding.Trigger),
		Value:   strings.TrimSpace(binding.Value),
		Target:  strings.TrimSpace(binding.Target),
		Label:   strings.TrimSpace(binding.Label),
		Start:   binding.Start,
		End:     binding.End,
	}
	if normalized == (MentionBinding{}) {
		return nil
	}
	return &normalized
}

func normalizeMentionBindingValue(binding *MentionBinding) *MentionBinding {
	if binding == nil {
		return nil
	}
	return normalizeMentionBinding(*binding)
}
