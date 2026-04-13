---
goal: Converge public assembly ownership to harness and remove runtime/appkit facade residue
version: 1.0
date_created: 2026-04-13
last_updated: 2026-04-13
owner: Core Runtime Team
status: Planned
tags: [architecture, refactor, migration, harness, runtime, appkit]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This implementation plan executes the approved maximum-P0 direction in `docs/superpowers/specs/2026-04-13-public-assembly-convergence-design.md`. The goal is to make `harness.Feature` the only canonical public assembly unit, remove runtime-owned public assembly entrypoints, reduce `appkit` root to builder/preset APIs only, and migrate all remaining callers without preserving compatibility shims.

## 1. Requirements & Constraints

- **REQ-001**: `harness.Feature` must become the only canonical public assembly surface for runtime capability composition.
- **REQ-002**: Remove runtime public assembly entrypoints including `runtime.Setup(...)`, `runtime.Option`, `runtime.ContextOption`, planning/context/offload/bootstrap installation hooks, `runtime.AutoCompactHook(...)`, `runtime.ServeCLI(...)`, `runtime.PlanningTodoItem`, and `runtime.WithKernelSessionStore(...)`.
- **REQ-003**: Replace `runtime.Option` semantics with harness-owned runtime setup options and update `harness.RuntimeSetup(...)` to own the public setup contract.
- **REQ-004**: Update `appkit.DeepAgentConfig.DefaultSetupOptions` to stop depending on `runtime.Option` and consume the new harness-owned setup options instead.
- **REQ-005**: Remove appkit root non-builder/non-preset facades and their orphaned companion types: `REPL`, `REPLConfig`, `Serve`, `ServeConfig`, `Health*`, `HealthOutput`, `HealthStatus`, `RuntimeBuilder`, `ContextWithSignal`, and `FirstNonEmpty`.
- **REQ-006**: Convert deep-agent generic assembly closures in `appkit/deep_agent_packs.go` into canonical harness feature primitives for state catalog installation, execution surface installation, and execution capability reporting.
- **REQ-007**: Preserve runtime capability behavior, trust-gated agent discovery behavior, planning/context behavior, and execution capability reporting semantics after the public surface migration.
- **REQ-008**: Explicitly retain runtime reporting substrate types: `runtime.CapabilityReporter`, `runtime.NewCapabilityReporter(...)`, `runtime.CapabilityStatusPath()`, `runtime.CapabilityStatus`, and `runtime.CapabilitySnapshot`.
- **SEC-001**: Deleted compatibility surfaces must fail at compile time; do not add silent forwarding wrappers or deprecated aliases.
- **SEC-002**: Preserve explicit error propagation for invalid setup option combinations, missing required stores/managers, and capability installation failures.
- **CON-001**: Do not perform the P1 execution substrate owner migration (`harness.Backend` vs `runtime.ExecutionSurface`) in this change.
- **CON-002**: Do not perform the P2 execution-policy enforcement-plane convergence in this change.
- **CON-003**: Do not perform the P3 kernel execution API convergence (`Run` vs `RunAgent`) in this change.
- **CON-004**: Do not introduce a new public facade package to replace the deleted appkit/runtime convenience shells.
- **GUD-001**: Shared runtime capability assembly logic must move into `internal/runtimeassembly`; do not leave runtime public setup hooks alive.
- **GUD-002**: Planning install/state logic must move into `internal/runtimeplanning`, and context/offload install/state logic must move into `internal/runtimecontext`; do not defer these package boundaries until mid-migration.
- **GUD-003**: `harness.ContextOption` must become a harness-owned functional option over a harness-local config struct, then be translated into the concrete `internal/runtimecontext` config at installation time.
- **GUD-004**: Keep preset-specific behavior in `appkit`, but move generic assembly primitives to canonical harness features.
- **PAT-001**: Public ownership model after the migration must be: `harness` = assembly facade, `appkit` = builders/presets, `runtime` = substrate/service/reporting internals.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Lock the non-public package boundaries and replace runtime public setup entrypoints with a harness-owned public setup contract.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Before code movement begins, define and document the exact package split for `internal/runtimeassembly`, `internal/runtimeplanning`, and `internal/runtimecontext`. This checkpoint must also lock the harness-local `ContextOption` mechanism (`type ContextOption func(*contextFeatureConfig)` or equivalent) and the translation path from harness config into `internal/runtimecontext` config. |  |  |
| TASK-002 | Create `internal/runtimeassembly` and move the runtime capability assembly pipeline out of `runtime/runtime_setup.go` and `runtime/lifecycle_manager.go`. The new package must own config resolution, capability sequencing, and the install/validate/activate flow currently driven by `runtime.Setup(...)`. |  |  |
| TASK-003 | In `harness/runtime_features.go`, replace the current `RuntimeSetup(workspaceDir, trust string, opts ...runtime.Option)` contract with a harness-owned option type (for example `RuntimeSetupOption`) and harness-owned option constructors covering builtin tools, MCP servers, skills, progressive skills, agents, and capability reporter injection. `trust` must remain an explicit input to `RuntimeSetup(...)`; do not recreate `WithWorkspaceTrust(...)` as a public runtime option. |  |  |
| TASK-004 | Update `harness/runtime_features.go` so `RuntimeSetup(...)` calls `internal/runtimeassembly` instead of `runtime.Setup(...)`. Delete the dead `planning` runtime setup config field and remove `WithPlanning(...)` instead of migrating it. |  |  |
| TASK-005 | Update `appkit/deep_agent.go`, `appkit/deep_agent_packs.go`, and `appkit/deep_agent_test.go` so `DeepAgentConfig.DefaultSetupOptions` changes from `[]runtime.Option` to the new harness-owned setup option type and all existing tests use harness-owned option constructors instead of `runtime.With*`. |  |  |
| TASK-006 | Update direct setup call sites that currently rely on `runtime.Setup(...)` or `runtime.With*` configuration, including `contrib/tui/app_test.go`, `appkit/appkit_test.go`, `runtime/runtime_test.go`, and `runtime/runtime_agents_service_test.go`. Use the harness-owned setup surface for cross-package callers and keep any runtime-internal direct assembly coverage on `internal/runtimeassembly` only if package-internal tests require it. |  |  |

