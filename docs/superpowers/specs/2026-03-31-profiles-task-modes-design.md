# Profiles and Task Modes Design

## Problem

`moss` now has several production-relevant control knobs, but they are still exposed as separate low-level switches instead of one coherent product concept:

- trust is selected independently through `trusted` vs `restricted`
- approval posture is selected independently through `read-only`, `confirm`, and `full-auto`
- execution policy derives from trust + approval mode, but has no higher-level task intent
- `mosscode` CLI and TUI expose raw flags and slash commands, not named operating modes
- `SessionConfig.Mode` already carries command/session shape such as `interactive` and `oneshot`, so it is **not** the right place to overload product profiles

That leaves a real Codex-parity gap:

- users cannot say "start in research mode" or "switch this session to coding mode"
- teams cannot package shared task defaults as a named preset
- approval mode and execution policy remain understandable only as separate internals
- later command-rule and MCP-governance work has no clean product-level container to attach to

Today `mosscode` users effectively assemble behavior by hand from `--trust`, `--approval`, and whatever runtime defaults happen to follow. That is functional, but not production-ready as a standalone product surface.

## Goal

Deliver a first production-safe **profile / task mode** system for `moss` / `mosscode`.

## Goals

- introduce a named profile concept separate from existing session command modes
- let a profile resolve trust, approval posture, and execution-policy defaults together
- support layered resolution across built-in presets, global config, project config, and explicit CLI/TUI overrides
- surface profiles in `mosscode` CLI and TUI as the primary user-facing operating mode
- keep backward compatibility for existing `--trust` and `--approval` users
- create the right foundation for later command-rule and MCP-governance work

## Non-goals

- no org-wide policy-pack distribution in this subproject
- no full command-rule DSL or regex-rule config files in this subproject
- no per-profile MCP installation lifecycle or marketplace UX in this subproject
- no attempt to replace existing session `Mode` semantics such as `interactive` / `oneshot`
- no prompt-system rewrite beyond passing profile/task-mode context into existing builders

## Candidate Approaches

### A. Alias-only presets

Add a few hardcoded names such as `coding`, `research`, and `readonly` that simply map to trust + approval mode.

Pros:

- smallest code delta
- easy to explain and ship

Cons:

- too weak to justify a real profile system
- no place for execution-policy defaults, task-mode metadata, or later rule/MCP bindings
- would create a second migration later when richer profiles are needed

### B. Layered profile resolution with built-in presets (recommended)

Add a first-class profile model with named presets, layered config resolution, and a resolved runtime view that drives trust, approval, execution policy, and task-mode metadata.

Pros:

- creates a durable product abstraction instead of more flags
- keeps current trust/approval behavior backward-compatible
- lets later command rules and MCP governance attach to profiles instead of inventing another container
- can be delivered incrementally without changing core loop behavior

Cons:

- touches config, runtime, product, CLI, and TUI surfaces
- requires careful override semantics to avoid confusing users

### C. Full policy-pack system now

Introduce profiles together with file-based command rules, MCP allowlists, and org-level managed policy distribution in one pass.

Pros:

- closest to a long-term enterprise model

Cons:

- too large for the next bounded P1 subproject
- mixes foundational UX work with later governance/distribution work
- increases rollout and debugging risk

## Recommendation

Choose **Approach B**.

The next bounded subproject should add a first-class, layered profile model that turns existing trust + approval + execution-policy primitives into one user-facing operating mode. This is the smallest design that still creates a durable abstraction for later command rules, MCP governance, and product UX.

## Scope of the first slice

### In-scope behavior

1. add a first-class `Profile` concept distinct from session `Mode`
2. ship a small built-in preset set:
   - `default`
   - `coding`
   - `research`
   - `planning`
   - `readonly`
3. resolve profiles through the following precedence:
   - built-in presets
   - global config overrides
   - project config overrides
   - explicit CLI / TUI overrides
4. let a resolved profile drive:
   - trust
   - approval mode
   - execution-policy defaults
   - task-mode metadata for prompts and UX
5. add CLI and TUI entrypoints to inspect/select/switch profile
6. persist both the requested profile and the resolved effective posture on session creation so history and replay can show the real operating mode

### In-scope entrypoints

