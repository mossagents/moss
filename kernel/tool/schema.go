package tool

import (
	"encoding/json"
	"reflect"
	"strings"
)

// SchemaFor 从 Go 类型自动生成 JSON Schema。
// T 通常是一个 struct，字段通过 json tag 命名，通过 description tag 描述。
//
// 示例:
//
//	type Args struct {
//	    Path  string `json:"path"  description:"文件路径"`
//	    Force bool   `json:"force" description:"是否强制执行"`
//	}
//	schema := tool.SchemaFor[Args]()
func SchemaFor[T any]() json.RawMessage {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return json.RawMessage(`{}`)
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	schema := typeToSchema(t)
	b, _ := json.Marshal(schema)
	return b
}

type jsonSchema struct {
	Type        string                `json:"type"`
	Description string                `json:"description,omitempty"`
	Properties  map[string]jsonSchema `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *jsonSchema           `json:"items,omitempty"`
	Enum        []string              `json:"enum,omitempty"`
}

func typeToSchema(t reflect.Type) jsonSchema {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return structToSchema(t)
	case reflect.Slice, reflect.Array:
		items := typeToSchema(t.Elem())
		return jsonSchema{Type: "array", Items: &items}
	case reflect.Map:
		return jsonSchema{Type: "object"}
	case reflect.String:
		return jsonSchema{Type: "string"}
	case reflect.Bool:
		return jsonSchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return jsonSchema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return jsonSchema{Type: "number"}
	default:
		return jsonSchema{Type: "string"}
	}
}

func structToSchema(t reflect.Type) jsonSchema {
	props := make(map[string]jsonSchema, t.NumField())
	var required []string

	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts := parseJSONTag(f)
		if name == "-" {
			continue
		}
		prop := typeToSchema(f.Type)
		if desc := f.Tag.Get("description"); desc != "" {
			prop.Description = desc
		}
		if enum := f.Tag.Get("enum"); enum != "" {
			prop.Enum = strings.Split(enum, ",")
		}
		props[name] = prop
		if !opts.omitempty {
			required = append(required, name)
		}
	}
	return jsonSchema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

type tagOpts struct {
	omitempty bool
}

func parseJSONTag(f reflect.StructField) (string, tagOpts) {
	tag := f.Tag.Get("json")
	if tag == "" || tag == "-" {
		if tag == "-" {
			return "-", tagOpts{}
		}
		return f.Name, tagOpts{}
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = f.Name
	}
	opts := tagOpts{}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			opts.omitempty = true
		}
	}
	return name, opts
}