### Implementation Phase 2

- **GOAL-002**: Make planning/context/offload/bootstrap install surfaces fully harness-owned and delete runtime public assembly hooks.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-007 | Create `internal/runtimeplanning` and move the planning installer/state machine currently exposed by `runtime/planning.go` into it. Update `harness/runtime_features.go` so the public planning install surface is fully harness-owned, then remove `runtime.WithPlanningSessionManager(...)`, `runtime.RegisterPlanningTools(...)`, and `runtime.WithPlanningDefaults(...)` from the runtime public package while preserving `write_todos` tool behavior and prompt text semantics. |  |  |
| TASK-008 | Create `internal/runtimecontext` and move the context/offload installer/state machine currently exposed by `runtime/context.go` and used by `runtime/context_manager.go` into it. Update `harness/runtime_features.go` so `ContextOffload(...)` and `ContextManagement(...)` become fully harness-owned and translate harness-local context config into the new internal package config. Remove `runtime.ContextOption`, `runtime.WithContextSessionStore(...)`, `runtime.WithContextSessionManager(...)`, `runtime.ConfigureContext(...)`, `runtime.WithOffloadSessionStore(...)`, `runtime.RegisterOffloadTools(...)`, and `runtime.AutoCompactHook(...)` from the runtime public package while preserving `offload_context`, `compact_conversation`, auto-compact, and prompt-fragment behavior. |  |  |
| TASK-009 | Delete the runtime bootstrap convenience wrappers in `runtime/bootstrap.go` and keep `harness.BootstrapContext(...)`, `harness.BootstrapContextValue(...)`, and `harness.LoadedBootstrapContext(...)` as the only canonical bootstrap assembly surface. Update any remaining callers to use `harness` or `bootstrap` directly. |  |  |

