---
goal: Review Apply Rollback v1 Implementation Plan
version: 1.0
date_created: 2026-03-31
last_updated: 2026-03-31
owner: Copilot CLI
status: Completed
tags: [feature, architecture, safety, review, rollback]
---

> **Archived historical implementation plan.** This file is retained for reference only and is not the canonical source for the current architecture or execution state.

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

Implement the approved `review / apply / rollback` v1 design for `mosscode` and shared product helpers. The implementation must add a product-level `ChangeOperation` workflow that keeps mutation explicit, requires a clean repository before apply, records recovery metadata for every apply, supports exact reverse-patch rollback, and exposes manual recovery artifacts when exact rollback is unavailable.

## 1. Requirements & Constraints

- **REQ-001**: Add a persisted product-level `ChangeOperation` record scoped by resolved `repo_root`.
- **REQ-002**: Require a clean git repository before any `apply` operation mutates the worktree.
- **REQ-003**: Capture pre-apply recovery metadata using existing repo-state capture primitives on every successful apply path.
- **REQ-004**: Attempt checkpoint creation when a persisted session ID is provided and a session store is available, without making checkpoint creation mandatory for apply success.
- **REQ-005**: Support exact rollback only through reverse patch application using the recorded `patch_id`.
- **REQ-006**: When exact rollback is unavailable, fail rollback and surface recorded manual recovery artifacts instead of performing a repo-wide restore.
- **REQ-007**: Extend `review` to list and inspect change operations in both text and JSON output.
- **REQ-008**: Add `mosscode` CLI surfaces for `apply`, `rollback`, and `changes`.
- **REQ-009**: Add TUI slash command surfaces for `/changes`, `/apply`, and `/rollback`.
- **REQ-010**: Surface inconsistent states explicitly when repo mutation and metadata persistence diverge.
- **SEC-001**: Do not allow cross-repo rollback by loading a change operation from a different git root.
- **SEC-002**: Do not infer patch content from the current dirty worktree.
- **CON-001**: Reuse existing substrate in `sandbox/git_patch.go`, `sandbox/git_repo.go`, `kernel/checkpoint.go`, and `appkit/product/runtime.go`; do not introduce a second recovery system.
- **CON-002**: Keep the agent loop semantics unchanged; mutation remains an explicit product/operator action.
- **GUD-001**: Follow the existing file-backed JSON store pattern used by `kernel/port/FileCheckpointStore`.
- **GUD-002**: Keep CLI/TUI JSON and text output consistent with existing `review`, `doctor`, and `checkpoint` command families.
- **PAT-001**: Prefer new focused files in `appkit/product` for change-operation logic instead of overloading `runtime.go` with storage and orchestration internals.

## 2. Implementation Steps

### Implementation Phase 1

- GOAL-001: Add product-level change-operation types, file-backed persistence, and orchestration primitives in `appkit/product`.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Create `appkit/product/change_ops.go` to define `ChangeOperation`, `ChangeStatus`, `RollbackMode`, `ApplyChangeReport`, `RollbackChangeReport`, and helper constructors used by CLI/TUI surfaces. | ✅ | 2026-03-31 |
| TASK-002 | Create `appkit/product/change_store.go` to implement a file-backed `ChangeOperationStore` rooted at `product.ChangeStoreDir()` using one JSON file per operation and repo-root-aware list/load helpers. | ✅ | 2026-03-31 |
| TASK-003 | Extend `appkit/product/runtime.go` and/or new focused helpers to expose `BuildReviewReport` modes `changes` and `change <id>`, plus text renderers for change summaries and change detail. | ✅ | 2026-03-31 |
| TASK-004 | Implement apply orchestration in `appkit/product/change_apply.go`: resolve git repo, validate clean repo, capture repo state, optionally create checkpoint from a persisted session, create `preparing` operation, call `PatchApply`, finalize to `applied`, and mark `apply_inconsistent` on metadata divergence. | ✅ | 2026-03-31 |
| TASK-005 | Implement rollback orchestration in `appkit/product/change_rollback.go`: load change by ID, validate repo-root match and `applied` status, reverse patch via `PatchRevert`, finalize to `rolled_back`, and fail with recorded manual recovery artifacts when exact rollback is unavailable. | ✅ | 2026-03-31 |

### Implementation Phase 2

- GOAL-002: Add product-facing CLI support in `examples/mosscode`.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-006 | Extend `examples/mosscode/main.go` config parsing to recognize top-level commands `apply`, `rollback`, and `changes`, including `--json`, `--patch-file`, `--summary`, and `--session` arguments where applicable. | ✅ | 2026-03-31 |
| TASK-007 | Add `runApply`, `runRollback`, and `runChanges` handlers in `examples/mosscode/main.go` that call the new `appkit/product` helpers and emit stable JSON/text reports. | ✅ | 2026-03-31 |
| TASK-008 | Update `printUsage()` in `examples/mosscode/main.go` so the new command families are discoverable and phrased consistently with existing `review` and `checkpoint` usage sections. | ✅ | 2026-03-31 |

### Implementation Phase 3

