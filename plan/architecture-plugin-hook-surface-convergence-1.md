---
goal: Converge lifecycle extension wiring onto one canonical kernel.Plugin model and remove public raw hooks.Registry surfaces
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: Core Runtime Team
status: Completed
tags: [architecture, refactor, migration, harness, kernel, hooks, plugins]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This implementation plan executes `docs/superpowers/specs/2026-04-14-plugin-hook-surface-convergence-design.md`. The goal is to make `kernel.Plugin` the only canonical lifecycle extension primitive, move interceptor use-cases onto that contract, route harness assembly through one `harness.Plugins(...)` wrapper, delete public raw `*hooks.Registry` install/access surfaces, and hard-cut remaining capability/runtime/agent/public APIs that still leak hook registries.

## 1. Requirements & Constraints

- **REQ-001**: `kernel.Plugin`, `kernel.WithPlugin(...)`, and `(*kernel.Kernel).InstallPlugin(...)` must remain the only canonical public lifecycle extension primitive/install surfaces.
- **REQ-002**: `kernel.Plugin` must cover both hook and interceptor use-cases by adding explicit interceptor slots for every supported pipeline kind in `kernel\plugin.go`.
- **REQ-003**: Plugin validation must be shared and deterministic: `Name` is required, at least one slot must be populated, hook+interceptor combinations are valid, duplicate names are allowed, and repeated installs remain additive.
- **REQ-004**: For a single plugin and a single pipeline, interceptor registration must happen before hook registration so the plugin's own hook executes inside the plugin's interceptor.
- **REQ-005**: `Plugin.Order` must apply uniformly to every populated slot; equal-order execution must preserve registration order.
- **REQ-006**: `(*kernel.Kernel).InstallPlugin(...)` must remain a setup-time API only. Calling it after the kernel begins serving concurrent work must fail immediately and explicitly.
- **REQ-007**: `harness` must expose exactly one lifecycle wrapper feature: `harness.Plugins(...kernel.Plugin)`. `harness.SessionLifecycleHooks(...)` and `harness.ToolLifecycleHooks(...)` must be deleted.
- **REQ-008**: Public raw hook surfaces must be removed from all exported owner layers, not only `kernel`: `kernel.WithPluginInstaller(...)`, `(*Kernel).InstallHooks(...)`, exported `(*Kernel).Hooks()`, `capability.Deps.Hooks`, `runtime.CapabilityDeps(...)` raw hook export, `kernel.LLMAgentConfig.Hooks`, and `(*kernel.LLMAgent).Hooks()`.
- **REQ-009**: Builtin helper names must be finalized as `builtins.LoggerPlugin()` and `builtins.EventEmitterPlugin(pattern, handler)`. `k.OnEvent(...)` may remain only as convenience sugar over `EventEmitterPlugin`.
- **REQ-010**: Capability providers that need lifecycle behavior must go through `Deps.Kernel.InstallPlugin(...)` during provider initialization / activation only.
- **REQ-011**: `kernel.LLMAgentConfig` must expose plugin-based configuration (`Plugins []Plugin`) instead of raw hook-registry injection. Shared registry wiring for `BuildLLMAgent(...)` must remain internal to `kernel`.
- **REQ-012**: Production migration must cover `appkit\builder.go`, `contrib\tui\app_kernel_init.go`, `contrib\tui\app_runtime_posture.go`, `kernel\session_persistence.go`, `runtime\runtime_capability_service.go`, `capability\capability.go`, `kernel\llm_agent.go`, and all tests that currently depend on raw hook surfaces.
- **SEC-001**: Do not preserve backward compatibility shims, deprecated forwarders, alias helpers, or second public escape hatches for raw `*hooks.Registry` mutation.
- **SEC-002**: Invalid plugin definitions and forbidden late runtime installs must fail explicitly; they must not be auto-normalized, auto-named, or silently skipped.
- **SEC-003**: The merged end state must not expose any exported public field, accessor, or mutator whose extension-wiring contract is `*hooks.Registry`.
- **CON-001**: Do not redesign `kernel/hooks/pipeline.go` dependency-graph semantics or introduce plugin dependency graphs in this change.
- **CON-002**: Do not turn `InstallPlugin(...)` into a live hot-reload feature; provider/runtime installs are setup-time only.
- **CON-003**: Do not redesign `harness.Feature` ownership, `runtime` assembly ownership, or unrelated posture/governance surfaces in this plan.
- **GUD-001**: Keep owner boundaries explicit: `kernel` owns plugin primitives and internal hook registry plumbing, `kernel/hooks` remains the internal typed pipeline engine, `harness` owns public assembly wrappers, `runtime`/`capability`/`appkit` are consumers only.
- **GUD-002**: Use compile-time failure and search-based cleanup instead of temporary compatibility APIs in the merged end state.
- **PAT-001**: Each implementation phase must leave the repository compiling; temporary overlap inside the branch is acceptable only when needed to keep intermediate phases buildable.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Establish the final canonical plugin contract, fixed helper names, and plugin-based public replacement surfaces.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | In `kernel\plugin.go`, extend `Plugin` with `BeforeLLMInterceptor`, `AfterLLMInterceptor`, `OnSessionLifecycleInterceptor`, `OnToolLifecycleInterceptor`, and `OnErrorInterceptor`. Add shared validation that enforces non-empty `Name` plus at least one non-nil slot, and make `installPlugin(...)` register interceptor slots before hook slots for the same pipeline while preserving uniform `Order` semantics. Update `kernel\plugin_test.go` to cover hook+interceptor composition, equal-order registration order, duplicate-name additive installs, and panic-on-invalid-plugin behavior. | ✅ | 2026-04-14 |
| TASK-002 | In `kernel\kernel.go`, `kernel\option.go`, and `kernel\run_supervisor.go`, route `WithPlugin(...)` and `InstallPlugin(...)` through the shared validator and add an explicit guard that panics when `InstallPlugin(...)` is called after the kernel has started serving concurrent work. Preserve setup-time installs during configure, boot, and provider activation. Add tests in `kernel\plugin_test.go` and `kernel\kernel_test.go` that prove startup-time installs succeed and forbidden late installs fail explicitly. | ✅ | 2026-04-14 |
| TASK-003 | In `kernel\llm_agent.go` and `kernel\kernel.go`, replace public raw hook injection with plugin-based agent construction. Remove `LLMAgentConfig.Hooks`, add `LLMAgentConfig.Plugins []Plugin`, remove `(*LLMAgent).Hooks()`, and keep `BuildLLMAgent(...)` shared-registry wiring internal to `kernel`. Update `kernel\agent_test.go` and other agent-construction tests so public callers only exercise plugin-based configuration. | ✅ | 2026-04-14 |
| TASK-004 | In `capability\capability.go` and `runtime\runtime_capability_service.go`, remove `Deps.Hooks` and stop exporting raw hook registries from `CapabilityDeps(...)`. Update `internal\runtimeassembly\assembly.go` and its tests so providers that need lifecycle behavior install plugins via `Deps.Kernel.InstallPlugin(...)` during activation only. | ✅ | 2026-04-14 |
| TASK-005 | In `kernel\hooks\builtins\logger.go`, `kernel\hooks\builtins\events.go`, and `kernel\kernel.go`, replace raw installer helpers with fixed plugin-returning helpers `LoggerPlugin()` and `EventEmitterPlugin(pattern, handler)`. Keep `k.OnEvent(...)` as thin sugar over `EventEmitterPlugin(...)` and update `kernel\plugin_test.go` plus `kernel\kernel_test.go` to assert the new helper names and plugin wiring path. | ✅ | 2026-04-14 |

