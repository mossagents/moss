---
goal: Rebuild examples\agent-swarm as a product-style research swarm example on top of the new swarm runtime
version: 1.0
date_created: 2026-04-23
last_updated: 2026-04-23
owner: mossagents/moss contributors
status: Planned
tags: [feature, example, swarm, planning]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This plan replaces the legacy `examples\agent-swarm` parallel demo with a product-style research swarm example that exposes `run`, `resume`, `inspect`, and `export` commands while reusing the existing `harness\swarm` runtime, persistent stores, recovery model, governance facts, and swarm event bridge. The implementation must stay inside the example module unless compilation exposes a missing public API, and every command must consume the same persisted swarm facts.

## 1. Requirements & Constraints

- **REQ-001**: Replace the current one-shot demo entrypoint in `examples\agent-swarm\main.go` with a subcommand-oriented CLI that supports `run`, `resume`, `inspect`, and `export`.
- **REQ-002**: Use the existing `harness\swarm.RuntimeFeature()` and `harness\swarm.ResearchOrchestrator` as the execution substrate; do not reintroduce legacy `ParallelAgent` / persona-worker orchestration from `examples\agent-swarm\swarm.go`.
- **REQ-003**: Persist all run facts through the file-backed session, task, artifact, and runtime event stores so that `resume`, `inspect`, and `export` read the same data written by `run`.
- **REQ-004**: Implement deterministic `--demo` execution that uses the same command flow and stores as real LLM mode; the only difference is artifact content generation.
- **REQ-005**: Implement shared target resolution with the precedence defined in `docs\superpowers\specs\2026-04-23-agent-swarm-example-redesign-design.md`: explicit `--session`, explicit `--run-id`, explicit `--latest`, then mode-specific default fallback.
- **REQ-006**: Implement recovery using a single `RecoveredRunSnapshot` model that is consumed by both `resume` and the `inspect` / `export` surfaces.
- **REQ-007**: Enforce persisted execution mode consistency: a run created as `demo` cannot resume as `real`, and a run created as `real` cannot resume as `demo`.
- **REQ-008**: Export must support `bundle`, `json`, and `jsonl` formats with the exact file contract defined by the approved design spec.
- **REQ-009**: Update `examples\agent-swarm\README.md` so contributors can run the new example without reading internal code.
- **CON-001**: Do not modify `kernel\` or `harness\` public APIs in this implementation plan; consume existing exported behavior only.
- **CON-002**: Remove the legacy example-only research flags (`--agents`, `--rounds`, `--batch`, `--personas`) from the new CLI surface.
- **CON-003**: Keep all runtime data under an example-specific app directory so the example does not share storage with `apps\mosscode`.
- **GUD-001**: Follow the approved spec in `docs\superpowers\specs\2026-04-23-agent-swarm-example-redesign-design.md` as the single source of truth for command behavior and failure semantics.
- **GUD-002**: Prefer small files with one clear responsibility, matching the spec’s recommended split: command entrypoints, runtime assembly, workflow, demo provider, and README.
- **PAT-001**: Reuse `harness\appkit` / `harness\appkit\product` patterns for runtime assembly, inspect rendering, export rendering, and persistent storage handling.
- **PAT-002**: Make every validation path runnable from the example module directory with existing Go tooling: `Push-Location examples\agent-swarm; go build .; go test .; Pop-Location`.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Replace the legacy single-run CLI with a product-style command shell and shared configuration model.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Rewrite `examples\agent-swarm\main.go` so `main()` delegates to a single command dispatcher function (`runMain` or equivalent) that parses the first positional argument as `run`, `resume`, `inspect`, or `export` and returns an exit code. |  |  |
| TASK-002 | Create `examples\agent-swarm\config.go` with explicit structs for `globalConfig`, `runConfig`, `resumeConfig`, `inspectConfig`, and `exportConfig`; implement flag parsing functions for each command and central validation for `--topic`, `--run-id`, `--session`, `--latest`, `--thread-id`, `--output`, `--format`, `--demo`, and `--force-degraded-resume`. |  |  |
| TASK-003 | Remove legacy flag parsing and orchestration wiring from `examples\agent-swarm\main.go`, `examples\agent-swarm\swarm.go`, and `examples\agent-swarm\personas.go`; after replacement code exists, delete `swarm.go` and `personas.go` from the module. |  |  |

### Implementation Phase 2

- **GOAL-002**: Build the shared runtime assembly, target resolution, recovery snapshot, and run lock services required by all commands.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Create `examples\agent-swarm\runtime.go` with a single assembly entrypoint (`buildRuntimeEnv` or equivalent) that constructs the example-specific app directory, enables the swarm preset, opens session/task/artifact/event stores, and returns typed handles plus `harness\swarm.Runtime`. |  |  |
| TASK-005 | Implement `TargetResolver` in `examples\agent-swarm\runtime.go` with explicit functions for `ResolveForResume`, `ResolveForInspect`, and `ResolveForExport`; enforce the spec’s explicit-target precedence and mode-specific fallback (`resume` => latest recoverable run, `inspect/export` => latest run). |  |  |
| TASK-006 | Implement `RecoveryResolver` in `examples\agent-swarm\runtime.go` that reconstructs `RecoveredRunSnapshot` from session/task/message/artifact/event facts, computes `recoverable`, `degraded`, `events_partial`, and `execution_mode`, and exposes one load function used by `resume`, `inspect`, and `export`. |  |  |
| TASK-007 | Implement `RunLockService` in `examples\agent-swarm\runtime.go` using the example app directory, a `<swarm-run-id>.lock` file, and the 5-minute TTL contract from the spec; return typed lease errors that command handlers render without guessing. |  |  |

### Implementation Phase 3

- **GOAL-003**: Implement the actual swarm run/resume workflow on top of the new runtime and deterministic demo content generation.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Create `examples\agent-swarm\workflow.go` with explicit functions for `startRun`, `resumeRun`, `executePendingWork`, and `publishFinalReport`; seed runs through `ResearchOrchestrator.Seed(...)`, persist root-session metadata for `execution_mode`, and ensure `resume` never reseeds completed work. |  |  |
| TASK-009 | Create `examples\agent-swarm\demo_mode.go` with deterministic content builders for worker findings, synthesis output, and review outcomes; wire these helpers into `workflow.go` so demo mode exercises the same swarm facts, governance messages, and artifact publication path as real mode. |  |  |
| TASK-010 | Create `examples\agent-swarm\commands_run.go` and `examples\agent-swarm\commands_resume.go`; implement command handlers that acquire a run lease, assemble runtime handles, call `TargetResolver` / `RecoveryResolver` as required, execute `workflow.go`, and print root session ID, swarm run ID, status, and next-step hints. |  |  |
| TASK-011 | Enforce mode consistency in `commands_resume.go` and `workflow.go`: `resume` must read persisted `execution_mode` from `RecoveredRunSnapshot`, reject mismatched `--demo` usage, reject completed runs, and require `--force-degraded-resume` for degraded snapshots. |  |  |

### Implementation Phase 4

- **GOAL-004**: Deliver inspect/export surfaces and contributor-facing documentation that operate purely on recovered snapshots.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-012 | Create `examples\agent-swarm\commands_inspect.go` that resolves the target, loads `RecoveredRunSnapshot`, supports `--view run|threads|thread|events`, requires `--thread-id` for `--view thread`, and renders either human-readable output or JSON from snapshot data only. |  |  |
| TASK-013 | Create `examples\agent-swarm\commands_export.go` that resolves the target, loads `RecoveredRunSnapshot`, and writes `summary.json`, `artifacts.json`, `events.jsonl`, optional payload files, and conditional `final-report.md` exactly as defined by the spec for `bundle`, `json`, and `jsonl` formats. |  |  |
| TASK-014 | Rewrite `examples\agent-swarm\README.md` to document the new commands, the `--demo` path, real-provider invocation, app-data layout, lock/recovery behavior, and the expected export bundle structure. |  |  |

### Implementation Phase 5

- **GOAL-005**: Add example-module tests that lock in the new command semantics and validate the supported build/test workflow.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-015 | Add `examples\agent-swarm\runtime_test.go` to cover `TargetResolver` precedence, `RunLockService` lease/TTL behavior, and `RecoveryResolver` derivation of `recoverable`, `degraded`, `events_partial`, and `execution_mode`. |  |  |
| TASK-016 | Add `examples\agent-swarm\workflow_test.go` to cover demo-mode `run` seeding, mid-run interruption after initial task/artifact persistence, successful `resume`, rejection of completed-run `resume`, and mode-mismatch rejection. |  |  |
| TASK-017 | Add `examples\agent-swarm\commands_test.go` to cover `inspect --view thread --thread-id`, `export --format bundle`, `export --format json`, `export --format jsonl`, and failure rendering when required files or event streams are unavailable. |  |  |
| TASK-018 | Validate the finished module with `Push-Location examples\agent-swarm; go build .; go test .; Pop-Location` and ensure `README.md` command examples match the actual implemented CLI flags and output paths. |  |  |

## 3. Alternatives

- **ALT-001**: Keep the existing `main.go` + `swarm.go` persona/batch demo and layer `inspect` / `export` around it. This was rejected because it would preserve a second orchestration model outside `harness\swarm` and would not exercise the new recovery/governance/event bridge substrate.
- **ALT-002**: Rebuild the example as a thin alias over `apps\mosscode`. This was rejected because the example must stay small, self-explanatory, and focused on swarm research flows rather than inherit the full product surface of another app.
- **ALT-003**: Add new helper APIs to `kernel\` or `harness\` before rewriting the example. This was rejected for v1 because the approved spec constrains the rewrite to public, already-landed swarm capabilities.

## 4. Dependencies

- **DEP-001**: `docs\superpowers\specs\2026-04-23-agent-swarm-example-redesign-design.md` — authoritative source for CLI, recovery, export, and testing semantics.
- **DEP-002**: `harness\swarm.RuntimeFeature()` and `harness\swarm.ResearchOrchestrator` — required to seed and manage the new example’s research swarm.
- **DEP-003**: `harness\appkit` runtime assembly and persistent storage helpers — required to open example-specific session/task/artifact/event stores.
- **DEP-004**: `harness\appkit\product` inspect/export rendering patterns — required so the example reuses established surfaces instead of inventing parallel viewers.
- **DEP-005**: `examples\agent-swarm\go.mod` — module boundary for `go build .` and `go test .`.

## 5. Files

- **FILE-001**: `examples\agent-swarm\main.go` — replace legacy one-shot demo entrypoint with the new command dispatcher.
- **FILE-002**: `examples\agent-swarm\config.go` — new command/config parser and validator.
- **FILE-003**: `examples\agent-swarm\runtime.go` — new runtime assembly, target resolution, recovery snapshot, and run lock services.
- **FILE-004**: `examples\agent-swarm\workflow.go` — new run/resume workflow built on `ResearchOrchestrator`.
- **FILE-005**: `examples\agent-swarm\demo_mode.go` — new deterministic demo content provider.
- **FILE-006**: `examples\agent-swarm\commands_run.go` — new `run` command handler.
- **FILE-007**: `examples\agent-swarm\commands_resume.go` — new `resume` command handler.
- **FILE-008**: `examples\agent-swarm\commands_inspect.go` — new `inspect` command handler.
- **FILE-009**: `examples\agent-swarm\commands_export.go` — new `export` command handler.
- **FILE-010**: `examples\agent-swarm\README.md` — contributor-facing usage and storage documentation.
- **FILE-011**: `examples\agent-swarm\runtime_test.go` — resolver/recovery/lock tests.
- **FILE-012**: `examples\agent-swarm\workflow_test.go` — run/resume/demo lifecycle tests.
- **FILE-013**: `examples\agent-swarm\commands_test.go` — inspect/export CLI contract tests.
- **FILE-014**: `examples\agent-swarm\swarm.go` — delete after the new workflow is fully wired.
- **FILE-015**: `examples\agent-swarm\personas.go` — delete after the new demo provider replaces persona-driven orchestration.

## 6. Testing

- **TEST-001**: Resolver contract test — verify `--session`, `--run-id`, `--latest`, and no-target fallbacks for `resume`, `inspect`, and `export`.
- **TEST-002**: Recovery snapshot test — verify `RecoveredRunSnapshot` contains messages, governance actions, artifact summaries, `execution_mode`, and diagnostic markers.
- **TEST-003**: Demo interruption/resume test — seed a demo run, interrupt after initial task/artifact persistence, then `resume` and verify completion without reseeding.
- **TEST-004**: Completed-run rejection test — verify `resume` fails for a completed run and suggests `inspect` / `export`.
- **TEST-005**: Mode-mismatch test — verify `resume --demo` fails when the persisted run mode is `real`.
- **TEST-006**: Inspect thread view test — verify `inspect --view thread` fails without `--thread-id` and succeeds with a known thread ID.
- **TEST-007**: Export bundle test — verify `bundle` writes `summary.json`, `artifacts.json`, `events.jsonl`, and `final-report.md` for completed runs.
- **TEST-008**: Export partial-event diagnostics test — verify `jsonl` fails when events are unavailable and `bundle` degrades to a warning only when `events_partial=true`.
- **TEST-009**: Module validation test — run `Push-Location examples\agent-swarm; go build .; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: Existing exported `harness\appkit` or `harness\swarm` helpers may not expose every fact needed by the example; if that occurs, implementation must stop and open a follow-up design decision instead of adding ad-hoc private behavior.
- **RISK-002**: Lock-file TTL handling can produce flaky tests if time is read directly from `time.Now`; tests should inject a controllable clock or TTL evaluator in `runtime.go`.
- **RISK-003**: Reusing product inspect/export builders may surface formatting assumptions that are tuned for `apps\mosscode`; the example must wrap them with swarm-first parameter handling instead of forking the builders.
- **ASSUMPTION-001**: The example can fully implement the approved spec without modifying `kernel\` or `harness\` public APIs.
- **ASSUMPTION-002**: The example module remains independently buildable and testable from `examples\agent-swarm`.
- **ASSUMPTION-003**: Deterministic demo outputs are sufficient to validate persistence, governance, and export flows without requiring live provider credentials in default tests.

## 8. Related Specifications / Further Reading

- `docs\superpowers\specs\2026-04-23-agent-swarm-example-redesign-design.md`
- `harness\swarm\runtime.go`
- `harness\swarm\research.go`
- `harness\appkit\product\inspect_run.go`
- `harness\appkit\product\trace.go`
