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

3. **`harness` adds lifecycle-specific wrapper features on top.**
   - `harness.SessionLifecycleHooks(...)`
   - `harness.ToolLifecycleHooks(...)`

4. **Production code still uses both models.**
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

### 2. Remove raw registry mutation from the public surface

Remove:

- `kernel.WithPluginInstaller(...)`
- `(*Kernel).InstallHooks(...)`
- `(*Kernel).Hooks()` as a public accessor
- `harness.SessionLifecycleHooks(...)`
- `harness.ToolLifecycleHooks(...)`

The goal is not to hide lifecycle hooks from the repo; the goal is to stop exposing `*hooks.Registry` as an app/runtime-facing contract.

The typed pipeline engine remains inside `kernel/hooks`, but public callers no longer wire themselves by mutating it directly.

### 3. Expand `Plugin` so it can cover interceptor use-cases

Today, the strongest reason raw installers still exist is interceptor installation:

- `builtins.InstallLogger(...)` installs interceptors
- TUI permission override logic installs an `OnToolLifecycle` interceptor

This convergence therefore requires `Plugin` to model both hooks and interceptors.

Recommended `Plugin` shape:

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

This keeps the public contract intentionally simple:

- one named unit
- one ordering field
- one typed slot per pipeline kind

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

- `harness.Plugins(...kernel.Plugin)` (name can be finalized during implementation if a better noun emerges)

Delete:

- `harness.SessionLifecycleHooks(...)`
- `harness.ToolLifecycleHooks(...)`

Future rule:

- if a feature needs lifecycle behavior, it installs one or more `kernel.Plugin` values
- `harness` does not offer lifecycle-specific hook constructors that bypass or duplicate the plugin model

### 6. Builtins and helper surfaces move to plugin-returning helpers

Current raw installer helpers should converge to plugin-returning helpers.

Examples:

- `builtins.InstallLogger(...)` -> `builtins.LoggerPlugin()`
- `builtins.InstallEventEmitter(...)` -> `builtins.EventEmitterPlugin(pattern, handler)`

This matters because builtins are the most visible examples of how extension authors are expected to wire lifecycle behavior.

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

### Phase 1: canonical plugin contract

- expand `kernel.Plugin` to cover interceptors
- update plugin installation helper(s) accordingly
- remove raw-registry public APIs from `kernel`

### Phase 2: builtin/internal migration

- convert logger/event-emitter helpers to plugin-returning helpers
- migrate kernel/runtime/internal users to `WithPlugin(...)` or `InstallPlugin(...)`

### Phase 3: harness/public wrapper convergence

- add unified plugin feature wrapper in `harness`
- delete lifecycle-specific hook feature sugar
- migrate external-facing usages

### Phase 4: cleanup and validation

- run search-based cleanup for deleted symbols
- update docs/comments that still describe registry-level installation as public
- run repo validation

## Error Handling

- Invalid plugin definitions should fail explicitly at installation time rather than silently skipping unsupported combinations.
- No deprecated forwarders or alias helpers should remain once the canonical plugin path exists.
- Any caller that still depends on deleted raw-registry surfaces should fail at compile time.

## Validation

Search-based validation should show no remaining production/public references to:

- `WithPluginInstaller(`
- `InstallHooks(`
- public `Hooks()` call sites outside kernel-internal testing needs
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
