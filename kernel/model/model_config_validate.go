package model

import (
	"fmt"
	"strings"
)

// ExtraFieldKind 表示 ModelConfig.Extra 字段的值类型约束。
type ExtraFieldKind string

const (
	ExtraFieldString ExtraFieldKind = "string"
	ExtraFieldNumber ExtraFieldKind = "number"
	ExtraFieldBool   ExtraFieldKind = "bool"
	ExtraFieldAny    ExtraFieldKind = "any"
)

// ExtraFieldDef 描述 ModelConfig.Extra 中一个字段的约束规则。
type ExtraFieldDef struct {
	// Kind 约束值类型；ExtraFieldAny 表示不限类型。
	Kind ExtraFieldKind
	// AllowedValues 若非空，则值必须在此列表中（仅对 string 类型有效）。
	AllowedValues []string
	// Required 若为 true，Extra 中必须提供此字段。
	Required bool
	// Description 字段描述（仅文档用途）。
	Description string
}

// ExtraSchema 定义 ModelConfig.Extra 字段的合法结构。
type ExtraSchema map[string]ExtraFieldDef

// ValidateModelConfigExtra 按 schema 校验 ModelConfig.Extra 字段。
// 返回首个发现的校验错误；Extra 为 nil 时跳过非必填字段检查。
func ValidateModelConfigExtra(cfg ModelConfig, schema ExtraSchema) error {
	// 检查 schema 中 required 但缺失的字段
	for name, def := range schema {
		if !def.Required {
			continue
		}
		if _, ok := cfg.Extra[name]; !ok {
			return fmt.Errorf("model config extra: required field %q is missing", name)
		}
	}

	// 检查 Extra 中每个字段是否符合 schema 规则
	for name, val := range cfg.Extra {
		def, ok := schema[name]
		if !ok {
			// schema 中未定义的字段视为合法（宽松模式）
			continue
		}
		if err := validateExtraField(name, val, def); err != nil {
			return err
		}
	}
	return nil
}

// ValidateModelConfigExtraStrict 与 ValidateModelConfigExtra 类似，但拒绝 schema 中未定义的字段。
func ValidateModelConfigExtraStrict(cfg ModelConfig, schema ExtraSchema) error {
	if err := ValidateModelConfigExtra(cfg, schema); err != nil {
		return err
	}
	for name := range cfg.Extra {
		if _, ok := schema[name]; !ok {
			return fmt.Errorf("model config extra: unknown field %q (strict mode)", name)
		}
	}
	return nil
}

func validateExtraField(name string, val any, def ExtraFieldDef) error {
	if def.Kind == ExtraFieldAny {
		return nil
	}
	switch def.Kind {
	case ExtraFieldString:
		s, ok := toString(val)
		if !ok {
			return fmt.Errorf("model config extra: field %q must be a string, got %T", name, val)
		}
		if len(def.AllowedValues) > 0 && !containsStr(def.AllowedValues, s) {
			return fmt.Errorf("model config extra: field %q value %q is not in allowed values [%s]",
				name, s, strings.Join(def.AllowedValues, ", "))
		}
	case ExtraFieldNumber:
		if !isNumber(val) {
			return fmt.Errorf("model config extra: field %q must be a number, got %T", name, val)
		}
	case ExtraFieldBool:
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("model config extra: field %q must be a bool, got %T", name, val)
		}
	}
	return nil
}

func toString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

func isNumber(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return true
	}
	return false
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
