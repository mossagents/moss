# Execution Substrate Owner Convergence Design

## Problem

P0 public assembly convergence established one architectural direction:

- `harness` is the canonical public assembly surface.
- `appkit` should remain a thin builder/preset layer.
- `runtime` should shrink toward substrate, service, and reporting responsibilities rather than remain a second assembly API.

That direction is still not fully enforced for execution substrate ownership.

Today, execution capability assembly is split across three layers:

1. **`harness` exposes a public execution feature, but it still depends on a runtime-owned public assembly object.**
   - `harness/features.go`
     - `ExecutionSurface(surface *runtime.ExecutionSurface)`
     - `ExecutionCapabilityReport(...)` calls `runtime.ExecutionSurfaceFromKernel(...)`

2. **`kernel` already owns the sandbox-to-port adaptation that should sit underneath execution assembly.**
   - `kernel/option.go`
     - `WithSandbox(...)`
     - `sandboxWorkspaceAdapter`
     - `sandboxExecutorAdapter`

3. **`runtime` still exposes a second public execution substrate model and uses it as both adapter layer and diagnostics model.**
   - `runtime/execution_surface.go`
     - `ExecutionSurface`
     - `NewExecutionSurface(...)`
     - `ProbeExecutionSurface(...)`
     - `ExecutionSurfaceFromKernel(...)`
     - `KernelOptions()`
     - `WorkspacePort()`
     - `ExecutorPort()`
   - `runtime/builtin_tools_registry.go`
     - `RegisteredBuiltinToolNames(...)`
     - `RegisterBuiltinTools(...)`
     - `RegisterBuiltinToolsForKernel(...)`
   - `runtime/builtin_tools_filesystem.go`
     - repeated `newExecutionSurface(sb, nil, nil)` bridge construction
   - `runtime/builtin_tools_exec.go`
     - repeated `newExecutionSurface(sb, nil, nil)` bridge construction

There is also a secondary split in app/product call sites:

- `appkit/deep_agent_packs.go` still creates a `runtime.NewExecutionSurface(...)` and passes it into `harness.ExecutionSurface(...)`
- `appkit/product/runtime_doctor.go` and `appkit/product/inspect_threads.go` use `runtime.ProbeExecutionSurface(...)` for standalone execution diagnostics

This creates several architectural problems:

- there is no single owner for execution substrate assembly
- `runtime.ExecutionSurface` mixes two concepts:
  - assembly input / kernel option source
  - offline diagnostics / capability reporting
- builtin tools still act like private adapter owners instead of consuming already-installed ports
- `harness` public assembly still leaks a runtime public type, which conflicts with the P0 model

This redesign intentionally does **not** preserve backward compatibility for the execution substrate assembly path.

## Goals

- Make `harness` the only public assembly owner for execution substrate configuration.
- Keep `harness.Backend` focused on `workspace.Workspace` and `workspace.Executor`.
- Remove runtime-owned execution adapter bridges from builtin tool registration and handlers.
- Preserve a standalone execution diagnostics/probe API for doctor/inspect flows that do not have an assembled `Kernel`.
- Separate assembly, ports, and diagnostics into distinct owners.

## Non-Goals

- P2 execution policy plane convergence.
- P3 kernel execution API convergence.
- Reworking `harness.Backend` into a larger execution-services abstraction.
- Changing doctor/inspect UX or capability snapshot semantics beyond the ownership split.
- Reworking `runtime` HTTP/ask-user builtin tool semantics outside the substrate wiring they consume.

## User-Approved Design Decisions

- Compatibility posture: **hard cut; do not preserve backward compatibility**
- Diagnostics boundary: **keep a standalone probe API even when no `Kernel` has been assembled**
- Assembly boundary: **remove `runtime.ExecutionSurface` from `harness` public input**
- Selected approach: **harness-owned execution assembly + runtime diagnostics-only**

## Rejected Approaches

### 1. Backend-centric convergence

Push isolation, repo-state, patch, and snapshot ownership into `harness.Backend` / `BackendFactory`.

Rejected because it would overload `Backend` from a deployment unit into a larger execution-runtime owner. That would collapse one split by creating a new monolith and would make future policy/reporting convergence harder.

### 2. Kernel-centric public substrate API

Move public execution assembly down into `kernel` and let `harness` become a thin wrapper.

Rejected because it reverses the P0 direction. `kernel` should own ports, not regain public application assembly responsibilities.

## Target Architecture

After this migration, execution ownership is:

- **`harness`** — public execution assembly
- **`kernel`** — canonical execution ports
- **`runtime`** — standalone diagnostics and capability reporting
- **`internal/` packages** — assembly implementation details

### 1. Harness owns the public execution assembly surface

Remove:

- `harness.ExecutionSurface(surface *runtime.ExecutionSurface)`

Introduce a harness-owned public feature:

- `harness.ExecutionServices(workspace, isolationRoot string, isolationEnabled bool) Feature`

Responsibilities:

- construct the execution-support services needed around an already-activated backend
- install:
  - `workspace.WorkspaceIsolation`
  - `workspace.RepoStateCapture`
  - `workspace.PatchApply`
  - `workspace.PatchRevert`
  - `workspace.WorktreeSnapshotStore`
- validate the requested execution-service configuration before runtime capability assembly proceeds

This feature does **not** own `workspace.Workspace` or `workspace.Executor`; those remain backend responsibilities.

### 2. Backend stays narrow and does not absorb auxiliary execution services

`harness.Backend` remains:

- `workspace.Workspace`
- `workspace.Executor`

`BackendInstaller` / `BackendFactory` continue to be the only place where sandbox fallback is allowed to materialize into concrete `Workspace` / `Executor` ports.

This means:

