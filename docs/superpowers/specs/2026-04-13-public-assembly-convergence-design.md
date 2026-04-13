# Public Assembly Convergence Design

## Problem

The repository has already converged on one architectural direction:

- `harness` is the canonical public surface for framework capability composition.
- `appkit` should remain a thin builder/preset layer.
- `runtime` should shrink toward substrate, service, and reporting internals rather than remain a second public assembly API.

That direction is still not fully enforced. The public assembly model is split across three layers:

1. **`harness` exposes canonical features, but some of them are still wrappers around runtime-owned public assembly hooks.**
   - `harness/runtime_features.go`
     - `RuntimeSetup(...)` delegates to `runtime.Setup(...)`
     - `Planning()` delegates to `runtime.WithPlanningDefaults()`
     - `ContextOffload(...)` delegates to `runtime.RegisterOffloadTools(...)`
     - `ContextManagement(...)` delegates to `runtime.WithContextSessionStore(...)` and `runtime.ConfigureContext(...)`

2. **`runtime` still exposes public assembly entrypoints instead of just keeping the underlying services.**
   - `runtime/runtime_setup.go`
     - `Setup(...)`
     - `Option`
     - `WithBuiltinTools(...)`
     - `WithMCPServers(...)`
     - `WithSkills(...)`
     - `WithProgressiveSkills(...)`
     - `WithAgents(...)`
     - `WithPlanning(...)`
     - `WithWorkspaceTrust(...)`
     - `WithCapabilityReporter(...)`
   - `runtime/context.go`
     - `WithContextSessionStore(...)`
     - `WithContextSessionManager(...)`
     - `ConfigureContext(...)`
     - `WithOffloadSessionStore(...)`
     - `RegisterOffloadTools(...)`
   - `runtime/planning.go`
     - `WithPlanningSessionManager(...)`
     - `RegisterPlanningTools(...)`
     - `WithPlanningDefaults(...)`
   - `runtime/bootstrap.go`
     - `WithBootstrapContext(...)`
     - `WithLoadedBootstrapContext(...)`
     - `WithLoadedBootstrapContextWithTrust(...)`
     - `LoadBootstrapContext(...)`
     - `LoadBootstrapContextWithTrust(...)`
   - `runtime/gateway.go`
     - `ServeConfig`
     - `ServeCLI(...)`
   - `runtime/sessionstore.go`
     - `WithKernelSessionStore(...)`

3. **`appkit` root still exports non-builder, non-preset convenience shells and duplicate utilities.**
   - `appkit/repl.go`
     - `REPL(...)`
   - `appkit/serve.go`
     - `Serve(...)`
     - `Health(...)`
     - `HealthJSON(...)`
     - `HealthText(...)`
   - `appkit/runtime_builder.go`
     - `RuntimeResolution`
     - `RuntimeFeatureFlags`
     - `RuntimeBuilder`
     - `NewRuntimeBuilder(...)`
   - `appkit/appkit.go`
     - `ContextWithSignal(...)`
     - `FirstNonEmpty(...)`

There is also one remaining appkit-side preset split:

- `appkit/deep_agent_packs.go` still defines framework-like feature closures that should either be canonical harness features or remain purely preset-private:
  - `deepAgentStateCatalogFeature(...)`
  - `deepAgentExecutionSurfaceFeature(...)`
  - `deepAgentExecutionCapabilityReportFeature(...)`

This is inconsistent with the intended model because users can still assemble the runtime from `runtime` and from `appkit` root instead of going through `harness.Feature`.

This redesign intentionally does **not** preserve backward compatibility for the runtime-level and appkit-level assembly shells.

## Goals

- Make `harness.Feature` the only canonical public assembly unit.
- Remove runtime-owned public assembly entrypoints.
- Reduce `appkit` root to builder/preset APIs only.
- Make deep-agent preset composition reuse canonical harness features for general assembly primitives.
- Remove dead or duplicate public config/utilities that only exist to preserve old assembly paths.

## Non-Goals

