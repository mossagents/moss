---
goal: Converge kernel execution onto one request-shaped RunAgent path and remove loop-shaped outer execution contracts
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: Core Runtime Team
status: Completed
tags: [architecture, refactor, migration, kernel, execution, agent]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This implementation plan executes `docs/superpowers/specs/2026-04-14-kernel-execution-api-convergence-design.md`. The goal is to make `kernel.RunAgent(...)` the only canonical kernel execution entry, move sync callers onto one shared collector that returns `session.LifecycleResult`, delete `Run(...)` / `RunWithUserIO(...)` / `RunWithTools(...)` / `Runner`, and remove `loop.SessionResult` from outer-layer app, runtime, gateway, and agent contracts.

## 1. Requirements & Constraints

- **REQ-001**: `kernel.RunAgent(...)` must become the only canonical kernel execution entry. No production caller may continue to depend on `Kernel.Run(...)`, `RunWithUserIO(...)`, `RunWithTools(...)`, `NewRunnerFromKernel(...)`, `Runner`, or `RunnerConfig`.
- **REQ-002**: `BuildLLMAgent(name string)` must remain the canonical root-agent factory. P3 must not reintroduce a second hidden root-agent creation path.
- **REQ-003**: `RunAgent(...)` must become request-shaped. The canonical request must carry `Session`, `Agent`, optional `UserContent`, optional `IO`, and optional `Tools`.
- **REQ-004**: `RunAgent(...)` must preserve goroutine-safe IO normalization on the canonical path for both kernel-default IO and request-scoped IO overrides.
- **REQ-005**: `RunAgentRequest.Tools` must be semantically real. For `*kernel.LLMAgent` runs, request-scoped tools must rebind a request-scoped agent instance. If an agent cannot honor a request-scoped tool override, the run must fail explicitly.
- **REQ-006**: The shared sync collector must return `*session.LifecycleResult`, not `*loop.SessionResult`, and must only succeed for authoritative result-producing runs.
- **REQ-007**: `InvocationContext.UserContent()` must be populated only from `RunAgentRequest.UserContent`; execution must not infer top-level trigger content by mutating or peeking at session history.
- **REQ-008**: Top-level callers must append user messages to the session explicitly before calling `RunAgent(...)`.
- **REQ-009**: `gateway`, `runtime.ScheduledRunner`, `agent.Delegator`, `apps\mosscode`, `contrib\tui`, `apps\mosswork`, and example programs must migrate in one pass so deleted symbols fail at compile time.
- **REQ-010**: Outer layers must stop importing `kernel/loop` for run-result handling. If `loop.SessionResult` remains after P3, it must be loop-internal only.
- **SEC-001**: Do not preserve backward compatibility shims, deprecated forwarders, or alias types for removed execution APIs.
- **SEC-002**: Unsupported request combinations such as tool override on a non-rebindable agent or sync collection on a non-result-producing agent must fail explicitly.
- **CON-001**: Do not redesign the loop engine beyond what is needed to expose authoritative terminal results on the canonical agent path.
- **CON-002**: Do not alter P2 tool-policy ownership or planner semantics as part of this phase.
- **CON-003**: Do not add a second `Kernel` method as a replacement sync facade. The sync bridge must be a helper on the canonical agent path, not a restored `Kernel.Run(...)`.
- **GUD-001**: Preserve the approved owner split: `kernel` owns execution request/result helpers, `kernel/session` owns `LifecycleResult`, `kernel/loop` is internal engine only, outer layers are consumers.
- **GUD-002**: Prefer compile-time failure over compatibility layers. Search-based cleanup is part of the phase, not optional follow-up work.
- **PAT-001**: Any authoritative terminal result capture used by the sync collector must come from the canonical agent-path run, not from best-effort reduction over arbitrary events.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Establish the canonical request-shaped `RunAgent(...)` path and its authoritative result/IO semantics inside `kernel`.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | In `kernel\kernel.go`, replace `RunAgent(ctx context.Context, sess *session.Session, agent Agent)` with a request-shaped form such as `RunAgent(ctx context.Context, req RunAgentRequest)`. Add `kernel\run_agent_request.go` to define `RunAgentRequest`, request validation, canonical defaulting for `IO`/`Tools`, and exact `UserContent` semantics. Remove `runSession(...)` from `kernel\kernel.go`; it must not survive as a hidden legacy path. | ✅ | 2026-04-14 |
| TASK-002 | In `kernel\kernel.go`, `kernel\invocation_context.go`, `kernel\llm_agent.go`, and a new helper file such as `kernel\run_collect.go`, add the authoritative terminal-result path required by the spec. `LLMAgent.Run(...)` must write a `session.LifecycleResult` into the canonical run context; the shared collector helper in `kernel\run_collect.go` must execute `RunAgent(...)`, consume the stream, and return `*session.LifecycleResult` without introducing a second `Kernel` method. | ✅ | 2026-04-14 |
| TASK-003 | In `kernel\kernel.go` and `kernel\run_supervisor.go`, preserve canonical IO sync-wrapping and simplify wrapper-shaped run-kind handling. Remove `runKindWithUserIO`; either derive delegated semantics from the request shape or delete `runKind` entirely if no behavior remains. Update `kernel\kernel_test.go` and add focused tests in a new file such as `kernel\run_collect_test.go` for request validation, `UserContent` propagation, IO sync-wrapping, tool override success/failure, timeout behavior, and collector success/failure. | ✅ | 2026-04-14 |

