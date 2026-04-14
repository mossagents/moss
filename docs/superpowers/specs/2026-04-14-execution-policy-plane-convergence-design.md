# Execution Policy Plane Convergence Design

## Problem

After P0 public assembly convergence and P1 execution substrate owner convergence, the next largest remaining split is the execution-policy plane.

Today, policy semantics are still fragmented across three different models:

1. **Runtime keeps a structured execution-policy state model.**
   - `runtime/execution_policy.go`
     - `ExecutionPolicy`
     - `WithExecutionPolicy(...)`
     - `SetExecutionPolicy(...)`
     - `ExecutionPolicyOf(...)`
     - `ResolveExecutionPolicyForWorkspace(...)`
     - `ResolveExecutionPolicyForKernel(...)`
     - `ExecutionPolicyRules(...)`
     - `ExecutionPolicyForToolContext(...)`

2. **Harness and app/product still use raw hook rules as a parallel public policy surface.**
   - `harness/features.go`
     - `ExecutionPolicy(rules ...builtins.PolicyRule)`
   - `kernel/kernel.go`
     - `WithPolicy(rules ...builtins.PolicyRule)`
   - `appkit/product/approval.go`
     - `ApprovalModePolicyRules(...)`
     - `ApprovalModePolicyRulesForTrust(...)`
     - `approvalModePolicyRulesForPolicy(...)`
     - `ApplyApprovalModeWithTrust(...)`
     - `ApplyResolvedProfile(...)`

3. **Turn planning still derives approval/visibility through heuristics instead of canonical policy.**
   - `kernel/loop/turn_plan.go`
     - `shouldRequireApprovalForRestricted(...)`
     - `shouldRequireApprovalForTurn(...)`
     - reason codes such as `restricted_trust` and `approval_class_requires_approval`

This causes multiple architectural inconsistencies:

- the public policy surface is not single-owner
- the structured runtime model and raw hook-rule model can drift
- turn planning can disagree with actual tool enforcement
- the current name `ExecutionPolicy` is too narrow once product approval rules also govern workspace writes, memory writes, graph mutation, protected paths, and approval-class behavior

The user explicitly wants to replace the raw public policy surface with a structured one, and wants `turn_plan` to derive strictly from canonical policy rather than retain heuristic policy semantics.

## Goals

- Make structured policy the only canonical public policy surface.
- Remove raw `builtins.PolicyRule` from the canonical harness/app-product execution-policy path.
- Expand the canonical policy scope from command/http-only execution controls to general tool-governance controls.
- Ensure tool enforcement, turn planning, and session/profile posture all derive from the same canonical structured policy.
- Keep `kernel.WithPolicy(...)` as a low-level generic hook API if needed, but remove it from the canonical execution/tool-policy path.

## Non-Goals

- P3 kernel execution API convergence (`Run` vs `RunAgent`).
- Reworking unrelated tool metadata semantics (`Risk`, `ApprovalClass`, `PlannerVisibility`) outside how canonical policy consumes them.
- Deleting the generic kernel hook/policy infrastructure itself.
- Redesigning the execution substrate ownership model completed in P1.

## User-Approved Design Decisions

- Replace the raw hook-rule public surface with a structured policy surface.
- Make `turn_plan` derive strictly from canonical policy rather than retain heuristic approval logic.
- Expand the structured policy scope to cover general tool governance, not just command/http.
- Choose **harness-owned structured policy with an internal rule adapter** over both kernel-native policy ownership and runtime-owned public policy wrappers.

## Rejected Approaches

### 1. Keep raw rules as the public escape hatch

Rejected because it preserves the split between structured runtime state and raw harness/appkit hook rules.

### 2. Move the entire canonical policy engine into `kernel`

Rejected because it would push application/runtime-specific policy assembly semantics down into the kernel, fighting the P0/P1 ownership direction.

### 3. Keep `runtime.ExecutionPolicy` as-is and only remove `turn_plan` heuristics

Rejected because it would still leave a concept mismatch: the canonical model would still be named and scoped as execution-only while product approval continues to govern broader tool behavior.

## Target Architecture