- P1 execution substrate owner migration (`harness.Backend` vs `runtime.ExecutionSurface`).
- P2 execution-policy enforcement-plane convergence.
- P3 kernel execution API convergence (`Run` vs `RunAgent`).
- Removing runtime substrate/service types such as `StateCatalog`, store wrappers, execution-policy resolution, or profile resolution in this round.
- Reworking the deep-agent preset-specific `general-purpose-agent` behavior in this round.

## User-Approved Design Decisions

- Scope: **maximum P0**
- Compatibility posture: **hard cut; do not preserve backward compatibility**
- Design preference: **prefer the clean final architecture over minimizing blast radius**

That means this round explicitly includes:

- removing runtime public assembly APIs
- removing appkit root convenience shells that are not builders/presets
- updating apps/examples/tests rather than keeping transitional wrappers

## Target Architecture

After this migration, the public assembly ownership model is:

- **`harness`** — the only canonical public assembly surface
- **`appkit`** — builders, presets, and CLI flag/config types only
- **`runtime`** — substrate, service, and reporting internals only
- **apps/examples** — consume `appkit` builders/presets, `harness` features, and lower-level packages directly when needed; they do not rely on a second convenience facade

### 1. Harness becomes the only public assembly facade

All general runtime capability composition should be expressed through `harness` feature constructors and harness-owned option/config types.

The canonical public assembly surface after the migration should be:

- `harness.RuntimeSetup(...)`
- `harness.Planning()`
- `harness.ContextOffload(...)`
- `harness.ContextManagement(...)`
- `harness.BootstrapContext(...)`
- `harness.BootstrapContextValue(...)`
- `harness.LoadedBootstrapContext(...)`
- existing canonical non-runtime features such as:
  - `SessionPersistence(...)`
  - `Checkpointing(...)`
  - `TaskDelegation(...)`
  - `PersistentMemories(...)`
  - `LLMResilience(...)`
  - `ExecutionPolicy(...)`
  - `PatchToolCalls()`

General assembly knobs that currently live in `runtime.Option` move to harness-owned setup options.

### 2. Runtime capability assembly becomes non-public

The capability orchestration currently exposed as `runtime.Setup(...)` remains implementation detail, not public API.

This round should extract the runtime capability assembly pipeline into a **non-public package under `internal/`**:

- `internal/runtimeassembly` owns:

- assembly config resolution
- builtin tools capability installation
- MCP capability installation
- prompt-skill capability installation
- agent discovery capability installation
- runtime capability reporting during assembly
- runtime validation / activation sequencing

`harness.RuntimeSetup(...)` becomes the only public caller of that assembly pipeline.

`internal/runtimeassembly` is intentionally limited to capability assembly orchestration. It does **not** absorb the planning/context/offload feature state machines.

Those move to dedicated non-public feature packages:

- `internal/runtimeplanning` owns the planning installer/state machine currently exposed through `runtime/planning.go`
- `internal/runtimecontext` owns the context/offload installer/state machine currently exposed through `runtime/context.go`

The public `runtime` package keeps only the lower-level substrate and reporting types that still belong there.

### 3. Harness-owned setup options replace runtime public options

The current `runtime.Option` surface is public assembly config and therefore should be removed from `runtime`.

Introduce harness-owned runtime setup options (names can stay close to the current semantics), covering:

- builtin tools enabled/disabled
- MCP servers enabled/disabled
- skills enabled/disabled
- progressive skills enabled/disabled
- agents enabled/disabled
- capability reporter injection

`trust` remains an explicit runtime-setup input, not a runtime public option.

`WithPlanning(...)` is removed instead of migrated: the field is dead public config today and does not participate in capability orchestration.

### 4. Context, planning, and bootstrap install surfaces become harness-owned end to end

The following runtime public assembly hooks are removed:

- planning installers:
  - `runtime.WithPlanningSessionManager(...)`
  - `runtime.RegisterPlanningTools(...)`
  - `runtime.WithPlanningDefaults(...)`
