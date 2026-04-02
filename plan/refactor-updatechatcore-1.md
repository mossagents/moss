---
goal: Refactor updateChatCore into ordered staged handlers with behavior parity
version: 1
date_created: 2026-04-02
last_updated: 2026-04-02
owner: tui-runtime
status: Planned
tags: [refactor, architecture, tui, go]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This plan defines deterministic steps to refactor `userio/tui/app_update_chat.go` so `updateChatCore` becomes an orchestration entrypoint with explicit stage ordering, reduced omission risk, and unchanged external behavior.

## 1. Requirements & Constraints

- **REQ-001**: Keep public behavior unchanged for `cancelMsg`, `switchModelMsg`, `switchTrustMsg`, `switchApprovalMsg`, `switchProfileMsg`, `kernelReadyMsg`, and default message flow.
- **REQ-002**: `updateChatCore` must call stage handlers in this exact order: control -> profile switch -> kernel ready -> fallback.
- **REQ-003**: Stage handlers must use deterministic return contract: `(handled bool, next appModel, cmd tea.Cmd)`; fallback uses `(next appModel, cmd tea.Cmd)`.
- **REQ-004**: Preserve user-visible message kinds and wording patterns (`msgError`, `msgSystem`) from current implementation.
- **SEC-001**: Do not weaken approval/trust/profile propagation; all existing state synchronization logic must remain.
- **CON-001**: Modify only TUI runtime files required for this refactor.
- **CON-002**: No new dependencies and no protocol/schema changes.
- **GUD-001**: Follow existing file/function naming and receiver patterns in `userio/tui`.
- **PAT-001**: Use focused helper functions grouped by capability for kernel-ready callback wiring.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Create deterministic stage scaffolding in `app_update_chat.go` without changing runtime behavior.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Refactor `func (m appModel) updateChatCore(msg tea.Msg) (tea.Model, tea.Cmd)` to orchestration-only body that sequentially calls `handleControlMessages`, `handleProfileSwitch`, `handleKernelReady`, and `fallbackChatUpdate`. |  |  |
| TASK-002 | Add `handleControlMessages` with explicit `cancelMsg` special path (`m.agent.cancel` + `tea.Quit`) and rebuild paths for model/trust/approval using `stopAgentForKernelRebuild` + `rebuildKernelWithModel`. |  |  |
| TASK-003 | Add `handleProfileSwitch` and move the full existing profile-switch logic (checkpoint handoff, profile resolve, config/chat sync, post-init text handling, rebuild). |  |  |
| TASK-004 | Add `fallbackChatUpdate` that performs `m.chat.Update(msg)` and mirrors `m.theme = m.chat.theme`. |  |  |

### Implementation Phase 2

- **GOAL-002**: Extract kernel-ready wiring into grouped helpers with explicit state ordering.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Add `handleKernelReady` and move the full existing `kernelReadyMsg` logic into it, preserving ordering and return behavior. |  |  |
| TASK-006 | Add helper `bindSessionCallbacks(agent *interactiveAgent)` and wire: `sessionInfoFn`, `sessionListFn`, `sessionRestoreFn`, `newSessionFn`, `offloadFn`. |  |  |
| TASK-007 | Add helpers `bindChangeCallbacks`, `bindCheckpointCallbacks`, `bindTaskCallbacks`, `bindDebugCallbacks`, `bindToolingCallbacks` and wire exact callback sets from spec. |  |  |
| TASK-008 | Keep non-callback orchestration state in `handleKernelReady`: `m.agent`, `m.chat.trust/profile/approvalMode`, `m.chat.scheduleCtrl`, `m.chat.setDiscoveredSkills(...)`, `m.chat.currentSessionID`, `m.chat.model`, `m.chat.streaming`. |  |  |

### Implementation Phase 3

- **GOAL-003**: Validate parity and finalize structure quality.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Run `gofmt -w userio/tui/app_update_chat.go` after refactor and ensure imports stay minimal and deterministic. |  |  |
| TASK-010 | Run `go test ./userio/tui/...` and fix only regressions caused by this refactor. |  |  |
| TASK-011 | Run `go vet ./...` and resolve only refactor-related findings. |  |  |
| TASK-012 | Review function lengths and ensure `updateChatCore` remains orchestration-only with no direct deep branching retained. |  |  |

## 3. Alternatives

- **ALT-001**: Registry-based dynamic message dispatch was rejected because it obscures ordering guarantees and increases tracing complexity.
- **ALT-002**: Keep a single monolithic `updateChatCore` with inline comments was rejected because it does not reduce omission risk in state updates.
- **ALT-003**: Move handlers to multiple new files immediately was rejected for this iteration to reduce cross-file churn; function decomposition inside current file is lower-risk first step.

## 4. Dependencies

- **DEP-001**: Existing `appModel` methods: `stopAgentForKernelRebuild`, `rebuildKernelWithModel`.
- **DEP-002**: Existing runtime/profile resolver usage: `runtime.ResolveProfileForWorkspace`.
- **DEP-003**: Existing report utilities and helpers used by kernel-ready debug/tooling wiring.
- **DEP-004**: Existing chat model callback fields in `chatModel`.

## 5. Files

- **FILE-001**: `userio/tui/app_update_chat.go` — primary refactor target for stage extraction and helper grouping.
- **FILE-002**: `docs/superpowers/specs/2026-04-02-updatechatcore-design.md` — design source of truth referenced by this plan.

## 6. Testing

- **TEST-001**: Verify cancellation path still quits and cancels active run when `cancelMsg` arrives.
- **TEST-002**: Verify model/trust/approval switches still trigger rebuild with updated config values.
- **TEST-003**: Verify profile switch still preserves checkpoint notice behavior and post-init run behavior.
- **TEST-004**: Verify kernel-ready path still wires callbacks and emits connection/notices as before.
- **TEST-005**: Verify default message path still delegates to `m.chat.Update` and theme sync remains intact.
- **TEST-006**: Execute project checks: `go test ./userio/tui/...` and `go vet ./...`.

## 7. Risks & Assumptions

- **RISK-001**: Callback wiring omission during helper extraction can break slash/session/checkpoint/task features.
- **RISK-002**: Reordering state assignment in kernel-ready path may create subtle behavior drift if not kept equivalent.
- **ASSUMPTION-001**: Current behavior in `app_update_chat.go` is the baseline to preserve exactly.
- **ASSUMPTION-002**: Existing tests and vet coverage are sufficient to detect major regressions in this area.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-02-updatechatcore-design.md`