After this migration, policy ownership is:

- **`runtime`** — canonical structured tool-policy model and profile/posture resolution
- **`harness`** — canonical public policy assembly surface
- **`internal/runtimepolicy`** — compiler/adapter from canonical policy to enforcement + session summary views
- **`kernel/session`** — policy summary metadata contract consumed by turn planning
- **`kernel`** — generic hook substrate only

### 1. Canonical policy model is renamed and expanded

Rename the canonical structured model from `ExecutionPolicy` to **`ToolPolicy`**.

The model expands beyond command/http execution to include:

- trust
- approval mode
- command governance
- HTTP governance
- workspace-write governance
- memory-write governance
- graph-mutation governance
- protected path-prefix governance
- approval-class governance
- command/http rule overrides

This round should also rename the corresponding runtime APIs so the canonical concept stays internally consistent:

- `ExecutionPolicy` -> `ToolPolicy`
- `WithExecutionPolicy(...)` -> `WithToolPolicy(...)`
- `SetExecutionPolicy(...)` -> `SetToolPolicy(...)`
- `ExecutionPolicyOf(...)` -> `ToolPolicyOf(...)`
- `ResolveExecutionPolicyForWorkspace(...)` -> `ResolveToolPolicyForWorkspace(...)`
- `ResolveExecutionPolicyForKernel(...)` -> `ResolveToolPolicyForKernel(...)`

`runtime/profile.go`, `runtime/profile_test.go`, and any posture/profile types that currently expose `ExecutionPolicy` should be renamed accordingly:

- `ResolvedProfile.ExecutionPolicy` -> `ResolvedProfile.ToolPolicy`
- `SessionPosture.ExecutionPolicy` -> `SessionPosture.ToolPolicy`

### 2. Harness becomes the only public policy assembly surface

Remove:

- `harness.ExecutionPolicy(rules ...builtins.PolicyRule)`

Introduce:

- `harness.ToolPolicy(policy runtime.ToolPolicy) Feature`

Responsibilities:

- install the canonical structured policy into kernel-scoped runtime state
- install the internal enforcement adapter compiled from that policy
- install the session-lifecycle synchronization needed for turn planning to observe the same policy semantics

This makes structured policy the only canonical public assembly surface. Raw hook rules stop being public execution/tool-policy inputs.

### 3. Product approval becomes a structured-policy mutator, not a rule author

`appkit/product/approval.go` should stop returning or appending raw `builtins.PolicyRule` slices for the canonical execution/tool-policy path.

Instead:

- approval-mode helpers resolve or mutate canonical `runtime.ToolPolicy`
- project approval amendments persist structured policy deltas/config, not raw hook bundles
- `ApplyApprovalModeWithTrust(...)` and `ApplyResolvedProfile(...)` install the canonical structured policy path, not `k.WithPolicy(...)`

If low-level raw rule helpers remain at all, they must become internal implementation details behind the canonical structured path rather than product-visible policy logic.

### 4. Introduce an internal policy compiler/adapter

Add:

- `internal/runtimepolicy`

Responsibilities:

- compile canonical `runtime.ToolPolicy` into the hook-enforcement rules consumed by kernel policy hooks
- derive a **summary** of canonical policy semantics suitable for turn planning
- provide deterministic mapping from canonical policy to route reason codes and approval/visibility outcomes

This package is an adapter only. It does **not** own the canonical model or the public API.

### 5. Add a lower-layer session metadata summary for turn planning

`kernel/loop/turn_plan.go` cannot read kernel runtime state because it only has `Session + ToolRegistry`, not `Kernel`.

Therefore, this migration should add a lower-layer session metadata contract in `kernel/session`, for example:

- `MetadataToolPolicy`
- `MetadataToolPolicySummary`

And a summary type such as:

- `session.ToolPolicySummary`

The full canonical `runtime.ToolPolicy` remains stored for runtime/profile/posture use, while the summary is stored for kernel-side consumers that must remain runtime-independent.

The summary should capture the planner-relevant policy semantics, including at minimum:

- trust
- approval mode
- per-domain default access (command, HTTP, workspace-write, memory-write, graph mutation)
- protected path-prefix requirements
- approval-class behavior relevant to tool routing

