---
goal: Converge execution substrate ownership so harness owns public assembly, kernel owns ports, and runtime owns diagnostics
version: 1.0
date_created: 2026-04-13
last_updated: 2026-04-13
owner: Core Runtime Team
status: Completed
tags: [architecture, refactor, migration, harness, runtime, execution]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This implementation plan executes `docs/superpowers/specs/2026-04-13-execution-substrate-owner-convergence-design.md`. The goal is to remove `runtime.ExecutionSurface` from the public harness assembly path, converge builtin execution tooling on kernel-owned ports, retain standalone runtime diagnostics/probe behavior for doctor/inspect flows, and eliminate the remaining execution substrate owner split across `harness`, `kernel`, and `runtime`.

## 1. Requirements & Constraints

- **REQ-001**: `harness` must become the only public assembly owner for execution substrate configuration.
- **REQ-002**: Delete `harness.ExecutionSurface(surface *runtime.ExecutionSurface)` and replace it with a harness-owned execution assembly feature.
- **REQ-003**: Keep `harness.Backend` focused on `workspace.Workspace` and `workspace.Executor`; do not move isolation, repo-state, patch, or snapshot ownership into `Backend`.
- **REQ-004**: Preserve a standalone runtime execution diagnostics/probe API that can be used by `appkit/product/runtime_doctor.go` and `appkit/product/inspect_threads.go` without assembling a `kernel.Kernel`.
- **REQ-005**: Remove `runtime.ExecutionSurface` assembly behavior, including `KernelOptions()`, `WorkspacePort()`, `ExecutorPort()`, `HasWorkspace()`, `HasExecutor()`, `Sandbox()`, and any private helper such as `newExecutionSurface(...)` that only exists to bridge builtin handlers.
- **REQ-006**: Make runtime builtin tools consume already-installed kernel ports instead of recreating their own execution surface bridge.
- **REQ-007**: Keep execution capability constants, status calculation, diagnostics/probe models, and reporting helpers under a single canonical runtime owner.
- **REQ-008**: Preserve family-scoped builtin tool gating: filesystem tools depend on `workspace.Workspace`, `run_command` depends on `workspace.Executor`, and non-execution tools remain independently registrable.
- **REQ-009**: Update `appkit/deep_agent_packs.go` to compose execution substrate through a harness-owned feature instead of constructing a runtime assembly object.
- **REQ-010**: Update `harness.ExecutionCapabilityReport(...)` so it no longer depends on a runtime assembly object created from the kernel.
- **SEC-001**: Do not preserve backward compatibility for removed execution assembly APIs; deleted symbols must fail at compile time.
- **SEC-002**: Missing required execution-service configuration, local-root mismatch, and unsupported backend/service combinations must fail explicitly.
- **CON-001**: Do not perform execution policy plane convergence in this change.
- **CON-002**: Do not perform kernel `Run` vs `RunAgent` execution API convergence in this change.
- **CON-003**: Do not introduce a new public facade package to replace `runtime.ExecutionSurface`.
- **GUD-001**: Sandbox fallback remains legal only in `kernel.WithSandbox(...)` and backend activation flows; builtin tools must not recreate fallback bridges after assembly.
- **GUD-002**: Any new execution assembly implementation code must live under `internal/`; do not leave runtime as a second public assembly owner.
- **PAT-001**: Ownership after the migration must be: `harness` = public execution assembly, `kernel` = execution ports, `runtime` = diagnostics/reporting, `internal/*` = assembly implementation.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Lock the public execution assembly contract and remove runtime assembly semantics from the execution diagnostics model.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | In `harness/features.go`, delete `ExecutionSurface(surface *runtime.ExecutionSurface)` and add `ExecutionServices(workspaceRoot, isolationRoot string, isolationEnabled bool) Feature`. Give the feature explicit metadata with `Key: "execution-services"` and `Phase: FeaturePhaseConfigure`. The new feature must validate `workspaceRoot`, install only auxiliary execution services, and never install `Workspace` or `Executor` ports. | ✅ | 2026-04-13 |
| TASK-002 | Create `internal/runtimeexecution` with an assembly entrypoint that builds `workspace.WorkspaceIsolation`, `workspace.RepoStateCapture`, `workspace.PatchApply`, `workspace.PatchRevert`, and `workspace.WorktreeSnapshotStore` from `workspaceRoot`, `isolationRoot`, and `isolationEnabled`. This package must expose a harness-internal install path and must not export a public runtime-facing assembly type. | ✅ | 2026-04-13 |
| TASK-003 | In the new `internal/runtimeexecution` package, implement explicit validation rules: empty or invalid local path inputs fail; local backend root mismatch against `workspaceRoot` fails; unsupported backends for local path-scoped execution services fail. Wire these failures into `harness.ExecutionServices(...)` as install-time errors. | ✅ | 2026-04-13 |
| TASK-004 | Rename or reshape `runtime/execution_surface.go` into a diagnostics-only model such as `runtime/execution_probe.go`. Keep capability constants, status calculation, probe helpers, and reporting helpers in `runtime`; remove assembly-oriented methods and any private helper whose sole purpose is builtin-tool bridge creation. Update `runtime/execution_surface_test.go` into diagnostics-oriented tests that no longer reference assembly methods. | ✅ | 2026-04-13 |

