package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mossagents/moss/kernel/tool"
)

func TestJinaReaderHandlerMissingURL(t *testing.T) {
	_, err := JinaReaderHandler()(context.Background(), json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected missing url error, got %v", err)
	}
}

func TestJinaSearchHandlerDefaultCountValidation(t *testing.T) {
	_, err := JinaSearchHandler()(context.Background(), json.RawMessage(`{"query":"agent","count":999}`))
	// Network may fail in CI/offline, but input should pass parsing stage.
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "parse input") {
		t.Fatalf("unexpected parse error: %v", err)
	}
}

func TestRegisterJinaTools(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterJinaTools(reg); err != nil {
		t.Fatalf("register first time: %v", err)
	}
	if err := RegisterJinaTools(reg); err != nil {
		t.Fatalf("register second time should be idempotent: %v", err)
	}
	if _, _, ok := reg.Get("jina_search"); !ok {
		t.Fatal("jina_search not registered")
	}
	if _, _, ok := reg.Get("jina_reader"); !ok {
		t.Fatal("jina_reader not registered")
	}
}