### 6. Turn planning derives from canonical policy summary, not heuristics

`kernel/loop/turn_plan.go` should delete the current heuristic policy branches:

- `shouldRequireApprovalForRestricted(...)`
- `shouldRequireApprovalForTurn(...)`
- reason codes that exist only because of those heuristics

Instead:

- `buildToolRoute(...)` should read `session.ToolPolicySummary`
- route status should be derived deterministically from:
  - canonical policy summary
  - tool spec metadata (`Effects`, `ApprovalClass`, `PlannerVisibility`, `Capabilities`, `ResourceScope`, etc.)
  - task/profile mode rules that are still intentionally planner-owned (for example planning-mode visibility)

The important rule is:

- **no approval/visibility semantics may be invented outside canonical policy or planner-owned non-policy presentation rules**

### 7. Kernel generic policy hook API becomes non-canonical

`kernel.WithPolicy(rules ...builtins.PolicyRule)` may remain as a low-level generic hook/plugin utility.

However:

- harness features
- product approval flows
- runtime profile application
- turn planning

must no longer treat it as the canonical execution/tool-policy surface.

## Data Flow

### Canonical policy resolution flow

1. profile/trust/approval/project config resolve into canonical `runtime.ToolPolicy`
2. harness/product installation applies that canonical policy to kernel/runtime state
3. `internal/runtimepolicy` compiles:
   - enforcement rules
   - session policy summary
4. session creation or policy application sync writes the summary into session metadata
5. builtin tools and turn planning consume the same canonical policy semantics through their dedicated derived views

### Enforcement flow

1. command/http builtin tools read canonical structured policy state directly
2. generic tool approval/denial hooks read the compiled rule bundle derived from the same canonical policy
3. no product layer appends an extra raw rule surface after the fact

### Turn planning flow

1. `buildToolRoute(...)` reads `session.ToolPolicySummary`
2. route status is computed from policy summary + tool metadata
3. route reason codes describe canonical policy outcomes rather than heuristic guesses

## Error Handling

- Invalid policy configuration or invalid persisted amendment is a resolve/merge-time error.
- `harness.ToolPolicy(...)` must reject zero/invalid canonical policies at install time.
- Policy compilation failure is an install-time error; do not partially apply structured state without its derived enforcement/summary views.
- Missing `ToolPolicySummary` must not fall back to the old heuristics.
  - It should produce a deterministic safe degradation path with explicit reason codes such as `policy_summary_missing`.
  - Non-read-only high-side-effect tools should degrade to approval-required or hidden according to fixed safe defaults.
- The compiled rule bundle and the session summary are derived artifacts only; they must never be manually edited by separate code paths.

## Testing Strategy

### Runtime / profile

- renaming from `ExecutionPolicy` to `ToolPolicy` is complete and no old production references remain
- profile resolution, trust/approval mode resolution, and project amendments produce canonical structured `ToolPolicy`
- `ResolvedProfile` and `SessionPosture` remain internally consistent with the renamed policy model

### Harness / product

- `harness.ToolPolicy(...)` becomes the only canonical public policy feature
- `appkit/product/approval.go` no longer constructs extra raw hook-rule bundles in the canonical path
- policy installation synchronizes both kernel state and session summary metadata

### Kernel / turn planning

- `turn_plan` approval-required / hidden / visible decisions are derived from `ToolPolicySummary`
- legacy heuristic helpers are deleted
- reason codes reflect canonical policy-derived outcomes

### Runtime enforcement

- command/http builtin tools still enforce the canonical structured policy directly
- generic policy hooks enforce the rule bundle derived from the same canonical policy
- there is no drift between structured state, compiled rules, and planner summary for equivalent scenarios

## Expected Outcome

After this migration:

- raw hook rules are no longer the canonical public policy surface
- command/http execution control and broader tool governance share one structured canonical model
- product approval, runtime enforcement, and turn planning all derive from the same policy semantics
- `kernel.WithPolicy(...)` remains only a low-level hook utility rather than a competing execution/tool-policy API
