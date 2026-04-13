---
goal: Move subagent registry ownership from runtime into the agent substrate while keeping harness as the canonical public facade
version: 1.0
date_created: 2026-04-13
last_updated: 2026-04-13
owner: Core Runtime Team
status: Planned
tags: [architecture, refactor, migration, agents, harness, runtime]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This implementation plan executes the approved design in `docs/superpowers/specs/2026-04-13-agent-registry-owner-migration-design.md`. The goal is to move the kernel-scoped subagent registry and delegation control-plane substrate out of `runtime` and into `agent`, preserve `harness` as the canonical public surface, and reduce `runtime` to discovery/reporting behavior only.

## 1. Requirements & Constraints

- **REQ-001**: Create a kernel-scoped collaboration substrate in `agent` that owns the subagent registry, task tracker, and delegation-tool installation lifecycle.
- **REQ-002**: Preserve `harness.SubagentCatalogOf(...)`, `harness.SubagentCatalogValue(...)`, `harness.RegisterSubagent(...)`, and `harness.LoadSubagentsFromYAML(...)` as the canonical public subagent surface.
- **REQ-003**: Remove runtime-owned registry/configuration hooks: `runtime.AgentRegistry`, `runtime.WithAgentRegistry`, `runtime.WithTaskRuntime`, `runtime.WithMailbox`, and `runtime.WithWorkspaceIsolation`.
- **REQ-004**: Keep `runtime.setupAgents(...)` and `collectAgentDirs(...)` as the owner of trusted/restricted directory discovery and capability reporting only.
- **REQ-005**: Guarantee same-boot delegatability: if file-discovered agents are loaded during boot, delegation tools must already be installed for that same kernel lifecycle.
- **SEC-001**: Preserve explicit error propagation for registry injection failures, duplicate file-loaded agent names, and delegation-tool registration errors.
- **CON-001**: Do not redesign agent tool semantics, task contracts, or task runtime storage in this change.
- **CON-002**: Do not move `.agents/agents` discovery logic into `harness`.
- **CON-003**: Keep current duplicate-name semantics unchanged in this round: `harness.RegisterSubagent(...)` remains idempotent for existing code-registered names; `agent.Registry.LoadDir(...)` remains strict and fails on duplicates.
- **GUD-001**: Use `kernel.Services().LoadOrStore(...)` for kernel-scoped collaboration state instead of adding ad hoc globals.
- **PAT-001**: Follow the current architecture direction: `kernel` hosts shared service slots, `agent` owns delegation substrate semantics, `harness` owns public composition, `runtime` consumes the substrate for setup/reporting.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Introduce the `agent`-owned kernel collaboration substrate and make same-boot delegation deterministic.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Create `agent/kernel_service.go` implementing the kernel-scoped collaboration substrate: define the `kernel.ServiceKey`, service state (`Registry`, `TaskTracker`, installation state), accessor/ensure helper, and the installation path that resolves `k.TaskRuntime()`, `k.Mailbox()`, and `k.WorkspaceIsolation()` plus existing memory fallbacks. |  |  |
| TASK-002 | In `agent/kernel_service.go`, implement explicit same-boot installation behavior: if the substrate is initialized before boot, install one boot hook; if it is initialized after boot has started, register delegation tools synchronously so file-discovered agents are delegatable in that same booted kernel. |  |  |
| TASK-003 | Create `agent/kernel_service_test.go` covering singleton-per-kernel behavior, idempotent installation, same-boot installation when first touched during boot, registry replacement pre-boot vs post-install rejection, and memory-backed fallback behavior when kernel task/mailbox/isolation dependencies are absent. |  |  |
| TASK-004 | Update `agent/tools_test.go` only where necessary so low-level delegation-tool assertions continue to pass when the control plane is installed through the new kernel-scoped substrate rather than runtime-owned state. |  |  |

### Implementation Phase 2

- **GOAL-002**: Switch `harness` and `runtime` to the new substrate and delete runtime-owned registry/configuration hooks.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Update `harness/subagents.go` so `SubagentCatalogOf(...)` wraps the registry exposed by the new `agent` substrate and `SubagentCatalogValue(...)` injects/replaces the registry through the new substrate instead of `runtime.WithAgentRegistry(...)`. Keep nil-catalog rejection and the existing public API shape unchanged. |  |  |
| TASK-006 | Update `harness/subagents_test.go` to validate that `RegisterSubagent(...)` and `LoadSubagentsFromYAML(...)` now use the `agent` substrate while preserving current behavior and duplicate-name semantics. |  |  |
| TASK-007 | Refactor `runtime/runtime_agents_service.go` to remove `agentsState`, `ensureAgentsState(...)`, `AgentRegistry(...)`, `WithAgentRegistry(...)`, `WithTaskRuntime(...)`, `WithMailbox(...)`, and `WithWorkspaceIsolation(...)`. Keep only directory discovery/reporting concerns and consume the new `agent` substrate as a plain registry reader/writer. |  |  |
| TASK-008 | Update compile/test callers affected by the hook removal, including `appkit/deep_agent.go`, `appkit/deep_agent_test.go`, `appkit/appkit_test.go`, and any remaining runtime-facing call sites discovered by `rg "AgentRegistry|WithAgentRegistry|WithTaskRuntime|WithMailbox|WithWorkspaceIsolation"`. Remove or rewrite tests that only assert the deleted runtime compatibility surface. |  |  |