### Implementation Phase 2

- **GOAL-002**: Migrate harness, kernel internals, and production callers onto the canonical plugin path while keeping the repo compiling.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-006 | In `harness\features.go`, add `Plugins(...kernel.Plugin) Feature` as a configure-phase additive wrapper with no metadata key, then delete `SessionLifecycleRegistration`, `ToolLifecycleRegistration`, `SessionLifecycleHooks(...)`, and `ToolLifecycleHooks(...)`. Update `harness\harness_test.go` to cover plugin feature ordering, additive multiple-feature installs, and the absence of lifecycle-specific hook wrappers. | ✅ | 2026-04-14 |
| TASK-007 | Refactor `appkit\builder.go` so debug logging installs `builtins.LoggerPlugin()` through the canonical plugin path instead of `InstallHooks(...)`. Update any impacted tests such as `appkit\deep_agent_test.go` so they verify plugin installation behavior without depending on exported `k.Hooks()`. | ✅ | 2026-04-14 |
| TASK-008 | Refactor `contrib\tui\app_kernel_init.go` and `contrib\tui\app_runtime_posture.go` so permission override wiring becomes a named `kernel.Plugin` using `OnToolLifecycleInterceptor` rather than raw `hooks.Registry` mutation. Preserve current ordering semantics and update TUI tests that cover kernel init and posture rebuild flows. | ✅ | 2026-04-14 |
| TASK-009 | Refactor internal raw-registry call sites in `kernel\session_persistence.go`, `kernel\lifecycle_dispatch.go`, `internal\runtimecontext\context.go`, and any remaining `kernel` same-package tests to use either canonical plugin installation or package-private hook-registry access. The exported `Hooks()` accessor must no longer be needed by any non-deletion migration code. | ✅ | 2026-04-14 |

