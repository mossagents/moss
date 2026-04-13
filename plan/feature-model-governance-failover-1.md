---
goal: Implement model governance and failover v1 for moss and mosscode
version: 1.0
date_created: 2026-03-31
last_updated: 2026-03-31
owner: Copilot
status: 'Completed'
tags: [feature, governance, failover, reliability, architecture]
---

> **Archived historical implementation plan.** This file is retained for reference only and is not the canonical source for the current architecture or execution state.

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This plan implements the first production-safe model failover path for `moss` and `mosscode`. The implementation extends router selection into ordered candidate ranking, adds a `FailoverLLM` runtime wrapper with per-candidate retry and breaker ownership, introduces selected-model metadata propagation for observability, and wires `mosscode` governance and doctor surfaces without changing default single-model behavior when failover is disabled.

## 1. Requirements & Constraints

- **REQ-001**: Reuse the existing `models.yaml` router configuration and do not introduce a second failover-specific YAML file in v1.
- **REQ-002**: Preserve current single-model behavior when failover is disabled or when no router configuration is available.
- **REQ-003**: Extend `adapters/router.go` so candidate ordering is deterministic for both explicit requirements and nil/empty requirements.
- **REQ-004**: Add a runtime `FailoverLLM` that implements `port.LLM` and `port.StreamingLLM`.
- **REQ-005**: Scope retry and breaker handling to each candidate model when failover is enabled.
- **REQ-006**: Surface the actual serving model and failover-attempt summary through loop-owned observer events and trace outputs.
- **REQ-007**: Ensure failover can occur only before visible output or executable tool-use payload has been emitted.
- **SEC-001**: Do not silently switch models after partial visible output or tool-use side effects have begun.
- **SEC-002**: Do not add success-shaped fallbacks when all candidates fail; return explicit aggregated failure details.
- **CON-001**: Current `kernel/loop` emits model names from `sess.Config.ModelConfig.Model`, so implementation must add selected-model propagation instead of inferring from existing config.
- **CON-002**: Current loop streaming logic falls back from `Stream` to `Complete`, so implementation must gate that fallback to safe pre-emission errors only.
- **CON-003**: Current breaker implementation is in-memory only; v1 must keep breaker state process-local and keyed by stable model profile identity.
- **GUD-001**: Follow existing governance/reporting patterns in `appkit/product/governance.go` and CLI flag binding patterns in `examples/mosscode/main.go`.
- **PAT-001**: Prefer additive changes that preserve current public behavior unless failover is explicitly enabled.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Extend routing, response metadata, and loop safety contracts required for failover.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Modify `adapters/router.go` and `adapters/router_test.go` to expose ordered candidate selection, preserve current single-selection behavior through existing `Complete`/`Stream` entrypoints, and define deterministic nil/empty-requirements ordering with default model first and remaining models in file order. | ✅ | 2026-03-31 |
| TASK-002 | Modify `kernel/port/llm.go` to add explicit selected-model and failover-attempt metadata fields on the LLM response path, plus any structured error metadata required for aggregated failover exhaustion reporting. | ✅ | 2026-03-31 |
| TASK-003 | Modify `kernel/loop/loop.go` and related loop tests to consume selected-model metadata for `LLMCallEvent` / `ExecutionLLMCompleted`, allow `ExecutionLLMStarted` to omit unknown actual-model values, and restrict stream-to-sync fallback to explicitly safe pre-emission failures only. | ✅ | 2026-03-31 |

### Implementation Phase 2

- **GOAL-002**: Implement runtime failover behavior with per-candidate retry and breaker ownership.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Add `adapters/failover.go` implementing `FailoverLLM` for synchronous and streaming calls, including ordered candidate execution, per-candidate retries, per-candidate breakers keyed by `ModelProfile.Name`, failover-attempt metadata propagation, and aggregated exhaustion errors. | ✅ | 2026-03-31 |
| TASK-005 | Add `adapters/failover_test.go` covering first-candidate success, first-candidate failure with second-candidate recovery, per-candidate retry limits, breaker-open skip, aggregated all-candidates-failed behavior, streaming startup failover, and post-emission streaming non-failover behavior. | ✅ | 2026-03-31 |
| TASK-006 | Extend `appkit/product/governance.go` and `appkit/product/governance_test.go` with failover config defaults, failover governance report fields, router-availability diagnostics, and explicit configuration helpers for CLI/env wiring. | ✅ | 2026-03-31 |

### Implementation Phase 3

