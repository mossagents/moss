# Agent Registry Owner Migration Design

## Problem

`harness` is already the canonical public surface for subagent configuration, but the actual owner of the subagent registry and delegation control plane still lives in `runtime`.

That leaves one split model in place:

1. `harness.SubagentCatalog` is the public API.
2. `runtime.AgentRegistry` / `runtime.WithAgentRegistry` are still the real low-level owner hooks.
3. `runtime` also owns delegation tool registration and the task-tracking substrate behind subagent execution.

This is inconsistent with the current architecture direction:

- `harness` should be the canonical public surface.
- `runtime` should shrink toward setup, discovery, and reporting.
- domain-specific execution substrates should live with their domain, not in a generic runtime shell.

This redesign intentionally does **not** preserve backward compatibility for the runtime-level registry/configuration hooks.

## Goals

- Move subagent registry ownership out of `runtime`.
- Move delegation tool installation to the same domain substrate that owns the registry.
- Keep `harness` as the canonical public facade for configuring and reading subagents.
- Reduce `runtime` to directory discovery and capability reporting for subagents.
- Remove runtime-only compatibility hooks that are no longer the canonical architecture.

## Non-Goals

- Rewriting the agent tool protocol or task model.
- Changing the public `harness.SubagentCatalog` API shape.
- Moving project/user agent directory discovery into `harness`.
- Changing trust gating semantics for `.agents/agents` discovery.

## User-Approved Design Decisions

- Migration depth: **Move registry state and delegation tool installation substrate out of `runtime`; keep directory scanning and capability reporting in `runtime`.**
- Landing zone: **Sink the substrate into `agent`, not `kernel`.**
- Compatibility posture: **Remove runtime-level registry/configuration setters instead of preserving them.**

## Target Architecture

After the migration, the ownership model becomes:

- **`kernel`** — shared service container and boot lifecycle host.
- **`agent`** — owner of the subagent registry and delegation control-plane substrate.
- **`harness`** — canonical public facade for subagent configuration and registration.
- **`runtime`** — consumer that discovers agent definitions on disk and reports capability state.

The key change is that `runtime` stops being the hidden owner of the subagent registry. It becomes just another consumer of the `agent` substrate.

## Core Components

### 1. Agent kernel-scoped collaboration substrate

Add a new low-level substrate in `agent` (for example `agent/kernel_service.go` or `agent/collaboration_service.go`) that is responsible for:

- holding the kernel-scoped `Registry`
- holding the kernel-scoped `TaskTracker`
- resolving delegation runtime dependencies from the kernel at boot
- registering delegation tools exactly once during boot

The substrate should be created through `k.Services().LoadOrStore(...)` so the kernel still remains the lifetime owner of the shared service slot, while `agent` owns the domain semantics of what lives in that slot.

The substrate must also track its own installation state explicitly. Creation and installation are related but not identical steps.

### 2. Boot-time dependency resolution

The new `agent` substrate should resolve runtime dependencies from the kernel directly during boot:

- `k.TaskRuntime()`
- `k.Mailbox()`
- `k.WorkspaceIsolation()`

If these are absent, it should preserve the current behavior by falling back to in-memory implementations where the existing runtime path already does so.

This removes the need for separate runtime-level setters like `runtime.WithTaskRuntime`, `runtime.WithMailbox`, and `runtime.WithWorkspaceIsolation`.

### 3. Delegation tool installation

Delegation tool installation should move with the substrate owner.

Instead of `runtime` creating agent state and then calling `agent.RegisterToolsWithDeps(...)`, the new `agent` substrate should own:

- when the registry is considered boot-ready
- when the tracker is created
- when `RegisterToolsWithDeps(...)` runs

That keeps the registry owner and the tool installer in one place instead of splitting them across `agent` and `runtime`.

### 3a. Same-boot initialization rule

The substrate must guarantee that delegation tools become available in the same kernel lifecycle in which the substrate is first needed.

That means:

- if the substrate is initialized before boot starts, it may register one boot hook
- if the substrate is first initialized after boot has already started, it must install delegation tools synchronously instead of relying on a missed future boot pass

`runtime.setupAgents(...)` is therefore allowed to consume the substrate, but it must not be able to populate the registry during boot while delegation tools are still absent for that same booted kernel.

### 4. Harness facade

`harness` stays the only canonical public facade for subagent configuration.

That means:

- `harness.SubagentCatalogOf(k)` wraps the registry exposed by the `agent` substrate.
- `harness.SubagentCatalogValue(...)` injects/replaces the registry through the `agent` substrate instead of `runtime.WithAgentRegistry(...)`.
- `harness.RegisterSubagent(...)` and `harness.LoadSubagentsFromYAML(...)` keep their current surface and behavior.

The public model remains `harness`-owned even though the low-level service is now `agent`-owned.

Registry replacement is explicitly a **configure-phase, pre-boot operation**. Once delegation tools have been installed for a kernel, replacing the registry is invalid and must fail fast with an explicit error rather than silently swapping the backing registry out from under already-installed tools.

