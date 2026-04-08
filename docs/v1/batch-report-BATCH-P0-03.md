# Batch Report: BATCH-P0-03

- Start: 2026-04-08T00:00:00Z
- End: 2026-04-08T00:00:00Z
- Executor: AI

## Tasks
- P0-RUNTIME-001: done

## Changes
- `appkit/flags.go`: add `--enable-summarize` and `--enable-rag` runtime flags.
- `appkit/runtime_builder.go`: add `RuntimeFeatureFlags` to resolution output.
- `appkit/runtime_builder_test.go`: add feature flag resolution tests.
- `kernel/middleware/builtins/summarize.go`: add explicit `Enabled` switch (nil/true=enabled, false=disabled).
- `kernel/middleware/builtins/rag.go`: add explicit `Enabled` switch (nil/true=enabled, false=disabled).
- `kernel/middleware/builtins/summarize_test.go`: add disabled-mode stability test.
- `kernel/middleware/builtins/rag_test.go`: add disabled-mode stability test.
- `docs/v1/status.md`: sync task and batch status.

## Validation
- `go test ./kernel/... ./appkit/...` : pass
- `go test ./testing/eval/...` : pass

## Risks
- RAG middleware still requires a runtime `MemoryManager`; enabling flag alone does not inject manager wiring.

## Rollback Actions
- Keep `--enable-summarize=false` and `--enable-rag=false`.
- Keep truncate fallback strategy for degraded runs.

## Next
- start `P1-PROMPT-001` (unify prompt assembly path).