- `kernel.WithSandbox(...)` remains the low-level adapter owner for sandbox-to-port fallback
- `harness.ActivateBackend(...)` remains the assembly-time place where that fallback becomes real
- builtin tools and higher layers no longer recreate their own surface/adapter bridge later

### 3. Internal execution assembly becomes the implementation owner

Introduce a non-public internal package for execution assembly implementation:

- `internal/runtimeexecution`

Responsibilities:

- construct execution-support services from `workspace` root, isolation root, and isolation-enabled flag
- return/install the resulting kernel options for harness execution assembly
- provide kernel-based capability status/report inputs for `harness.ExecutionCapabilityReport(...)`

This package is intentionally internal because it is assembly logic, not public substrate.

### 4. Runtime keeps diagnostics-only execution probing

`runtime/execution_surface.go` should be narrowed and renamed to reflect its retained responsibility.

Recommended shape:

- rename the public diagnostics model from `ExecutionSurface` to something diagnostics-specific such as `ExecutionProbe`
- keep standalone constructors/probe helpers for no-kernel flows
- keep capability status calculation and reporting helpers
- remove all assembly-oriented methods and fields

Specifically remove from the runtime diagnostics type:

- `KernelOptions()`
- `WorkspacePort()`
- `ExecutorPort()`
- `HasWorkspace()`
- `HasExecutor()`
- `Sandbox()`
- any private `newExecutionSurface(...)` helper that exists only to bridge builtin handlers

Allowed retained runtime diagnostics responsibilities:

- `ProbeExecution...(...)`
- diagnostics from a live kernel for reporting
- capability constants and status calculation
- `ReportExecution...(...)`

The retained runtime diagnostics API is intentionally read-only and report-oriented. It must not be reusable as a public assembly input.

### 5. Builtin tools become port consumers, not adapter owners

`runtime/builtin_tools_registry.go`, `runtime/builtin_tools_filesystem.go`, and `runtime/builtin_tools_exec.go` should be converged around one rule:

- builtin tools read the already-installed execution ports
- builtin tools do not create or infer their own `ExecutionSurface`

Concretely:

- delete `newExecutionSurface(...)`
- remove repeated bridge creation in filesystem and exec handlers
- collapse registration logic around kernel/ports rather than the `sb/ws/exec` triad

Preferred direction:

- runtime builtin tool registration keeps a kernel-driven API for runtime assembly
- handler helpers operate on explicit `workspace.Workspace` / `workspace.Executor` inputs
- if a required port is missing at registration time, registration returns an explicit error

This removes the current “late silent fallback” behavior where runtime recreates a bridge after assembly should already be complete.

### 6. Capability reporting uses kernel state directly

`harness.ExecutionCapabilityReport(...)` should stop depending on `runtime.ExecutionSurfaceFromKernel(...)`.

Instead:

- it should ask the internal execution assembly/reporting owner for capability statuses derived from the live kernel ports
- runtime diagnostics may still expose a report helper for offline or post-assembly inspection, but not an assembly object

This keeps runtime reporting as reporting, while preventing the reporting model from remaining the de facto assembly contract.

### 7. Appkit and product call-site migration

The following call sites move to the new ownership model:

- `appkit/deep_agent_packs.go`
  - stop constructing `runtime.NewExecutionSurface(...)`
  - use `harness.ExecutionServices(...)`
- `appkit/product/runtime_doctor.go`
- `appkit/product/inspect_threads.go`
  - keep using the retained runtime standalone probe API
  - migrate to the diagnostics-only type/function names if renamed

This preserves offline diagnostics while removing runtime from public assembly composition.

## Data Flow

### Normal assembly flow

1. Backend activation materializes `Workspace` / `Executor`
2. `harness.ExecutionServices(...)` installs auxiliary execution services
3. runtime builtin tool registration reads the kernel ports already present
4. execution capability reporting reads live kernel state

### Offline diagnostics flow

1. doctor/inspect calls runtime standalone probe helpers
2. runtime diagnostics inspects local execution readiness
3. diagnostics returns capability statuses without requiring an assembled kernel

## Error Handling

- Missing or invalid execution-service config in `harness.ExecutionServices(...)` is an install-time error.
- Missing required ports during builtin tool registration is a registration-time error.
- Runtime diagnostics may report degraded/failed capability states, but must not silently create new assembly bridges.
- Sandbox fallback is allowed only in:
  - `kernel.WithSandbox(...)`
  - backend activation/factory flows that intentionally use that kernel adapter

## Testing Strategy

### Harness

- adding `ExecutionServices(...)` installs the expected kernel ports
- invalid workspace/isolation configuration fails explicitly
- `ExecutionCapabilityReport(...)` reports from live kernel state without a runtime assembly object

### Runtime builtin tools

- registration succeeds from already-installed ports
- registration fails explicitly when required ports are absent
- filesystem and exec handlers no longer depend on `newExecutionSurface(...)`

### Runtime diagnostics

- standalone probe still reports capability readiness without a kernel
- diagnostics-from-kernel reporting still works
- diagnostics type no longer exposes assembly methods

### Appkit/product

- deep-agent execution pack uses the harness-owned execution feature
- doctor/inspect continue to work through the retained standalone probe API

## Validation

At minimum, this round should re-run:

- `go test ./...`
- `go build ./...`
- `Push-Location contrib\tui; go test .; Pop-Location`

## Expected Outcome

After this migration:

- `harness` becomes the only public execution assembly owner
- `runtime` no longer provides a second public substrate model for assembly
- builtin tools stop acting as hidden adapter owners
- offline diagnostics remain available without pulling runtime back into the assembly surface
- the repo is positioned for the next convergence step, especially execution policy plane cleanup