### 5. Runtime as discovery/reporting consumer

`runtime` should retain only the concerns that actually belong to runtime setup:

- resolve the ordered list of agent definition directories
- gate workspace-local discovery by trust posture
- call `LoadDir(...)` on the registry
- report capability/subagent readiness through the capability reporter

It should no longer define or expose the registry substrate itself.

## Data Flow and Lifecycle

The boot/data flow after the migration is:

1. Kernel starts and creates its service registry.
2. The `agent` collaboration substrate is initialized the first time a caller asks for subagent/delegation state or explicitly installs a subagent catalog.
3. The substrate either installs one boot hook before boot or performs same-boot synchronous installation if boot is already in progress.
4. During installation, it resolves task runtime, mailbox, and workspace isolation from the kernel, creates the task tracker if needed, and registers delegation tools.
5. `harness` reads and writes subagent definitions through the `agent` substrate.
6. `runtime.setupAgents(...)` loads file-based agent definitions into the same registry and emits capability reports.

This preserves one shared registry and one shared task/delegation control plane while removing runtime as the hidden owner.

## API Changes

### New low-level substrate

Add new `agent`-level helpers for the kernel-scoped collaboration substrate. The exact names can vary, but the shape should include:

- an accessor for the kernel-scoped collaboration service
- a way to read the registry from that service
- a way for `harness.SubagentCatalogValue(...)` to inject/replace the registry before boot
- a way to determine whether delegation tools have already been installed so post-boot replacement can be rejected explicitly

The service itself may remain low-level and not be treated as a primary public API; `harness` remains the preferred public facade.

### Removed runtime-level hooks

Delete the following runtime surfaces:

- `runtime.AgentRegistry`
- `runtime.WithAgentRegistry`
- `runtime.WithTaskRuntime`
- `runtime.WithMailbox`
- `runtime.WithWorkspaceIsolation`

This is an intentional hard cut. After the migration, `runtime` is not the extension/configuration owner for this domain anymore.

## Migration Plan

Implement in this order:

1. Add the new kernel-scoped collaboration substrate in `agent` with unit tests.
2. Switch `harness.SubagentCatalogOf(...)` and `harness.SubagentCatalogValue(...)` to the new substrate.
3. Switch `runtime.setupAgents(...)` to consume the new substrate as a plain registry consumer.
4. Delete the runtime-level registry/configuration hooks.
5. Rename or shrink the remaining runtime file so it clearly represents discovery/reporting ownership rather than state ownership.

This order keeps the change incremental and makes it possible to validate the substrate before deleting the old runtime entry points.

## Error Handling and Invariants

The migration must preserve these invariants:

- `harness` remains the canonical public surface for subagent configuration.
- `runtime` remains responsible for trust-gated directory discovery.
- delegation tools are registered at most once per kernel.
- no duplicate registry instances are created for one kernel.
- lack of task runtime/mailbox/isolation does not crash setup; it preserves the current memory-backed fallback behavior.
- capability reporting semantics for newly discovered subagents remain unchanged.
- registry replacement is only valid before delegation tool installation.
- duplicate-name behavior remains explicit rather than being silently changed in this round.

Failure cases should stay explicit:

- invalid YAML in subagent definition files still returns or reports an error through the existing runtime setup/reporting path
- registry injection through `harness.SubagentCatalogValue(...)` still rejects nil catalogs
- registry injection after installation has started fails explicitly
- delegation tool registration errors still fail boot rather than being silently ignored
- duplicate file-loaded or directory-loaded agent names still fail through the underlying registry registration path

Duplicate-name semantics are intentionally unchanged in this round:

- `harness.RegisterSubagent(...)` remains idempotent for an already-registered code-defined name
- file-driven loading through `Registry.LoadDir(...)` remains strict and fails on duplicate names

This preserves current behavior while moving owner boundaries; harmonizing duplicate handling is a separate design question.

## Testing

Required coverage:

### `agent`

- kernel-scoped substrate is singleton-per-kernel
- boot hook is idempotent
- same-boot initialization works when the substrate is first touched during boot
- delegation tools register successfully with kernel-provided dependencies
- in-memory fallback path works when kernel dependencies are absent

### `harness`

- `SubagentCatalogOf(...)` reads from the new substrate
- `SubagentCatalogValue(...)` injects the registry through the new substrate
- `RegisterSubagent(...)` and YAML loading behavior stay unchanged

### `runtime`

- trusted vs restricted directory discovery behavior is unchanged
- discovered subagents still appear in capability reporting
- runtime no longer needs to own registry state to perform scanning
- agents discovered from runtime-managed directories are delegatable in the same booted kernel

### Repository validation

- `go test ./...`
- `go build ./...`
- `Push-Location contrib\tui; go test .; Pop-Location`

## Acceptance Criteria

- no runtime-owned registry/configuration surface remains for subagent delegation
- the kernel-scoped collaboration substrate lives in `agent`
- `harness` remains the canonical public facade
- `runtime` only consumes the substrate for discovery/reporting
- full repository validation passes after the migration
