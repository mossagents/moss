package domain

import "time"

type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

type RunMode string

const (
	RunModeInteractive RunMode = "interactive"
	RunModeSafe        RunMode = "safe"
	RunModeAutopilot   RunMode = "autopilot"
)

type Run struct {
	RunID        string
	Goal         string
	Mode         RunMode
	Workspace    string
	Status       RunStatus
	StartedAt    time.Time
	EndedAt      *time.Time
	FinalResult  string
	ActiveTaskID string
	ArtifactRefs []string
	Budget       *Budget
	Plan         *Plan
}

type Budget struct {
	MaxTokens  int
	MaxSteps   int
	UsedTokens int
	UsedSteps  int
}