- **GOAL-003**: Wire product assembly, doctor output, and validate the full failover slice.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-007 | Modify `examples/mosscode/main.go` and `examples/mosscode/main_test.go` to bind failover flags/env vars, construct router-backed `FailoverLLM` when enabled, disable loop-level retry/breaker ownership for the failover path, and expose failover fields in doctor JSON/text output. | ✅ | 2026-03-31 |
| TASK-008 | Update observability-facing code and tests in files such as `appkit/product/trace.go`, `kernel/middleware/builtins/audit.go`, or other touched surfaces only as required to preserve accurate final-model and failover-attempt reporting without regressing current output. | ✅ | 2026-03-31 |
| TASK-009 | Run targeted validation for `adapters`, `kernel/loop`, `appkit/product`, and `examples/mosscode`, then run full-repo `go test ./...` and `go build ./...`, and update this file's task table and status to reflect the final implementation state. | ✅ | 2026-03-31 |

## 3. Alternatives

- **ALT-001**: Router-internal failover was rejected because it mixes model selection with runtime recovery and makes router behavior stateful and harder to validate.
- **ALT-002**: Loop-level failover was rejected because it pushes governance policy into `kernel/loop` and creates a larger-risk substrate change than needed for v1.
- **ALT-003**: A separate failover YAML file was rejected because existing `models.yaml` plus governance flags/env provide enough information for v1 and avoid configuration drift.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-03-31-model-governance-failover-design.md` defines the approved architecture, safety boundaries, and acceptance criteria.
- **DEP-002**: `adapters/router.go` remains the source of truth for model profile loading and candidate ordering.
- **DEP-003**: `kernel/retry/breaker.go` provides the existing breaker primitive that failover must reuse per candidate.
- **DEP-004**: `kernel/loop/loop.go` owns observer emission and therefore must consume selected-model metadata.
- **DEP-005**: `examples/mosscode/main.go` owns governance flag/env parsing and kernel assembly decisions for product wiring.

## 5. Files

- **FILE-001**: `adapters/router.go` — add ordered candidate selection APIs and deterministic default ordering.
- **FILE-002**: `adapters/router_test.go` — cover ordered candidate semantics and nil/empty-requirements ordering.
- **FILE-003**: `adapters/failover.go` — new failover runtime wrapper implementation.
- **FILE-004**: `adapters/failover_test.go` — new sync/streaming failover regression tests.
- **FILE-005**: `kernel/port/llm.go` — selected-model and failover metadata propagation structures.
- **FILE-006**: `kernel/loop/loop.go` — actual-model event emission and streaming fallback safety changes.
- **FILE-007**: `appkit/product/governance.go` — failover config defaults/reporting.
- **FILE-008**: `appkit/product/governance_test.go` — governance report/config regression coverage.
- **FILE-009**: `examples/mosscode/main.go` — failover assembly, flags/env, doctor output.
- **FILE-010**: `examples/mosscode/main_test.go` — CLI/doctor/build-path regression coverage.

## 6. Testing

- **TEST-001**: `go test ./adapters` must pass with added router and failover coverage.
- **TEST-002**: `go test ./kernel/loop` must pass with updated streaming fallback and selected-model reporting tests.
- **TEST-003**: `go test ./appkit/product` must pass with new governance report/config assertions.
- **TEST-004**: Run `Push-Location examples\mosscode; go test .; Pop-Location` to validate example CLI wiring in the directory that already hosts the most reliable example-package test path.
- **TEST-005**: Full repository validation must pass with `go test ./...` and `go build ./...`.

## 7. Risks & Assumptions

- **RISK-001**: Adding selected-model metadata to `CompletionResponse` changes a core port type and can affect observers, trace recorders, and tests beyond the failover wrapper.
- **RISK-002**: Tightening loop streaming fallback can regress non-failover streaming behavior if safe/unsafe error boundaries are implemented incorrectly.
- **RISK-003**: Per-candidate breaker handling can accidentally duplicate loop-level breaker behavior if `examples/mosscode/main.go` does not disable the old ownership path when failover is active.
- **ASSUMPTION-001**: Existing retry classification is sufficient for v1 failover triggers; no provider-specific error taxonomy is required in this batch.
- **ASSUMPTION-002**: Router profile names are stable enough to key in-memory per-candidate breaker state.
- **ASSUMPTION-003**: No TUI-specific command changes are required for this milestone because failover is exercised through existing run/doctor surfaces.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-03-31-model-governance-failover-design.md`
- `docs/superpowers/specs/2026-03-30-review-apply-rollback-design.md`
