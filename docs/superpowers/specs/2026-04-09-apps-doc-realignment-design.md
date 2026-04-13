# Apps Doc Realignment Design

> **Archived historical design note.** This file records a past design iteration and is not the canonical source for the current architecture.

## Problem

The repository structure changed in two important ways:

1. `mosscode` and `mosswork` moved out of `examples\` and now live under `apps\`.
2. `mosswork-desktop` was renamed to `mosswork`.

Those two applications are now core Moss Agents applications rather than examples. Existing repository documentation still describes them as example entrypoints in multiple places, including user-facing docs and historical spec/plan documents. That leaves the repo with conflicting narratives about where the primary apps live, how to run them, and what the `moss` CLI points to.

## Goals

1. Make all repository docs reflect `apps\mosscode` and `apps\mosswork` as core applications.
2. Keep `examples\` documented as a normal examples directory for reference applications.
3. Update historical spec and plan files so they no longer point `mosscode` or `mosswork` at `examples\...`.
4. Document that the command users invoke remains `moss`, and that this CLI entrypoint targets the `mosscode` application surface.

## Non-Goals

1. Do not rewrite historical technical decisions unrelated to path and app-position changes.
2. Do not broadly rewrite docs for other applications that remain under `examples\`.
3. Do not change code or runtime behavior as part of this task.

## Recommended Approach

Use a focused documentation realignment sweep:

1. Update the primary external narrative in `README.md`, `README_ZH.md`, and the main `docs\` pages.
2. Update historical `docs\superpowers\specs\` and `plan\` files only where they refer to `mosscode` or `mosswork-desktop` with outdated paths, names, or roles.
3. Preserve the rest of each historical document as-is.

This approach keeps the repo historically readable while removing path drift and conflicting entrypoint descriptions.

## Design

### 1. Repository narrative

The repository documentation should consistently describe the application surfaces like this:

- `apps\mosscode`: core coding application and the primary interactive app surface
- `apps\mosswork`: core work/collaboration application
- `examples\`: reference examples that remain useful for smaller patterns and integrations
- `moss` CLI: the command users run is still `moss`, and this CLI entrypoint targets `mosscode`

Where docs currently describe `examples\mosscode` as the primary product surface, that should become `apps\mosscode`.

### 2. Rename and path replacement rules

Apply these rules consistently:

- `examples\mosscode` → `apps\mosscode`
- `examples/mosscode` → `apps/mosscode`
- `examples\mosswork-desktop` → `apps\mosswork`
- `examples/mosswork-desktop` → `apps/mosswork`
- `examples\mosswork` → `apps\mosswork`
- `examples/mosswork` → `apps/mosswork`
- `mosswork-desktop` → `mosswork` when referring to the application name

Only apply the rename/path update when the reference is specifically about the relocated core applications. References to other `examples\...` apps stay unchanged.

### 3. Historical document policy

Historical spec and plan docs should be updated narrowly:

- fix outdated paths to the two relocated apps
- fix outdated app naming for `mosswork`
- fix outdated app-role wording that still calls those two apps “examples”

Do not rewrite implementation status, design rationale, or unrelated file lists beyond what is required to keep the moved app references correct.
Completed historical task tables, file inventories, and recorded test commands should remain unchanged when they are serving as historical implementation snapshots.

Apply this historical wording rule:

- normalize wording when the old wording would be false or misleading to a current reader about the app path, app name, repo role, or CLI entrypoint
- keep wording unchanged when it is about unrelated historical implementation detail, sequencing, or design rationale that does not depend on the moved app location
- keep completed task logs, completed file lists, and completed historical test-command records unchanged even if they mention the old paths, unless the surrounding prose is being corrected for current repository facts
- keep `mosswork-desktop` only when the text is explicitly describing rename history; otherwise normalize it to `mosswork`

### 4. Validation

After edits, verify:

1. User-facing docs no longer describe `mosscode` or `mosswork` as example apps.
2. Historical specs/plans no longer point those two apps at `examples\...`.
3. The repo still describes `examples\` as a valid examples directory for the other reference apps.
4. The `moss` CLI narrative consistently says that users run `moss`, and that this entrypoint targets `mosscode`.
5. Repo-wide documentation search checks show no stale references for the moved apps in the old locations.
6. Every changed Markdown link, relative path, and run-command example that was updated from `examples\...` to `apps\...` is verified against the current tree.

Required search checklist:

- `examples[\\/]mosscode`
- `examples[\\/]mosswork-desktop`
- `examples[\\/]mosswork`
- `mosswork-desktop`
- `mosscode` lines that still describe it as an example app or example entrypoint
- `mosswork` lines that still describe it as an example app or example entrypoint

Allowed validation exception:

- `mosswork-desktop` may remain only in explicit rename-history wording that explains the old name before it became `mosswork`
- `examples\mosscode`, `examples\mosswork`, and `examples\mosswork-desktop` may remain only in explicit move-history wording or in this spec's rename-rule mapping section where old paths are being mapped to new paths
- old paths may remain inside preserved completed task tables, preserved historical file inventories, and preserved recorded historical test commands

## Expected Files

Likely touched files include:

- `README.md`
- `README_ZH.md`
- `apps\mosscode\README.md`
- `apps\mosswork\README.md`
- `docs\getting-started.md`
- `docs\architecture.md`
- `docs\production-readiness.md`
- `docs\roadmap.md`
- `docs\changelog.md`
- `docs\skills.md`
- historical files under `docs\superpowers\specs\`
- historical files under `plan\`

## Risks

1. Over-updating historical documents could accidentally change old design intent rather than just correcting repository facts.
2. Under-updating the docs would leave mixed `apps\...` and `examples\...` narratives in the same repo.

## Recommendation

Proceed with the narrow fact-correction sweep described above, with special care around historical documents so the edits are limited to paths, naming, and current repository role descriptions for `mosscode` and `mosswork`.
