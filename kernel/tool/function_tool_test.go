package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewFunctionTool_TypeSafety(t *testing.T) {
	type GreetArgs struct {
		Name string `json:"name" description:"Person to greet"`
	}
	type GreetResult struct {
		Message string `json:"message"`
	}

	ft := NewFunctionTool(ToolSpec{
		Name:        "greet",
		Description: "Greet someone",
	}, func(_ context.Context, args GreetArgs) (GreetResult, error) {
		return GreetResult{Message: "Hello, " + args.Name + "!"}, nil
	})

	if ft.Name() != "greet" {
		t.Fatalf("Name = %q, want %q", ft.Name(), "greet")
	}
	if ft.Description() != "Greet someone" {
		t.Fatalf("Description = %q, want %q", ft.Description(), "Greet someone")
	}

	// Execute with valid input
	result, err := ft.Execute(context.Background(), json.RawMessage(`{"name":"Alice"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got GreetResult
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got.Message != "Hello, Alice!" {
		t.Fatalf("Message = %q, want %q", got.Message, "Hello, Alice!")
	}
}

func TestNewFunctionTool_InvalidInput(t *testing.T) {
	type Args struct {
		Count int `json:"count"`
	}
	ft := NewFunctionTool(ToolSpec{Name: "counter"}, func(_ context.Context, args Args) (string, error) {
		return "ok", nil
	})

	_, err := ft.Execute(context.Background(), json.RawMessage(`{"count":"not_a_number"}`))
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
	if !strings.Contains(err.Error(), "unmarshal args") {
		t.Fatalf("error = %q, want to contain 'unmarshal args'", err.Error())
	}
}

func TestNewFunctionTool_AutoSchema(t *testing.T) {
	type Args struct {
		Path  string `json:"path" description:"File path"`
		Force bool   `json:"force,omitempty" description:"Force overwrite"`
	}
	ft := NewFunctionTool(ToolSpec{Name: "write"}, func(_ context.Context, args Args) (string, error) {
		return "ok", nil
	})

	spec := ft.Spec()
	if len(spec.InputSchema) == 0 {
		t.Fatal("InputSchema should be auto-generated")
	}

	var schema map[string]any
	if err := json.Unmarshal(spec.InputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type = %v, want 'object'", schema["type"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties missing")
	}
	if _, ok := props["path"]; !ok {
		t.Fatal("schema missing 'path' property")
	}
	if _, ok := props["force"]; !ok {
		t.Fatal("schema missing 'force' property")
	}

	// "path" should be required (no omitempty), "force" should not
	required, _ := schema["required"].([]any)
	hasPath := false
	hasForce := false
	for _, r := range required {
		if r == "path" {
			hasPath = true
		}
		if r == "force" {
			hasForce = true
		}
	}
	if !hasPath {
		t.Fatal("'path' should be in required list")
	}
	if hasForce {
		t.Fatal("'force' should not be in required list (has omitempty)")
	}
}

func TestNewFunctionTool_PreserveExplicitSchema(t *testing.T) {
	explicit := json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`)
	ft := NewFunctionTool(ToolSpec{
		Name:        "test",
		InputSchema: explicit,
	}, func(_ context.Context, args struct{ X string }) (string, error) {
		return args.X, nil
	})

	spec := ft.Spec()
	if string(spec.InputSchema) != string(explicit) {
		t.Fatalf("InputSchema should preserve explicit schema, got %s", spec.InputSchema)
	}
}

func TestNewFunctionTool_EmptyInput(t *testing.T) {
	type Args struct{}
	ft := NewFunctionTool(ToolSpec{Name: "noop"}, func(_ context.Context, _ Args) (string, error) {
		return "done", nil
	})

	result, err := ft.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute with nil args: %v", err)
	}
	var got string
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != "done" {
		t.Fatalf("result = %q, want %q", got, "done")
	}
}

