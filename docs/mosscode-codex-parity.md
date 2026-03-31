# MossCode vs Codex CLI: Gap Analysis and Optimization Directions

## Context

`mosscode` aims to become a production-usable coding agent product while preserving `moss` as a reusable, custom-model-friendly runtime core.

This document compares the current `MossCode` / `Moss` capability surface against the core product expectations established by `OpenAI Codex CLI`, then identifies the most important optimization directions.

Assumption for this analysis:

- `mosscode` currently targets custom model backends.
- **Auth/Login is out of scope for now.**

That means the highest-priority parity work shifts away from identity and toward trust boundaries, execution safety, governance, and operational productization.

## Codex CLI Core Product Modules

Codex CLI can be usefully decomposed into the following product modules:

1. **Configuration and trust model**
   - user / project / system / profile / CLI layering
   - trusted-project gate before loading project-scoped config
   - managed defaults and requirements

2. **Sandbox, approvals, and network control**
   - read-only / workspace-write / full-access modes
   - protected paths
   - outbound network policy
   - approval escalation model
   - command rules / exec policy

3. **Core coding-agent workflow**
   - code understanding
   - editing and command execution
   - debugging and review
   - task automation

4. **Customization layer**
   - `AGENTS.md`
   - skills
   - MCP
   - subagents
   - hooks
   - rules

5. **Operational product surface**
   - install / update / distribution
   - state storage and history
   - notifications
   - diagnostics
   - logs and observability

6. **Enterprise governance**
   - managed policy
   - feature constraints
   - MCP allowlists
   - telemetry and audit integrations

## Current MossCode / Moss Position

### Already strong

`MossCode` and `Moss` already have a surprisingly strong coding-agent substrate:

- interactive TUI + one-shot flows
- file, command, memory, task, offload, delegation, and checkpoint flows
- approval modes (`read-only`, `confirm`, `full-auto`)
- audit logger and observer abstractions
- structured task / checkpoint / rollback primitives
- custom model integration and router/failover governance

In particular, `checkpoint`, `fork`, `replay`, and `change operation` flows give `MossCode` a stronger explicit recovery story than many coding-agent CLIs.

### Main product gaps

Without considering Auth/Login, the highest-value product gaps are:

1. **Trusted project boundary**
   - project config, project prompt templates, project skills, project agents, and project bootstrap context are still too easy to load implicitly
   - trust today mostly affects approval posture, not project capability loading

2. **Execution isolation depth**
   - current local sandbox is closer to a constrained executor than a Codex-class strong sandbox
   - network isolation is explicitly degraded in local mode

3. **Layered governance**
   - profile layers now exist, and a first `run_command` command-rules slice is in place
   - the remaining gap is the broader governance surface: managed defaults, requirements, HTTP-side rules, auditability, and policy-pack style distribution

4. **Extension governance**
   - MCP and skills exist, but there is not yet a full product control plane for project trust, allow/deny policy, lifecycle hooks, and operator-visible enablement state

5. **Operational maturity**
   - install / upgrade / packaging / diagnostics / observability are improving, but not yet at fully productized CLI level

## Recommended Optimization Directions

## P0: Required before calling MossCode a production-grade standalone product

### 1. Trusted project / trusted workspace

Make project-scoped capability loading explicit:

- project `moss.yaml`
- project system prompt templates
- project `AGENTS.md` / bootstrap files
- project `SKILL.md`
- project `.agents/agents`
- project MCP definitions

When trust is restricted, the runtime should still work, but only with:

- global config
- global skills
- global agents
- global bootstrap context

### 2. Stronger sandbox and network control

Upgrade from path-gated local execution to a clearer execution-security model:

- protected roots
- writable root control
- explicit network mode
- stronger command execution boundaries
- future-ready container / OS-enforced isolation path

### 3. Layered configuration and managed policy

Add a first-class configuration model for:

- user vs project vs system layering
- project trust gating
- profile selection
- managed defaults
- non-overridable requirements / policy constraints

### 4. Operational product loop

Strengthen:

- packaging and release ergonomics
- upgrade / migration behavior
- stable log locations
- machine-readable diagnostics
- operator-visible runtime state

## P1: Needed soon after P0

- hooks / lifecycle extensions — core session lifecycle and pre/post tool hooks are implemented through the extension bridge and appkit; richer operator UX and broader governance flows remain
- command rules / exec policy — core first slice is implemented for `run_command` and `http_request`, with rule-hit audit events and doctor/TUI visibility; broader governance UX and policy-pack ergonomics remain
- MCP management UX — core operator surface is implemented with `config mcp list/show/enable/disable` and doctor-level per-server visibility; interactive install/wizard flows and deeper governance UX remain
- profiles / task modes — largely implemented; remaining work is mostly hardening and follow-up ergonomics
- unified state store and indexed history — implemented
- notification surface for long-running work — implemented

## P2: Enterprise / scale work

- OTel / metrics / SIEM export
- policy packs and org-level governance
- IDE distribution and fleet rollout
- extension / skill marketplace and version governance

## What Moss Core Should Strengthen First

The most important `moss` core work, in order, is:

1. **trusted-project-aware capability loading**
2. **stronger execution isolation substrate**
3. **unified policy model**
4. **unified state layer**
5. **telemetry and rule-system foundation**

## Recommended Phase Order

### Phase 1

**Trusted project / configuration governance**

Make trust affect what project-scoped capabilities are allowed to load.

### Phase 2

**Sandbox and network policy strengthening**

Make execution isolation less dependent on conventions and more dependent on enforced policy.

### Phase 3

**Layered governance and operator controls**

Introduce profiles, managed defaults, requirements, and a clearer runtime policy model.

### Phase 4

**Operational hardening**

Converge diagnostics, install/update flow, observability, and runtime state visibility.

## Immediate Implementation Recommendation

Start with **Phase 1: trusted project / configuration governance**.

Reasons:

- it addresses the most obvious safety gap relative to Codex CLI
- it is foundational for later policy work
- it has a bounded implementation surface
- it improves both `MossCode` and `Moss` core behavior without requiring Auth/Login
