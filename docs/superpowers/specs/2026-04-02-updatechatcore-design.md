# updateChatCore Refactor Design

## Problem Statement

`userio/tui/app_update_chat.go` currently contains a long `updateChatCore` method that mixes:
- control-message handling (cancel/model/trust/approval/profile),
- kernel-ready binding and initialization,
- fallback chat message update routing.

This increases the risk of partial state updates and makes ordering assumptions implicit.

## Goals

1. Preserve external behavior.
2. Make state-update ordering explicit and auditable.
3. Reduce omission risk when adding new message handling.
4. Improve readability by grouping kernel-ready bindings by capability.

## Non-Goals

1. No user-visible behavior changes.
2. No command/protocol changes.
3. No architectural rewrite outside `updateChatCore` flow.

## Proposed Approach

Adopt a staged pipeline inside `updateChatCore`:

1. `handleControlMessages`  
   Handles `cancelMsg`, `switchModelMsg`, `switchTrustMsg`, `switchApprovalMsg`.
2. `handleProfileSwitch`  
   Handles `switchProfileMsg`, including checkpoint handoff messaging and profile resolution updates.
3. `handleKernelReady`  
   Handles `kernelReadyMsg`, including agent wiring, chat callback binding, notices, and post-init replay/dispatch.
4. `fallbackChatUpdate`  
   Delegates to `m.chat.Update` and mirrors theme when prior stages do not handle the message.

The top-level `updateChatCore` becomes orchestration only: invoke stages in fixed order and return on first handled stage.

Suggested stage signature pattern:

```go
func (m appModel) handleControlMessages(msg tea.Msg) (handled bool, next appModel, cmd tea.Cmd)
```

The same `(handled, next, cmd)` contract is used for `handleProfileSwitch` and `handleKernelReady`.
`fallbackChatUpdate` is terminal and uses:

```go
func (m appModel) fallbackChatUpdate(msg tea.Msg) (next appModel, cmd tea.Cmd)
```

## State and Data Flow

### Global ordering rules

1. Rebuild-triggering messages always call `stopAgentForKernelRebuild()` before mutating rebuild-driving config fields.
   - Explicit exception: `cancelMsg` does not rebuild; it cancels current agent run (if present) and returns `tea.Quit`.
2. Profile switch success path updates fields in this order:
   - `m.config` (`Profile`, `Trust`, `ApprovalMode`)
   - mirrored `m.chat` fields (`profile`, `trust`, `approvalMode`)
   - post-init texts (`postInitDisplayText`, `postInitRunText`)
3. Kernel-ready handling updates in this order:
   - `m.agent` and shared references
   - all `m.chat` callback bindings
   - `m.chat.model` sync from `m.config.Model` (when configured)
   - connection/notices messages
   - optional post-init dispatch or progress replay
4. Kernel-ready post-init branching is explicit:
   - when `postInitRunText != ""`: call `dispatchUserSubmission(...)`, refresh viewport, then `publishProgressReplay`
   - otherwise: refresh viewport, then `publishProgressReplay` only
5. `syncCustomCommands` notice append is part of the connection/notices step.

### Stage contract

Each stage follows one of two outcomes:
1. `handled=true`: completes and returns final `(model, cmd)`.
2. `handled=false`: performs no side effects and allows next stage.

This avoids partially-applied state before fallback routing.

## Component Boundaries

### `handleControlMessages`
- Purpose: capture simple control/rebuild triggers.
- Depends on: `stopAgentForKernelRebuild`, `rebuildKernelWithModel`, `m.config`.
- Output: handled decision + model/cmd.

### `handleProfileSwitch`
- Purpose: profile-switch-specific lifecycle and messaging.
- Depends on: runtime profile resolution, agent checkpoint handoff, chat system/error messaging.
- Output: handled decision + model/cmd.

### `handleKernelReady`
- Purpose: bind runtime agent to chat runtime.
- Depends on: agent APIs and chat callback slots.
- Internal helpers split by capability group:
  - `bindSessionCallbacks`
  - `bindChangeCallbacks`
  - `bindCheckpointCallbacks`
  - `bindTaskCallbacks`
  - `bindDebugCallbacks`
  - `bindToolingCallbacks`
- Non-callback state mutations remain in `handleKernelReady` orchestration:
  - `m.agent` assignment
  - `m.chat.trust/profile/approvalMode` sync
  - `m.chat.currentSessionID` update
  - `m.chat.scheduleCtrl` assignment
  - `m.chat.streaming = false`
- Callback mapping by helper:
  - `bindSessionCallbacks`: `sessionInfoFn`, `sessionListFn`, `sessionRestoreFn`, `newSessionFn`, `offloadFn`
  - `bindChangeCallbacks`: `changeListFn`, `changeShowFn`, `applyChangeFn`, `rollbackChangeFn`
  - `bindCheckpointCallbacks`: `checkpointListFn`, `checkpointShowFn`, `checkpointCreateFn`, `checkpointForkFn`, `checkpointReplayFn`
  - `bindTaskCallbacks`: `taskListFn`, `taskQueryFn`, `taskCancelFn`
  - `bindDebugCallbacks`: `permissionSummaryFn`, `setPermissionFn`, `refreshSystemPromptFn`, `debugPromptFn`, `debugConfigFn`
  - `bindToolingCallbacks`: `sendFn`, `cancelRunFn`, `gitRunFn`, `skillListFn`
- Output: handled decision + model/cmd.

### `fallbackChatUpdate`
- Purpose: preserve existing default route.
- Depends on: `m.chat.Update`.
- Output: final model/cmd.

## Error Handling

1. Preserve current user-facing message kind and wording pattern (`msgError`, `msgSystem`).
2. Preserve existing early-return behavior for switch failures and resolution failures.
3. Keep all fatal rebuild/boot errors in existing upstream flow (no swallowing).

## Testing and Validation

Run unchanged project checks:

1. `gofmt -w userio/tui/*.go` (or targeted files)
2. `go test ./userio/tui/...`
3. `go vet ./...`

Acceptance criteria:

1. Existing chat flows (switch model/trust/approval/profile, kernel ready, normal input) keep behavior.
2. `updateChatCore` reads as orchestration with explicit stage ordering.
3. No regressions in compile/tests/vet.
