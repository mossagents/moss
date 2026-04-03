package port

import "testing"

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

func TestValidateContentParts_ReservedTypesRejected(t *testing.T) {
	err := ValidateContentParts([]ContentPart{{Type: ContentPartInputAudio, URL: "https://example.com/a.wav"}})
	if err == nil {
		t.Fatal("expected reserved type rejection")
	}
}

func TestContentPartsToPlainText(t *testing.T) {
	out := ContentPartsToPlainText([]ContentPart{
		{Type: ContentPartText, Text: "a"},
		{Type: ContentPartInputImage, URL: "https://example.com/a.png"},
		{Type: ContentPartText, Text: "b"},
	})
	if out != "a\nb" {
		t.Fatalf("plain text = %q, want %q", out, "a\nb")
	}
}