### Implementation Phase 2

- **GOAL-002**: Delete `Runner` and migrate kernel-adjacent abstraction layers (`gateway`, `runtime`, `agent`) to the canonical request/result contract.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | Delete `kernel\runner.go` and remove `NewRunnerFromKernel(...)` from `kernel\kernel.go`. Rewrite `kernel\agent_test.go` runner coverage so the tests exercise `RunAgent(...)` plus `RunAgentRequest.UserContent` instead of `Runner.Run(...)`. Remove all remaining test references to `Runner` and `RunnerConfig`. | ✅ | 2026-04-14 |
| TASK-005 | Refactor `gateway\gateway.go`: update the `Kernel` interface to the canonical request-path surface, migrate `Gateway.handleMessage(...)` to explicit session append plus `RunAgentRequest{UserContent: msg, Agent: <built root agent>}`, and use the shared collector helper to obtain final output. Remove `loop.SessionResult` from the gateway contract and update gateway comments/docstrings that still describe `Kernel.Run()`. | ✅ | 2026-04-14 |
| TASK-006 | Refactor `runtime\scheduled_runner.go`, `examples\mossquant\main.go`, and `examples\mossclaw\main.go` to use `RunAgent(...)` plus the shared collector. Change `ScheduledRunnerConfig.OnComplete`, `RunScheduledJob(...)`, and all scheduled-run callbacks from `*loop.SessionResult` to `*session.LifecycleResult`. Preserve explicit job-goal append and pass the same message through `RunAgentRequest.UserContent`. | ✅ | 2026-04-14 |
| TASK-007 | Refactor `agent\tools.go`, `agent\tools_helpers.go`, `agent\tools_delegate.go`, `agent\tools_lifecycle.go`, and `agent\tools_test.go` so delegation no longer depends on `RunWithTools(...)` or `*loop.SessionResult`. Replace the delegator abstraction with one delegated run path that preserves scoped tools, uses `&io.NoOpIO{}` explicitly for non-interactive delegated runs, passes the delegated task message through `RunAgentRequest.UserContent`, and returns `*session.LifecycleResult`. | ✅ | 2026-04-14 |

### Implementation Phase 3

- **GOAL-003**: Migrate interactive app callers, remaining examples, and compile-time tests off legacy kernel execution APIs.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-008 | Refactor `apps\mosscode\commands_exec.go`, `contrib\tui\app.go`, `contrib\tui\app_update_chat.go`, and `apps\mosswork\chatservice.go` to stop calling `k.Run(...)` / `k.RunWithUserIO(...)`. Each caller must append the user message explicitly, pass the same content via `RunAgentRequest.UserContent`, choose the correct root agent through `BuildLLMAgent(...)`, and consume the final result through the shared collector. Update TUI helpers such as `runTraceStatus(...)` and `runTraceError(...)` from `*loop.SessionResult` to `*session.LifecycleResult`. | ✅ | 2026-04-14 |
| TASK-009 | Refactor example and support code that still uses legacy run APIs: `examples\basic\main.go`, `examples\custom-tool\main.go`, `examples\mosswriter\main.go`, `examples\mossresearch\main.go`, `examples\websocket\main.go`, `examples\mossroom\room.go`, `harness\session_persistence_test.go`, and `internal\runtimecontext\context_test.go`. Use explicit session append plus `RunAgent(...)` and the shared collector where a sync summary is needed; if a caller only needs streamed events, consume the sequence directly and remove result-type imports entirely. | ✅ | 2026-04-14 |
| TASK-010 | Update kernel/harness tests that depend on top-level user-content semantics, especially `kernel\agent_test.go`, `kernel\kernel_test.go`, `harness\patterns\patterns_test.go`, and any tests exercising `harness\patterns\research.go`, so `InvocationContext.UserContent()` is validated through `RunAgentRequest.UserContent` instead of runner-owned hidden input append. | ✅ | 2026-04-14 |