### Implementation Phase 2

- **GOAL-002**: Converge runtime builtin tool registration and capability reporting on kernel-owned ports and runtime-owned diagnostics.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Refactor `runtime/builtin_tools_registry.go` so builtin registration is driven by kernel/explicit ports, not by `newExecutionSurface(...)` or a runtime assembly object. Preserve family-scoped gating: register filesystem tools only when `workspace.Workspace` is available, register `run_command` only when `workspace.Executor` is available, and always register `http_request`, `datetime`, and `ask_user` independently. | ✅ | 2026-04-13 |
| TASK-006 | Refactor `runtime/builtin_tools_filesystem.go` so `readFileHandler*`, `writeFileHandler*`, `editFileHandler*`, `globHandler*`, `listFilesHandler*`, and `grepHandler*` use explicit `workspace.Workspace` inputs only. Delete the sandbox bridge path that rebuilds a runtime surface. Any family-specific registration path that lacks a required port must return an explicit error. | ✅ | 2026-04-13 |
| TASK-007 | Refactor `runtime/builtin_tools_exec.go` so `runCommandHandler*` uses explicit `workspace.Executor` and optional `workspace.Workspace` inputs, never a reconstructed runtime surface. Preserve existing execution-policy enforcement behavior while removing the surface bridge. Keep output offload behavior unchanged. | ✅ | 2026-04-13 |
| TASK-008 | Update `internal/runtimeassembly/assembly.go` builtin tools provider so it calls the converged runtime registration entrypoint and computes advertised tool names from the same family-scoped gating rules. The runtime assembly package must not reintroduce `sb/ws/exec` substrate ownership through a second abstraction. | ✅ | 2026-04-13 |
| TASK-009 | Update `harness/features.go` and any new `internal/runtimeexecution` helpers so `ExecutionCapabilityReport(...)` obtains statuses from the retained runtime diagnostics owner using live kernel state, without recreating a runtime assembly object. Keep `runtime.NewCapabilityReporter(...)`, `runtime.CapabilityStatusPath()`, and snapshot/report file semantics unchanged. | ✅ | 2026-04-13 |

### Implementation Phase 3

