package kernel

import (
	"context"
	"github.com/mossagents/moss/kernel/io"
	kt "github.com/mossagents/moss/kernel/testing"
	"strings"
	"testing"
)

func TestBoot_RequiresLLMAndUserIO(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{
			name:    "no LLM no UserIO",
			opts:    nil,
			wantErr: "LLM port is required",
		},
		{
			name:    "LLM only",
			opts:    []Option{WithLLM(&kt.MockLLM{})},
			wantErr: "UserIO port is not set",
		},
		{
			name: "both set",
			opts: []Option{WithLLM(&kt.MockLLM{}), WithUserIO(&io.NoOpIO{})},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := New(tt.opts...)
			err := k.Boot(context.Background())
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestBoot_AllowsRepairAfterValidationFailure(t *testing.T) {
	k := New()
	if err := k.Boot(context.Background()); err == nil {
		t.Fatal("expected validation error")
	}

	k.Apply(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot after repair: %v", err)
	}
}

func TestBoot_RejectsSecondBoot(t *testing.T) {
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
	)
	if err := k.Boot(context.Background()); err != nil {
		t.Fatalf("Boot: %v", err)
	}
	err := k.Boot(context.Background())
	if err == nil {
		t.Fatal("expected repeated boot to fail")
	}
	if !strings.Contains(err.Error(), "can only run once") {
		t.Fatalf("error %q should mention repeated boot", err.Error())
	}
}

func TestBoot_RejectsServingBeforeBoot(t *testing.T) {
	k := New(
		WithLLM(&kt.MockLLM{}),
		WithUserIO(&io.NoOpIO{}),
	)
	_, runID, cancel, err := k.beginRunContext(context.Background(), "sess-boot-order", 0)
	if err != nil {
		t.Fatalf("beginRunContext: %v", err)
	}
	defer cancel()
	defer k.runs.end(runID)

	err = k.Boot(context.Background())
	if err == nil {
		t.Fatal("expected boot after serving start to fail")
	}
	if !strings.Contains(err.Error(), "before serving work starts") {
		t.Fatalf("error %q should mention serving order", err.Error())
	}
}