### Implementation Phase 4

- **GOAL-004**: Remove stale symbols, eliminate outer-layer `loop.SessionResult` dependencies, and validate the repo end to end.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-011 | Run search-based cleanup across `kernel`, `agent`, `gateway`, `runtime`, `apps`, `examples`, `harness`, and `internal` to delete or internalize legacy execution symbols: `Kernel.Run(...)`, `RunWithUserIO(...)`, `RunWithTools(...)`, `runSession(...)`, `NewRunnerFromKernel(...)`, `Runner`, `RunnerConfig`, and outer-layer `loop.SessionResult` references. If `kernel\loop\SessionResult` has no remaining internal necessity after migration, delete it from `kernel\loop\loop.go`; otherwise keep it internal and remove every non-loop import. | ✅ | 2026-04-14 |
| TASK-012 | Update any current-facing docs/comments that still describe `Kernel.Run()` or `Runner` as canonical. Then run `go test ./kernel ./agent ./gateway ./runtime ./harness ./internal/runtimecontext`, `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`. Fix every regression and then update this plan file with completion marks and dates. | ✅ | 2026-04-14 |

## 3. Alternatives

- **ALT-001**: Keep `Kernel.Run(...)` as a thin deprecated forwarder to the new request path; rejected because it preserves loop-shaped outer contracts and weakens the hard-cut goal.
- **ALT-002**: Keep `Runner` as the top-level place that owns `UserContent` and input append; rejected because it would preserve a second execution shell and hidden session mutation.
- **ALT-003**: Build the sync collector by inferring final output/tokens from arbitrary streamed events only; rejected because authoritative result semantics already exist for loop-backed runs and unsupported agents should fail explicitly instead of receiving guessed summaries.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-14-kernel-execution-api-convergence-design.md` is the approved source of truth for this plan.
- **DEP-002**: `kernel\invocation_context.go` owns `UserContent` semantics that must remain usable by agent/harness pattern code after `Runner` deletion.
- **DEP-003**: `kernel\llm_agent.go` and `kernel\loop\loop_run.go` currently own the only authoritative `session.LifecycleResult` production path; the collector must anchor to that path rather than invent a parallel result engine.
- **DEP-004**: `agent\tools_helpers.go` depends on scoped tool registries plus non-interactive delegated execution, so request-scoped tool rebinding and `NoOpIO` preservation are mandatory.
- **DEP-005**: `gateway\gateway.go`, `runtime\scheduled_runner.go`, `apps\mosscode\commands_exec.go`, `contrib\tui\app.go`, and `apps\mosswork\chatservice.go` are the highest-value production callers that currently enforce the legacy execution/result contract.
- **DEP-006**: Example programs and compile-time tests (`examples\*`, `harness\session_persistence_test.go`, `internal\runtimecontext\context_test.go`) will fail immediately once legacy symbols are deleted and therefore must migrate in the same phase.

## 5. Files

- **FILE-001**: `docs\superpowers\specs\2026-04-14-kernel-execution-api-convergence-design.md` — approved design specification for this migration.
- **FILE-002**: `plan\architecture-kernel-execution-api-convergence-1.md` — implementation plan for this work.
- **FILE-003**: `kernel\kernel.go` — legacy run wrappers, canonical `RunAgent(...)`, root-agent factory, and execution entry refactor.
- **FILE-004**: `kernel\run_agent_request.go` — new request type, validation, and request normalization helpers.
- **FILE-005**: `kernel\run_collect.go` — shared collector helper for authoritative `session.LifecycleResult` folding on the agent path.
- **FILE-006**: `kernel\invocation_context.go` — `UserContent` propagation and any internal terminal-result capture plumbing.
- **FILE-007**: `kernel\llm_agent.go` — loop-backed result production on the canonical agent path.
- **FILE-008**: `kernel\run_supervisor.go` — dead wrapper-shaped run-kind cleanup.
- **FILE-009**: `kernel\runner.go` — delete this file entirely.
- **FILE-010**: `kernel\kernel_test.go` — canonical run-path behavior tests.
- **FILE-011**: `kernel\agent_test.go` — runner deletion and `UserContent` propagation coverage.
- **FILE-012**: `kernel\run_collect_test.go` — collector, IO normalization, and request validation coverage.
- **FILE-013**: `gateway\gateway.go` — gateway kernel interface and `handleMessage(...)` migration.
- **FILE-014**: `runtime\scheduled_runner.go` — scheduled execution API/result migration.
- **FILE-015**: `agent\tools.go` — delegator abstraction changes.
- **FILE-016**: `agent\tools_helpers.go` — delegated session creation and delegated run execution path.
- **FILE-017**: `agent\tools_delegate.go` and `agent\tools_lifecycle.go` — delegated/run-task call sites.
- **FILE-018**: `agent\tools_test.go` — delegator and result-shape test migration.
- **FILE-019**: `apps\mosscode\commands_exec.go` — mosscode execution entry migration.
- **FILE-020**: `contrib\tui\app.go` and `contrib\tui\app_update_chat.go` — TUI append/run flow and result trace helpers.
- **FILE-021**: `apps\mosswork\chatservice.go` — Wails chat execution migration with IO override.
- **FILE-022**: `examples\basic\main.go`, `examples\custom-tool\main.go`, `examples\mosswriter\main.go`, `examples\mossresearch\main.go`, `examples\websocket\main.go`, `examples\mossroom\room.go`, `examples\mossquant\main.go`, and `examples\mossclaw\main.go` — example migration off legacy run/result APIs.
- **FILE-023**: `harness\session_persistence_test.go` and `internal\runtimecontext\context_test.go` — compile-time migration tests that currently use `k.Run(...)`.
- **FILE-024**: `kernel\loop\loop.go` — possible deletion/internalization of `SessionResult`.

## 6. Testing

- **TEST-001**: Kernel API tests verify `RunAgentRequest` validation, `UserContent` propagation into `InvocationContext`, IO sync-wrapping, request-scoped tool override semantics, timeout behavior, and explicit errors on unsupported tool override combinations.
- **TEST-002**: Collector tests verify loop-backed runs return authoritative `session.LifecycleResult`, failed runs surface `error`, and non-result-producing agents fail explicitly instead of returning guessed summaries.
- **TEST-003**: Gateway tests or compile checks verify `Gateway.handleMessage(...)` appends inbound content explicitly and no longer depends on `Kernel.Run(...)` or `*loop.SessionResult`.
- **TEST-004**: Scheduled runner tests verify `RunScheduledJob(...)` and `OnComplete` use `*session.LifecycleResult` and preserve explicit user-goal propagation.
- **TEST-005**: Agent delegation tests verify scoped tool registries, `NoOpIO`, delegated `UserContent`, and result handling all survive the migration.
- **TEST-006**: App/TUI tests verify result trace/status helpers, interactive IO override paths, and top-level append-and-run flows still behave correctly after the request-path migration.
- **TEST-007**: Search-based verification shows no production `.go` callers of `Kernel.Run(...)`, `RunWithUserIO(...)`, `RunWithTools(...)`, `Runner`, or outer-layer `*loop.SessionResult`.
- **TEST-008**: Repository validation runs `go test ./kernel ./agent ./gateway ./runtime ./harness ./internal/runtimecontext`, `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: If request-scoped tool rebinding only changes the request object but not the actual agent instance, delegated runs will silently lose scoped-tool isolation.
- **RISK-002**: If IO sync-wrapping is not preserved on the canonical path, parallel tool execution can regress into unsafe or garbled output.
- **RISK-003**: If the sync collector is implemented as a heuristic event reducer rather than an authoritative result reader, app/runtime callers may receive incomplete token/output summaries.
- **RISK-004**: `Gateway`, `ScheduledRunner`, and delegated-agent flows all depend on explicit input propagation; a missed `UserContent` handoff can break custom-agent or harness-pattern behavior even if session messages still exist.
- **RISK-005**: Example programs and tests span many packages; partial migration will leave the repo uncompilable once the legacy kernel methods are deleted.
- **ASSUMPTION-001**: The only production sync consumers that require authoritative final summaries are loop-backed/root-agent runs, so explicit failure on unsupported agent types is acceptable.
- **ASSUMPTION-002**: `BuildLLMAgent(...)` remains sufficient as the canonical root-agent factory for migrated top-level callers.
- **ASSUMPTION-003**: No compatibility requirement exists for preserving `Runner`, `Run(...)`, or `*loop.SessionResult` in public or semi-public outer-layer contracts.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-14-kernel-execution-api-convergence-design.md`
- `docs/superpowers/specs/2026-04-14-execution-policy-plane-convergence-design.md`
- `plan/architecture-execution-policy-plane-convergence-1.md`
