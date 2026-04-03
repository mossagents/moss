---
goal: Implement audio/video multimodal parity across kernel, adapters, and TUI using content-parts
version: 1.0
date_created: 2026-04-03
last_updated: 2026-04-03
owner: Copilot
status: 'Completed'
tags: [feature, multimodal, audio, video, adapters, tui, protocol]
---

# Introduction

![Status: Completed](https://img.shields.io/badge/status-Completed-brightgreen)

This plan implements the approved specification in `docs/superpowers/specs/2026-04-03-multimodal-audio-video-parity-design.md` to add first-class audio/video input and output behavior with parity to existing image flows across `kernel/port`, OpenAI/Claude adapters, and TUI input/output interactions.

## 1. Requirements & Constraints

- **REQ-001**: Enable active validation for `input_audio`, `output_audio`, `input_video`, `output_video` in `kernel/port` using the same source and MIME invariants used for images.
- **REQ-002**: Map canonical `[]port.ContentPart` audio/video parts to provider-native request blocks in `adapters/openai/openai.go` and `adapters/claude/claude.go`.
- **REQ-003**: Normalize provider response media blocks back into canonical `output_audio` and `output_video` parts in both adapters.
- **REQ-004**: Ensure unsupported provider/model media capability returns explicit structured errors and fails the current request without partial media dropping.
- **REQ-005**: Add TUI input support for audio/video via `@file` and `/media attach <path>` producing canonical media parts.
- **REQ-006**: Add unified TUI media output rows for audio/video with inline preview/playback attempt and `/media open` + `/media save` fallback behavior.
- **REQ-007**: Preserve text-only behavior and existing image behavior with no regressions.
- **SEC-001**: Never send local-only metadata (`source_path`) to remote model APIs.
- **SEC-002**: Reject ambiguous MIME detection outcomes with explicit actionable errors; do not guess media family.
- **SEC-003**: Do not silently drop invalid or unsupported media parts at kernel, adapter, or TUI boundaries.
- **CON-001**: One-shot runtime cutover; no compatibility mode and no dual runtime schema.
- **CON-002**: Scope excludes transcoding, media cache service, and browser-grade playback UI.
- **GUD-001**: Reuse existing image content-part patterns and helper structure; avoid parallel ad-hoc logic paths per media type.
- **PAT-001**: Keep kernel provider-agnostic; isolate provider-specific payload mappings inside adapter packages.

## 2. Implementation Steps

### Implementation Phase 1

- **GOAL-001**: Activate kernel-level audio/video content-part validation and helper coverage.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-001 | Update `kernel/port/content_parts.go` to remove reserved-type rejection for audio/video and enforce active validation for `input_audio`, `output_audio`, `input_video`, and `output_video` with exact source-rule invariants (inline xor url, required source, MIME prefix family check). | ✅ | 2026-04-03 |
| TASK-002 | Add or update helper constructors/converters in `kernel/port/content_parts.go` for deterministic audio/video part creation and text fallback extraction behavior parity with image helpers. | ✅ | 2026-04-03 |
| TASK-003 | Extend `kernel/port/content_parts_test.go` with table-driven tests for valid/invalid audio/video inline/url combinations, MIME mismatch cases, missing-source cases, and mixed-part serialization roundtrip assertions. | ✅ | 2026-04-03 |
| TASK-004 | Ensure all direct validation call sites in kernel paths (`kernel/loop/loop.go`, `kernel/session/context.go`, related tool-result append paths) continue to call `ValidateContentParts` for mixed media payloads with no skipped branches. | ✅ | 2026-04-03 |

### Implementation Phase 2

- **GOAL-002**: Implement OpenAI and Claude audio/video request and response mapping with explicit capability failure semantics.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-005 | Extend OpenAI request mapping in `adapters/openai/openai.go` to encode `input_audio` and `input_video` parts into provider-native multimodal content blocks for user/tool-result paths using existing image mapping style. | ✅ | 2026-04-03 |
| TASK-006 | Extend OpenAI response normalization in `adapters/openai/openai.go` to decode provider media payloads into canonical `output_audio` and `output_video` parts; return explicit unsupported-capability errors when model/path cannot carry requested media type. | ✅ | 2026-04-03 |
| TASK-007 | Extend Claude request mapping in `adapters/claude/claude.go` to encode `input_audio` and `input_video` parts into anthropic-native block shapes with strict field validation and no `source_path` leakage. | ✅ | 2026-04-03 |
| TASK-008 | Extend Claude response normalization in `adapters/claude/claude.go` to decode media blocks into canonical `output_audio` and `output_video` parts; enforce fail-request behavior for unsupported paths instead of partial media omission. | ✅ | 2026-04-03 |
| TASK-009 | Update `adapters/openai/openai_test.go`, `adapters/claude/claude_test.go`, and any shared adapter tests to cover request mapping, response mapping, and explicit unsupported-capability error assertions for audio/video. | ✅ | 2026-04-03 |

### Implementation Phase 3

- **GOAL-003**: Deliver TUI parity for audio/video input, rendering, and media interaction commands.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-010 | Extend mention/attach parsing in `userio/tui/external_context.go` so `@file` and `/media attach <path>` detect audio/video files, resolve MIME via extension+content sniff policy, and emit canonical media parts or explicit mismatch/ambiguity errors. | ✅ | 2026-04-03 |
| TASK-011 | Update queue/send path in `userio/tui/chat_update.go` and `userio/tui/chat.go` so mixed text+audio+video content is submitted as canonical `[]port.ContentPart` with deterministic ordering and existing text/image behavior preserved. | ✅ | 2026-04-03 |
| TASK-012 | Add unified audio/video output row rendering in `userio/tui/message.go` and dependent chat view files so assistant `output_audio` and `output_video` display actionable entries consistent with image output rows. | ✅ | 2026-04-03 |
| TASK-013 | Extend slash command handling in `userio/tui/chat_slash_core.go` to support `/media open` and `/media save` for audio/video outputs with explicit fallback when inline preview/playback is unavailable in terminal/runtime. | ✅ | 2026-04-03 |
| TASK-014 | Add or update TUI tests (`userio/tui/external_context_test.go`, `userio/tui/chat_test.go`, `userio/tui/message_test.go`) for attach parsing, MIME mismatch error path, output row parity, and media command behavior. | ✅ | 2026-04-03 |

### Implementation Phase 4

- **GOAL-004**: Stabilize repository-wide behavior and verify release gates.

| Task | Description | Completed | Date |
|------|-------------|-----------|------|
| TASK-015 | Sweep compile and behavioral call sites in `appkit/`, `agent/`, `kernel/`, `adapters/`, and `userio/` for assumptions that only image media exists; patch to generic media-family logic without changing unrelated behavior. | ✅ | 2026-04-03 |
| TASK-016 | Run `gofmt -w` on all changed Go files and resolve formatting drift. | ✅ | 2026-04-03 |
| TASK-017 | Run targeted tests: `go test ./kernel/port ./adapters/openai ./adapters/claude ./userio/tui ./kernel/loop ./kernel/session`; fix all regressions. | ✅ | 2026-04-03 |
| TASK-018 | Run full gates: `go test ./...` and `go build ./...`; update plan status fields and completion table entries after all gates pass. | ✅ | 2026-04-03 |

## 3. Alternatives

- **ALT-001**: Implement audio first and video later was rejected because it breaks required media parity and duplicates migration/testing effort.
- **ALT-002**: Add adapter-side silent downgrade for unsupported media was rejected because spec requires explicit capability errors and no silent media drop.
- **ALT-003**: Introduce a new media transport schema separate from `ContentPart` was rejected because current architecture already standardizes multimodal behavior through content-parts.

## 4. Dependencies

- **DEP-001**: `docs/superpowers/specs/2026-04-03-multimodal-audio-video-parity-design.md` is the governing design specification.
- **DEP-002**: Existing content-part protocol implementation in `kernel/port/content_parts.go` and image-phase behavior in adapters/TUI provide required baseline patterns.
- **DEP-003**: OpenAI and Anthropic Go SDK capabilities currently used in `adapters/openai` and `adapters/claude` for multimodal block encoding/decoding.
- **DEP-004**: Existing media command and output row infrastructure in `userio/tui` from image phase.

## 5. Files

- **FILE-001**: `kernel/port/content_parts.go` — activate audio/video validation and helpers.
- **FILE-002**: `kernel/port/content_parts_test.go` — audio/video validation test matrix.
- **FILE-003**: `adapters/openai/openai.go` — OpenAI audio/video request/response mappings.
- **FILE-004**: `adapters/openai/openai_test.go` — OpenAI media mapping tests.
- **FILE-005**: `adapters/claude/claude.go` — Claude audio/video request/response mappings.
- **FILE-006**: `adapters/claude/claude_test.go` — Claude media mapping tests.
- **FILE-007**: `userio/tui/external_context.go` — media mention/attach parsing and MIME policy.
- **FILE-008**: `userio/tui/chat.go` — send path content-part assembly.
- **FILE-009**: `userio/tui/chat_update.go` — queued message payload wiring.
- **FILE-010**: `userio/tui/message.go` — output media row rendering.
- **FILE-011**: `userio/tui/chat_slash_core.go` — media open/save command handling.
- **FILE-012**: `userio/tui/external_context_test.go` — media parse and MIME error tests.
- **FILE-013**: `userio/tui/chat_test.go` — send and command behavior tests.
- **FILE-014**: `userio/tui/message_test.go` — output rendering parity tests.
- **FILE-015**: `plan/feature-multimodal-audio-video-parity-1.md` — this implementation plan lifecycle record.

## 6. Testing

- **TEST-001**: `go test ./kernel/port` validates audio/video content-part invariants.
- **TEST-002**: `go test ./adapters/openai` validates OpenAI audio/video mapping and failure semantics.
- **TEST-003**: `go test ./adapters/claude` validates Claude audio/video mapping and failure semantics.
- **TEST-004**: `go test ./adapters` validates cross-adapter behavior.
- **TEST-005**: `go test ./userio/tui` validates media attach/parse/render/open-save flows.
- **TEST-006**: `go test ./kernel/loop ./kernel/session` validates runtime path stability with mixed media parts.
- **TEST-007**: `go test ./...` validates repository-wide regression safety.
- **TEST-008**: `go build ./...` validates full compile gate.

## 7. Risks & Assumptions

- **RISK-001**: Provider SDK block support for specific audio/video payload shapes may differ by model and could require guarded mapping branches.
- **RISK-002**: MIME detection ambiguity on uncommon extensions may increase user-facing attach errors until explicit format guidance is improved.
- **RISK-003**: Terminal/runtime preview limitations can vary by environment and may increase fallback open/save usage.
- **RISK-004**: Broad parity changes across adapters and TUI can cause cross-package regressions if one media family path diverges.
- **ASSUMPTION-001**: Existing image content-part infrastructure is stable and suitable as the direct parity baseline for audio/video.
- **ASSUMPTION-002**: Inline media payload size constraints remain governed by current provider/runtime limits and are not changed in this phase.
- **ASSUMPTION-003**: One-shot cutover policy remains accepted for this phase with no runtime compatibility mode.

## 8. Related Specifications / Further Reading

- `docs/superpowers/specs/2026-04-03-multimodal-audio-video-parity-design.md`
- `plan/feature-multimodal-image-contentparts-1.md`
- `kernel/port/types.go`
- `kernel/port/content_parts.go`
