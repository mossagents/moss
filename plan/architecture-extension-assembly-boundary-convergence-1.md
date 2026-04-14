---
goal: Converge extension and assembly ownership so harness.Feature is the only canonical public assembly unit
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: Core Runtime Team
status: Completed
tags: [architecture, refactor, migration, harness, appkit, runtime, capability, kernel]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This implementation plan executed `docs/superpowers/specs/2026-04-14-extension-assembly-boundary-convergence-design.md`. The result makes `harness.Feature` the only canonical public assembly/composition unit, keeps `kernel.Plugin` as the lifecycle primitive instead of a competing assembly story, moves runtime capability mutation helpers behind internal assembly ownership, and tightens `ServiceRegistry` posture so it is treated as substrate state rather than a public extension model.

## 1. Requirements & Constraints

- **REQ-001**: `harness.Feature` must remain the only canonical public assembly/composition unit. App/product callers must assemble runtime behavior through features rather than through parallel public builder or runtime mutation surfaces.
- **REQ-002**: `appkit.BuildKernel(...)` must stop accepting raw `kernel.Option` variadics. Pre-backend port injection must happen through direct `kernel.New(...)` + `harness.NewWithBackendFactory(...)`; `BuildKernelWithFeatures(..., harness.KernelOptions(...))` is only for post-backend configure-phase options.
- **REQ-003**: `appkit.BuildKernelWithFeatures(...)` must remain the canonical public feature-first builder. `appkit.BuildDeepAgent(...)` must remain a preset wrapper that resolves feature packs and delegates to `BuildKernelWithFeatures(...)`.
- **REQ-004**: `kernel.Plugin` must remain a public lifecycle primitive, but public assembly guidance must route app/product composition through `harness.Plugins(...)` instead of treating `InstallPlugin(...)` as a competing top-level public story.
- **REQ-005**: Public `runtime` capability helpers must become lookup/read oriented only. Exported mutation/setup helpers such as `CapabilityDeps(k)`, `WithCapabilityManager(...)`, `SetSkillManifests(...)`, `EnableProgressiveSkills(...)`, and `RegisterProgressiveSkillTools(...)` must be removed or internalized.
- **REQ-006**: Public read-side replacements must be explicit and side-effect free. Introduce `runtime.LookupCapabilityManager(k)` and `runtime.LookupSkillManifests(k)` (or exact equivalents with the same lookup-only semantics) for UI/product inspection paths.
- **REQ-007**: `internal/runtimeassembly` must stop calling public runtime mutation helpers for capability assembly. Capability registration, manifest storage, progressive-skill enablement, and runtime activation must use internal setup ownership only.
- **REQ-008**: `ServiceRegistry` must remain available only as kernel-scoped substrate/state. No new app/product/public API may expose raw `Kernel.Services()` semantics as an assembly or extension contract.
- **REQ-009**: Current read-side callers in `userio/prompting`, `contrib/tui`, `apps/mosswork`, and related tests must migrate to lookup-only runtime helpers without triggering setup-time mutation on first access.
- **REQ-010**: Appkit-owned logger wiring must be expressed through feature composition instead of direct builder-time `k.InstallPlugin(...)` mutation.
- **SEC-001**: Use hard cuts and compile-time failures instead of compatibility shims. Do not preserve deprecated public forwarders for removed runtime mutation helpers or removed `BuildKernel(..., extraOpts...)`.
- **SEC-002**: Lookup helpers must not lazily create or mutate capability state. They must not register prompts, shutdown hooks, tools, or managers on first read.
- **CON-001**: Do not redesign the internals of `capability.Manager`, `capability.Provider`, or the full `ServiceRegistry` substrate in this plan.
- **CON-002**: Do not fold posture/governance convergence or compat-tail cleanup into this implementation plan.
- **CON-003**: Every implementation phase must leave the repository compiling. Temporary overlap inside the branch is acceptable only when required to keep intermediate phases buildable.
- **GUD-001**: Keep boundary language explicit: `Feature` assembles systems, `Plugin` describes lifecycle behavior, `Provider` describes runtime-loadable capability units, and `ServiceRegistry` stores typed subsystem state.
- **GUD-002**: Prefer moving setup-time helpers inward over inventing a second public "advanced mode" for assembly.
- **PAT-001**: Public read APIs use lookup-only names and behavior; setup/activation helpers remain internal or package-private.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Compress `appkit` onto one feature-first public assembly story and remove raw option bypasses from the public builder layer.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | In `appkit\builder.go`, remove `extraOpts ...kernel.Option` from `BuildKernel(...)`. Make `BuildKernel(...)` construct only the official default feature list (at minimum `harness.RuntimeSetup(flags.Workspace, flags.Trust)`) and delegate to `BuildKernelWithFeatures(...)`. Update builder comments so they explicitly direct pre-backend control to direct `kernel.New(...)` + `harness.NewWithBackendFactory(...)`, and reserve `harness.KernelOptions(...)` for post-backend configure-phase options. | ✅ | 2026-04-14 |
| TASK-002 | In `appkit\builder.go`, replace direct debug logger mutation (`k.InstallPlugin(builtins.LoggerPlugin())`) with feature composition through `harness.Plugins(builtins.LoggerPlugin())` or an equivalent feature-first path. The appkit builder layer must not mutate plugins directly after feature installation. | ✅ | 2026-04-14 |
| TASK-003 | In `appkit\deep_agent.go`, `appkit\deep_agent_packs.go`, `appkit\appkit_test.go`, and `appkit\deep_agent_test.go`, preserve `BuildDeepAgent(...)` as a preset-to-feature wrapper and verify that default, feature-first, and deep-agent builders all converge on the same `BuildKernelWithFeatures(...)` assembly path with no remaining public raw-option escape hatch. | ✅ | 2026-04-14 |

