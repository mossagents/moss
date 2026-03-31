# Command Rules Roadmap

## Current status

The first `command rules / exec policy` slice is now implemented for `run_command`.

What is already in place:

- `profiles.<name>.execution.command_rules` is part of the profile config schema
- resolved `ExecutionPolicy` now carries command rules
- `run_command` rules are evaluated inside the existing policy middleware chain
- rule decisions reuse the same `allow / require-approval / deny` model as the rest of the product
- active command rules are visible through the TUI `/permissions` summary
- rules persist as part of the effective execution posture, so restore / replay compatibility checks naturally see them

This keeps the first slice bounded while establishing the right foundation for broader Codex-style rule governance.

## Follow-up optimization directions

### 1. Expand rule coverage to `http_request`

The next most valuable extension is to apply the same rule model to `http_request`.

Recommended additions:

- support URL / host / method matching alongside the current command-line matching
- keep the same `allow / require-approval / deny` outcomes
- persist HTTP rules inside resolved execution policy the same way command rules are persisted today

Why this matters:

- it closes the biggest gap in the current first slice
- it makes the rule model cover both local execution and network access
- it gets much closer to the Codex CLI operator posture

### 2. Improve operator-visible management surfaces

The current first slice proves enforcement, but the product surface is still thin.

Recommended improvements:

- extend `/permissions` to render profile-derived rules and local overrides more clearly
- surface active rules in `doctor`
- include rule summaries in session list / session summary / restore output where useful
- make it obvious whether a rule came from built-ins, global config, or project config

Why this matters:

- production users need to understand *why* a command was denied or required approval
- debugging policy mismatches is hard without a visible control plane

### 3. Add rule-hit audit events and indexed history

Today rules influence approval and denial, but the product still lacks a clean audit trail for rule matches.

Recommended additions:

- emit structured execution events when a command rule matches
- include matched rule name, action, and normalized target details
- index those events in the state catalog / searchable history

Why this matters:

- it gives operators and users a usable audit trail
- it prepares later governance, telemetry, and enterprise controls

### 4. Introduce managed defaults and policy packs

Once local rules are stable, the next step is not more syntax but better distribution and governance.

Recommended additions:

- global or org-level managed defaults that cannot be silently weakened by project config
- policy-pack style rule bundles
- explicit precedence between built-ins, global config, project config, and operator-managed policy

Why this matters:

- Codex-class governance is not just about rule matching, but about who can define and override rules
- this is the bridge from local product safety to enterprise policy control

### 5. Strengthen matching semantics

The current first slice intentionally uses a small matching model.

Recommended later improvements:

- richer matching for command path, full argv, working directory, and environment-sensitive cases
- reusable compiled matchers for performance and consistency
- explicit conflict handling rules when multiple patterns match

Why this matters:

- it reduces ambiguity in real-world command governance
- it keeps the rule system predictable as the policy surface grows

## Recommended implementation order

1. expand rules to `http_request`
2. improve operator-visible management surfaces
3. add rule-hit audit events and indexed history
4. introduce managed defaults / policy packs
5. strengthen matching semantics as needed

## Major next-step options

If we step back from `command rules` specifically, the next major implementation choices for MossCode parity are:

1. **Command rules phase 2**
   - extend the current rule model to `http_request`
   - improve management surfaces and auditability

2. **Hooks / lifecycle extensions**
   - add operator-visible pre/post tool and session lifecycle hook points
   - create a cleaner extensibility story aligned with Codex CLI

3. **MCP management UX**
   - make server enablement, trust, source visibility, and governance clearer at the product layer

4. **Restore posture auto-rebuild**
   - upgrade restore / replay / fork from fail-fast to rebuilding runtimes from persisted posture

These are all valid next moves, but the highest continuity path is still **Command rules phase 2**, because it directly builds on the new profile and execution-policy foundation.
