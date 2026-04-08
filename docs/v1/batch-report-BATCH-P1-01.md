# Batch Report: BATCH-P1-01

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P1-PROMPT-001: done
- P1-BUDGET-001: ready

## Changes
- `appkit/flags.go`: add `--prompt-assembly` and `--prompt-version` runtime flags with env support.
- `appkit/runtime_builder.go`: extend `RuntimeFeatureFlags` with prompt assembly/version values.
- `appkit/runtime_builder_test.go`: add tests for prompt assembly defaults and prompt version override.
- `kernel/session/turn_metadata.go`: add `prompt_version` and `prompt_assembly` metadata keys.
- `userio/prompting/composer.go`: add prompt assembly/version generation and metadata attachment.
- `userio/prompting/composer_test.go`: add tests for prompt assembly/version metadata attachment and decode.
- `kernel/loop/turn_plan.go`: include `PromptVersion` in turn plan.
- `kernel/loop/loop_run.go`: persist and emit `prompt_version` in turn/LLM events.
- `kernel/loop/turn_plan_test.go`: add prompt version carry-over test.
- `docs/v1/status.md`: sync board status.

## Validation
- `go test ./appkit/... ./kernel/prompt/... ./kernel/loop/... ./userio/prompting/...` : pass
- `go test ./testing/eval/...` : pass

## Risks
- Prompt assembly mode flag is now tracked in runtime config, but legacy-vs-unified execution branching is currently metadata-first and not hard-routed in all entrypoints.

## Rollback Actions
- Set `--prompt-assembly=legacy` and keep existing prompt templates.
- Use `--prompt-version` override for quick trace correlation while rollback is in progress.

## Next
- execute `P1-BUDGET-001` (global budget governance and aggregated budget report).

