package model

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

func TestValidateContentParts_FileReferenceValid(t *testing.T) {
	err := ValidateContentParts([]ContentPart{
		FileRefPart(
			AttachmentRef{Path: "README.md", Name: "README.md", MIMEType: "text/markdown"},
			&MentionBinding{Trigger: "@", Value: "@README.md", Target: "README.md", Start: 0, End: 10},
		),
	})
	if err != nil {
		t.Fatalf("expected valid file ref part, got error: %v", err)
	}
}

func TestValidateContentParts_FileReferenceRequiresAttachmentLocator(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{
		Type:       ContentPartFileRef,
		Attachment: &AttachmentRef{Name: "README.md"},
	}})
	if err == nil {
		t.Fatal("expected error for attachment without id/path/uri")
	}
}

func TestValidateContentParts_FileReferenceRejectsInvalidMentionRange(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{
		Type:       ContentPartFileRef,
		Attachment: &AttachmentRef{Path: "README.md"},
		Mention:    &MentionBinding{Value: "@README.md", Target: "README.md", Start: 8, End: 4},
	}})
	if err == nil {
		t.Fatal("expected mention range validation error")
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

// ─── factory constructors ────────────────────────────────────────────────────

func TestReasoningPart(t *testing.T) {
	p := ReasoningPart("chain of thought")
	if p.Type != ContentPartReasoning {
		t.Fatalf("expected ContentPartReasoning, got %s", p.Type)
	}
	if p.Text != "chain of thought" {
		t.Fatalf("unexpected text: %q", p.Text)
	}
}

func TestImageInlinePart(t *testing.T) {
	p := ImageInlinePart(ContentPartInputImage, "image/png", "abc123", "/path/img.png")
	if p.Type != ContentPartInputImage {
		t.Fatalf("unexpected type: %s", p.Type)
	}
	if p.MIMEType != "image/png" || p.DataBase64 != "abc123" || p.SourcePath != "/path/img.png" {
		t.Fatalf("unexpected fields: %+v", p)
	}
}

func TestMediaInlinePart_TrimsWhitespace(t *testing.T) {
	p := MediaInlinePart(ContentPartInputImage, "  image/jpeg  ", "  data  ", "  /src  ")
	if p.MIMEType != "image/jpeg" {
		t.Fatalf("mime not trimmed: %q", p.MIMEType)
	}
	if p.DataBase64 != "data" {
		t.Fatalf("data not trimmed: %q", p.DataBase64)
	}
	if p.SourcePath != "/src" {
		t.Fatalf("source path not trimmed: %q", p.SourcePath)
	}
}

func TestImageURLPart(t *testing.T) {
	p := ImageURLPart(ContentPartOutputImage, "https://example.com/img.png", "/local.png")
	if p.Type != ContentPartOutputImage {
		t.Fatalf("unexpected type: %s", p.Type)
	}
	if p.URL != "https://example.com/img.png" || p.SourcePath != "/local.png" {
		t.Fatalf("unexpected fields: %+v", p)
	}
}

func TestMediaURLPart_TrimsWhitespace(t *testing.T) {
	p := MediaURLPart(ContentPartOutputImage, "  http://host/img  ", "  /src  ")
	if p.URL != "http://host/img" {
		t.Fatalf("URL not trimmed: %q", p.URL)
	}
	if p.SourcePath != "/src" {
		t.Fatalf("source path not trimmed: %q", p.SourcePath)
	}
}

func TestStripReasoningParts(t *testing.T) {
	parts := []ContentPart{
		ReasoningPart("internal thought"),
		TextPart("visible answer"),
		ReasoningPart("more internal"),
	}
	stripped := StripReasoningParts(parts)
	for _, p := range stripped {
		if p.Type == ContentPartReasoning {
			t.Fatal("StripReasoningParts should remove all reasoning parts")
		}
	}
	if len(stripped) != 1 || stripped[0].Text != "visible answer" {
		t.Fatalf("unexpected stripped: %+v", stripped)
	}
}

func TestStripReasoningParts_Empty(t *testing.T) {
	if result := StripReasoningParts(nil); result != nil {
		t.Fatal("nil input should return nil")
	}
}

func TestStripReasoningParts_NoReasoning(t *testing.T) {
	parts := []ContentPart{TextPart("a"), TextPart("b")}
	if result := StripReasoningParts(parts); len(result) != 2 {
		t.Fatalf("non-reasoning parts should be preserved; got %d", len(result))
	}
}
