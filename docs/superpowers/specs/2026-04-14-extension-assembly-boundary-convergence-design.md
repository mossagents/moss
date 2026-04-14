# Extension / Assembly Boundary Convergence Design

## Problem

After public assembly convergence, plugin convergence, kernel phase/freeze convergence, and `RunAgentRequest` contraction, the next remaining architectural split is not one single API. It is a **boundary problem** across four adjacent layers:

1. **`harness.Feature` is already the canonical public assembly unit.**
   - `harness/feature.go`
   - `harness/features.go`
   - `harness/harness.go`

2. **Other public surfaces still perform overlapping assembly work.**
   - `appkit.BuildKernel(...)` still accepts raw `kernel.Option` escape hatches
   - `appkit.BuildKernelWithFeatures(...)` is also public
   - `appkit.BuildDeepAgent(...)` is a second public builder shell on top

3. **Runtime capability assembly still leaks through the public `runtime` package.**
   - `runtime.CapabilityManager(k)`
   - `runtime.CapabilityDeps(k)`
   - `runtime.SkillManifests(k)`
   - `runtime.SetSkillManifests(k)`
   - `runtime.EnableProgressiveSkills(k)`
   - `runtime.RegisterProgressiveSkillTools(k)`
   - `runtime.WithCapabilityManager(...)`

4. **Kernel-scoped state ownership is still represented by a public generic bag.**
   - `kernel.ServiceRegistry`
   - `(*kernel.Kernel).Services()`
   - current users span `agent`, `runtime`, `internal/runtime*`, `kernel`, and policy state owners

This leaves the repo with multiple partially overlapping extension/assembly stories:

- `harness.Feature` as the intended public composition unit
- `kernel.Plugin` as a lifecycle primitive
- `capability.Provider` as a runtime-loadable unit
- `runtime.*Capability*` helpers as public assembly control points
- `ServiceRegistry` as a generic cross-package state substrate
- `appkit` builders as partially overlapping façade entrypoints

The result is not a single bug. It is a persistent architectural tax:

- new extension work still has more than one "reasonable" owner path
- public API shape still overstates how many extension models are first-class
- `runtime` mixes read-side inspection helpers with install-side mutation helpers
- `appkit` still exposes both feature-first assembly and raw-option bypasses
- `ServiceRegistry` makes subsystem ownership easy to blur because any package can stash state under a string key

The previous convergence rounds established the working posture for this repo:

- **hard cuts are acceptable**
- **compile-time failures are preferred over compatibility shims**
- **one canonical public story is better than multiple "advanced" variants**

## Goals

- Keep **`harness.Feature`** as the only canonical **public assembly/composition unit**.
- Keep **`kernel.Plugin`** as the lifecycle primitive, but stop treating it as a competing public assembly story.
- Keep **`capability.Provider`** as the runtime-loadable capability contract, but stop exposing public runtime helpers that perform install-time orchestration.
- Split current runtime capability helpers into:
  - **public lookup/read surfaces**
  - **internal setup/activation surfaces**
- Compress `appkit` so its public builders align to the feature-first model instead of reintroducing raw option assembly.
- Tighten `ServiceRegistry` posture so it is clearly a substrate/state mechanism, not a public extension model.
- Make future extension work answer one clear question first: **is this a feature, a plugin, or a capability provider?**

## Non-Goals

- Removing `kernel.Plugin` as a public type.
- Removing `capability.Provider` as a public contract.
- Replacing `kernel.ServiceRegistry` with a brand new state framework.
- Solving posture/governance convergence or compat-tail cleanup in this spec.
- Redesigning the internal capability manager implementation.
- Moving every existing service state owner into the `kernel` package in this round.

## Working Design Decisions

Because the user requested the next highest-priority convergence and was unavailable during the clarification step, this spec proceeds with the default recommendation:

- **Selected direction:** strong convergence
- **Canonical public assembly unit:** `harness.Feature`
- **Compatibility posture:** hard cut; do not preserve public compatibility shims where they obscure ownership
- **Scope posture:** decompose the problem into one spec with serial migration phases instead of pretending it is one edit

## Rejected Approaches

### 1. Preserve `harness.Feature`, `runtime.CapabilityManager`, and `appkit.BuildKernel(..., extraOpts...)` as equal "advanced" public entrypoints

Rejected because that keeps multiple public assembly stories alive and guarantees future call sites will keep choosing different seams.

### 2. Remove `kernel.Plugin` and make `Feature` the only extension concept

Rejected because `Plugin` is already a clean low-level lifecycle primitive. The current problem is not that `Plugin` exists; it is that assembly ownership is still split around it.

### 3. Fully delete `ServiceRegistry` and `Kernel.Services()` in this round

Rejected because current typed state owners are still distributed across `kernel`, `agent`, `runtime`, and `internal/runtime*`. Forcing total removal now would turn this into a repo-wide state-framework rewrite rather than a focused boundary convergence.

