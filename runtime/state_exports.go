package runtime

// This file re-exports all public symbols from runtime/state so that callers
// using the runtime package directly do not need import-path changes.

import (
	rtstate "github.com/mossagents/moss/runtime/state"
)

// Type aliases — identical to the originals, no conversion required.
type (
	StateCatalog       = rtstate.StateCatalog
	StateCatalogHealth = rtstate.StateCatalogHealth
	StateKind          = rtstate.StateKind
	StateEntry         = rtstate.StateEntry
	StateQuery         = rtstate.StateQuery
	StatePage          = rtstate.StatePage
)

// StateKind constants.
const (
	StateKindSession        = rtstate.StateKindSession
	StateKindCheckpoint     = rtstate.StateKindCheckpoint
	StateKindTask           = rtstate.StateKindTask
	StateKindJob            = rtstate.StateKindJob
	StateKindJobItem        = rtstate.StateKindJobItem
	StateKindMemory         = rtstate.StateKindMemory
	StateKindExecutionEvent = rtstate.StateKindExecutionEvent
)

// Function re-exports.
var (
	NewStateCatalog           = rtstate.NewStateCatalog
	NewStateCatalogObserver   = rtstate.NewStateCatalogObserver
	StateEntryFromSession     = rtstate.StateEntryFromSession
	StateEntryFromCheckpoint  = rtstate.StateEntryFromCheckpoint
	StateEntryFromExecutionEvent = rtstate.StateEntryFromExecutionEvent
	StateEntryFromMemory      = rtstate.StateEntryFromMemory
	StateEntryFromTask        = rtstate.StateEntryFromTask
	StateEntryFromJob         = rtstate.StateEntryFromJob
	StateEntryFromJobItem     = rtstate.StateEntryFromJobItem
	WrapSessionStore          = rtstate.WrapSessionStore
	WrapCheckpointStore       = rtstate.WrapCheckpointStore
	WrapTaskRuntime           = rtstate.WrapTaskRuntime
)
