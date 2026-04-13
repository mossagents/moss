# Trusted Project Governance Design

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem

`moss` and `mosscode` already expose a `trust` concept, but today that signal is used mainly for approval posture. Project-scoped capability loading still happens too eagerly.

That means a workspace running in restricted mode can still pick up project-owned context and configuration surfaces such as:

- `moss.yaml`
- project system prompt templates
- project `SKILL.md`
- project `.agents/agents`
- project bootstrap files such as `AGENTS.md`

This is weaker than the `Codex CLI` trusted-project model, where project-scoped configuration is only loaded after the workspace is trusted.

## Goal

Deliver a first production-safe trusted-project baseline for `moss` / `mosscode` without adding Auth/Login.

## Goals

- Make project-scoped capability loading conditional on trust
- Preserve global configuration and global extensions in restricted mode
- Keep current trusted behavior unchanged
- Surface trust-driven loading state through `doctor`
- Keep the first delivery small enough to implement and validate safely

## Non-goals

- No login / account / tenancy work
- No full managed-policy system in this phase
- No command rules engine in this phase
- No container-grade sandbox redesign in this phase
- No migration of every example-specific prompt loader in one pass

## Candidate Approaches

### A. Product-layer-only gating

Teach `mosscode` to skip project config and prompt sources, but leave `appkit` / `runtime` behavior unchanged.

Pros:

- fastest to ship
- smallest blast radius

Cons:

- safety model remains inconsistent across entrypoints
- `moss` core and other apps still load project assets implicitly
- easy to regress later

### B. Shared trust-aware loading helpers (recommended)

Introduce reusable trust helpers and apply them to the main project-injection surfaces:

- config template loading
- runtime project config merging
- skill discovery
- bootstrap loading
- project agent loading
- doctor reporting

Pros:

- consistent behavior across `moss` and `mosscode`
- small enough for one phase
- creates a clear foundation for later governance work

Cons:

- requires touching several packages
- slightly broader test surface

### C. Full governance framework first

Implement trust gating together with profiles, requirements, managed config, and rules.

Pros:

- maximally complete

Cons:

- too large for the first phase
- increases delivery risk
- mixes foundational safety with broader product governance

## Recommendation

Choose **Approach B**.

It is the smallest change that meaningfully upgrades the trust model at the runtime boundary instead of only at the product shell.

## Scope of Phase 1

Phase 1 should gate the following project-scoped surfaces:

1. project `moss.yaml`
2. project system prompt templates (`./.<app>/system_prompt.tmpl`)
3. project `SKILL.md`
4. project `.agents/agents`
5. project bootstrap files (`AGENTS.md`, `.agents/AGENTS.md`, `.<app>/AGENTS.md`, and related bootstrap context files)

Restricted mode should continue to allow:

1. global config
2. global system prompt template
3. global skills
4. global agents
5. global bootstrap files under the app home directory

### In-scope entrypoints

Phase 1 applies to the shared loading paths that currently inject project-owned assets into the runtime:

1. `config.LoadSystemPromptTemplateForTrust(...)` and `config.RenderSystemPromptForTrust(...)`
2. `bootstrap.LoadWithAppNameAndTrust(...)`
3. `appkit/runtime.Setup(...)` for project config merge, project skill discovery, and project agent loading
4. `appkit/runtime.WithLoadedBootstrapContextWithTrust(...)` and the `appkit` extension wrapper that delegates to it
5. `appkit/deepagent.BuildKernel(...)` and the default TUI / product prompt-loading paths that consume the trust-aware helpers
6. `mosscode doctor` reporting for trust-sensitive config state

### Out-of-scope entrypoints

Phase 1 does not attempt to redesign every custom example or every direct file read in the repository. The contract for this phase is:

- shared runtime/bootstrap/config loading paths become trust-aware
- first-party product entrypoints built on those paths adopt the trust-aware helpers
- future work can migrate any remaining bespoke loaders onto the shared path

## Design

## 1. Shared trust helper

Add a reusable trust-normalization helper in the config layer:

- normalize trust values to `trusted` or `restricted`
- expose a helper that answers whether project-scoped assets may load

This keeps policy interpretation centralized and avoids reimplementing string checks across packages.

### Trust normalization semantics

The shared helper is the source of truth for Phase 1:

- `""` -> `trusted`
- `"trusted"` -> `trusted`
- `"restricted"` -> `restricted`
- any other value -> `restricted`

This preserves backward compatibility for existing callers that omit trust while making malformed or unknown trust values fail closed.

## 2. Trust-aware runtime setup

Extend `appkit/runtime.Setup(...)` configuration so it knows the workspace trust level.

When trust is restricted:

- skip project config during global+project config merge
- skip project skill discovery
- skip project agent directory loading

When trust is trusted:

- preserve current behavior

The builder path should automatically pass `flags.Trust` into runtime setup so callers do not have to remember this manually.

### Ownership

`appkit/runtime.Setup(...)` is the owning boundary for:

- project `moss.yaml` merge behavior
- project skill discovery / registration
- project agent directory loading

This is important because agent gating is not a `doctor` concern and not a product-shell-only concern. Product entrypoints should pass trust through, but runtime owns the actual allow/deny decision for these shared loading surfaces.

## 3. Trust-aware prompt/template loading

System prompt template loading should become trust-aware:

- trusted: project template overrides global template
- restricted: only global template may override defaults

This should be implemented as reusable config helpers so product entrypoints can adopt it consistently.

## 4. Trust-aware bootstrap loading

Bootstrap context loading should also become trust-aware:

- trusted: search project directories first, then global app directory
- restricted: skip project directories and load only global app directory

This ensures project-owned prompt injection cannot bypass trust gating through bootstrap files.

## 5. Doctor visibility

`mosscode doctor` should report the normalized trust posture and whether project-scoped config is currently **active** or **suppressed by trust**.

This makes the new behavior discoverable and debuggable.

### Doctor semantics for Phase 1

Phase 1 doctor output is intentionally narrow and should match the implemented boundary:

- report normalized trust value, not raw input
- report whether project-scoped assets are allowed at all
- report whether project `moss.yaml` exists
- report whether project `moss.yaml` is active under the current trust level

For this phase, doctor does **not** need to enumerate every suppressed project skill, agent, or bootstrap file individually. It only needs to make the trust posture and project-config activation state explicit enough to explain why project-owned behavior is or is not being picked up.

## Proposed API Additions

These names are illustrative; exact implementation can vary slightly to fit code style.

### config package

- `NormalizeTrustLevel(trust string) string`
- `ProjectAssetsAllowed(trust string) bool`
- `LoadSystemPromptTemplateForTrust(workspace, trust string) (string, error)`
- `RenderSystemPromptForTrust(workspace, trust, defaultTemplate string, data map[string]any) string`

### skill package

- `DiscoverSkillManifestsForTrust(workspace, trust string) []Manifest`

### bootstrap package

- `LoadWithAppNameAndTrust(workspace, appName, trust string) *Context`

### runtime package

- `WithWorkspaceTrust(trust string) Option`

## Behavioral Semantics

The following semantics apply to the **in-scope shared loading paths and adopted first-party entrypoints in Phase 1**, not to every bespoke loader in the repository.

### Trusted

- project config loads
- project templates load
- project skills load
- project agents load
- project bootstrap files load

### Restricted

- project config is ignored
- project templates are ignored
- project skills are ignored
- project agents are ignored
- project bootstrap files are ignored
- global equivalents continue to work

## Edge Cases

### Missing trust value

Treat empty trust as `trusted` to preserve current defaults and compatibility with existing callers.

### Invalid trust value

Normalize any unknown value to `restricted`.

This is a deliberate fail-closed behavior at the shared helper layer, even if some product entrypoints already validate flags earlier.

### Mixed global and project sources

Restricted mode must not silently merge project and global sources. It should behave as if project sources do not exist.

### Existing project files in restricted mode

The runtime should not error just because project files are present. It should simply skip them.

## Testing Strategy

Add tests for the exact trust boundary introduced in Phase 1:

1. **trust normalization**
   - empty trust normalizes to `trusted`
   - `trusted` and `restricted` remain stable
   - invalid trust normalizes to `restricted`

2. **template loading**
   - trusted loads project override
   - restricted ignores project override and uses global/default

3. **bootstrap loading**
   - trusted includes project bootstrap context when present
   - restricted suppresses project bootstrap context and keeps global bootstrap loading

4. **skill discovery**
   - trusted includes project manifests
   - restricted excludes project manifests but still includes global manifests

5. **runtime / builder behavior**
   - restricted mode does not merge project config
   - restricted mode does not load project agents
   - restricted mode does not expose project skills through runtime setup
   - trusted mode preserves existing project-loading behavior

6. **doctor reporting**
   - normalized trust is rendered correctly
   - project-config existence is rendered correctly
   - project-assets-allowed state is rendered correctly
   - project-config-active state is rendered correctly

## Implementation Steps

### Step 1

Add shared trust helpers in config/bootstrap/skill layers.

### Step 2

Make runtime setup trust-aware for project config, skills, and agents.

### Step 3

Wire trust-aware prompt/bootstrap loading into product entrypoints and default TUI flows.

### Step 4

Expose trust-sensitive state in `doctor`.

### Step 5

Add tests and validate with targeted `go test` plus repository build/test commands.

## Success Criteria

This phase is done when:

- for in-scope shared loading paths and adopted first-party entrypoints, restricted mode no longer loads project-scoped config/prompt/skill/agent/bootstrap assets
- for those same in-scope paths, trusted mode preserves current behavior
- invalid trust values fail closed to restricted behavior
- `doctor` explains normalized trust, whether project assets are allowed, whether project config exists, and whether project config is active or suppressed
- tests cover the new trust boundary behavior