## Target Architecture

After this convergence, extension/assembly ownership is:

- **`harness.Feature`** — the only canonical public assembly/composition unit
- **`kernel.Plugin`** — the low-level lifecycle primitive, typically installed through features
- **`capability.Provider`** — the runtime-loadable capability contract
- **`internal/runtimeassembly`** (or a sibling internal owner) — the owner of capability assembly orchestration
- **`runtime`** — read-side runtime inspection/reporting helpers, not public assembly control
- **`appkit`** — feature-first builders and presets, not a second raw assembly model
- **`ServiceRegistry`** — substrate/state bag used by typed owners, not a public extension story

### 1. Public assembly story: `harness.Feature`

This round makes one repo-wide rule explicit:

- if an app/product wants to assemble capabilities, prompts, policy, plugins, or setup-time services, it does so through **`harness.Feature`**

That means:

- `BuildKernelWithFeatures(...)` remains the canonical public builder
- `BuildKernel(...)` becomes a convenience wrapper that only supplies the official default feature list
- `BuildDeepAgent(...)` remains a preset builder that produces feature packs and then delegates to `BuildKernelWithFeatures(...)`
- low-level `kernel.Option` injection remains available only as an explicit escape hatch via `harness.KernelOptions(...)` or direct `kernel.New(...)`, not as an appkit builder variadic

### 2. Appkit surface compression

`appkit` currently exposes:

- `BuildKernel(ctx, flags, io, extraOpts...)`
- `BuildKernelWithFeatures(ctx, flags, io, features...)`
- `BuildDeepAgent(ctx, flags, io, cfg)`

Final ownership after this round:

- **`BuildKernelWithFeatures(...)`** — canonical public feature-first builder
- **`BuildKernel(...)`** — thin convenience wrapper for the default official feature list only
- **`BuildDeepAgent(...)`** — preset wrapper that resolves declarative packs into features, then calls `BuildKernelWithFeatures(...)`

Concrete changes:

- remove `extraOpts ...kernel.Option` from `BuildKernel(...)`
- keep low-level option assembly available through `harness.KernelOptions(...)`
- move builder-owned debug logger installation from direct `k.InstallPlugin(...)` mutation to feature composition (for example `harness.Plugins(builtins.LoggerPlugin())` in the assembled feature set when debug logging is enabled)
- keep `buildKernel(...)` private helper only if it is still a pure implementation detail; it must not preserve a second conceptual assembly layer

This removes the current "feature-first builder, except when appkit also accepts raw kernel options" ambiguity.

### 3. Plugin boundary: primitive, not competing assembly model

`kernel.Plugin` remains public and continues to be the canonical lifecycle primitive. This spec does **not** demote or remove it.

What changes is its role:

- app/product assembly should prefer `harness.Plugins(...)`
- direct `(*Kernel).InstallPlugin(...)` remains a low-level substrate/setup API
- `Plugin` is no longer described as a parallel top-level public composition story beside `Feature`

Rule of thumb after this round:

- use **`Feature`** to assemble systems
- use **`Plugin`** to describe lifecycle behavior inside that assembly

### 4. Capability boundary: provider contract stays public, assembly owner moves inward

`capability.Provider` remains the public runtime-loadable capability contract:

- `Metadata()`
- `Init(ctx, deps)`
- `Shutdown(ctx)`

That contract is not the current problem.

The problem is the public `runtime` package still mixing:

- read-side inspection helpers (`CapabilityManager`, `SkillManifests`)
- install-side mutation helpers (`SetSkillManifests`, `EnableProgressiveSkills`, `RegisterProgressiveSkillTools`, `WithCapabilityManager`)

Final posture after this round:

- **public `runtime` surfaces become lookup/reporting oriented**
- **install/activation helpers move behind internal capability assembly ownership**

Public read-side target shape:

- `runtime.LookupCapabilityManager(k) (*capability.Manager, bool)`
- `runtime.LookupSkillManifests(k) []skill.Manifest`

Concrete caller migration rule:

- current read-side/UI callers such as `userio/prompting`, `contrib/tui`, and `apps/mosswork` migrate to the lookup helpers
- current setup-side callers such as `internal/runtimeassembly/assembly.go` stop calling public runtime helpers for registration/activation and instead use internal setup ownership directly

Public mutation helpers to remove from `runtime`:

- `runtime.WithCapabilityManager(...)`
- `runtime.CapabilityDeps(k)`
- `runtime.SetSkillManifests(...)`
- `runtime.EnableProgressiveSkills(...)`
- `runtime.RegisterProgressiveSkillTools(...)`

Install-side orchestration target owner:

- `internal/runtimeassembly` keeps ownership, or
- if needed for clarity, a new focused internal owner such as `internal/runtimecapability`