func TestNewRawTool(t *testing.T) {
	spec := ToolSpec{Name: "echo", Description: "Echo input"}
	rt := NewRawTool(spec, func(_ context.Context, input json.RawMessage) (json.RawMessage, error) {
		return input, nil
	})

	if rt.Name() != "echo" {
		t.Fatalf("Name = %q, want %q", rt.Name(), "echo")
	}
	if rt.Description() != "Echo input" {
		t.Fatalf("Description = %q, want %q", rt.Description(), "Echo input")
	}
	if rt.Spec().Name != "echo" {
		t.Fatalf("Spec().Name = %q, want %q", rt.Spec().Name, "echo")
	}

	result, err := rt.Execute(context.Background(), json.RawMessage(`"hello"`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(result) != `"hello"` {
		t.Fatalf("result = %s, want %s", result, `"hello"`)
	}
}

func TestSchemaFor_StructTypes(t *testing.T) {
	type Nested struct {
		Value string `json:"value"`
	}
	type Complex struct {
		Name    string   `json:"name" description:"The name"`
		Count   int      `json:"count"`
		Enabled bool     `json:"enabled,omitempty"`
		Tags    []string `json:"tags,omitempty"`
		Score   float64  `json:"score"`
		Meta    Nested   `json:"meta,omitempty"`
		Status  string   `json:"status" enum:"active,inactive,pending"`
	}

	schema := SchemaFor[Complex]()
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if s["type"] != "object" {
		t.Fatalf("type = %v, want 'object'", s["type"])
	}

	props := s["properties"].(map[string]any)
	// Check name has description
	nameP := props["name"].(map[string]any)
	if nameP["type"] != "string" {
		t.Fatalf("name.type = %v, want 'string'", nameP["type"])
	}
	if nameP["description"] != "The name" {
		t.Fatalf("name.description = %v, want 'The name'", nameP["description"])
	}

	// Check count is integer
	countP := props["count"].(map[string]any)
	if countP["type"] != "integer" {
		t.Fatalf("count.type = %v, want 'integer'", countP["type"])
	}

	// Check score is number
	scoreP := props["score"].(map[string]any)
	if scoreP["type"] != "number" {
		t.Fatalf("score.type = %v, want 'number'", scoreP["type"])
	}

	// Check tags is array
	tagsP := props["tags"].(map[string]any)
	if tagsP["type"] != "array" {
		t.Fatalf("tags.type = %v, want 'array'", tagsP["type"])
	}

	// Check status has enum
	statusP := props["status"].(map[string]any)
	enumArr, ok := statusP["enum"].([]any)
	if !ok || len(enumArr) != 3 {
		t.Fatalf("status.enum = %v, want 3 enum values", statusP["enum"])
	}

	// Check required: name, count, score, status should be required; enabled, tags, meta should not
	required, _ := s["required"].([]any)
	reqSet := make(map[string]bool)
	for _, r := range required {
		reqSet[r.(string)] = true
	}
	for _, want := range []string{"name", "count", "score", "status"} {
		if !reqSet[want] {
			t.Errorf("%q should be required", want)
		}
	}
	for _, notWant := range []string{"enabled", "tags", "meta"} {
		if reqSet[notWant] {
			t.Errorf("%q should NOT be required (has omitempty)", notWant)
		}
	}
}

func TestSchemaFor_EmptyStruct(t *testing.T) {
	type Empty struct{}
	schema := SchemaFor[Empty]()
	var s map[string]any
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s["type"] != "object" {
		t.Fatalf("type = %v, want 'object'", s["type"])
	}
}

func TestToolInterface_Compliance(t *testing.T) {
	// Verify both implementations satisfy Tool interface
	var _ Tool = NewFunctionTool(ToolSpec{Name: "a"}, func(context.Context, struct{}) (string, error) { return "", nil })
	var _ Tool = NewRawTool(ToolSpec{Name: "b"}, func(context.Context, json.RawMessage) (json.RawMessage, error) { return nil, nil })
}
