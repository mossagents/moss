package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	kernio "github.com/mossagents/moss/kernel/io"
	"github.com/mossagents/moss/kernel/tool"
)

// ─── ask_user ────────────────────────────────────────

var askUserSpec = tool.ToolSpec{
	Name:        "ask_user",
	Description: "Ask the user a question and wait for their response. Supports free text and schema-driven forms.",
	InputSchema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"question": {"type": "string", "description": "The question to ask the user"},
			"requestedSchema": {"type": "object", "description": "Optional JSON Schema-like definition for structured input"}
		},
		"required": ["question"]
	}`),
	Risk:         tool.RiskLow,
	Capabilities: []string{"interaction"},
}

func askUserHandler(io kernio.UserIO) tool.ToolHandler {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var params struct {
			Question        string         `json:"question"`
			RequestedSchema map[string]any `json:"requestedSchema"`
		}
		if err := unmarshalAskUserInputWithRetry(input, &params); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if io == nil {
			return json.Marshal("ask_user: no user IO available")
		}
		req := kernio.InputRequest{
			Type:   kernio.InputFreeText,
			Prompt: params.Question,
		}
		fields, err := buildAskUserFields(params.RequestedSchema)
		if err != nil {
			return nil, err
		}
		if len(fields) > 0 {
			req.Type = kernio.InputForm
			req.Fields = fields
			req.ConfirmLabel = "Confirm"
		}
		resp, err := io.Ask(ctx, req)
		if err != nil {
			return nil, err
		}
		if req.Type == kernio.InputForm {
			return json.Marshal(resp.Form)
		}
		return json.Marshal(resp.Value)
	}
}

func buildAskUserFields(schema map[string]any) ([]kernio.InputField, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	propsAny, ok := schema["properties"]
	if !ok {
		return nil, nil
	}
	props, ok := propsAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("requestedSchema.properties must be an object")
	}
	requiredSet := map[string]bool{}
	if reqAny, ok := schema["required"].([]any); ok {
		for _, name := range reqAny {
			if s, ok := name.(string); ok {
				requiredSet[s] = true
			}
		}
	} else if reqStr, ok := schema["required"].([]string); ok {
		for _, name := range reqStr {
			requiredSet[name] = true
		}
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]kernio.InputField, 0, len(keys))
	for _, name := range keys {
		rawDef, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		field := kernio.InputField{
			Name:        name,
			Title:       toString(rawDef["title"]),
			Description: toString(rawDef["description"]),
			Required:    requiredSet[name],
		}
		if field.Title == "" {
			field.Title = name
		}
		ftype := strings.ToLower(toString(rawDef["type"]))
		switch ftype {
		case "boolean":
			field.Type = kernio.InputFieldBoolean
		case "array":
			field.Type = kernio.InputFieldMultiSelect
		case "number":
			field.Type = kernio.InputFieldNumber
		case "integer":
			field.Type = kernio.InputFieldInteger
		default:
			if enum := toStringSlice(rawDef["enum"]); len(enum) > 0 {
				field.Type = kernio.InputFieldSingleSelect
				field.Options = enum
			} else {
				field.Type = kernio.InputFieldString
			}
		}
		if field.Type == kernio.InputFieldMultiSelect {
			items, _ := rawDef["items"].(map[string]any)
			field.Options = toStringSlice(items["enum"])
		}
		if def, ok := rawDef["default"]; ok {
			field.Default = normalizeDefaultForField(field.Type, def)
		}
		fields = append(fields, field)
	}
	return fields, nil
}

func toString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func toStringSlice(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func normalizeDefaultForField(ft kernio.InputFieldType, v any) any {
	switch ft {
	case kernio.InputFieldBoolean:
		if b, ok := v.(bool); ok {
			return b
		}
	case kernio.InputFieldNumber:
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
				return parsed
			}
		}
	case kernio.InputFieldInteger:
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				return parsed
			}
		}
	case kernio.InputFieldMultiSelect:
		if vals := toStringSlice(v); len(vals) > 0 {
			return vals
		}
	default:
		if s, ok := v.(string); ok {
			return s
		}
	}
	return v
}

func unmarshalAskUserInputWithRetry(raw json.RawMessage, out any) error {
	if err := json.Unmarshal(raw, out); err == nil {
		return nil
	} else if !isUnexpectedJSONEOF(err) {
		return err
	}

	repaired := repairTruncatedJSON(string(raw))
	if strings.TrimSpace(repaired) == "" {
		return fmt.Errorf("unexpected end of JSON input")
	}
	return json.Unmarshal([]byte(repaired), out)
}

func isUnexpectedJSONEOF(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") || strings.Contains(msg, "unexpected EOF")
}

func repairTruncatedJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	stack := make([]rune, 0, 8)
	inString := false
	escaped := false

	for _, r := range s {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		switch r {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, r)
		case '}':
			if len(stack) > 0 && stack[len(stack)-1] == '{' {
				stack = stack[:len(stack)-1]
			}
		case ']':
			if len(stack) > 0 && stack[len(stack)-1] == '[' {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if inString {
		if escaped {
			s = strings.TrimSuffix(s, `\`)
		}
		s += `"`
	}
	s = strings.TrimRight(s, ", \t\r\n")

	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			s += "}"
		} else {
			s += "]"
		}
	}
	return s
}
