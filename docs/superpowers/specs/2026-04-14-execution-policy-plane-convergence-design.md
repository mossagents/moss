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

- **`runtime`** — canonical structured tool-policy model and profile/posture resolution only
- **`harness`** — canonical public policy assembly surface
- **`internal/runtimepolicy`** — the only policy apply/compiler path
- **`kernel/session`** — policy summary metadata contract consumed by turn planning
- **`kernel/loop`** — deterministic route derivation from policy summary + tool metadata
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
- `ResolveExecutionPolicyForWorkspace(...)` -> `ResolveToolPolicyForWorkspace(...)`

`runtime/profile.go`, `runtime/profile_test.go`, and any posture/profile types that currently expose `ExecutionPolicy` should be renamed accordingly:

- `ResolvedProfile.ExecutionPolicy` -> `ResolvedProfile.ToolPolicy`
- `SessionPosture.ExecutionPolicy` -> `SessionPosture.ToolPolicy`

Runtime should **not** retain a second public installation path such as `WithToolPolicy(...)` or `SetToolPolicy(...)`. Public callers should not be able to bypass the canonical harness surface when assembling policy behavior.

Runtime should also **not** retain public kernel-aware lookup/resolution APIs such as:

- `ToolPolicyOf(...)`
- `ResolveToolPolicyForKernel(...)`
- `ToolPolicyRules(...)`
- `ToolPolicyForToolContext(...)`

Kernel-scoped state access remains an internal implementation detail behind the one canonical apply path.

The current exported rule-compilation and tool-context merge helpers move behind the internal apply/compiler boundary:

- rule compilation becomes an `internal/runtimepolicy` responsibility
- tool-context policy merging becomes an internal runtime/runtimepolicy responsibility, not a public API

### 2. Harness becomes the only public policy assembly surface

Remove:

- `harness.ExecutionPolicy(rules ...builtins.PolicyRule)`

Introduce:

- `harness.ToolPolicy(policy runtime.ToolPolicy) Feature`

Responsibilities:

- call the one shared internal apply boundary
- avoid exposing any raw-rule or alternate public install path

This makes structured policy the only canonical public assembly surface. Raw hook rules stop being public execution/tool-policy inputs.

### 3. Product approval becomes a structured-policy mutator, not a rule author

`appkit/product/approval.go` should stop returning or appending raw `builtins.PolicyRule` slices for the canonical execution/tool-policy path.

Instead:

- approval-mode helpers resolve or mutate canonical `runtime.ToolPolicy`
- project approval amendments persist structured policy deltas/config, not raw hook bundles
- `ApplyApprovalModeWithTrust(...)` and `ApplyResolvedProfile(...)` call the same internal apply boundary used by `harness.ToolPolicy(...)`

If low-level raw rule helpers remain at all, they must become internal implementation details behind the canonical structured path rather than product-visible policy logic.

### 4. Introduce an internal policy compiler/adapter

Add:

- `internal/runtimepolicy`

Responsibilities:

- provide **the only** `Apply(...)` / install path for canonical policy
- write canonical policy state into kernel-scoped runtime state
- compile canonical `runtime.ToolPolicy` into the hook-enforcement rules consumed by kernel policy hooks
- derive a **summary** of canonical policy semantics suitable for turn planning
- synchronize that summary onto sessions through one deterministic path

This package is an adapter/apply boundary only. It does **not** own the canonical model or the public API, and it does **not** own final turn-plan reason-code generation.

`internal/runtimepolicy.Apply(...)` must be **idempotent and replace-based**, not append-based:

- repeated apply on the same kernel replaces prior canonical policy state
- repeated apply replaces the previously compiled rule bundle instead of appending another copy
- repeated apply replaces or updates the previously installed session-summary synchronization hook instead of stacking duplicate hooks

The implementation should use stable service/plugin/hook identities so policy changes do not accumulate stale enforcement or stale metadata synchronization behavior.

### 5. Add a lower-layer session metadata summary for turn planning

`kernel/loop/turn_plan.go` cannot read kernel runtime state because it only has `Session + ToolRegistry`, not `Kernel`.

Therefore, this migration should add a lower-layer session metadata contract in `kernel/session`, for example:

- `MetadataToolPolicy = "tool_policy"`
- `MetadataToolPolicySummary = "tool_policy_summary"`

And a summary type such as:

- `session.ToolPolicySummary`

The contract should be exact, not illustrative. `session.ToolPolicySummary` should include:

- `Version int`
- `Trust string`
- `ApprovalMode string`
- `CommandAccess string`
- `HTTPAccess string`
- `WorkspaceWriteAccess string`
- `MemoryWriteAccess string`
- `GraphMutationAccess string`
- `ProtectedPathPrefixes []string`
- `ApprovalRequiredClasses []string`
- `DeniedClasses []string`

The full canonical `runtime.ToolPolicy` remains stored for runtime/profile/posture use, while the summary is stored for kernel-side consumers that must remain runtime-independent.

`MetadataToolPolicy` must also be exact, not illustrative. It stores a **versioned serialized canonical policy payload**, not a live Go value. The contract is:

- key: `MetadataToolPolicy = "tool_policy"`
- payload kind: structured metadata object written by runtime-owned encode/decode helpers
- payload fields:
  - `version int`
  - `policy map[string]any` containing the serialized canonical `runtime.ToolPolicy`

Runtime owns the serialization contract for that full payload. Kernel-side code does not interpret it directly; it exists so runtime/profile/posture code can restore the canonical policy and so summary repair can deterministically re-derive `ToolPolicySummary`.

Update semantics are explicit:

1. `runtime.ApplyResolvedProfileToSessionConfig(...)` writes both `MetadataToolPolicy` and `MetadataToolPolicySummary` for newly created sessions.
2. `internal/runtimepolicy.Apply(...)` installs a session-lifecycle synchronization hook so sessions created through canonical policy assembly also receive the same metadata contract.
3. Existing or restored sessions with missing/older summaries are repaired by deterministic resynchronization from the versioned `MetadataToolPolicy` payload when possible; if that payload is missing, unknown-version, or malformed, turn planning enters the explicit safe-degradation path.

### 6. Turn planning derives from canonical policy summary, not heuristics

`kernel/loop/turn_plan.go` should delete the current heuristic policy branches:

- `shouldRequireApprovalForRestricted(...)`
- `shouldRequireApprovalForTurn(...)`
- reason codes that exist only because of those heuristics

Instead:

- `buildToolRoute(...)` should read `session.ToolPolicySummary`
- route status and reason codes should be derived deterministically in `kernel/loop/turn_plan.go` from:
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
2. `harness.ToolPolicy(...)` and product approval flows both call `internal/runtimepolicy.Apply(...)`
3. `internal/runtimepolicy.Apply(...)` writes canonical kernel/runtime state and compiles:
   - enforcement rules
   - session policy summary
4. session creation or policy application sync writes the summary into session metadata under the exact contract above
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
- `internal/runtimepolicy.Apply(...)` is the one canonical installer and must fail atomically if any derived artifact cannot be produced.
- Re-applying policy to the same kernel/session replaces prior compiled artifacts; duplicate rule bundles or duplicate sync hooks are a correctness bug.
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
- both harness and product approval flows call the same internal `Apply(...)` boundary and produce identical state/summaries for identical policies

### Kernel / turn planning

- `turn_plan` approval-required / hidden / visible decisions are derived from `ToolPolicySummary`
- legacy heuristic helpers are deleted
- reason codes are generated only in `kernel/loop/turn_plan.go`, not in the internal adapter

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
