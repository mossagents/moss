package main

import (
	"github.com/mossagents/moss/harness/appkit"
	"github.com/mossagents/moss/harness/appkit/product"
	"github.com/mossagents/moss/kernel/observe"
)

const appName = "mosscode"

type sessionRequest struct {
	Preset            string
	CollaborationMode string
	RunMode           string
	PermissionProfile string
	PromptPack        string
	SessionPolicy     string
	ModelProfile      string
}

type config struct {
	flags                     *appkit.AppFlags
	request                   sessionRequest
	debug                     bool
	prompt                    string
	approvalMode              string
	governance                product.GovernanceConfig
	execJSON                  bool
	resumeSessionID           string
	resumeLatest              bool
	configArgs                []string
	doctorJSON                bool
	debugConfigJSON           bool
	reviewJSON                bool
	reviewArgs                []string
	completionArgs            []string
	forkSessionID             string
	forkCheckpointID          string
	forkLatest                bool
	forkRestoreWorktree       bool
	forkJSON                  bool
	checkpointAction          string
	checkpointJSON            bool
	checkpointLimit           int
	checkpointID              string
	checkpointCreateSessionID string
	checkpointCreateNote      string
	checkpointLatest          bool
	checkpointReplayMode      string
	checkpointRestoreWorktree bool
	applyPatchFile            string
	applySummary              string
	applySessionID            string
	applyJSON                 bool
	rollbackChangeID          string
	rollbackJSON              bool
	changesAction             string
	changesJSON               bool
	changesLimit              int
	changesShowID             string
	explicitFlags             []string
	observer                  observe.Observer
	pricingCatalog            *product.PricingCatalog
}

type commandExitError struct {
	code int
}

func (e *commandExitError) Error() string {
	return ""
}

func newConfig() *config {
	return &config{
		flags:      &appkit.AppFlags{},
		governance: product.DefaultGovernanceConfig(),
	}
}