- context/offload installers:
  - `runtime.WithContextSessionStore(...)`
  - `runtime.WithContextSessionManager(...)`
  - `runtime.ConfigureContext(...)`
  - `runtime.WithOffloadSessionStore(...)`
  - `runtime.RegisterOffloadTools(...)`
- bootstrap convenience wrappers:
  - `runtime.WithBootstrapContext(...)`
  - `runtime.WithLoadedBootstrapContext(...)`
  - `runtime.WithLoadedBootstrapContextWithTrust(...)`
  - `runtime.LoadBootstrapContext(...)`
  - `runtime.LoadBootstrapContextWithTrust(...)`

Bootstrap already has a canonical public harness surface in `harness/features.go`; the runtime wrappers are pure residue and should be deleted.

Planning and context should follow the same rule: the public install/config surface becomes harness-owned, while the implementation moves into the dedicated non-public packages above.

The concrete ownership split is:

- `harness` owns the exported feature constructors and exported option/config surface
- `internal/runtimeplanning` owns planning install/state logic
- `internal/runtimecontext` owns context/offload install/state logic

`harness.ContextOption` becomes a harness-owned functional option over a harness-local unexported config struct (for example `type ContextOption func(*contextFeatureConfig)`), not a wrapper over any runtime public type. `ContextManagement(...)` resolves that harness-local config into the concrete `internal/runtimecontext` config before installation.

### 5. Deep-agent preset stops defining general assembly primitives

`appkit/deep_agent_packs.go` should keep preset-specific behavior, but it should stop defining appkit-local general assembly primitives when the same concepts belong to canonical harness composition.

This round adds canonical harness features for the remaining generic primitives that deep-agent currently implements privately:

- a harness feature for installing a `runtime.StateCatalog`
- a harness feature for applying a `runtime.ExecutionSurface` to a kernel
- a harness feature for reporting execution-surface capability status

After that:

- `deepAgentStateCatalogFeature(...)` is removed
- `deepAgentExecutionSurfaceFeature(...)` is removed
- `deepAgentExecutionCapabilityReportFeature(...)` is removed
- `deepAgentGeneralPurposeFeature(...)` remains, because it is preset-specific behavior rather than a general assembly primitive

### 6. Appkit root is reduced to builders/presets only

The appkit root package should retain only builder/preset and CLI-config surfaces such as:

- `AppFlags`
- `ParseAppFlags(...)`
- `BindAppFlags(...)`
- `BindAppPFlags(...)`
- `BuildKernel(...)`
- `BuildKernelWithFeatures(...)`
- `DeepAgentConfig`
- `DeepAgentDefaults(...)`
- `BuildDeepAgent(...)`

The following appkit root exports are removed:

- `ServeConfig`
- `REPLConfig`
- `REPL(...)`
- `Serve(...)`
- `HealthStatus`
- `HealthOutput`
- `Health(...)`
- `HealthJSON(...)`
- `HealthText(...)`
- `RuntimeResolution`
- `RuntimeFeatureFlags`
- `RuntimeBuilder`
- `NewRuntimeBuilder(...)`
- `ContextWithSignal(...)`
- `FirstNonEmpty(...)`

Callers should move to the correct lower-level owners instead:

- signal handling → `signal.NotifyContext(...)`
- first-non-empty string logic → `internal/strutil.FirstNonEmpty(...)`
- CLI/gateway demo wiring → direct example-local code or lower-level framework packages, not a public appkit facade

`appkit.REPL(...)` does not get a new shared framework replacement in this round. Callers that still want a simple stdin loop must inline that loop locally in the example/app code. This is intentional: no new shared REPL abstraction is introduced as part of maximum P0.

### 7. Thin convenience wrappers are deleted instead of migrated

Remove thin wrappers that no longer represent canonical ownership:

- `runtime/sessionstore.go`
  - `WithKernelSessionStore(...)`
- `runtime/gateway.go`
  - `ServeConfig`
  - `ServeCLI(...)`

These wrappers only keep old public assembly routes alive. They are not part of the desired final model.

