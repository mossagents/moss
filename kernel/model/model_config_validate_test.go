package model

import (
	"testing"
)

func TestValidateModelConfigExtra_RequiredMissing(t *testing.T) {
	cfg := ModelConfig{Extra: nil}
	schema := ExtraSchema{
		"timeout": {Kind: ExtraFieldNumber, Required: true},
	}
	if err := ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected error for missing required field")
	}
}

func TestValidateModelConfigExtra_RequiredPresent(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"timeout": 30.0}}
	schema := ExtraSchema{
		"timeout": {Kind: ExtraFieldNumber, Required: true},
	}
	if err := ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateModelConfigExtra_WrongType_String(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"name": 42}}
	schema := ExtraSchema{
		"name": {Kind: ExtraFieldString},
	}
	if err := ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected type error for string field with non-string value")
	}
}

func TestValidateModelConfigExtra_AllowedValues_Valid(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"mode": "fast"}}
	schema := ExtraSchema{
		"mode": {Kind: ExtraFieldString, AllowedValues: []string{"fast", "slow"}},
	}
	if err := ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateModelConfigExtra_AllowedValues_Invalid(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"mode": "turbo"}}
	schema := ExtraSchema{
		"mode": {Kind: ExtraFieldString, AllowedValues: []string{"fast", "slow"}},
	}
	if err := ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected error for value not in allowed list")
	}
}

func TestValidateModelConfigExtra_NumberValid(t *testing.T) {
	for _, v := range []any{1, int64(10), float64(3.14), uint(5)} {
		cfg := ModelConfig{Extra: map[string]any{"n": v}}
		schema := ExtraSchema{"n": {Kind: ExtraFieldNumber}}
		if err := ValidateModelConfigExtra(cfg, schema); err != nil {
			t.Fatalf("expected number %T to be valid, got: %v", v, err)
		}
	}
}

func TestValidateModelConfigExtra_NumberWrongType(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"n": "not-a-number"}}
	schema := ExtraSchema{"n": {Kind: ExtraFieldNumber}}
	if err := ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected type error for number field")
	}
}

func TestValidateModelConfigExtra_BoolValid(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"flag": true}}
	schema := ExtraSchema{"flag": {Kind: ExtraFieldBool}}
	if err := ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateModelConfigExtra_BoolWrongType(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"flag": "yes"}}
	schema := ExtraSchema{"flag": {Kind: ExtraFieldBool}}
	if err := ValidateModelConfigExtra(cfg, schema); err == nil {
		t.Fatal("expected type error for bool field")
	}
}

func TestValidateModelConfigExtra_AnyAcceptsAll(t *testing.T) {
	for _, v := range []any{"str", 42, true, []int{1, 2, 3}} {
		cfg := ModelConfig{Extra: map[string]any{"x": v}}
		schema := ExtraSchema{"x": {Kind: ExtraFieldAny}}
		if err := ValidateModelConfigExtra(cfg, schema); err != nil {
			t.Fatalf("ExtraFieldAny should accept %T, got: %v", v, err)
		}
	}
}

func TestValidateModelConfigExtra_UnknownFieldAllowed(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"mystery": "value"}}
	schema := ExtraSchema{}
	if err := ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("unknown fields should be allowed in non-strict mode, got: %v", err)
	}
}

func TestValidateModelConfigExtraStrict_UnknownFieldRejected(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"mystery": "value"}}
	schema := ExtraSchema{}
	if err := ValidateModelConfigExtraStrict(cfg, schema); err == nil {
		t.Fatal("unknown fields should be rejected in strict mode")
	}
}

func TestValidateModelConfigExtraStrict_KnownFieldValid(t *testing.T) {
	cfg := ModelConfig{Extra: map[string]any{"mode": "fast"}}
	schema := ExtraSchema{
		"mode": {Kind: ExtraFieldString, AllowedValues: []string{"fast", "slow"}},
	}
	if err := ValidateModelConfigExtraStrict(cfg, schema); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateModelConfigExtraStrict_PropagateLenientError(t *testing.T) {
	// Strict should still propagate type errors from lenient validation.
	cfg := ModelConfig{Extra: map[string]any{"count": "not-a-number"}}
	schema := ExtraSchema{"count": {Kind: ExtraFieldNumber}}
	if err := ValidateModelConfigExtraStrict(cfg, schema); err == nil {
		t.Fatal("strict mode should propagate type errors")
	}
}

func TestValidateModelConfigExtra_NilExtra_NoRequired(t *testing.T) {
	cfg := ModelConfig{Extra: nil}
	schema := ExtraSchema{
		"optional": {Kind: ExtraFieldString, Required: false},
	}
	if err := ValidateModelConfigExtra(cfg, schema); err != nil {
		t.Fatalf("nil extra with no required fields should pass, got: %v", err)
	}
}