1. `config`
2. `kernel/session`
3. `appkit/runtime`
4. `appkit/product`
5. `appkit` builder / extension assembly
6. `apps/mosscode`
7. `userio/tui`

### Out-of-scope behavior

- project-defined custom rule languages
- per-profile MCP allow / deny enforcement
- remote profile registries
- org-admin distribution workflows
- hidden background profile mutation without a user-visible switch

## Design

## 1. First-class profile model

The system should add a dedicated profile concept instead of reusing `SessionConfig.Mode`.

`SessionConfig.Mode` already records session shape like `interactive`, `oneshot`, `review`, and command submodes. Reusing it for profiles would blur two different concepts:

- **session shape**: how this session is being used by the product
- **profile / task mode**: what operational defaults and user intent should govern the session

Recommended new fields:

- `SessionConfig.Profile string`
- optional session metadata entry for resolved task mode, such as `metadata["task_mode"]`

This keeps history and replay typed and readable without destabilizing existing `Mode` consumers.

## 2. Layered resolution model

Profiles should resolve through a simple, deterministic precedence chain:

1. built-in preset definition
2. global config profile override
3. project config profile override
4. explicit runtime overrides from CLI/TUI flags or commands

This gives `mosscode` the same kind of layered product behavior users expect from Codex-like tooling without forcing every environment to redefine all profile details.

### Proposed config shape

Recommended additions to config:

```yaml
default_profile: default

profiles:
  default:
    label: Default
    task_mode: coding
    trust: trusted
    approval: confirm

  coding:
    label: Coding
    task_mode: coding
    trust: trusted
    approval: full-auto
    session:
      max_steps: 200
    execution:
      command_access: allow
      http_access: allow
      command_timeout: 30s

  research:
    task_mode: research
    trust: trusted
    approval: confirm
    execution:
      command_access: require-approval
      http_access: allow

  readonly:
    task_mode: readonly
    trust: restricted
    approval: read-only
```

Recommended config additions:

- `Config.DefaultProfile string`
- `Config.Profiles map[string]ProfileConfig`

Recommended profile config shape:

- `Label string`
- `TaskMode string`
- `Trust string`
- `Approval string`
- `Session SessionProfileConfig`
- `Execution ExecutionProfileConfig`

Keep the first slice intentionally small: do **not** add command-rule lists or MCP allowlists yet. The profile config should be the place those later attach to, but not the place we prematurely implement them.

Overlay semantics must be explicit:

- `profiles.<name>` is a **deep per-field overlay** on top of the built-in preset of the same name
- omitted fields inherit from the lower-precedence layer
- zero-value replacement must be represented explicitly where needed rather than inferred from omission
- profile resolution must use a dedicated resolver / merge path
- `config.MergeConfigs()` must **not** be reused for profile layering because it is skills-specific today

## 3. Built-in presets

The first slice should ship with a small opinionated preset set so the feature is usable without writing config.

### `default`

- trust: `trusted`
- approval: `confirm`
- execution: current default execution posture
- task mode: `coding`

This preset exists to preserve today's no-flag `mosscode` behavior.

### `coding`

- trust: `trusted`
- approval: `full-auto`
- execution: command + HTTP allowed with normal local limits
- task mode: `coding`

### `research`

- trust: `trusted`
- approval: `confirm`
- execution: HTTP allowed, commands require approval
- task mode: `research`

### `planning`

- trust: `trusted`
- approval: `confirm`
- execution: command and HTTP require approval
- task mode: `planning`

### `readonly`

- trust: `restricted`
- approval: `read-only`
- execution: command + HTTP denied
- task mode: `readonly`

These presets deliberately map to already-supported runtime semantics. The first slice stays backward-compatible only if the implicit fallback remains today's `trusted + confirm` posture through the built-in `default` profile.

## 4. Resolved runtime profile

Runtime should work with a fully resolved, concrete profile object rather than scattered strings.

Recommended runtime type:

- `ResolvedProfile`
  - `RequestedName`
  - `Name`
  - `Label`
  - `TaskMode`
  - `Trust`
  - `ApprovalMode`
  - `ExecutionPolicy`
  - `SessionDefaults`

Resolution contract:

1. normalize requested profile name
2. load built-in preset
3. overlay global config override if present
4. overlay project config override if project config is trusted and allowed
5. overlay explicit runtime overrides such as `--trust` and `--approval`
6. derive concrete `ExecutionPolicy` from the resolved profile

This is where current trust and approval logic become unified instead of merely adjacent.

## 5. Backward-compatibility contract

Existing users must not be broken.

Recommended behavior:

- if no profile is requested, resolve an implicit profile from:
  - explicit `--profile`, else config `default_profile`, else built-in `default`
- explicit `--trust` continues to override the resolved profile trust
- explicit `--approval` continues to override the resolved profile approval posture
- if profile resolution fails, return a user-visible error instead of silently falling back to a different profile

This means existing automation that sets `--trust` and `--approval` keeps working, while new users get a higher-level entrypoint.

## 6. CLI and TUI UX

### CLI

`apps/mosscode` should add:

- `--profile <name>`
- `config profiles`
- `config profile show [name]`

Usage/help should describe profile as the primary operating mode, with `--trust` and `--approval` documented as advanced overrides.

### TUI

The first slice should add:

- profile visible in the header/status metadata
- `/profile` to show current profile
- `/profile list`
- `/profile set <name>`

Changing profile inside the TUI should:

1. validate the requested profile
2. reject switching while a run is active
3. autosave or checkpoint the current session before any rebuild
4. rebuild the runtime and switch into a fresh session under the new effective posture, unless the user explicitly chose a fork-style action
5. update visible header metadata and show where the previous session was preserved

The first slice should **not** do in-place live profile mutation on the active kernel.

That is unsafe today because:

- trust changes affect project-asset loading at boot time
- approval mode installation currently appends policy rules rather than replacing them

So `/profile set` should use **rebuild-and-switch semantics**, not hot swapping.

## 7. Execution policy integration

Profiles should become the preferred input to execution-policy resolution.

Recommended runtime changes:

- add a runtime profile resolver in `appkit/runtime`
- let `ResolveExecutionPolicyForWorkspace(...)` continue to exist as a low-level helper
- add a higher-level `ResolveProfile(...)` / `ResolveExecutionPolicyForProfile(...)`

This preserves current low-level APIs while making product entrypoints consume a richer abstraction.

The first slice should not duplicate policy logic. It should reuse existing policy derivation and treat the profile as the input container that chooses the posture.

## 8. Prompt and task-mode integration

Profiles should also provide product-intent context, not just sandbox posture.

Recommended first-slice behavior:

- pass resolved `task_mode` into existing prompt-build context
- store resolved profile/task-mode into session metadata
- expose it in session summaries, history, and TUI header metadata

The prompt integration should stay additive:

- no rewrite of the system prompt framework
- no per-profile prompt file loading in this slice
- only pass extra structured context so existing prompt builders can use it when helpful

## 9. Effective posture persistence

Persisting only the requested profile name is not enough, because explicit overrides may change the actual runtime posture.

Recommended session persistence:

- `SessionConfig.Profile` stores the requested profile name
- `SessionConfig.Metadata` stores only the resolved effective posture snapshot:
  - `effective_trust`
  - `effective_approval`
  - `task_mode`
  - `execution_policy`

History, replay, and TUI session summaries should render the effective posture first, with the requested profile name shown as provenance when useful.

## 10. Trust-aware project config interaction

Project profiles must respect the existing trusted-project model.

That means:

- global config profiles may always load
- project config profiles may only affect runtime when project assets are allowed for the workspace trust level
- in restricted mode, project profile config must not even be read/parsed
- if a requested profile or `default_profile` exists only in project config and the workspace is restricted, resolution should fail explicitly

This keeps the Phase 1 trust model intact instead of quietly bypassing it through profile config.

## 11. Resume, restore, fork, and replay semantics

The first slice must define how historical sessions interact with mutable profile definitions.

Recommended first-slice rule:

- session restore / fork / replay must prefer the persisted **effective posture snapshot** over re-resolving a mutable profile name from current config

Operationally, that means the product should either:

1. rebuild runtime directly from the persisted effective posture snapshot, or
2. fail fast if the current runtime cannot honor the recorded posture

