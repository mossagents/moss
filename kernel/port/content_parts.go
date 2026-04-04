package port

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
		if strings.TrimSpace(part.MIMEType) != "" || strings.TrimSpace(part.DataBase64) != "" || strings.TrimSpace(part.URL) != "" {
			return fmt.Errorf("%s part forbids mime_type, data_base64, and url", part.Type)
		}
		return nil
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

func expectedMediaMIMEPrefix(typ ContentPartType) string {
	switch typ {
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
