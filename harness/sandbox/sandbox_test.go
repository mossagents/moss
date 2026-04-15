package sandbox

import (
	"testing"
)

func TestLimitsExceeded_AllWithinLimits(t *testing.T) {
	limits := ResourceLimits{
		MaxMemoryBytes: 1024,
		MaxCPUPercent:  80,
		MaxDiskBytes:   2048,
		MaxProcesses:   10,
		MaxOpenFiles:   100,
	}
	usage := ResourceUsage{
		MemoryBytes:   512,
		CPUPercent:    40.0,
		DiskBytes:     1024,
		ProcessCount:  5,
		OpenFileCount: 50,
	}
	violations := LimitsExceeded(limits, usage)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %v", violations)
	}
}

func TestLimitsExceeded_AllExceeded(t *testing.T) {
	limits := ResourceLimits{
		MaxMemoryBytes: 1024,
		MaxCPUPercent:  80,
		MaxDiskBytes:   2048,
		MaxProcesses:   10,
		MaxOpenFiles:   100,
	}
	usage := ResourceUsage{
		MemoryBytes:   2048,
		CPUPercent:    95.0,
		DiskBytes:     4096,
		ProcessCount:  20,
		OpenFileCount: 200,
	}
	violations := LimitsExceeded(limits, usage)
	if len(violations) != 5 {
		t.Errorf("expected 5 violations, got %d: %v", len(violations), violations)
	}
}

func TestLimitsExceeded_ZeroMeansUnlimited(t *testing.T) {
	limits := ResourceLimits{} // all zero = unlimited
	usage := ResourceUsage{
		MemoryBytes:   999999,
		CPUPercent:    100.0,
		DiskBytes:     999999,
		ProcessCount:  999,
		OpenFileCount: 999,
	}
	violations := LimitsExceeded(limits, usage)
	if len(violations) != 0 {
		t.Errorf("expected no violations when limits are zero (unlimited), got %v", violations)
	}
}

func TestLimitsExceeded_PartialLimits(t *testing.T) {
	limits := ResourceLimits{
		MaxMemoryBytes: 1024,
		// other limits left at zero (unlimited)
	}
	usage := ResourceUsage{
		MemoryBytes:   2048,
		CPUPercent:    100.0,
		DiskBytes:     999999,
		ProcessCount:  999,
		OpenFileCount: 999,
	}
	violations := LimitsExceeded(limits, usage)
	if len(violations) != 1 {
		t.Errorf("expected 1 violation (memory only), got %d: %v", len(violations), violations)
	}
}

func TestLimitsExceeded_ExactlyAtLimit(t *testing.T) {
	limits := ResourceLimits{
		MaxMemoryBytes: 1024,
		MaxCPUPercent:  80,
		MaxProcesses:   10,
	}
	usage := ResourceUsage{
		MemoryBytes:  1024,
		CPUPercent:   80.0,
		ProcessCount: 10,
	}
	violations := LimitsExceeded(limits, usage)
	if len(violations) != 0 {
		t.Errorf("expected no violations at exact limit, got %v", violations)
	}
}
