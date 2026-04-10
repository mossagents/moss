package kernel

import (
	"context"
	"github.com/mossagents/moss/kernel/io"
	kt "github.com/mossagents/moss/testing"
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
