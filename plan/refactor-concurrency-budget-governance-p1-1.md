---
goal: P1 Concurrency & Budget Governance Plan (kernel/session/loop)
version: 1.0
date_created: 2026-04-01
last_updated: 2026-04-01
owner: Moss Runtime
status: Planned
tags: [p1, concurrency, budget, kernel, session, loop, reliability]
---

> **Archived historical implementation plan.** This file is retained for reference only and is not the canonical source for the current architecture or execution state.

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This plan targets P1 governance hardening for `kernel/session/loop`, focusing on atomic budget control, concurrent safety under parallel tool execution, and deterministic cancellation/termination semantics.

## 1. Requirements & Constraints

- **REQ-001**: Eliminate budget check-then-act race in session loop execution.
- **REQ-002**: Keep existing public behavior unless explicitly tightened for safety.
- **REQ-003**: Preserve compatibility with existing `Session` persistence and tests.
- **REQ-004**: Ensure loop behavior remains deterministic under `ParallelToolCall`.
- **CON-001**: Do not redesign kernel API surface in P1.
- **CON-002**: Keep changes scoped to `kernel/session`, `kernel/loop`, and directly coupled tests.
- **CON-003**: Validate with repository tests (`go test ./...`) after changes.

## 2. Implementation Steps

### Phase 1 — Budget Data Model Hardening (`kernel/session`)

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| P1-1 | Introduce concurrency-safe budget accounting (atomic counters or lock-protected increments) in `kernel/session/session.go`. | | |
| P1-2 | Add budget reservation API (e.g., `TryConsume(tokens, steps)`) that atomically checks + records. | | |
| P1-3 | Keep legacy read fields/JSON compatibility path for store serialization tests. | | |
| P1-4 | Add session budget concurrency tests for high-contention consume paths. | | |

### Phase 2 — Loop Consumption Semantics (`kernel/loop`)

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| P1-5 | Replace separate `Exhausted()` + `Record()` call pattern with single atomic consume call in `loop.Run`. | | |
| P1-6 | Define over-budget behavior (stop iteration immediately and emit consistent lifecycle/observer signals). | | |
| P1-7 | Review parallel tool call path for shared state mutation safety (`sess.Messages`, tool results, step accounting). | | |
| P1-8 | Add targeted loop tests for bounded termination at token/step limits under serial and parallel tool paths. | | |

### Phase 3 — Cancellation and Supervisor Consistency

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| P1-9 | Audit `run_supervisor` + loop exit path to guarantee all started runs always hit cleanup on errors/cancel. | | |
| P1-10 | Add regression tests for cancellation mid-iteration and mid-tool execution. | | |

### Phase 4 — Validation & Stabilization

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| P1-11 | Run focused tests: `go test ./kernel/session ./kernel/loop ./kernel`. | | |
| P1-12 | Run full repository test suite `go test ./...` and fix only introduced regressions. | | |

## 3. Alternatives

- **ALT-001**: Keep current budget design and only add extra `Exhausted()` checks. Rejected: still race-prone.
- **ALT-002**: Move all budget governance into middleware. Rejected: too indirect for core safety guarantees.
- **ALT-003**: Introduce global scheduler lock for all loop steps. Rejected: excessive contention and performance impact.

## 4. Dependencies

- `kernel/session/session.go` budget model.
- `kernel/loop/loop.go` run iteration and consumption points.
- Existing tests in `kernel/session/*_test.go` and `kernel/loop/*_test.go`.

## 5. Files

- `kernel/session/session.go`
- `kernel/session/session_test.go`
- `kernel/loop/loop.go`
- `kernel/loop/loop_test.go`
- `kernel/run_supervisor.go` (if cleanup invariants require adjustment)
- `kernel/run_supervisor_test.go` (if adjusted)

## 6. Testing

- Concurrency stress tests for budget consume paths.
- Loop termination tests at exact and exceeded budget boundaries.
- Parallel tool-call tests ensuring no runaway steps/tokens.
- Cancellation propagation tests under active loop iteration.
- Full `go test ./...` regression run.

## 7. Risks & Assumptions

- **RISK-001**: Atomic model may subtly change persisted field update timing.
- **RISK-002**: Tightened budget stopping may alter expected step counts in existing tests.
- **ASSUMPTION-001**: Current observer/lifecycle hooks tolerate earlier stop conditions.

## 8. Related Specs

- `docs/superpowers/specs/2026-04-01-runtime-capability-pack-p0-design.md`
- `plan/refactor-runtime-capability-pack-p0-1.md`