## API Changes

### New or reshaped public harness surfaces

Add or reshape the following canonical harness-owned surfaces:

- `RuntimeSetup(..., opts ...harness.RuntimeSetupOption)`
- harness-owned runtime setup option constructors replacing the current runtime ones
- harness-owned context option type that no longer aliases a runtime public type
- canonical harness features for:
  - state catalog installation
  - execution surface installation
  - execution-surface capability reporting

### Removed runtime public assembly surfaces

Delete:

- `runtime.Option`
- `runtime.Setup(...)`
- `runtime.ContextOption`
- `runtime.WithBuiltinTools(...)`
- `runtime.WithMCPServers(...)`
- `runtime.WithSkills(...)`
- `runtime.WithProgressiveSkills(...)`
- `runtime.WithAgents(...)`
- `runtime.WithPlanning(...)`
- `runtime.WithWorkspaceTrust(...)`
- `runtime.WithCapabilityReporter(...)`
- `runtime.WithPlanningSessionManager(...)`
- `runtime.RegisterPlanningTools(...)`
- `runtime.WithPlanningDefaults(...)`
- `runtime.WithContextSessionStore(...)`
- `runtime.WithContextSessionManager(...)`
- `runtime.ConfigureContext(...)`
- `runtime.WithOffloadSessionStore(...)`
- `runtime.RegisterOffloadTools(...)`
- `runtime.AutoCompactHook(...)`
- `runtime.WithBootstrapContext(...)`
- `runtime.WithLoadedBootstrapContext(...)`
- `runtime.WithLoadedBootstrapContextWithTrust(...)`
- `runtime.LoadBootstrapContext(...)`
- `runtime.LoadBootstrapContextWithTrust(...)`
- `runtime.WithKernelSessionStore(...)`
- `runtime.ServeConfig`
- `runtime.ServeCLI(...)`
- `runtime.PlanningTodoItem`

### Removed appkit root public surfaces

Delete:

- `appkit.ServeConfig`
- `appkit.REPLConfig`
- `appkit.REPL(...)`
- `appkit.Serve(...)`
- `appkit.HealthStatus`
- `appkit.HealthOutput`
- `appkit.Health(...)`
- `appkit.HealthJSON(...)`
- `appkit.HealthText(...)`
- `appkit.RuntimeResolution`
- `appkit.RuntimeFeatureFlags`
- `appkit.RuntimeBuilder`
- `appkit.NewRuntimeBuilder(...)`
- `appkit.ContextWithSignal(...)`
- `appkit.FirstNonEmpty(...)`

### Appkit preset config updates

`appkit.DeepAgentConfig.DefaultSetupOptions` changes from `[]runtime.Option` to the new harness-owned runtime setup option type.

This is an intentional hard cut and is required so deep-agent presets no longer depend on runtime public assembly config.

## Migration Plan

Implement in this order:

0. **Define all internal package boundaries before any implementation starts**
   - lock the package split for:
     - `internal/runtimeassembly`
     - `internal/runtimeplanning`
     - `internal/runtimecontext`
   - lock the concrete harness-owned `ContextOption` mechanism and the harness-local config struct that feeds `internal/runtimecontext`

1. **Create the non-public runtime assembly package**
   - extract the runtime capability assembly config and lifecycle runner out of runtime public API
   - keep runtime substrate/service helpers reusable by the internal assembly package

2. **Move canonical setup options into harness**
   - introduce harness-owned runtime setup options
   - switch `harness.RuntimeSetup(...)` to the new option type
   - switch tests/presets that currently pass `runtime.With*` options

3. **Make planning/context/offload fully harness-owned**
   - remove runtime public planning/context/offload installers
   - keep only harness-owned public configuration/install surface

4. **Delete runtime bootstrap wrappers**
   - keep the canonical bootstrap surface in `harness`
   - remove runtime bootstrap convenience functions

5. **Add canonical harness features for state catalog / execution surface / execution reporting**
   - switch deep-agent packs off appkit-local general assembly closures