### Implementation Phase 2

- **GOAL-002**: Split runtime capability surfaces into public lookup-only helpers and internal setup-only assembly ownership.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | In `runtime\runtime_capability_service.go`, remove exported mutation/setup helpers from the public runtime package. Introduce lookup-only public replacements such as `LookupCapabilityManager(k)` and `LookupSkillManifests(k)` that return current state without creating or mutating capability state. Preserve only the read-side API that UI/product callers need. | ✅ | 2026-04-14 |
| TASK-005 | In `internal\runtimeassembly\assembly.go` plus the new internal owner `internal\runtimecapability\service.go`, move capability setup ownership inward: manager creation, dependency assembly, manifest storage, progressive-skill enablement, and progressive-skill tool registration must all happen through internal helpers rather than exported `runtime.*` mutation APIs. | ✅ | 2026-04-14 |
| TASK-006 | Migrate read-side callers and tests to the lookup-only runtime surfaces: `userio\prompting\composer.go`, `contrib\tui\app.go`, `apps\mosswork\chatservice.go`, `appkit\appkit_test.go`, and `internal\runtimeassembly\assembly_test.go`. Ensure these callers can inspect loaded capabilities/manifests without triggering setup-time mutation. | ✅ | 2026-04-14 |

### Implementation Phase 3

- **GOAL-003**: Tighten `ServiceRegistry` posture so the repo treats it as typed substrate state instead of a public extension model.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-007 | In `kernel\services.go`, `kernel\kernel.go`, and the typed owner packages that currently sit on the registry (`agent\kernel_service.go`, `kernel\session_persistence.go`, `runtime\state_service.go`, `runtime\memory.go`, `internal\runtimeplanning\planning.go`, `internal\runtimecontext\context.go`, `internal\runtimepolicy\policystate\state.go`), update comments and helper names so `ServiceRegistry` is explicitly described as substrate-only state. Do not add any new public helper that returns or exposes raw bag semantics. | ✅ | 2026-04-14 |
| TASK-008 | Add or update targeted regression coverage and search-based checks so public assembly/package code no longer depends on removed runtime mutation helpers or direct `Kernel.Services()` composition. Keep raw `Services()` usage confined to typed state-owner packages only. | ✅ | 2026-04-14 |