### Implementation Phase 3

- **GOAL-003**: Hard-cut every remaining public raw hook surface and delete legacy helper APIs once all callers are migrated.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-010 | Delete `kernel.WithPluginInstaller(...)`, `(*kernel.Kernel).InstallHooks(...)`, exported `(*Kernel).Hooks()`, `capability.Deps.Hooks`, public `LLMAgentConfig.Hooks`, and public `(*LLMAgent).Hooks()`. Update affected doc comments in `kernel\plugin.go`, `kernel\kernel.go`, `kernel\llm_agent.go`, and `capability\capability.go` so no current-facing comment still describes raw hook-registry mutation as public API. | ✅ | 2026-04-14 |
| TASK-011 | Delete legacy builtin installer helpers `builtins.InstallLogger(...)` and `builtins.InstallEventEmitter(...)`, plus the removed harness lifecycle wrapper constructors and registration structs. Run search-based cleanup across `kernel`, `harness`, `runtime`, `capability`, `internal`, `appkit`, `contrib\tui`, and tests so no production/public `.go` file still references raw hook installers, raw hook accessors, or removed wrapper symbols. | ✅ | 2026-04-14 |
| TASK-012 | Update current-facing docs or examples only where they still describe public raw hook installation as canonical. Do not rewrite archived historical specs or plans. Ensure `docs\superpowers\specs\2026-04-14-plugin-hook-surface-convergence-design.md` and this plan remain the only current design inputs for the migration. | ✅ | 2026-04-14 |

### Implementation Phase 4

- **GOAL-004**: Validate the repo end to end and record completion directly in the plan.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-013 | Run targeted validation for touched owners: `go test ./kernel/... ./harness ./capability ./runtime ./internal/runtimecontext ./internal/runtimeassembly ./appkit/...`. Fix every regression before proceeding to full-repo validation. | ✅ | 2026-04-14 |
| TASK-014 | Run `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`. After all commands pass, update this file with completion marks and dates for every finished task and set the front matter status to `Completed`. | ✅ | 2026-04-14 |

## 3. Alternatives