6. **Trim appkit root public surface**
   - remove `REPL`, `Serve`, `Health*`, `RuntimeBuilder`, `ContextWithSignal`, `FirstNonEmpty`
   - update apps/examples/tests to direct lower-level usage

7. **Delete thin runtime convenience wrappers**
   - remove `ServeCLI`, `ServeConfig`, and `WithKernelSessionStore`

8. **Run full validation**
   - fix all callers and test coverage until repository validation passes

## Error Handling and Invariants

This migration must preserve these invariants:

- `harness.Feature` is the only canonical public assembly surface.
- `appkit` root only exposes builders/presets and CLI-config helpers.
- `runtime` public package no longer exposes assembly entrypoints.
- `runtime.CapabilityReporter`, `runtime.NewCapabilityReporter(...)`, `runtime.CapabilityStatusPath(...)`, `runtime.CapabilityStatus`, and `runtime.CapabilitySnapshot` remain in `runtime` as reporting substrate.
- deep-agent preset composition does not depend on runtime public assembly options.
- deep-agent preset-specific behavior remains allowed, but general assembly primitives do not stay appkit-private.
- capability reporting semantics stay unchanged when runtime capabilities are installed through harness.
- planning/context/bootstrap behavior remains unchanged for correctly configured callers.

The new canonical harness features added in this round must declare explicit metadata:

- state catalog feature:
  - `Phase: FeaturePhaseConfigure`
  - `Key: "state-catalog"`
- execution surface feature:
  - `Phase: FeaturePhaseConfigure`
  - `Key: "execution-surface"`
- execution capability report feature:
  - `Phase: FeaturePhasePostRuntime`
  - `Key: "execution-capability-report"`
  - `Requires: []string{"execution-surface"}`

Failure behavior should remain explicit:

- invalid setup option combinations still fail with explicit errors
- missing required stores/managers still fail explicitly where the current canonical feature already requires them
- execution-surface capability reporting still reports readiness/degraded states explicitly
- callers that depended on removed public runtime/appkit shells fail at compile time and must migrate to the new canonical surface

## Testing

Required coverage:

### Harness

- `RuntimeSetup(...)` installs the same capability set as before
- harness-owned runtime setup options replace `runtime.With*` semantics
- `Planning()` still installs `write_todos`
- `ContextOffload(...)` still installs `offload_context`
- `ContextManagement(...)` still installs auto-compact + compact tool behavior
- bootstrap canonical features still inject the same prompt sections
- new canonical state-catalog / execution-surface / capability-report features install correctly

### Appkit

- `BuildKernel(...)` and `BuildKernelWithFeatures(...)` still compose runtime setup through harness
- `BuildDeepAgent(...)` still builds the same preset packs after switching `DefaultSetupOptions` to harness-owned options
- deep-agent no longer depends on runtime public assembly config or appkit-local general assembly closures

### Runtime

- runtime capability reporting still behaves the same through the new non-public assembly pipeline
- trusted vs restricted agent discovery remains unchanged
- capability validation/activation ordering remains unchanged
- no dead runtime setup config field remains (`planning`)

### Apps / Examples

- former `appkit.ContextWithSignal(...)` call sites use `signal.NotifyContext(...)`
- former `appkit.FirstNonEmpty(...)` call sites use `internal/strutil.FirstNonEmpty(...)` or local logic
- examples that used `appkit.REPL(...)` / `appkit.Serve(...)` are updated without introducing a new public facade

### Repository validation

- `go test ./...`
- `go build ./...`
- `Push-Location contrib\tui; go test .; Pop-Location`

## Acceptance Criteria

- no runtime public assembly entrypoints remain
- no appkit root non-builder/non-preset facade remains
- `harness.Feature` is the only canonical public assembly unit
- `DeepAgentConfig.DefaultSetupOptions` no longer references `runtime.Option`
- deep-agent pack composition reuses canonical harness feature primitives for generic assembly concerns
- the repository builds and tests clean after all call sites migrate