- GOAL-003: Add interactive TUI command surfaces in `userio/tui`.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Extend `userio/tui/app.go` `agentState` with callbacks for listing/showing changes and applying/rolling back changes using the new product helpers. | ✅ | 2026-03-31 |
| TASK-010 | Extend `userio/tui/chat.go` slash-command parsing, help text, and slash completion candidates for `/changes list`, `/changes show <id>`, `/apply <patch_file> [summary...]`, and `/rollback <change_id>`. | ✅ | 2026-03-31 |
| TASK-011 | Ensure TUI messaging clearly distinguishes exact rollback success from rollback failure that only leaves manual recovery artifacts. | ✅ | 2026-03-31 |

### Implementation Phase 4

- GOAL-004: Add regression coverage and full validation.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-012 | Add focused tests in `appkit/product/change_ops_test.go` and/or `appkit/product/runtime_test.go` covering repo-root scoping, apply preconditions, review change listing/detail, exact rollback, and inconsistent-state transitions. | ✅ | 2026-03-31 |
| TASK-013 | Extend `examples/mosscode/main_test.go` with CLI coverage for `apply`, `rollback`, `changes list/show`, and `review changes/change`. | ✅ | 2026-03-31 |
| TASK-014 | Extend `userio/tui/chat_test.go` with slash-command coverage for `/changes`, `/apply`, `/rollback`, and their validation/error states. | ✅ | 2026-03-31 |
| TASK-015 | Run `go test ./...` and `go build ./...` from `D:\Codes\qiulin\moss`, fixing any regressions introduced by the new feature. | ✅ | 2026-03-31 |

## 3. Alternatives

- **ALT-001**: Expose raw `patch_id`-based apply/revert without a product-level change record. Rejected because it is too low-level for operator UX and weak for auditing.
- **ALT-002**: Allow `rollback` to perform repo-wide capture restore as a degraded fallback. Rejected in v1 because the current substrate makes that path too broad and unsafe for ordinary rollback semantics.
- **ALT-003**: Make checkpoint creation mandatory for every apply. Rejected because v1 only needs recovery metadata capture; checkpoint creation should be best-effort when session context is available.

## 4. Dependencies

- **DEP-001**: `sandbox.NewGitRepoStateCapture` for clean-repo validation and pre-apply recovery metadata.
- **DEP-002**: `sandbox.NewGitPatchApply` and `sandbox.NewGitPatchRevert` through kernel ports for exact apply/rollback.
- **DEP-003**: `session.NewFileStore` and kernel checkpoint APIs for optional apply-time checkpoint creation.
- **DEP-004**: Existing product app-dir helpers in `appkit/product/runtime.go` and app name setup in `config`.

## 5. Files

- **FILE-001**: `appkit/product/runtime.go` — extend review report modes and directory helpers.
- **FILE-002**: `appkit/product/runtime_test.go` — extend runtime-level report coverage.
- **FILE-003**: `appkit/product/change_ops.go` — new change-operation types and report helpers.
- **FILE-004**: `appkit/product/change_store.go` — new file-backed change operation store.
- **FILE-005**: `appkit/product/change_apply.go` — new apply orchestration.
- **FILE-006**: `appkit/product/change_rollback.go` — new rollback orchestration.
- **FILE-007**: `appkit/product/change_ops_test.go` — new unit/integration coverage for change operations.
- **FILE-008**: `examples/mosscode/main.go` — new CLI commands and usage text.
- **FILE-009**: `examples/mosscode/main_test.go` — CLI regression coverage.
- **FILE-010**: `userio/tui/app.go` — TUI callback wiring.
- **FILE-011**: `userio/tui/chat.go` — slash commands, help text, completions.
- **FILE-012**: `userio/tui/chat_test.go` — TUI regression coverage.

## 6. Testing

- **TEST-001**: Validate that apply rejects dirty repositories and missing patch input.
- **TEST-002**: Validate that a successful apply creates a durable `ChangeOperation` scoped to the active `repo_root`.
- **TEST-003**: Validate that exact rollback transitions a change from `applied` to `rolled_back`.
- **TEST-004**: Validate that rollback fails cleanly when exact reverse patch is unavailable and surfaces recorded checkpoint/capture metadata.
- **TEST-005**: Validate that cross-repo rollback is rejected.
- **TEST-006**: Validate that `review changes` and `review change <id>` work in JSON and text output.
- **TEST-007**: Validate that TUI help text and slash completion expose the new commands.
- **TEST-008**: Validate repository-wide `go test ./...` and `go build ./...`.

## 7. Risks & Assumptions

- **RISK-001**: The current patch/journal substrate can leave mutation and metadata persistence out of sync; explicit inconsistent-state reporting is required to avoid silent corruption.
- **RISK-002**: `examples/mosscode` tests may still require directory-local execution quirks; test commands must follow the repository's known Windows behavior if package-level invocation misbehaves.
- **ASSUMPTION-001**: `rollback v1` only needs exact reverse-patch semantics; broader restore tooling can remain future work.
- **ASSUMPTION-002**: Existing repo capture metadata is sufficient for manual recovery guidance even though it is not sufficient for automated change-scoped rollback.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-03-30-review-apply-rollback-design.md`
- `appkit/product/runtime.go`
- `examples/mosscode/main.go`
- `userio/tui/chat.go`
- `sandbox/git_patch.go`