The first slice should not silently restore an old session into a runtime built from unrelated current profile defaults.

Legacy sessions created before the profile feature need an explicit fallback contract:

- if profile/effective posture metadata is missing, restore should infer posture from persisted legacy session fields such as `TrustLevel`
- if no reliable historical approval posture can be recovered, restore should proceed under the current runtime with a visible warning that the session predates profile persistence and posture was inferred
- this warning should appear in TUI/session-restore output rather than remaining implicit

## 12. Data model and persistence

Recommended additions:

- `config.Config`
  - `DefaultProfile`
  - `Profiles`
- `session.SessionConfig`
  - `Profile`

Recommended session metadata additions:

- `effective_trust`
- `effective_approval`
- `task_mode`
- `execution_policy`

The first slice should update history/index surfaces so profile is queryable where session summaries are already normalized. That keeps later UX such as session lists and doctor/status output able to explain how a run was configured.

Recommended summary/index additions:

- `SessionSummary.Profile`
- `SessionSummary.EffectiveTrust`
- `SessionSummary.EffectiveApproval`
- `SessionSummary.TaskMode`

Session list/history/index surfaces should populate these fields without requiring every caller to load full session blobs.

## 13. Error handling

The first slice should fail loudly for configuration mistakes.

Examples:

- unknown profile name
- invalid trust value inside profile config
- invalid approval value inside profile config
- contradictory execution settings that cannot be normalized

Avoid silent fallback to a different preset. Silent fallback would make production troubleshooting much harder.

## 14. Rollout posture

This subproject can ship as a backward-compatible additive feature.

Recommended rollout posture:

- built-in default remains effectively today's `trusted + confirm` workflow, surfaced as the named `default` profile
- old flags stay supported
- new profile UX is introduced incrementally
- add an operational feature flag / env gate that disables profile resolution and profile UX, reverting the product to the current trust/approval-only path

Kill-switch behavior must also be explicit for stored profile-backed sessions:

- when the profile feature gate is off, new profile selection UX is disabled
- restore/replay/fork of sessions that already carry persisted effective posture may still proceed by using the recorded effective posture snapshot only
- if the runtime cannot honor that recorded posture while the feature gate is off, restore/replay/fork must fail with a clear operator-facing error instead of silently degrading to current defaults

No migration of persisted sessions is required. Older sessions simply have no `Profile` field.

## 15. Testing

Required test coverage:

1. config load / merge for `default_profile` and `profiles`
2. built-in preset resolution
3. project-over-global precedence
4. explicit CLI override precedence over resolved profile
5. trust-aware blocking of project-only profiles in restricted workspaces
6. runtime installation of approval + execution policy from profile
7. TUI `/profile` command parsing, autosave, and switch blocking behavior
8. session creation persisting `SessionConfig.Profile` plus effective posture metadata
9. compatibility behavior when no profile is specified
10. resume/replay honoring persisted effective posture
11. repeated profile switches do not accumulate stale policy rules
12. session summaries / store serialization include profile posture fields
13. legacy-session restore warning + fallback behavior
14. feature-flag disable path restoring trust/approval-only behavior

## 16. Key files and seams

Primary expected touch points:

- `config/config.go`
- `kernel/session/session.go`
- `kernel/session/store.go`
- `kernel/session/store_file.go`
- `appkit/runtime/execution_policy.go`
- `appkit/runtime/statestore.go`
- `appkit/product/approval.go`
- `appkit/extensions.go`
- `appkit/builder.go`
- `apps/mosscode/main.go`
- `userio/tui/chat.go`
- `userio/tui/app.go`

Likely new files:

- `appkit/runtime/profile.go`
- `appkit/product/profile.go`
- profile-focused tests under `config`, `appkit/runtime`, and `userio/tui`

## Implementation sketch

1. add config/session data model for profiles
2. add built-in preset catalog + resolver
3. persist requested profile + effective posture snapshot into sessions
4. derive execution policy and approval posture from resolved profile
5. wire CLI `--profile` and TUI `/profile` with rebuild-and-switch semantics that always create a fresh session (or explicit fork), never silently rebind the current conversation
6. update restore/replay/session summary surfaces to honor persisted posture
7. add tests and validate with full repo test/build workflow
