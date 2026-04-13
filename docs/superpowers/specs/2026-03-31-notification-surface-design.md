# Notification Surface for Long-Running Work Design

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem

`moss` now has the core primitives needed for long-running work visibility:

- `ExecutionEvent` already models run / LLM / tool / approval / checkpoint lifecycle
- `Observer` already gives a shared runtime event fan-out boundary
- the new `StateCatalog` already persists execution events into an indexed local history
- the TUI already has a composable observer seam and in-memory trace recording

But the product still lacks a real **notification surface** for long-running work:

- users cannot see live progress while a run is in flight
- long tool / LLM activity is only visible after the run completes
- indexed event history exists, but there is no user-facing replay or progress UI built on it
- delivery abstractions exist, but the first product slice should not yet depend on remote push channels

This leaves `mosscode` short of the production feedback loop users expect from Codex-like tooling: visible progress, visible phase changes, and a clear sense that a long task is still alive.

## Goal

Deliver a first production-safe notification surface for long-running work, with a **local UI-first** rollout.

## Goals

- add additive progress/lifecycle events for long-running iterations
- surface those events in the TUI while a run is active
- reuse `Observer` + `ExecutionEvent` + `StateCatalog` instead of inventing a parallel notification stack
- support replay of recent long-running activity from persisted event history
- keep the first slice local and product-visible before adding remote delivery channels

## Non-goals

- no webhook / Slack / Discord / email integrations in the first slice
- no OS-native desktop toast integration in the first slice
- no multi-client subscription server in the first slice
- no generalized remote pub/sub protocol in the first slice
- no cancellation API flowing from observers back into the loop in this phase

## Candidate Approaches

### A. Local UI-first notifications (recommended)

Add progress event types to `ExecutionEvent`, emit them from the main loop, and render them inside the TUI using the existing observer seam and persisted event history.

Pros:

- smallest implementation-safe slice
- aligns with current product entrypoints
- builds directly on the new state catalog
- keeps behavior easy to validate locally

Cons:

- remote subscribers still wait for a later slice

### B. Remote channel-first notifications

Prioritize SSE / WebSocket delivery first, then let the TUI subscribe over a remote channel abstraction.

Pros:

- stronger long-term external integration story

Cons:

- adds more moving parts before local UX is proven
- larger rollout risk for the second P1 subproject

### C. Full notification bus

Build local UI, remote delivery, replay, escalation rules, and subscription management in one pass.

Pros:

- most complete end state

Cons:

- too large for the next bounded subproject
- mixes core feedback UX with later delivery/platform work

## Recommendation

Choose **Approach A**.

The second P1 subproject should make long-running work clearly visible inside the first-party product first. Remote delivery channels can layer on top once the event model and local UX are stable.

## Scope of the first slice

### In-scope behavior

1. add additive long-running progress event types to `ExecutionEvent`
2. emit progress/lifecycle events from the main loop during multi-step runs
3. create a runtime notification observer / stream seam for in-process consumers
4. render live long-running progress in the TUI
5. replay recent progress/history for the current session from `StateCatalog`
6. keep existing trace/audit observers compatible through normal observer composition

### In-scope entrypoints

1. `kernel/port/execution_event.go`
2. `kernel/loop/loop.go`
3. `kernel/port/observer.go`
4. `appkit/runtime/statestore.go`
5. `appkit/product/trace.go`
6. `userio/tui/app.go`
7. `userio/tui/chat.go`
8. new TUI notification/progress component files if needed

### Out-of-scope behavior

- new remote `Channel` implementations such as SSE / WebSocket
- `DeliveryQueue` routing of live progress events to external recipients
- webhook-style notification subscriptions
- cross-process notification replay services

## Design

## 1. Additive progress event model

The first slice should extend `ExecutionEventType` with additive long-running progress events such as:

- `iteration.started`
- `iteration.progress`
- `progress.updated`

The existing `ExecutionEvent.Data` field already gives enough structure for the first slice, so this phase should not introduce a brand-new progress payload type unless implementation friction proves high.

Recommended metadata fields:

- `iteration`
- `max_steps`
- `elapsed_ms`
- `status`
- `tool_calls`
- `llm_calls`
- `message`

This keeps the first slice backward-compatible while still making long-running work machine-readable.

## 2. Loop emission contract

The main loop should emit progress events at stable lifecycle points:

1. emit `iteration.started` at the top of each main loop iteration
2. emit `iteration.progress` once per iteration after that iteration's model/tool work has completed and before the next iteration starts or the run terminates
3. continue using the existing `approval.requested` / `approval.resolved` events as the canonical approval-wait signals
4. continue using the existing terminal run events as the canonical completion boundary:
   - `run.completed`
   - `run.failed`
   - `run.cancelled`