### Implementation Phase 3

- **GOAL-003**: Remove appkit-local generic assembly closures, add canonical harness replacements, and trim appkit/runtime facade residue.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-010 | Add canonical harness features for state catalog installation, execution surface installation, and execution capability reporting in `harness/features.go` or a dedicated new harness feature file. The feature metadata must be explicit: state catalog = `Phase: Configure`, `Key: "state-catalog"`; execution surface = `Phase: Configure`, `Key: "execution-surface"`; execution capability report = `Phase: PostRuntime`, `Key: "execution-capability-report"`, `Requires: []string{"execution-surface"}`. Implement these features using `runtime.WithStateCatalog(...)`, `runtime.ExecutionSurface.KernelOptions()`, `runtime.ExecutionSurfaceFromKernel(...)`, `runtime.ReportExecutionSurface(...)`, and retained reporting substrate types such as `runtime.NewCapabilityReporter(...)` and `runtime.CapabilityStatusPath()`. |  |  |
| TASK-011 | Replace `deepAgentStateCatalogFeature(...)`, `deepAgentExecutionSurfaceFeature(...)`, and `deepAgentExecutionCapabilityReportFeature(...)` in `appkit/deep_agent_packs.go` with the new canonical harness features. Keep `deepAgentGeneralPurposeFeature(...)` unchanged because it is preset-specific behavior rather than a general assembly primitive. |  |  |
| TASK-012 | Delete appkit root facades and duplicate utilities: remove `appkit/repl.go`, `appkit/serve.go`, `appkit/runtime_builder.go`, and the exported helper functions/types in `appkit/appkit.go` plus the orphaned companion types `appkit.REPLConfig`, `appkit.ServeConfig`, `appkit.HealthOutput`, and `appkit.HealthStatus`. Update `apps/mosscode/root.go` to use `internal/strutil.FirstNonEmpty(...)` and `signal.NotifyContext(...)`, and update example programs in `examples/basic/main.go`, `examples/custom-tool/main.go`, `examples/mossclaw/main.go`, `examples/mossresearch/main.go`, `examples/mossroom/main.go`, `examples/mosswriter/main.go`, and `examples/websocket/main.go` so they no longer import deleted appkit root helpers or facades. For former `appkit.REPL(...)` call sites, inline the local stdin loop in the example itself; do not introduce a new shared REPL abstraction. |  |  |
| TASK-013 | Delete `runtime/gateway.go` (`ServeConfig`, `ServeCLI`) and `runtime/sessionstore.go` (`WithKernelSessionStore`) after their final callers are migrated. Delete `runtime.PlanningTodoItem` if no external repo caller remains; do not preserve it as a stray substrate type. Do not replace any of these with new public wrappers. If example programs still need CLI/gateway demo wiring, move that code to example-local composition rather than another public package. |  |  |

### Implementation Phase 4

- **GOAL-004**: Validate repository-wide correctness and remove stale references to the deleted assembly surfaces.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-014 | Update comments, docs, and tests that still describe deleted runtime/appkit assembly entrypoints, including `appkit/builder.go` comments and any test names/assertions that refer to `runtime.Setup(...)`, `runtime.Option`, `appkit.REPL(...)`, `appkit.REPLConfig`, `appkit.Serve(...)`, or health-output helper types. |  |  |
| TASK-015 | Run search-based validation proving no remaining production references to the deleted runtime/appkit surfaces exist, then run `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`. Fix all regressions before marking the migration complete. |  |  |

## 3. Alternatives

