package product

import (
	appconfig "github.com/mossagents/moss/harness/config"
	"strings"
	"testing"
)

func TestNormalizePersonality(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", PersonalityFriendly},
		{"friendly", PersonalityFriendly},
		{"FRIENDLY", PersonalityFriendly},
		{"  Friendly  ", PersonalityFriendly},
		{"pragmatic", PersonalityPragmatic},
		{"PRAGMATIC", PersonalityPragmatic},
		{"none", PersonalityNone},
		{"NONE", PersonalityNone},
		{"unknown", ""},
		{"invalid-name", ""},
	}
	for _, tc := range cases {
		got := NormalizePersonality(tc.input)
		if got != tc.want {
			t.Errorf("NormalizePersonality(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestValidatePersonality(t *testing.T) {
	validCases := []string{"friendly", "pragmatic", "none", "", "FRIENDLY"}
	for _, c := range validCases {
		if err := ValidatePersonality(c); err != nil {
			t.Errorf("ValidatePersonality(%q) unexpected error: %v", c, err)
		}
	}

	if err := ValidatePersonality("bogus"); err == nil {
		t.Error("ValidatePersonality(bogus) should return error")
	}
}

func TestNormalizeExperimentalFeature(t *testing.T) {
	// Valid features
	for _, name := range []string{
		ExperimentalBackgroundPS,
		ExperimentalComposerMentions,
		ExperimentalStatuslineCustomization,
		ExperimentalApps,
		ExperimentalMultimodalImages,
	} {
		got := NormalizeExperimentalFeature(name)
		if got != name {
			t.Errorf("NormalizeExperimentalFeature(%q) = %q, want %q", name, got, name)
		}
		// Case-insensitive
		got = NormalizeExperimentalFeature(strings.ToUpper(name))
		if got != name {
			t.Errorf("NormalizeExperimentalFeature(upper %q) = %q, want %q", name, got, name)
		}
	}

	// Unknown
	if got := NormalizeExperimentalFeature("not-a-feature"); got != "" {
		t.Errorf("expected empty for unknown feature, got %q", got)
	}
}

func TestValidateExperimentalFeature(t *testing.T) {
	if err := ValidateExperimentalFeature(ExperimentalApps); err != nil {
		t.Errorf("unexpected error for valid feature: %v", err)
	}
	if err := ValidateExperimentalFeature("bogus"); err == nil {
		t.Error("expected error for unknown feature")
	}
}

func TestDefaultExperimentalFeatures(t *testing.T) {
	defaults := DefaultExperimentalFeatures()
	if len(defaults) == 0 {
		t.Fatal("expected non-empty default features")
	}
	// All defaults should be valid feature names
	for _, f := range defaults {
		if NormalizeExperimentalFeature(f) == "" {
			t.Errorf("default feature %q is not a valid feature name", f)
		}
	}
}

func TestExperimentalFeatureEnabled(t *testing.T) {
	// Unknown feature → always false
	cfg := appconfig.TUIConfig{}
	if ExperimentalFeatureEnabled(cfg, "bogus") {
		t.Error("unknown feature should not be enabled")
	}

	// Empty cfg.Experimental → uses defaults
	for _, def := range DefaultExperimentalFeatures() {
		if !ExperimentalFeatureEnabled(cfg, def) {
			t.Errorf("default feature %q should be enabled with empty cfg", def)
		}
	}

	// Non-default feature with empty experimental → false
	nonDefault := ExperimentalApps
	if ExperimentalFeatureEnabled(cfg, nonDefault) {
		t.Errorf("non-default feature %q should not be enabled with empty cfg", nonDefault)
	}

	// Explicit cfg.Experimental overrides defaults
	cfg.Experimental = []string{ExperimentalApps}
	if !ExperimentalFeatureEnabled(cfg, ExperimentalApps) {
		t.Error("explicitly enabled feature should return true")
	}
	if ExperimentalFeatureEnabled(cfg, ExperimentalBackgroundPS) {
		t.Error("non-listed feature should return false when experimental is explicit")
	}
}

func TestSupportedExperimentalFeatures(t *testing.T) {
	features := SupportedExperimentalFeatures()
	if len(features) == 0 {
		t.Fatal("expected non-empty supported features")
	}
	// Should be sorted
	for i := 1; i < len(features); i++ {
		if features[i] < features[i-1] {
			t.Errorf("features not sorted: %q < %q at index %d", features[i], features[i-1], i)
		}
	}
}

func TestExperimentalFeatureDescription(t *testing.T) {
	desc := ExperimentalFeatureDescription(ExperimentalApps)
	if desc == "" {
		t.Errorf("expected non-empty description for %q", ExperimentalApps)
	}
	if ExperimentalFeatureDescription("unknown") != "" {
		t.Error("expected empty description for unknown feature")
	}
}

func TestRenderExperimentalFeatures(t *testing.T) {
	cfg := appconfig.TUIConfig{Experimental: []string{ExperimentalApps}}
	out := RenderExperimentalFeatures(cfg)
	if !strings.Contains(out, "Experimental features:") {
		t.Error("missing header")
	}
	if !strings.Contains(out, ExperimentalApps) {
		t.Errorf("missing %q in output", ExperimentalApps)
	}
	if !strings.Contains(out, "[enabled]") {
		t.Error("expected [enabled] for apps feature")
	}
	if !strings.Contains(out, "[disabled]") {
		t.Error("expected [disabled] for other features")
	}
	if !strings.Contains(out, "/experimental") {
		t.Error("expected usage instructions")
	}
}
