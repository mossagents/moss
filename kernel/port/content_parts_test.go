package port

import (
	"strings"
	"testing"
)

func TestValidateContentParts_TextValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartText, Text: "hello"}})
	if err != nil {
		t.Fatalf("expected valid text part, got error: %v", err)
	}
}

func TestValidateContentParts_TextInvalidFields(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartText, Text: "hello", URL: "https://x"}})
	if err == nil {
		t.Fatal("expected error for text part with url")
	}
}

func TestValidateContentParts_ImageInlineValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputImage, MIMEType: "image/png", DataBase64: "abcd"}})
	if err != nil {
		t.Fatalf("expected valid inline image part, got error: %v", err)
	}
}

func TestValidateContentParts_ImageURLValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartOutputImage, URL: "https://example.com/a.png"}})
	if err != nil {
		t.Fatalf("expected valid url image part, got error: %v", err)
	}
}

func TestValidateContentParts_ImageInvalidBothSources(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputImage, MIMEType: "image/png", DataBase64: "abcd", URL: "https://x"}})
	if err == nil {
		t.Fatal("expected error when both inline and url are set")
	}
}

func TestValidateContentParts_ImageInvalidMime(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputImage, MIMEType: "application/pdf", DataBase64: "abcd"}})
	if err == nil {
		t.Fatal("expected error for non-image mime type")
	}
}

func TestValidateContentParts_AudioURLValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputAudio, URL: "https://example.com/a.wav"}})
	if err != nil {
		t.Fatalf("expected valid url audio part, got error: %v", err)
	}
}

func TestValidateContentParts_AudioInlineValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartOutputAudio, MIMEType: "audio/wav", DataBase64: "abcd"}})
	if err != nil {
		t.Fatalf("expected valid inline audio part, got error: %v", err)
	}
}

func TestValidateContentParts_AudioInlineInvalidMime(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputAudio, MIMEType: "video/mp4", DataBase64: "abcd"}})
	if err == nil {
		t.Fatal("expected error for non-audio mime type")
	}
	if !strings.Contains(err.Error(), "audio/") {
		t.Fatalf("expected audio mime prefix error, got: %v", err)
	}
}

func TestValidateContentParts_VideoURLValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputVideo, URL: "https://example.com/a.mp4"}})
	if err != nil {
		t.Fatalf("expected valid url video part, got error: %v", err)
	}
}

func TestValidateContentParts_VideoInlineValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartOutputVideo, MIMEType: "video/mp4", DataBase64: "abcd"}})
	if err != nil {
		t.Fatalf("expected valid inline video part, got error: %v", err)
	}
}

func TestValidateContentParts_VideoInlineInvalidMime(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputVideo, MIMEType: "audio/mpeg", DataBase64: "abcd"}})
	if err == nil {
		t.Fatal("expected error for non-video mime type")
	}
	if !strings.Contains(err.Error(), "video/") {
		t.Fatalf("expected video mime prefix error, got: %v", err)
	}
}

func TestValidateContentParts_MixedMediaRoundtripValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{
		{Type: ContentPartText, Text: "hello"},
		{Type: ContentPartInputImage, URL: "https://example.com/a.png"},
		{Type: ContentPartInputAudio, URL: "https://example.com/a.wav"},
		{Type: ContentPartInputVideo, URL: "https://example.com/a.mp4"},
	})
	if err != nil {
		t.Fatalf("expected mixed content parts to validate, got: %v", err)
	}
}

func TestContentPartsToPlainText(t *testing.T) {
	out := ContentPartsToPlainText([]ContentPart{
		{Type: ContentPartText, Text: "a"},
		{Type: ContentPartReasoning, Text: "hidden"},
		{Type: ContentPartInputImage, URL: "https://example.com/a.png"},
		{Type: ContentPartText, Text: "b"},
	})
	if out != "a\nb" {
		t.Fatalf("plain text = %q, want %q", out, "a\nb")
	}
}

func TestValidateContentParts_ReasoningValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartReasoning, Text: "first inspect the page"}})
	if err != nil {
		t.Fatalf("expected valid reasoning part, got error: %v", err)
	}
}

func TestContentPartsToReasoningText(t *testing.T) {
	out := ContentPartsToReasoningText([]ContentPart{
		{Type: ContentPartReasoning, Text: "a"},
		{Type: ContentPartText, Text: "visible"},
		{Type: ContentPartReasoning, Text: "b"},
	})
	if out != "a\nb" {
		t.Fatalf("reasoning text = %q, want %q", out, "a\nb")
	}
}