- **ALT-001**: Keep `runtime.Setup(...)` and `runtime.Option` as deprecated forwarding wrappers to the new harness-owned setup contract; rejected because it preserves runtime as a second public assembly owner.
- **ALT-002**: Keep `appkit.REPL(...)`, `appkit.Serve(...)`, and `appkit.ContextWithSignal(...)` as convenience APIs; rejected because they keep appkit root wider than builder/preset scope and preserve the wrong public mental model.
- **ALT-003**: Leave `deepAgentStateCatalogFeature(...)` and related execution-surface closures private inside appkit; rejected because they are generic assembly primitives that should belong to the canonical harness surface.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-13-public-assembly-convergence-design.md` is the approved source of truth for scope and invariants.
- **DEP-002**: `harness/feature.go` and harness metadata governance remain the installation planner for official public features.
- **DEP-003**: `runtime` substrate/service types such as `StateCatalog`, `WrapSessionStore(...)`, `WrapCheckpointStore(...)`, `WrapTaskRuntime(...)`, `ResolveExecutionPolicyForWorkspace(...)`, and `ExecutionSurface` remain available to compose the new canonical harness features.
- **DEP-004**: `internal/runtimeassembly`, `internal/runtimeplanning`, and `internal/runtimecontext` are the non-public implementation homes for the removed runtime public assembly surfaces.
- **DEP-005**: `bootstrap.LoadWithAppName(...)` and `bootstrap.LoadWithAppNameAndTrust(...)` remain the source of bootstrap context loading once runtime bootstrap wrappers are deleted.
- **DEP-006**: `internal/strutil.FirstNonEmpty(...)` and `signal.NotifyContext(...)` replace the deleted appkit root helper exports at app/example call sites.
- **DEP-007**: Existing example-specific gateway/session wiring packages (`gateway`, `gateway/channel`, `kernel/session`) remain available for example-local composition after deleting `appkit.Serve(...)` and `runtime.ServeCLI(...)`.

## 5. Files

- **FILE-001**: `docs/superpowers/specs/2026-04-13-public-assembly-convergence-design.md` — approved design spec for this migration.
- **FILE-002**: `internal/runtimeassembly/*` — new non-public runtime capability assembly pipeline.
- **FILE-003**: `internal/runtimeplanning/*` — new non-public planning installer/state package.
- **FILE-004**: `internal/runtimecontext/*` — new non-public context/offload installer/state package.
- **FILE-005**: `harness/runtime_features.go` — canonical public runtime setup / planning / context feature surface.
- **FILE-006**: `harness/features.go` or a new harness feature file — canonical state-catalog / execution-surface / capability-report features.
- **FILE-007**: `runtime/runtime_setup.go` — remove public setup API and dead planning config.
- **FILE-008**: `runtime/lifecycle_manager.go` — move public orchestration responsibilities into the non-public assembly package.
- **FILE-009**: `runtime/planning.go` — remove public planning installers.
- **FILE-010**: `runtime/context.go` and `runtime/context_manager.go` — remove public context/offload installers and move the state machine into `internal/runtimecontext`.
- **FILE-011**: `runtime/bootstrap.go` — delete runtime bootstrap convenience wrappers.
- **FILE-012**: `runtime/gateway.go` — delete `ServeConfig` and `ServeCLI(...)`.
- **FILE-013**: `runtime/sessionstore.go` — delete `WithKernelSessionStore(...)`.
- **FILE-014**: `appkit/deep_agent.go` — switch `DefaultSetupOptions` away from `runtime.Option`.
- **FILE-015**: `appkit/deep_agent_packs.go` — replace appkit-local generic assembly closures with canonical harness features.
- **FILE-016**: `appkit/builder.go` — update comments that currently reference `runtime.Setup(...)`.
- **FILE-017**: `appkit/repl.go`, `appkit/serve.go`, `appkit/runtime_builder.go`, `appkit/appkit.go` — remove non-builder/non-preset root facades, helpers, and orphaned companion types.
- **FILE-018**: `apps/mosscode/root.go` — migrate off deleted appkit helper exports.
- **FILE-019**: `examples/basic/main.go`, `examples/custom-tool/main.go`, `examples/mossclaw/main.go`, `examples/mossresearch/main.go`, `examples/mossroom/main.go`, `examples/mosswriter/main.go`, `examples/websocket/main.go` — migrate off deleted appkit/runtime facades.
- **FILE-020**: `appkit/appkit_test.go`, `appkit/deep_agent_test.go`, `contrib/tui/app_test.go`, `runtime/runtime_test.go`, `runtime/runtime_agents_service_test.go` — update affected tests.

## 6. Testing

- **TEST-001**: Harness runtime-setup tests verify the new harness-owned setup options reproduce the prior `runtime.With*` semantics for builtin tools, MCP servers, skills, progressive skills, agents, and capability reporter injection.
- **TEST-002**: Harness planning/context tests verify `write_todos`, `offload_context`, `compact_conversation`, auto-compact, and prompt behavior remain unchanged after moving feature logic into `internal/runtimeplanning` and `internal/runtimecontext`.
- **TEST-003**: Appkit deep-agent tests verify `DeepAgentConfig.DefaultSetupOptions` uses harness-owned setup options and deep-agent pack assembly no longer relies on appkit-local generic assembly closures.
- **TEST-004**: Runtime tests verify capability reporting, trusted/restricted agent discovery, and validation/activation sequencing still behave the same through the new non-public assembly pipeline.
- **TEST-005**: `contrib/tui/app_test.go` no longer relies on `runtime.Setup(...)` and still validates the expected skills summary behavior.
- **TEST-006**: App/example compilation proves no call site still imports deleted appkit root helpers or runtime assembly facades.
- **TEST-007**: Search-based validation proves no remaining production references to `runtime.Setup`, `runtime.Option`, `runtime.ContextOption`, `runtime.AutoCompactHook`, `runtime.WithContextSessionStore`, `runtime.RegisterPlanningTools`, `runtime.PlanningTodoItem`, `runtime.ServeCLI`, `appkit.REPL`, `appkit.REPLConfig`, `appkit.Serve`, `appkit.ServeConfig`, `appkit.RuntimeBuilder`, `appkit.ContextWithSignal`, or `appkit.FirstNonEmpty` remain.
- **TEST-008**: Example-call-site validation proves former `appkit.REPL(...)` users now contain local inline loops rather than a new shared wrapper.
- **TEST-009**: Repository-wide validations: `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: Extracting runtime capability assembly into a non-public package may introduce hidden dependency cycles if capability installers are not moved coherently with their config/orchestration code.
- **RISK-002**: The chosen split across `internal/runtimeassembly`, `internal/runtimeplanning`, and `internal/runtimecontext` may still need minor file reshaping to avoid import cycles or duplicated config structs.
- **RISK-003**: Context/offload refactoring may accidentally change prompt-fragment or auto-compact behavior if public installer deletion is coupled too tightly to runtime implementation code.
- **RISK-004**: Example-program migrations may accidentally reintroduce a new public helper facade if shared demo code is moved to another public package instead of staying example-local.
- **RISK-005**: Deleting appkit root helpers can break examples/apps unexpectedly if all call sites are not found before file deletion.
- **ASSUMPTION-001**: No external consumer needs backward compatibility for the deleted runtime/appkit public assembly surfaces.
- **ASSUMPTION-002**: `runtime.ExecutionSurface` and `runtime.StateCatalog` can remain exported substrate types in this phase even though the owner convergence for execution substrate is deferred to P1.
- **ASSUMPTION-003**: The architecture reviewer may still request boundary refinements; if that happens, this plan will be updated rather than preserving a stale draft.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-13-public-assembly-convergence-design.md`
- `harness/runtime_features.go`
- `harness/features.go`
- `appkit/deep_agent_packs.go`
- `runtime/runtime_setup.go`
- `runtime/context.go`
- `runtime/planning.go`
- `runtime/bootstrap.go`