- **ALT-001**: Keep `kernel.Plugin` plus `WithPluginInstaller(...)` / `InstallHooks(...)` as an advanced public mode; rejected because it preserves two competing public extension models and guarantees future drift.
- **ALT-002**: Make `harness.Feature` the only external lifecycle model and demote `kernel.Plugin`; rejected because it turns this focused convergence into a larger kernel-versus-harness extensibility redesign.
- **ALT-003**: Promote dependency-graph ordering or live hot-reload semantics in the same change; rejected because current production code does not need those semantics and the approved spec explicitly limits scope to surface convergence.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-14-plugin-hook-surface-convergence-design.md` is the approved source of truth for this plan.
- **DEP-002**: `kernel\hooks\pipeline.go` already provides stable equal-order execution and separate hook/interceptor registration primitives; the migration must reuse those semantics rather than inventing a second pipeline engine.
- **DEP-003**: `kernel\kernel.go`, `kernel\run_supervisor.go`, and `kernel\llm_agent.go` currently own the runtime/install boundary that must enforce setup-time-only plugin installation.
- **DEP-004**: `capability\capability.go`, `runtime\runtime_capability_service.go`, and `internal\runtimeassembly\assembly.go` own provider dependency injection and must converge on `Deps.Kernel.InstallPlugin(...)`.
- **DEP-005**: `harness\features.go`, `appkit\builder.go`, `contrib\tui\app_kernel_init.go`, and `contrib\tui\app_runtime_posture.go` are the primary production assembly call sites that currently depend on raw hook wiring.
- **DEP-006**: `kernel\plugin_test.go`, `kernel\kernel_test.go`, `kernel\agent_test.go`, `harness\harness_test.go`, and `appkit\deep_agent_test.go` are the regression suites that must move off exported raw hook surfaces in the same migration.

## 5. Files

- **FILE-001**: `docs\superpowers\specs\2026-04-14-plugin-hook-surface-convergence-design.md` — approved design specification for this migration.
- **FILE-002**: `plan\architecture-plugin-hook-surface-convergence-1.md` — implementation plan for this work.
- **FILE-003**: `kernel\plugin.go` — canonical plugin contract, validation, and install ordering.
- **FILE-004**: `kernel\option.go` — `WithPlugin(...)` validation path and deletion of `WithPluginInstaller(...)`.
- **FILE-005**: `kernel\kernel.go` — `InstallPlugin(...)`, `OnEvent(...)`, `BuildLLMAgent(...)`, and deletion of raw hook install/access APIs.
- **FILE-006**: `kernel\run_supervisor.go` — active-run guard or equivalent runtime-state check for forbidden late installs.
- **FILE-007**: `kernel\llm_agent.go` — plugin-based public agent configuration and removal of raw hook accessors.
- **FILE-008**: `capability\capability.go` — remove public `Deps.Hooks`.
- **FILE-009**: `runtime\runtime_capability_service.go` — stop exporting raw hooks via `CapabilityDeps(...)`.
- **FILE-010**: `internal\runtimeassembly\assembly.go` — provider activation updates that rely on `Deps.Kernel.InstallPlugin(...)`.
- **FILE-011**: `kernel\hooks\builtins\logger.go` — `LoggerPlugin()` implementation.
- **FILE-012**: `kernel\hooks\builtins\events.go` — `EventEmitterPlugin(...)` implementation.
- **FILE-013**: `harness\features.go` — add `Plugins(...)` and delete lifecycle-specific hook wrappers.
- **FILE-014**: `appkit\builder.go` — canonical debug logger plugin installation.
- **FILE-015**: `contrib\tui\app_kernel_init.go` and `contrib\tui\app_runtime_posture.go` — permission override interceptor migration to plugins.
- **FILE-016**: `kernel\session_persistence.go` and `kernel\lifecycle_dispatch.go` — internal hook-registry ownership cleanup.
- **FILE-017**: `kernel\plugin_test.go`, `kernel\kernel_test.go`, `kernel\agent_test.go`, `harness\harness_test.go`, and `appkit\deep_agent_test.go` — regression coverage for the new plugin-only surface.

## 6. Testing

- **TEST-001**: Plugin contract tests verify interceptor slots, deterministic install ordering, validation panics for empty-name or nil-only plugins, and additive duplicate installs.
- **TEST-002**: Kernel runtime-boundary tests verify `InstallPlugin(...)` works during configure/startup paths and panics when called after concurrent work has started.
- **TEST-003**: Agent-construction tests verify public `LLMAgentConfig` uses `Plugins []Plugin` and no longer exposes raw `*hooks.Registry`.
- **TEST-004**: Capability/runtime assembly tests verify `CapabilityDeps(...)` no longer exports hooks and provider initialization installs plugins only through `Deps.Kernel.InstallPlugin(...)`.
- **TEST-005**: Harness tests verify `harness.Plugins(...)` preserves install order, allows multiple additive features, and replaces the deleted lifecycle-specific wrappers.
- **TEST-006**: Appkit/TUI tests verify logger and permission override behavior still work after migrating from raw registry mutation to plugins.
- **TEST-007**: Search-based verification shows no public production `.go` references to `WithPluginInstaller(`, `InstallHooks(`, exported `Hooks()`, `capability.Deps.Hooks`, `LLMAgentConfig.Hooks`, `(*LLMAgent).Hooks()`, `harness.SessionLifecycleHooks(`, or `harness.ToolLifecycleHooks(`.
- **TEST-008**: Repository validation runs `go test ./kernel/... ./harness ./capability ./runtime ./internal/runtimecontext ./internal/runtimeassembly ./appkit/...`, `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: If the active-run guard for `InstallPlugin(...)` is too weak, late runtime mutation may remain possible and violate the approved setup-time-only contract.
- **RISK-002**: Removing exported `Hooks()` without providing adequate package-private access for `kernel` internals and same-package tests can leave internal lifecycle dispatch or persistence code temporarily broken.
- **RISK-003**: TUI permission override behavior depends on interceptor semantics; if plugin slot ordering changes, tool approval behavior can regress even when compilation succeeds.
- **RISK-004**: Public raw hook surfaces are spread across `kernel`, `capability`, `runtime`, `harness`, `appkit`, and tests; partial migration will leave the repo uncompilable once deletions land.
- **ASSUMPTION-001**: Capability providers that install plugins do so only during initialization / activation and do not require live hot-loading after the runtime begins serving work.
- **ASSUMPTION-002**: Existing `kernel/hooks/pipeline.go` stable ordering semantics are sufficient for logger and permission-override behavior once plugins can carry interceptors.
- **ASSUMPTION-003**: No external compatibility requirement exists for deleted raw hook installer/accessor APIs or deleted harness lifecycle wrapper helpers.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-14-plugin-hook-surface-convergence-design.md`
- `docs/superpowers/specs/2026-04-14-kernel-execution-api-convergence-design.md`
- `plan/architecture-kernel-execution-api-convergence-1.md`
