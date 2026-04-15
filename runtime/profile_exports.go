package runtime

// This file re-exports all public symbols from runtime/profile so that callers
// using the runtime package directly do not need import-path changes.

import (
	rtprofile "github.com/mossagents/moss/runtime/profile"
)

// Type aliases.
type (
	ProfileResolveOptions = rtprofile.ProfileResolveOptions
	ResolvedProfile       = rtprofile.ResolvedProfile
	SessionPosture        = rtprofile.SessionPosture
)

// Function re-exports.
var (
	ProfileNamesForWorkspace           = rtprofile.ProfileNamesForWorkspace
	ResolveProfileForWorkspace         = rtprofile.ResolveProfileForWorkspace
	ResolveProfileFromPosture          = rtprofile.ResolveProfileFromPosture
	SessionPostureFromResolvedProfile  = rtprofile.SessionPostureFromResolvedProfile
	ResolveSessionPostureForWorkspace  = rtprofile.ResolveSessionPostureForWorkspace
	ApplyResolvedProfileToSessionConfig = rtprofile.ApplyResolvedProfileToSessionConfig
	SessionPostureFromSession          = rtprofile.SessionPostureFromSession
	SessionSummaryFields               = rtprofile.SessionSummaryFields
	ApplyProfileToolPolicy             = rtprofile.ApplyProfileToolPolicy
)
