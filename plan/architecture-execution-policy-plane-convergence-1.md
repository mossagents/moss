---
goal: Converge the execution policy plane so harness owns public tool-policy assembly, runtime owns the canonical model, and planner/enforcement consume one policy contract
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: Core Runtime Team
status: Planned
tags: [architecture, refactor, migration, harness, runtime, policy]
---

# Introduction

![Status: Planned](https://img.shields.io/badge/status-Planned-blue)

This implementation plan executes `docs/superpowers/specs/2026-04-14-execution-policy-plane-convergence-design.md`. The goal is to hard-cut the remaining split between structured runtime policy state, raw harness/appkit rule installation, and heuristic turn planning by introducing one canonical `runtime.ToolPolicy` model, one `harness.ToolPolicy(...)` public assembly surface, one `internal/runtimepolicy.Apply(...)` internal compiler/apply boundary, and one exact session metadata summary contract consumed by `kernel/loop`.

## 1. Requirements & Constraints

- **REQ-001**: Replace the canonical concept name `ExecutionPolicy` with `ToolPolicy` across production runtime/profile/posture code so the model scope matches command, HTTP, workspace, memory, graph, protected-path, and approval-class governance.
- **REQ-002**: `harness` must become the only public assembly owner for canonical tool policy through `harness.ToolPolicy(policy runtime.ToolPolicy) Feature`.
- **REQ-003**: `internal/runtimepolicy.Apply(...)` must be the only canonical apply/compiler path and must be idempotent plus replace-based for repeated installs on the same kernel.
- **REQ-004**: Delete the canonical public raw-rule path currently exposed through `harness.ExecutionPolicy(...)` and used by product/deep-agent approval composition.
- **REQ-005**: Move rule compilation and tool-context permission merging behind internal runtime/runtimepolicy code; `runtime` must not retain public APIs such as `ToolPolicyRules(...)` or `ToolPolicyForToolContext(...)`.
- **REQ-006**: `kernel/session` must expose an exact metadata contract with `MetadataToolPolicy`, `MetadataToolPolicySummary`, and `session.ToolPolicySummary` so planner code can derive tool routing without reading runtime kernel state.
- **REQ-007**: `kernel/loop/turn_plan.go` must derive approval-required, hidden, and visible routing from canonical policy summary plus tool metadata, not from trust/approval heuristics.
- **REQ-008**: Runtime direct enforcement and compiled hook enforcement must stay semantically aligned for equivalent tool-policy inputs.
- **REQ-009**: Runtime/profile/posture consumers in `runtime`, `contrib\tui`, `apps\mosscode`, `internal\runtimeassembly`, and `appkit\product` must migrate in one pass so deleted symbols fail at compile time.
- **SEC-001**: Do not preserve backward compatibility aliases for removed production names such as `ExecutionPolicy`, `ExecutionPolicyRules`, `SetExecutionPolicy`, or `session.MetadataExecutionPolicy`.
- **SEC-002**: Invalid canonical policy input, malformed serialized metadata payloads, and failed derived-artifact compilation must fail explicitly instead of degrading silently.
- **CON-001**: Do not perform P3 kernel execution API convergence (`Run` vs `RunAgent`) in this change.
- **CON-002**: Do not reintroduce runtime as a second public assembly surface through `WithToolPolicy(...)`, `SetToolPolicy(...)`, or other public install helpers.
- **CON-003**: Keep `kernel.WithPolicy(...)` only as a low-level generic hook utility; it must not remain part of the canonical execution/tool-policy path.
- **GUD-001**: Preserve the P0/P1 owner split: `runtime` = canonical model/resolution, `harness` = public assembly, `internal/*` = assembly/application implementation, `kernel/loop` = planner decisions.
- **GUD-002**: Use hard cuts instead of compat shims; any missed callers should fail at compile time and be migrated explicitly.
- **PAT-001**: Session metadata must store both a versioned serialized full canonical policy payload and a derived summary payload so runtime restore and planner routing stay deterministic.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Establish the canonical `ToolPolicy` model and the single internal apply/compiler boundary.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | In `runtime\execution_policy.go`, hard-rename the production policy domain to `ToolPolicy`. Rename `ExecutionAccess`, `CommandExecutionPolicy`, `HTTPExecutionPolicy`, `ExecutionPolicy`, `ResolveExecutionPolicyForWorkspace(...)`, `ResolveExecutionPolicyForKernel(...)`, `ExecutionPolicyOf(...)`, and `MergeExecutionPolicyPermissions(...)` to the `Tool*` equivalents. If needed, move the implementation into `runtime\tool_policy.go` and delete the old exported names instead of leaving aliases. |  |  |
| TASK-002 | Create `internal\runtimepolicy\apply.go`, `internal\runtimepolicy\compile.go`, and `internal\runtimepolicy\session_sync.go`. Implement `Apply(k *kernel.Kernel, policy runtime.ToolPolicy) error`, internal rule compilation, internal tool-context permission merging, canonical state replacement, summary derivation, and stable replace-based hook/plugin registration so repeated apply overwrites prior state and does not append stale rules or duplicate session hooks. |  |  |
| TASK-003 | Update `internal\runtimeassembly\assembly.go` and `internal\runtimeassembly\assembly_test.go` so default restricted-confirm policy installation resolves `runtime.ToolPolicy` and applies it through `internal/runtimepolicy.Apply(...)` rather than `SetExecutionPolicy(...)` plus ad hoc raw-rule installation. |  |  |

### Implementation Phase 2

- **GOAL-002**: Converge harness and product approval composition onto the structured public tool-policy surface.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-004 | In `harness\features.go`, delete `ExecutionPolicy(rules ...builtins.PolicyRule)` and add `ToolPolicy(policy runtime.ToolPolicy) Feature` with a dedicated metadata key. The new feature must call only `internal/runtimepolicy.Apply(...)`. Update `harness\harness_test.go` to replace `TestFeature_ExecutionPolicy` with `TestFeature_ToolPolicy` coverage that asserts the structured apply path is used and invalid zero/empty policy inputs fail explicitly. |  |  |
| TASK-005 | Refactor `appkit\product\approval.go` and `appkit\product\approval_test.go` so approval-mode helpers resolve or mutate `runtime.ToolPolicy` instead of returning `[]builtins.PolicyRule`. `ApplyApprovalModeWithTrust(...)` and `ApplyResolvedProfile(...)` must call the shared internal apply boundary. Any evaluator retained for tests must consume internally compiled rules, not remain a canonical product API. |  |  |
| TASK-006 | Refactor `appkit\deep_agent_packs.go` so the restricted post-runtime pack stops building raw `builtins.PolicyRule` bundles. Replace the current `runtime.ExecutionPolicyRules(...)` + `harness.ExecutionPolicy(...)` flow with a structured `runtime.ToolPolicy` overlay that preserves existing restricted defaults through canonical policy fields such as protected path prefixes, approval-required classes, denied classes, and write-governance fields, then install it through `harness.ToolPolicy(...)`. |  |  |

### Implementation Phase 3

- **GOAL-003**: Move runtime/profile/posture state and planner routing onto the exact tool-policy metadata contract.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-007 | In `kernel\session\profile_metadata.go` and any new supporting file such as `kernel\session\tool_policy.go`, add `MetadataToolPolicy`, `MetadataToolPolicySummary`, and the exact `session.ToolPolicySummary` type described in the approved spec. In `runtime\profile.go` and `runtime\profile_test.go`, replace `ResolvedProfile.ExecutionPolicy` / `SessionPosture.ExecutionPolicy` with `ToolPolicy`, write the versioned serialized full-policy payload plus summary into session metadata, and restore posture from that new contract. Delete production use of `session.MetadataExecutionPolicy`. |  |  |
| TASK-008 | Update runtime posture consumers in `contrib\tui\app_runtime_posture.go`, `contrib\tui\app_kernel_init.go`, `contrib\tui\app.go`, `contrib\tui\app_test.go`, and `apps\mosscode\commands_exec.go` so they use `runtime.ToolPolicy`, the renamed posture fields, and the new session metadata contract. Remove remaining production references to the old execution-policy metadata shape. |  |  |
| TASK-009 | Refactor `kernel\loop\turn_plan.go` and `kernel\loop\turn_plan_test.go` so `buildToolRoute(...)` reads `session.ToolPolicySummary` and derives `ToolRouteStatus` and reason codes from summary plus `tool.ToolSpec` metadata. Delete `shouldRequireApprovalForRestricted(...)`, `shouldRequireApprovalForTurn(...)`, and any heuristic-only reason codes that exist solely because of the old trust/approval inference path. Preserve only planner-owned presentation gates such as lightweight-chat and planning/research visibility constraints. |  |  |

### Implementation Phase 4

- **GOAL-004**: Align direct runtime enforcement with the compiled policy contract, remove stale APIs, and validate the repository end to end.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-010 | Refactor `runtime\builtin_tools_exec.go`, related runtime policy helpers, and `runtime\builtintools_test.go` so command and HTTP enforcement consume canonical `runtime.ToolPolicy` state plus internal permission merges, and stay semantically aligned with the compiled hook bundle emitted by `internal/runtimepolicy`. |  |  |
| TASK-011 | Run search-based cleanup across `runtime`, `harness`, `appkit`, `internal\runtimeassembly`, `contrib\tui`, `apps\mosscode`, and tests to delete old production names and metadata keys, including `ExecutionPolicy`, `WithExecutionPolicy`, `SetExecutionPolicy`, `ExecutionPolicyOf`, `ResolveExecutionPolicyForWorkspace`, `ResolveExecutionPolicyForKernel`, `ExecutionPolicyRules`, `ExecutionPolicyForToolContext`, and `session.MetadataExecutionPolicy`. Update any active current-facing docs that still describe the old canonical surface; do not rewrite archived historical specs or plans. |  |  |
| TASK-012 | Run `go test ./runtime ./harness ./appkit/... ./internal/runtimeassembly ./kernel/loop ./kernel/session`, then `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`. Fix every regression and then update this plan file with completion marks and dates. |  |  |

## 3. Alternatives

- **ALT-001**: Keep `ExecutionPolicy` as the canonical name and only remove the raw harness feature; rejected because the concept would remain too narrow for general tool governance.
- **ALT-002**: Move the canonical policy engine into `kernel`; rejected because it would reverse the current P0/P1 owner direction and push app/runtime semantics into the kernel.
- **ALT-003**: Keep raw `builtins.PolicyRule` helpers as the public escape hatch for appkit/deep-agent defaults; rejected because it would preserve a second canonical surface and allow drift between planner, runtime state, and enforcement hooks.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-14-execution-policy-plane-convergence-design.md` is the approved source of truth for this plan.
- **DEP-002**: `runtime\profile.go` and `runtime\profile_test.go` remain the owner of profile resolution and posture reconstruction logic that must migrate to `ToolPolicy`.
- **DEP-003**: `internal\runtimeassembly\assembly.go` remains the runtime setup entrypoint that installs the default policy for assembled kernels.
- **DEP-004**: `kernel\session\profile_metadata.go` and `kernel\loop\turn_plan.go` are the lower-layer consumers that need the new metadata summary contract.
- **DEP-005**: `appkit\product\approval.go`, `appkit\deep_agent_packs.go`, `contrib\tui\app_runtime_posture.go`, and `apps\mosscode\commands_exec.go` are the primary non-runtime call sites that currently depend on the old execution-policy concept.
- **DEP-006**: `runtime\builtin_tools_exec.go` and kernel policy hooks remain the two enforcement surfaces that must stay semantically aligned after the migration.

## 5. Files

- **FILE-001**: `docs\superpowers\specs\2026-04-14-execution-policy-plane-convergence-design.md` — approved design specification for this migration.
- **FILE-002**: `plan\architecture-execution-policy-plane-convergence-1.md` — implementation plan for this work.
- **FILE-003**: `runtime\execution_policy.go` or its renamed replacement `runtime\tool_policy.go` — canonical policy model and resolution entrypoints.
- **FILE-004**: `runtime\profile.go` — resolved profile, session posture, metadata write/restore, and policy override application.
- **FILE-005**: `runtime\profile_test.go` — profile, metadata, and compiled-policy coverage that must move to `ToolPolicy`.
- **FILE-006**: `runtime\builtin_tools_exec.go` — direct runtime command/HTTP enforcement against canonical policy.
- **FILE-007**: `runtime\builtintools_test.go` — runtime policy enforcement alignment coverage.
- **FILE-008**: `internal\runtimepolicy\apply.go` — canonical internal apply boundary.
- **FILE-009**: `internal\runtimepolicy\compile.go` — compiled hook bundle generation from canonical policy.
- **FILE-010**: `internal\runtimepolicy\session_sync.go` — session metadata synchronization for full payload and summary.
- **FILE-011**: `internal\runtimepolicy\apply_test.go` — replace-based apply semantics and summary synchronization coverage.
- **FILE-012**: `internal\runtimeassembly\assembly.go` — default policy installation during runtime setup.
- **FILE-013**: `internal\runtimeassembly\assembly_test.go` — default policy assembly tests after the rename.
- **FILE-014**: `harness\features.go` — delete raw rule feature and add canonical `ToolPolicy(...)`.
- **FILE-015**: `harness\harness_test.go` — structured policy feature coverage.
- **FILE-016**: `appkit\product\approval.go` — approval-mode resolution/application and project approval amendment flow.
- **FILE-017**: `appkit\product\approval_test.go` — structured product approval behavior coverage.
- **FILE-018**: `appkit\deep_agent_packs.go` — deep-agent default policy composition.
- **FILE-019**: `kernel\session\profile_metadata.go` and any new `kernel\session\tool_policy.go` — exact metadata keys, summary type, and read helpers.
- **FILE-020**: `kernel\loop\turn_plan.go` — canonical planner routing derived from policy summary plus tool metadata.
- **FILE-021**: `kernel\loop\turn_plan_test.go` — planner route/reason-code coverage for the new summary-based logic.
- **FILE-022**: `contrib\tui\app_runtime_posture.go`, `contrib\tui\app_kernel_init.go`, `contrib\tui\app.go`, and `contrib\tui\app_test.go` — TUI posture and runtime rebuild migration to `ToolPolicy`.
- **FILE-023**: `apps\mosscode\commands_exec.go` — mosscode command posture/reporting migration to `ToolPolicy`.

## 6. Testing

- **TEST-001**: Runtime model tests verify the `ExecutionPolicy` to `ToolPolicy` rename is complete, profile resolution still yields the correct command/HTTP defaults, and no old exported production references remain.
- **TEST-002**: `internal/runtimepolicy` tests verify repeated `Apply(...)` replaces prior canonical state, replaces compiled rule bundles, and replaces session sync hooks instead of stacking duplicates.
- **TEST-003**: Harness tests verify `harness.ToolPolicy(...)` is the only canonical public policy feature and invalid canonical policy input fails explicitly.
- **TEST-004**: Product approval tests verify `ApplyApprovalModeWithTrust(...)`, `ApplyResolvedProfile(...)`, and deep-agent restricted defaults all converge on the same canonical tool-policy semantics.
- **TEST-005**: Session/profile tests verify the exact metadata contract writes and restores both `MetadataToolPolicy` and `MetadataToolPolicySummary`, including safe degradation for missing or malformed summary payloads.
- **TEST-006**: Planner tests verify `kernel/loop` route status and reason codes are derived from `session.ToolPolicySummary` plus tool metadata, not from trust/approval heuristics.
- **TEST-007**: Runtime builtin tool tests verify direct command/HTTP enforcement stays aligned with the compiled hook bundle produced by `internal/runtimepolicy`.
- **TEST-008**: Repository-wide validation runs `go test ./runtime ./harness ./appkit/... ./internal/runtimeassembly ./kernel/loop ./kernel/session`, `go test ./...`, `go build ./...`, and `Push-Location contrib\tui; go test .; Pop-Location`.

## 7. Risks & Assumptions

- **RISK-001**: The rename from `ExecutionPolicy` to `ToolPolicy` touches runtime, TUI, appkit, apps, and tests; partial migration will leave the repo uncompilable.
- **RISK-002**: Translating deep-agent restricted defaults away from raw named rules may expose gaps in current tool metadata; any such gaps must be fixed explicitly rather than retaining raw-rule shortcuts.
- **RISK-003**: If `internal/runtimepolicy.Apply(...)` is not truly replace-based, policy changes will accumulate duplicate hooks or stale compiled rules and recreate the drift this phase is meant to remove.
- **RISK-004**: Planner summary drift is possible if runtime writes the full canonical payload and the derived summary through different code paths; one shared synchronization path must own both writes.
- **ASSUMPTION-001**: Existing tool metadata (`Effects`, `ApprovalClass`, `PlannerVisibility`, `ResourceScope`) is rich enough to express planner routing once canonical policy summary is available.
- **ASSUMPTION-002**: `kernel.WithPolicy(...)` can remain for low-level plugin use without being part of the canonical harness/appkit policy assembly path.
- **ASSUMPTION-003**: No external compatibility requirement exists for deleted execution-policy symbols or session metadata keys.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-14-execution-policy-plane-convergence-design.md`
- `docs/superpowers/specs/2026-04-13-execution-substrate-owner-convergence-design.md`
- `plan/architecture-execution-substrate-owner-convergence-1.md`
