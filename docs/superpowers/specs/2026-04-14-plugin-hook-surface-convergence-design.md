# Plugin Hook Surface Convergence Design

## Problem

After public assembly convergence and execution API convergence, the next high-value surface split is the extension / hook model itself.

Today, the repo still exposes multiple overlapping ways to install lifecycle behavior:

1. **`kernel.Plugin` already exists as a typed lifecycle unit.**
   - `kernel/plugin.go`
   - `kernel/option.go`
   - `kernel/kernel.go`

2. **Raw `hooks.Registry` mutation is also public.**
   - `kernel.WithPluginInstaller(...)`
   - `kernel.InstallHooks(...)`
   - `kernel.Hooks()`

3. **Other public setup surfaces still leak raw hook registries.**
   - `capability.Deps.Hooks`
   - `runtime.CapabilityDeps(...)`
   - `kernel.LLMAgentConfig.Hooks`
   - `(*kernel.LLMAgent).Hooks()`

4. **`harness` adds lifecycle-specific wrapper features on top.**
   - `harness.SessionLifecycleHooks(...)`
   - `harness.ToolLifecycleHooks(...)`

5. **Production code still uses both models.**
   - `appkit/builder.go` installs logger hooks through `InstallHooks(...)`
   - `contrib/tui/app_kernel_init.go` and `contrib/tui/app_runtime_posture.go` install interceptors through `InstallHooks(...)`
   - `kernel/session_persistence.go` and other internals mutate pipelines directly through `k.Hooks()`

That leaves the repo with several conflicting extension stories at once:

- a typed plugin unit (`Plugin`)
- raw registry installers
- lifecycle-specific harness sugar

This creates architectural problems:

- feature authors do not have one obvious lifecycle extension model
- public callers can bypass the typed plugin contract and bind directly to hook internals
- hook/interceptor ownership is blurred between `kernel`, `harness`, and call-site-specific helpers
- current-facing docs and comments are forced to describe multiple models instead of one clear extension story

The user explicitly chose the same posture used in the previous convergence rounds:

- **hard cut**
- **one canonical model only**
- **no backward compatibility shims**

## Goals

- Make `kernel.Plugin` the only canonical hook primitive.
- Keep `harness.Feature` as the assembly unit, but stop using it to introduce parallel hook models.
- Remove raw `hooks.Registry` mutation from public extension APIs.
- Fold current interceptor use-cases into the canonical `Plugin` contract.
- Delete lifecycle-specific `harness` sugar that duplicates the plugin model.
- Force missed migration sites to fail at compile time instead of preserving compatibility wrappers.

## Non-Goals

- Redesigning `harness.Feature` as a concept.
- Redesigning the typed pipeline engine in `kernel/hooks`.
- Solving later appkit builder compression, posture/governance convergence, or compat-tail cleanup in this spec.
- Adding dependency graphs, plugin bundles, or a new extension DSL beyond what is needed to replace raw registry mutation.

## User-Approved Design Decisions

- Compatibility posture: **hard cut; do not preserve backward compatibility**
- Decomposition posture: **strict serial subprojects**
- Selected direction for this subproject: **`kernel.Plugin` is the only canonical hook primitive**
- Public deletion posture: **remove raw registry installer/accessor surfaces instead of keeping advanced escape hatches**
- Harness rule: **`harness.Feature` remains an assembly wrapper, not a second hook model**

## Rejected Approaches

### 1. Keep `Plugin` plus raw registry installers as "advanced mode"

Rejected because that preserves two public models and guarantees future call sites will keep choosing different hook surfaces.

### 2. Make `harness.Feature` the only external extension model and demote `Plugin`

Rejected for this round because it is materially larger in scope: it would turn a hook-surface convergence task into a full kernel-vs-harness extensibility redesign.

### 3. Make raw `hooks.Registry` mutation the only official model

Rejected because it exposes pipeline internals as the public contract and moves the repo away from typed, named extension units.

## Target Architecture

After this migration, hook/extension ownership is:

- **`kernel.Plugin`** — the only canonical lifecycle extension primitive
- **`kernel/hooks`** — internal typed pipeline engine
- **`harness.Feature`** — assembly unit that may install plugins, but is not itself a second hook model
- **apps/runtime/internal packages** — consumers of `Plugin`, not owners of alternate registry mutation contracts

### 1. Canonical primitive: `kernel.Plugin`

`Plugin` remains the low-level typed lifecycle unit and becomes the only sanctioned way to attach lifecycle behavior.

Retain as public:

- `kernel.Plugin`
- `kernel.WithPlugin(...)`
- `(*Kernel).InstallPlugin(...)`

These are the same primitive at two different installation times:

1. build/configure time via `WithPlugin(...)`
2. runtime installation via `InstallPlugin(...)`

No second hook primitive remains.

`InstallPlugin(...)` remains a post-construction setup API, not a hot-reload contract:

- supported usage is before the kernel begins serving concurrent work
- this round does **not** promise atomic live reconfiguration for in-flight runs
- planners should assume active runs observe only the plugins that were fully installed before their lifecycle stage began
- calling `InstallPlugin(...)` after the kernel has started serving concurrent work is a programming error and should fail immediately with a descriptive panic in this round
- the same timing rule applies to capability providers: `Deps.Kernel.InstallPlugin(...)` is supported during provider initialization / activation only, not as a background live-mutation mechanism after the runtime is already serving requests

### 2. Remove raw registry mutation from the public surface

Remove:

- `kernel.WithPluginInstaller(...)`
- `(*Kernel).InstallHooks(...)`
- exported `(*Kernel).Hooks()`
- `capability.Deps.Hooks`
- raw hooks population inside `runtime.CapabilityDeps(...)`
- public `LLMAgentConfig.Hooks`
- public `(*LLMAgent).Hooks()`
- `harness.SessionLifecycleHooks(...)`
- `harness.ToolLifecycleHooks(...)`

The goal is not to hide lifecycle hooks from the repo; the goal is to stop exposing `*hooks.Registry` as an app/runtime-facing contract.

The typed pipeline engine remains inside `kernel/hooks`, but public callers no longer wire themselves by mutating it directly.

Precise fate of `Hooks()`:

- delete exported `(*Kernel).Hooks()` entirely
- do **not** replace it with another exported raw-registry accessor
- if `kernel` package code or same-package tests still need direct registry access, they do so through package-private access only (`k.chain` directly or a small unexported helper)

Other public-surface replacements in this same convergence:

- `capability.Deps` stops exposing `Hooks`; capability providers that need lifecycle behavior go through `Deps.Kernel.InstallPlugin(...)`
- `runtime.CapabilityDeps(...)` stops exporting raw hooks as a capability dependency
- `LLMAgentConfig` replaces raw `Hooks` injection with plugin-based configuration (`Plugins []Plugin`)
- `(*LLMAgent).Hooks()` is removed from the public API; agent-local lifecycle customization happens through plugins at construction time, while kernel-built agents keep shared hook registries as an internal implementation detail

### 3. Expand `Plugin` so it can cover interceptor use-cases

Today, the strongest reason raw installers still exist is interceptor installation:

- `builtins.InstallLogger(...)` installs interceptors
- TUI permission override logic installs an `OnToolLifecycle` interceptor

This convergence therefore requires `Plugin` to model both hooks and interceptors.

Final `Plugin` shape:

- existing identity/order fields:
  - `Name`
  - `Order`
- hook slots:
  - `BeforeLLM`
  - `AfterLLM`
  - `OnSessionLifecycle`
  - `OnToolLifecycle`
  - `OnError`
- interceptor slots:
  - `BeforeLLMInterceptor`
  - `AfterLLMInterceptor`
  - `OnSessionLifecycleInterceptor`
  - `OnToolLifecycleInterceptor`
  - `OnErrorInterceptor`

Installation semantics:

- hook slots install through `AddHook(...)`
- interceptor slots install through `AddInterceptor(...)`
- nil slots are ignored
- `Order` applies uniformly to every populated slot on the plugin
- when both a hook slot and an interceptor slot are set for the same pipeline on the same `Plugin`, register the interceptor first and the hook second so the plugin's own hook executes inside the plugin's interceptor

This keeps the public contract intentionally simple:

- one named unit
- one ordering field
- one typed slot per pipeline kind

Ordering rule:

- for entries that share the same pipeline and the same `Order`, execution follows registration order
- that means `harness.Plugins(...)` preserves outer feature install order, then plugin slice order, then per-plugin slot registration order

Validity rules for this round:

- `Name` is required and must be non-empty
- a `Plugin` must populate at least one hook or interceptor slot; an all-nil plugin is invalid
- hook and interceptor slots may be combined freely, including both slot types for the same pipeline
- duplicate plugin names are allowed in this round; this convergence does not add global name uniqueness or dependency-graph semantics
- reinstalling the same `Plugin` is additive, not deduplicating; the runtime must not silently replace or skip prior installs

### 4. Do not expose dependency-graph or per-slot custom ordering in this round

`kernel/hooks/pipeline.go` supports named dependency-aware registration (`OnNamed`, `InterceptNamed`), but current production code is not using that surface.

This spec intentionally does **not** promote those advanced ordering semantics into the canonical `Plugin` contract yet.

Reason:

- the current problem is public-surface fragmentation, not missing graph semantics
- promoting dependency graphs now would over-design the replacement contract before there is a proven production need

If a later convergence round finds stable dependency-driven plugin composition requirements, that should be handled in a separate focused spec.

### 5. Harness keeps one wrapper: plugin installation as a feature

`harness.Feature` remains the canonical assembly unit, but its hook story must collapse to one wrapper shape.

Add one unified feature constructor:

- `harness.Plugins(...kernel.Plugin)`

Delete:

- `harness.SessionLifecycleHooks(...)`
- `harness.ToolLifecycleHooks(...)`

Future rule:

- if a feature needs lifecycle behavior, it installs one or more `kernel.Plugin` values
- `harness` does not offer lifecycle-specific hook constructors that bypass or duplicate the plugin model

`harness.Plugins(...)` is an additive configure-phase feature:

- feature name: `"plugins"`
- metadata key: empty / unset (multiple plugin features are allowed to coexist)
- phase: `FeaturePhaseConfigure`
- behavior: install each plugin in argument order via `(*Kernel).InstallPlugin(...)`
- no feature-level dedupe or merge semantics beyond underlying plugin installation

### 6. Builtins and helper surfaces move to plugin-returning helpers

Current raw installer helpers converge to fixed plugin-returning helper names in this round.

Replacement helper names:

- `builtins.InstallLogger(...)` -> `builtins.LoggerPlugin()`
- `builtins.InstallEventEmitter(...)` -> `builtins.EventEmitterPlugin(pattern, handler)`

This matters because builtins are the most visible examples of how extension authors are expected to wire lifecycle behavior.

`k.OnEvent(...)` remains as a convenience API in this round, but only as sugar over `EventEmitterPlugin`; it is not a second primitive.

### 7. Kernel helpers and internal call sites migrate together

Migrate in one pass:

- `appkit/builder.go`
- `kernel/session_persistence.go`
- `internal/runtimepolicy`
- `internal/runtimecontext`
- `contrib/tui/app_kernel_init.go`
- `contrib/tui/app_runtime_posture.go`
- any tests that currently depend on `Hooks()`, `InstallHooks(...)`, or `WithPluginInstaller(...)`

`k.OnEvent(...)` should internally install an event-emitter plugin rather than mutating the registry directly.

The repo should reach a state where compile failures reveal every missed raw-registry usage immediately.

## Migration Plan

Hard-cut still applies to the merged end state: no deprecated forwarders, alias helpers, or compatibility shims survive once the branch lands. Temporary overlap inside this implementation branch is acceptable only when it is needed to keep each phase compiling while migrations are in progress.

Each phase is integration-complete. At the end of every phase, the repo must still compile, and the touched package set must remain testable.

### Phase 1: canonical replacements

- expand `kernel.Plugin` to cover interceptors
- define shared plugin validation (`Name` required, at least one slot required, additive duplicate-install semantics)
- update plugin installation helper(s) accordingly
- add plugin-returning builtin/helper replacements
- add `harness.Plugins(...)`
- add plugin-based `LLMAgentConfig` replacement surface

### Phase 2: repo-wide migration

- migrate `kernel` package internals / same-package tests off exported `Hooks()`, using package-private access where direct registry mutation is still required internally
- migrate capability/runtime surfaces off raw hooks exposure
- migrate kernel/runtime/internal users to `WithPlugin(...)` or `InstallPlugin(...)`
- migrate harness/appkit/TUI and other external-facing usages to the canonical plugin path

### Phase 3: hard-cut deletion

- remove raw-registry public APIs from `kernel`
- remove public raw-hook fields/accessors from capability/runtime/LLMAgent surfaces
- delete lifecycle-specific hook feature sugar from `harness`
- delete legacy builtin installer helpers once their plugin replacements are in use

### Phase 4: cleanup and validation

- run search-based cleanup for deleted symbols
- update docs/comments that still describe registry-level installation as public
- run repo validation

## Error Handling

- Invalid plugin definitions are programming/configuration errors in this round. `WithPlugin(...)` and `InstallPlugin(...)` should fail immediately with a descriptive panic when `Name` is empty or all slots are nil; they must not silently skip, auto-name, or auto-normalize invalid input.
- Calling `InstallPlugin(...)` after concurrent work has started is also a programming/configuration error in this round and should fail immediately with a descriptive panic.
- Duplicate names and repeated installs are **not** treated as errors in this round; they remain additive by design.
- No deprecated forwarders or alias helpers should remain once the canonical plugin path exists.
- Any caller that still depends on deleted raw-registry surfaces should fail at compile time.

## Validation

Validation must satisfy both the rule and the concrete inventory below:

- no exported public field, accessor, or mutator should expose `*hooks.Registry` as an extension-wiring contract
- no non-`kernel` package should mutate lifecycle pipelines through raw `hooks.Registry` access

Search-based validation should then show no remaining production/public references to:

- `WithPluginInstaller(`
- `InstallHooks(`
- exported `func (k *Kernel) Hooks()`
- `capability.Deps.Hooks`
- `LLMAgentConfig.Hooks`
- public `(*LLMAgent).Hooks()`
- `harness.SessionLifecycleHooks(`
- `harness.ToolLifecycleHooks(`

Repository validation remains:

- `go test ./...`
- `go build ./...`
- `Push-Location contrib\tui; go test .; Pop-Location`

## Expected End State

After this convergence:

- hook authors think in terms of `kernel.Plugin`
- assembly authors think in terms of `harness.Feature`
- raw `hooks.Registry` mutation is an internal mechanism, not a public extension contract
- the repo has one clear lifecycle extension story instead of three overlapping ones