- **GOAL-003**: Migrate appkit/product call sites and tests to the converged execution ownership model.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-010 | In `appkit/deep_agent_packs.go`, replace `runtime.NewExecutionSurface(...)` + `harness.ExecutionSurface(...)` with the new `harness.ExecutionServices(...)` feature. Preserve existing isolation-root directory creation and the current default `EnableWorkspaceIsolation` behavior. | ✅ | 2026-04-13 |
| TASK-011 | In `appkit/product/runtime_doctor.go` and `appkit/product/inspect_threads.go`, migrate `runtime.ProbeExecutionSurface(...)` and any `ExecutionSurface` type references to the retained diagnostics-only runtime API. Preserve capability item population, capability source strings, and detail strings. | ✅ | 2026-04-13 |
| TASK-012 | Update `harness/harness_test.go` to cover the new `ExecutionServices(...)` contract, including nil/invalid config failures, local-root mismatch failure, and successful installation of auxiliary execution services. Remove tests that only exist for the deleted `harness.ExecutionSurface(...)` input surface. | ✅ | 2026-04-13 |
| TASK-013 | Update runtime tests in `runtime/builtintools_test.go` and the renamed diagnostics test file so they assert: no hidden fallback bridge exists, family-scoped gating behaves correctly, runtime diagnostics still work without a kernel, and capability reporting still works from a live kernel. | ✅ | 2026-04-13 |

### Implementation Phase 4

- **GOAL-004**: Remove stale references, document the converged owner model, and validate the repository end to end.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-014 | Run search-based cleanup for stale references to `harness.ExecutionSurface`, `runtime.ExecutionSurface`, `runtime.NewExecutionSurface`, `runtime.ProbeExecutionSurface`, `runtime.ExecutionSurfaceFromKernel`, `KernelOptions()`, `WorkspacePort()`, `ExecutorPort()`, and `newExecutionSurface(...)`. Update code comments and docs that still describe runtime as a public execution assembly owner. | ✅ | 2026-04-13 |
| TASK-015 | Update active docs that explain execution assembly or diagnostics, including `docs/architecture.md` and any other current-facing docs that mention execution surface ownership, so they reflect the final split: harness assembly, kernel ports, runtime diagnostics. Do not rewrite archived historical specs/plans. | ✅ | 2026-04-13 |
| TASK-016 | Run `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`. Fix all regressions, then update this plan file with completion marks and dates for finished tasks. | ✅ | 2026-04-13 |

## 3. Alternatives