The first slice must **not** promise a generic "long external wait" progress signal, because opaque tool handlers do not currently expose that boundary in a consistent repo-wide way.

The goal is not to fire high-frequency micro-events. The goal is to surface user-meaningful state changes that prove the run is alive and making progress.

### Iteration semantics

An iteration progress event is an **iteration summary**, not a per-tool streaming protocol.

Its `Data` payload may include bounded counters such as:

- `iteration`
- `max_steps`
- `elapsed_ms`
- `tool_calls`
- `llm_calls`
- `status`
- `message`

It should summarize what is known at the end of the iteration and should not imply that every tool call inside the iteration produces its own extra progress sub-event.

## 3. In-process notification stream

The first slice should keep delivery in-process and product-local.

Recommended shape:

- a lightweight notification observer that converts execution events into UI-facing progress state
- a **non-blocking, local-adapter-only** handoff into the active TUI
- replay support that queries recent `execution_event` rows from `StateCatalog`

This avoids creating a remote transport dependency before the local UX is validated.

### Non-blocking delivery contract

The notification path must not be allowed to stall the main loop.

For this phase, the notification observer / adapter must therefore be:

- **non-blocking**
- **loss-tolerant**
- **latest-state-oriented**

This means:

- it may drop intermediate progress updates under load
- it may coalesce multiple updates into one latest visible state
- it must never block `JoinObservers(...)` on a slow or closed UI consumer

The first slice should reuse the existing local TUI delivery seam such as `BridgeIO` / `tea.Program.Send`, rather than introducing a second general-purpose transport stack inside the runtime.

## 4. TUI progress surface

The TUI should gain a dedicated long-running work surface, not just more text lines in the chat transcript.

Recommended behavior:

- show a compact status line or sidebar section while a run is active
- display current phase, iteration, elapsed time, and the latest meaningful message
- clear or collapse the progress surface when the run completes
- allow recent replay when reopening or resuming a session

The first slice should prefer a compact, always-visible summary over a verbose scrolling event log.

### Session scoping

Notification state must be strictly scoped to the active `SessionID`.

The first slice must therefore require that the TUI notification surface:

- only accepts live updates whose `SessionID` matches the active session
- resets its visible progress state on new-session, restore-session, session switch, fork, and replay preparation flows
- ignores unrelated local events from other sessions or background work

This prevents cross-session bleed-through when `StateCatalog` contains activity from multiple local runs.

## 5. Replay boundary

Replay for this phase should be session-scoped and recent-history-focused.

The TUI should be able to ask the `StateCatalog` for recent `execution_event` entries for the active session and reconstruct **one latest visible progress snapshot**.

Canonical terminal signals remain the existing run lifecycle events:

- `run.completed`
- `run.failed`
- `run.cancelled`

Replay for the first slice should therefore:

- scan recent `execution_event` rows for the active session
- fold them into one latest progress state
- stop at the latest terminal run event when present

This should not attempt to rebuild full transcripts, deterministic execution playback, or a raw event log player. It only needs to restore a user-facing sense of the last visible progress state.

## 6. Observer composition

Notification observers must compose through the existing `JoinObservers(...)` path so they can coexist with:

- audit logging
- pricing observers
- trace recording
- the state catalog observer

The first slice must not replace or special-case the existing observer chain.

## 7. Rollout plan

1. extend event types and loop emission
2. add in-process notification observer / stream
3. add TUI progress rendering
4. add replay from state catalog
5. validate coexistence with trace/audit/state observers

## 8. Testing

### Unit tests

- progress event constants remain additive and stable
- loop emits progress events at expected lifecycle points
- notification observer forwards events without breaking existing observers
- replay query returns the expected recent progress rows
- approval-requested / approval-resolved transitions update notification state correctly
- slow-consumer / overflow behavior remains non-blocking
- multi-tool iterations produce the expected event ordering

### Integration tests

- long-running run updates TUI progress state while active
- existing trace/audit observers still receive events
- resumed/reopened session can reconstruct recent progress from `StateCatalog`
- cancellation clears or finalizes visible progress correctly
- new-session / restore / fork / replay flows reset and rebuild progress state for the active session only

### Validation commands

- targeted `go test` for `kernel/loop`, `kernel/port`, `appkit/runtime`, `appkit/product`, `userio/tui`
- full `go test ./... -count=1`
- `apps\mosscode` independent `go build .`