### Implementation Phase 3

- **GOAL-003**: Prove runtime discovery/reporting parity and repository-wide validity after the owner migration.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-009 | Create `runtime/runtime_agents_service_test.go` covering trusted vs restricted directory discovery, capability reporting for discovered subagents, and end-to-end proof that a subagent loaded from a runtime-managed directory is delegatable in the same booted kernel. |  |  |
| TASK-010 | Run focused package validation for `agent`, `harness`, `runtime`, and `appkit`, fix all compile/test breakages from the substrate migration, and ensure no remaining production code references the deleted runtime hooks. |  |  |
| TASK-011 | Run repository validation exactly with `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`; fix all regressions before closing the migration. |  |  |

## 3. Alternatives

- **ALT-001**: Keep the substrate in `runtime` and only hide `runtime.AgentRegistry(...)` behind `harness`; rejected because it preserves runtime as the hidden owner and only renames the public seam.
- **ALT-002**: Sink the substrate into `kernel`; rejected because it pushes multi-agent/delegation domain semantics into the generic kernel layer.
- **ALT-003**: Move discovery of `.agents/agents` into `harness`; rejected because it mixes setup/reporting with public composition and violates the approved design boundary.

## 4. Dependencies

- **DEP-001**: `kernel.ServiceRegistry` in `kernel/services.go` remains the service-slot mechanism used by the new `agent` substrate.
- **DEP-002**: `agent.Registry`, `agent.TaskTracker`, and `agent.RegisterToolsWithDeps(...)` are the low-level domain primitives that the new substrate must compose without semantic changes.
- **DEP-003**: `harness/subagents.go` is the canonical public facade and must remain the preferred external path after the migration.
- **DEP-004**: `runtime/runtime_agents_service.go` currently owns discovery plus state; it must be reduced to discovery/reporting without changing trust-gated directory semantics.
- **DEP-005**: The approved design spec `docs/superpowers/specs/2026-04-13-agent-registry-owner-migration-design.md` is the source of truth for scope and invariants.

## 5. Files

- **FILE-001**: `agent/kernel_service.go` — new kernel-scoped collaboration substrate owner.
- **FILE-002**: `agent/kernel_service_test.go` — substrate lifecycle and same-boot installation tests.
- **FILE-003**: `agent/tools_test.go` — low-level delegation-tool assertions that may need adaptation.
- **FILE-004**: `harness/subagents.go` — canonical public facade retargeted to the `agent` substrate.
- **FILE-005**: `harness/subagents_test.go` — public subagent facade regression tests.
- **FILE-006**: `runtime/runtime_agents_service.go` — reduced to discovery/reporting consumer logic only.
- **FILE-007**: `runtime/runtime_agents_service_test.go` — discovery/reporting and same-boot delegatability tests.
- **FILE-008**: `appkit/deep_agent.go` — deep-agent composition path that consumes `harness.SubagentCatalogOf(...)`.
- **FILE-009**: `appkit/deep_agent_test.go` — deep-agent subagent catalog regression tests.
- **FILE-010**: `appkit/appkit_test.go` — trusted/restricted project agent discovery assertions.
- **FILE-011**: `docs/superpowers/specs/2026-04-13-agent-registry-owner-migration-design.md` — approved design spec referenced by this plan.

## 6. Testing

- **TEST-001**: `agent/kernel_service_test.go` verifies singleton substrate ownership, explicit installation state, and same-boot installation.
- **TEST-002**: `agent/kernel_service_test.go` verifies pre-boot registry injection is allowed and post-install replacement is rejected explicitly.
- **TEST-003**: `harness/subagents_test.go` verifies code-defined registration and YAML loading still work through the new substrate.
- **TEST-004**: `runtime/runtime_agents_service_test.go` verifies trusted/restricted discovery parity and capability reporting behavior.
- **TEST-005**: `runtime/runtime_agents_service_test.go` verifies a runtime-discovered agent can be delegated to in the same booted kernel.
- **TEST-006**: Search-based validation proves no remaining production references to `runtime.AgentRegistry`, `runtime.WithAgentRegistry`, `runtime.WithTaskRuntime`, `runtime.WithMailbox`, or `runtime.WithWorkspaceIsolation`.
- **TEST-007**: Repository-wide validations: `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: Same-boot installation may be implemented incorrectly if kernel boot state is inferred implicitly rather than checked explicitly.
- **RISK-002**: Registry replacement semantics can become racy if post-install mutation is not rejected deterministically.
- **RISK-003**: Runtime discovery tests may miss the end-to-end delegatability requirement and allow a latent boot-order bug to survive.
- **ASSUMPTION-001**: No production caller outside the current repository requires the deleted runtime-level registry/configuration hooks.
- **ASSUMPTION-002**: Existing duplicate-name behavior is intentional enough to preserve during owner migration, even though it differs between code registration and file loading.
- **ASSUMPTION-003**: `runtime/runtime_agents_service.go` can be reduced in place without introducing a second discovery file.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-13-agent-registry-owner-migration-design.md`
- `harness/subagents.go`
- `runtime/runtime_agents_service.go`
- `agent/registry.go`
- `agent/tools.go`