- **ALT-001**: Keep `runtime.ExecutionSurface` as the input type for `harness.ExecutionSurface(...)`; rejected because it preserves runtime as a second public assembly owner.
- **ALT-002**: Move isolation, repo-state, patch, and snapshot ownership into `harness.Backend`; rejected because it turns `Backend` into a broader execution-runtime owner and recreates a new monolith.
- **ALT-003**: Make builtin tool registration fail the entire runtime assembly whenever any execution family is unavailable; rejected because filesystem, command execution, and non-execution builtin tools have distinct dependency surfaces and should remain independently gateable.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-13-execution-substrate-owner-convergence-design.md` is the approved source of truth for this plan.
- **DEP-002**: `kernel/option.go` remains the owner of sandbox-to-port fallback through `WithSandbox(...)`, `sandboxWorkspaceAdapter`, and `sandboxExecutorAdapter`.
- **DEP-003**: `harness/backend.go` and `harness/harness.go` remain the owner of backend activation ordering and live `Workspace` / `Executor` installation.
- **DEP-004**: `internal/runtimeassembly/assembly.go` builtin tool provider is the runtime capability assembly entrypoint that must consume the converged registration API.
- **DEP-005**: `runtime` capability status snapshot/reporting helpers such as `runtime.NewCapabilityReporter(...)`, `runtime.CapabilityStatusPath()`, and `runtime.LoadCapabilitySnapshot(...)` remain the reporting substrate.
- **DEP-006**: `appkit/deep_agent_packs.go`, `appkit/product/runtime_doctor.go`, and `appkit/product/inspect_threads.go` are the primary non-test call sites that must migrate.

## 5. Files

- **FILE-001**: `docs/superpowers/specs/2026-04-13-execution-substrate-owner-convergence-design.md` — approved execution substrate convergence spec.
- **FILE-002**: `plan/architecture-execution-substrate-owner-convergence-1.md` — implementation plan for this work.
- **FILE-003**: `harness/features.go` — replace `ExecutionSurface(...)`, update `ExecutionCapabilityReport(...)`, add canonical execution assembly feature metadata.
- **FILE-004**: `harness/harness_test.go` — add execution-services tests and remove deleted execution-surface input tests.
- **FILE-005**: `harness/backend.go` — may require root-introspection or helper exposure only if needed for local-root validation; keep backend contract narrow.
- **FILE-006**: `internal/runtimeexecution/*` — new internal execution assembly package.
- **FILE-007**: `internal/runtimeassembly/assembly.go` — builtin tools provider migration to the converged runtime registration API.
- **FILE-008**: `runtime/execution_surface.go` or its replacement file — convert execution diagnostics type away from assembly semantics.
- **FILE-009**: `runtime/builtin_tools_registry.go` — converge registration gating and remove surface reconstruction.
- **FILE-010**: `runtime/builtin_tools_filesystem.go` — remove `newExecutionSurface(...)` bridge usage from filesystem handlers.
- **FILE-011**: `runtime/builtin_tools_exec.go` — remove `newExecutionSurface(...)` bridge usage from exec handlers.
- **FILE-012**: `runtime/builtintools_test.go` — update runtime builtin tool registration and handler coverage.
- **FILE-013**: `runtime/execution_surface_test.go` or renamed diagnostics test file — update probe/reporting coverage after the diagnostics-only reshape.
- **FILE-014**: `appkit/deep_agent_packs.go` — migrate execution pack composition.
- **FILE-015**: `appkit/product/runtime_doctor.go` — migrate standalone diagnostics probing.
- **FILE-016**: `appkit/product/inspect_threads.go` — migrate standalone diagnostics probing.
- **FILE-017**: `docs/architecture.md` and any other active docs that describe execution surface ownership — update current-facing architecture wording.

## 6. Testing

- **TEST-001**: `harness` tests verify `ExecutionServices(...)` installs the expected auxiliary ports and rejects invalid config, local-root mismatch, and unsupported backend/service combinations.
- **TEST-002**: Runtime builtin tool tests verify family-scoped gating remains correct and no handler path recreates a hidden execution surface bridge.
- **TEST-003**: Runtime diagnostics tests verify standalone probe behavior still works without a kernel and reporting from a live kernel still produces capability statuses.
- **TEST-004**: Appkit preset/product tests verify deep-agent execution pack, doctor, and inspect flows compile and behave against the diagnostics-only runtime API.
- **TEST-005**: Search-based validation verifies no production code still references deleted execution assembly symbols.
- **TEST-006**: Repository-wide validation runs `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: Renaming `runtime.ExecutionSurface` into a diagnostics-only model can cause a wide compile ripple if all call sites are not migrated in one pass.
- **RISK-002**: Local-root validation can become leaky if implemented by widening `Backend`; the implementation must avoid turning validation into a new public backend contract unless strictly necessary.
- **RISK-003**: Removing hidden fallback bridges may expose tests or edge flows that were accidentally depending on runtime adapter recreation instead of proper backend/kernel assembly.
- **RISK-004**: Capability reporting can drift if runtime and internal execution assembly both try to own status calculation logic; runtime must remain the sole status-model owner.
- **ASSUMPTION-001**: Current product execution services are still fundamentally local-path-scoped, so a canonical `workspaceRoot` input is acceptable for this phase.
- **ASSUMPTION-002**: `kernel.WithSandbox(...)` and backend activation already provide the only intended substrate fallback path, so runtime bridge recreation can be removed without losing legitimate behavior.
- **ASSUMPTION-003**: No external compatibility obligation exists for deleted execution assembly APIs.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-13-execution-substrate-owner-convergence-design.md`
- `docs/superpowers/specs/2026-04-13-public-assembly-convergence-design.md`
- `plan/architecture-public-assembly-convergence-1.md`
