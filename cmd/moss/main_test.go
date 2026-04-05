package main

import (
	"strings"
	"testing"
)

func TestStripLeadingDebugArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "no args", args: nil, want: nil},
		{name: "plain version", args: []string{"version"}, want: []string{"version"}},
		{name: "leading debug", args: []string{"--debug", "version"}, want: []string{"version"}},
		{name: "leading debug equals", args: []string{"--debug=true", "run", "--goal", "hi"}, want: []string{"run", "--goal", "hi"}},
		{name: "invalid leading debug kept", args: []string{"--debug=bogus", "version"}, want: []string{"--debug=bogus", "version"}},
		{name: "non leading debug kept", args: []string{"run", "--debug", "--goal", "hi"}, want: []string{"run", "--debug", "--goal", "hi"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripLeadingDebugArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("len=%d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("arg[%d]=%q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestUsageTextIncludesProductCommands(t *testing.T) {
	usage := usageText()
	for _, want := range []string{"moss doctor [flags]", "moss review [args]", "moss inspect [args]"} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage missing %q:\n%s", want, usage)
		}
	}
}