### Implementation Phase 4

- **GOAL-004**: Finish current-facing docs/comments and validate the converged boundary model end to end.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Update current-facing docs/comments where the public assembly story still implies raw appkit option bypasses or public runtime capability mutation helpers are canonical. This must at least cover `appkit\builder.go` comments and any current docs that mention removed runtime mutation helpers. Do not rewrite archived historical specs or plans. | ✅ | 2026-04-14 |
| TASK-010 | Run targeted validation for touched owners: `go test ./appkit/... ./runtime ./internal/runtimeassembly ./userio/...`, `Push-Location apps\mosswork; go test .; Pop-Location`, and `Push-Location contrib\tui; go test .; Pop-Location`. Fix every regression before proceeding to full validation. Then run `go test ./...`, `go build ./...`, plus the separate `contrib\tui` and `apps\mosswork` module validation. After all commands pass, update this file with completion marks/dates and set the front matter status to `Completed`. | ✅ | 2026-04-14 |

## 3. Alternatives

- **ALT-001**: Preserve `BuildKernel(..., extraOpts...)` and public runtime mutation helpers as an "advanced mode"; rejected because it keeps multiple public assembly stories alive and undermines the feature-first convergence goal.
- **ALT-002**: Remove `kernel.Plugin` and make `Feature` the only extension concept; rejected because `Plugin` is already a clean lifecycle primitive and the current problem is ownership overlap, not the existence of `Plugin`.
- **ALT-003**: Fully delete `ServiceRegistry` and replace it with a new typed state framework in the same round; rejected because that would turn this convergence into a broad state-substrate rewrite.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-14-extension-assembly-boundary-convergence-design.md` is the approved design source of truth for this plan.
- **DEP-002**: `harness\feature.go`, `harness\features.go`, and `harness\harness.go` already define the canonical feature-first assembly model that this migration must strengthen rather than bypass.
- **DEP-003**: `appkit\builder.go`, `appkit\deep_agent.go`, and `appkit\deep_agent_packs.go` currently own the overlapping public builder surfaces that must be compressed in Phase 1.
- **DEP-004**: `runtime\runtime_capability_service.go`, `internal\runtimeassembly\assembly.go`, and `internal\runtimecapability\service.go` define the final split between public lookup-only runtime helpers and internal capability setup ownership; Phase 2 converges that split without changing `capability.Provider`.
- **DEP-005**: `userio\prompting\composer.go`, `contrib\tui\app.go`, and `apps\mosswork\chatservice.go` are the main read-side callers that will prove lookup-only capability access remains sufficient for UI/product inspection.
- **DEP-006**: `kernel\services.go` plus typed state-owner packages (`agent`, `runtime`, `internal/runtime*`, `kernel`) already provide the substrate/state pattern that Phase 3 must clarify instead of replacing.

## 5. Files

- **FILE-001**: `docs\superpowers\specs\2026-04-14-extension-assembly-boundary-convergence-design.md` — approved design specification for this migration.
- **FILE-002**: `plan\architecture-extension-assembly-boundary-convergence-1.md` — implementation plan for this work.
- **FILE-003**: `appkit\builder.go` — public builder compression and removal of raw option variadic from `BuildKernel(...)`.
- **FILE-004**: `appkit\deep_agent.go` and `appkit\deep_agent_packs.go` — deep-agent preset wrapper alignment to the canonical feature-first builder path.
- **FILE-005**: `appkit\appkit_test.go` and `appkit\deep_agent_test.go` — regression coverage for the converged appkit builder model.
- **FILE-006**: `runtime\runtime_capability_service.go` — removal of exported setup-time capability helpers and introduction of lookup-only read helpers.
- **FILE-007**: `internal\runtimeassembly\assembly.go` and `internal\runtimecapability\service.go` — internal ownership of capability setup/manifest/progressive-skill orchestration.
- **FILE-008**: `userio\prompting\composer.go`, `contrib\tui\app.go`, and `apps\mosswork\chatservice.go` — migration to lookup-only capability read helpers.
- **FILE-009**: `kernel\services.go` and `kernel\kernel.go` — ServiceRegistry posture clarification at the kernel substrate boundary.
- **FILE-010**: `agent\kernel_service.go`, `kernel\session_persistence.go`, `runtime\state_service.go`, `runtime\memory.go`, `internal\runtimeplanning\planning.go`, `internal\runtimecontext\context.go`, and `internal\runtimepolicy\policystate\state.go` — typed state-owner packages whose comments/helpers must keep raw registry use internal to the owner layer.
- **FILE-011**: `internal\runtimeassembly\assembly_test.go`, `appkit\appkit_test.go`, and any touched runtime/UI/product tests — regression suites for capability helper split and builder convergence.

## 6. Testing

- **TEST-001**: Appkit builder tests verify `BuildKernel(...)` no longer accepts raw `kernel.Option` variadics, `BuildKernelWithFeatures(...)` remains the canonical public builder, and `BuildDeepAgent(...)` still routes through the same feature path.
- **TEST-002**: Runtime capability tests verify lookup helpers are side-effect free and do not create capability state, prompts, tools, or managers on first read.
- **TEST-003**: Internal runtime assembly tests verify builtin tools, MCP, skills, progressive skills, and provider activation still assemble correctly without calling public runtime mutation helpers.
- **TEST-004**: UI/product tests verify capability/manifests inspection in `userio/prompting`, `contrib/tui`, and `apps/mosswork` continues to work using lookup-only helpers.
- **TEST-005**: Search-based validation confirms removed public runtime mutation helpers and the removed `BuildKernel(..., extraOpts...)` public pattern no longer appear in production `.go` code.
- **TEST-006**: Validation runs `go test ./appkit/... ./runtime ./internal/runtimeassembly ./userio/...`, `Push-Location apps\mosswork; go test .; Pop-Location`, and `Push-Location contrib\tui; go test .; Pop-Location`, followed by `go test ./...`, `go build ./...`, and the separate module validation commands again.

## 7. Risks & Assumptions

- **RISK-001**: Removing `BuildKernel(..., extraOpts...)` can break external or example call sites that implicitly used appkit as a raw `kernel.Option` assembly façade.
- **RISK-002**: If lookup-only capability helpers still perform hidden `ensure*State(...)` side effects, phase/freeze regressions similar to the recent memory/context issue can reappear.
- **RISK-003**: Progressive skill activation is currently spread across runtime capability state and internal runtime assembly. Partial migration can leave discoverability, activation, or tool registration inconsistent.
- **RISK-004**: `ServiceRegistry` is used across multiple owner packages. If comments/tests are updated without a clear boundary rule, future code can easily drift back to raw bag semantics.
- **ASSUMPTION-001**: `BuildKernelWithFeatures(...)` is already the intended public builder and can absorb all official appkit composition without requiring a second public assembly contract.
- **ASSUMPTION-002**: `capability.Provider` and `capability.Manager` can remain stable while public runtime capability setup helpers are moved inward.
- **ASSUMPTION-003**: A full `ServiceRegistry` replacement is not required to make the public extension/assembly story coherent in this round.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-14-extension-assembly-boundary-convergence-design.md`
- `docs/superpowers/specs/2026-04-14-plugin-hook-surface-convergence-design.md`
- `docs/superpowers/specs/2026-04-14-kernel-execution-api-convergence-design.md`
- `plan/architecture-plugin-hook-surface-convergence-1.md`
- `plan/architecture-kernel-execution-api-convergence-1.md`