What matters is not the exact folder name. What matters is the boundary:

- `runtime` should no longer export the knobs that create/register/activate capability state
- apps/UI code can still inspect loaded capabilities and discovered manifests through lookup-only helpers
- capability assembly remains available to official runtime setup flows without staying public

### 5. ServiceRegistry posture: substrate only

This round does **not** remove `ServiceRegistry`.

Instead it makes its role explicit and narrows how the repo uses it:

- `ServiceRegistry` is a kernel-scoped state substrate
- it is **not** a public extension/composition model
- no new app/product-facing APIs should ask callers to interact with `Kernel.Services()`
- subsystem owners must expose typed helpers instead of handing out the raw bag

Current typed owners already exist in several places:

- `policystate.Lookup/Ensure(...)`
- session persistence state owner
- memory/context/planning state owners
- agent kernel service owner

This round strengthens that direction instead of replacing the substrate outright.

Target rule:

- repo code may continue using `Kernel.Services()` inside typed owner packages while the state substrate is still shared
- public documentation and app-facing APIs stop presenting it as something consumers should compose against directly
- if a later round wants to remove `Kernel.Services()` entirely, that follow-up must first move the remaining state owners behind narrower package boundaries

Future ADR note:

- a later round may replace `ServiceRegistry` with narrower typed ownership, but that is explicitly out of scope for this convergence

### 6. Runtime helper split must eliminate side-effectful "read" accessors

The recent phase/freeze work already exposed why this matters:

- a helper that looks read-only but secretly performs `ensure*State(...)` registration can cross phase boundaries and panic

This spec therefore adopts a strict naming/behavior rule:

- **lookup/read helpers must be lookup-only**
- **ensure/install helpers must stay on the setup side**

That rule applies especially to capability-related helpers and any similar exported runtime accessors that currently mutate install-time state on first call.

## Migration Plan

Phase dependency chain:

- `Phase 1 (appkit)` -> `Phase 2 (runtime capability split)` -> `Phase 3 (ServiceRegistry posture tightening)` -> `Phase 4 (docs/validation)`

### Phase 1 — Appkit builder convergence

- Remove `extraOpts ...kernel.Option` from `appkit.BuildKernel(...)`.
- Rebase `BuildKernel(...)` onto a feature list only.
- Keep `BuildKernelWithFeatures(...)` as the canonical public builder.
- Keep `BuildDeepAgent(...)` as a preset-to-features wrapper.
- Move appkit-owned logger/plugin wiring into feature composition.

### Phase 2 — Runtime capability owner split

- Split current `runtime/runtime_capability_service.go` into:
  - public lookup/read helpers
  - internal assembly/setup helpers
- Migrate `internal/runtimeassembly` to the setup helpers.
- Migrate read-side UI/product callers (`userio/prompting`, `contrib/tui`, `apps/mosswork`, related tests) to lookup-only helpers.
- Remove public mutation helpers from `runtime`.

### Phase 3 — ServiceRegistry posture tightening

- Audit app/product/public package usage of `Kernel.Services()` and keep it restricted to typed owner packages only.
- Update comments and docs so `ServiceRegistry` is described as substrate/state, not extension assembly.
- If any public helper still returns or effectively exposes the raw state bag, remove or narrow it in this phase.

### Phase 4 — Validation and doc cleanup

- Update README / architecture docs / appkit comments where the public assembly story still sounds multi-model.
- Search-clean old guidance that still points callers at raw option or runtime mutation paths as equally canonical.
- Validate the repo under the standard full test/build matrix.

## Error Handling and Compatibility

- This convergence uses **hard cuts** for misleading public surfaces.
- Compile-time failures are preferred over keeping dual APIs.
- Public read-side replacements must be explicit and named for lookup semantics.
- This round does **not** promise source compatibility for callers using removed runtime mutation helpers or `BuildKernel(..., extraOpts...)`.

## Testing Requirements

- Appkit tests must verify:
  - `BuildKernelWithFeatures(...)` remains the canonical feature-first builder
  - `BuildKernel(...)` stays equivalent to the official default feature set
  - deep-agent presets still assemble through the same feature path
- Runtime capability tests must verify:
  - lookup helpers are side-effect free
  - internal setup helpers still register capabilities, manifests, and progressive-skill tools correctly
  - UI/product callers can inspect capabilities/manifests without triggering setup-time mutation
  - `internal/runtimeassembly` can assemble capabilities without calling public runtime mutation helpers
- Search-based cleanup should confirm no remaining public guidance or production code uses removed runtime mutation helpers or appkit raw-option builder escape hatches.

## Open Follow-Ups Explicitly Left Out

- Full `Kernel.Services()` removal
- posture / governance owner-chain convergence
- compat-tail cleanup

Those remain valid later rounds, but they are intentionally not folded into this spec's implementation scope.
