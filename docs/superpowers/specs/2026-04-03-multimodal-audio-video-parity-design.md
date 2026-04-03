# Multimodal Audio/Video Parity Design (OpenAI + Claude)

Date: 2026-04-03  
Status: Draft (design approved in-chat, pending spec review loop)

## Goals

1. Extend current content-parts protocol from image-only behavior to full audio/video input and output.
2. Keep **media behavior parity** across image/audio/video wherever possible:
   - input parsing pattern,
   - part validation pattern,
   - adapter mapping pattern,
   - output rendering and interaction pattern.
3. Support OpenAI and Claude adapters with provider-native multimodal blocks.
4. Keep one-shot cutover strategy (no compatibility mode / no dual-write path for runtime contract).
5. In TUI, prefer inline preview/playback; if terminal capability is insufficient, fall back to open/save actions.
6. TUI composer supports normal text copy/paste and image paste/attachment flow.

## Scope

In scope:

- `kernel/port` schema+validation for audio/video parts.
- `adapters/openai` and `adapters/claude` request/response mapping for audio/video.
- `userio/tui` input/output UX for audio/video, aligned with image behavior model.
- Tests across kernel/adapters/TUI and end-to-end smoke.

Out of scope:

- Dedicated transcoding pipeline.
- Cross-provider media cache service.
- Browser-grade playback UI.

## Protocol & Validation

Canonical part types:

- `text`
- `input_image`, `output_image`
- `input_audio`, `output_audio`
- `input_video`, `output_video`

Source rule (all media types): exactly one source:

1. inline: `mime_type + data_base64`
2. URL/reference: `url`

Validation invariants:

1. media parts reject `text`.
2. inline and url are mutually exclusive.
3. missing both sources is invalid.
4. MIME prefix must match media family:
   - image => `image/*`
   - audio => `audio/*`
   - video => `video/*`

## Architecture

### 1) Kernel / Port

- Upgrade audio/video part types from reserved-reject mode to active validation mode.
- Preserve `ContentPart` as single canonical schema for all media families.
- Keep provider-agnostic kernel behavior; adapter-specific details stay outside kernel.

### 2) Adapters (OpenAI + Claude)

- Request path: map mixed `[]ContentPart` to provider-native multimodal blocks.
- Response path: normalize provider blocks back to canonical `[]ContentPart`.
- Never send `source_path` to provider.
- Unsupported provider/model capability must return explicit structured error and fail the current request (no partial media drop).
- Phase baseline provider matrix:
  - OpenAI: input `input_audio` + `input_video`, output `output_audio` + `output_video` when provider response contains corresponding media payload.
  - Claude: input `input_audio` + `input_video`, output `output_audio` + `output_video` when provider response contains corresponding media payload.
  - If a provider/model path is unavailable at runtime, return explicit capability error; do not continue with partial media omission.

### 3) TUI

- Input:
  - `@file` mentions and new `/media attach <path>` both produce media parts (parity behavior across image/audio/video).
  - MIME autodetect (best effort broad format detection), explicit unsupported-format error.
  - Keep text copy/paste support unchanged; support image paste in composer (when terminal/clipboard exposes image payload), otherwise show explicit fallback guidance to `@file` or `/media attach`.
- Output:
  - Unified media result row model for image/audio/video.
  - Inline preview/playback attempted first.
  - If unsupported in terminal/runtime, show fallback actions: `/media open`, `/media save`.

## Data Flow

1. User enters text + media references (mention/attach/paste).
2. TUI resolves files and builds `ContentParts` (text + `input_*` media).
3. Loop sends canonical parts to adapter.
4. Adapter maps to provider-native blocks and sends request.
5. Provider response mapped back to canonical `ContentParts` (`text` + `output_*` media).
6. TUI renders unified media entries and offers preview/open/save interactions.

## Error Handling

Unified media error model:

1. Input errors: missing file, unreadable file, MIME detect failure, unsupported format, encoding failure.
2. Adapter capability errors: provider/model unsupported media type.
3. Output preview errors: preview/playback failure is non-fatal; downgrade to open/save.
4. No silent drop of unsupported or invalid media parts.
5. MIME detection policy: detect by extension + content sniff when available; on mismatch/ambiguity, fail with explicit error and actionable hint instead of guessing.

## Testing Strategy

### Kernel

1. Validation for all media families (inline/url exclusivity, MIME family checks, missing fields).
2. Serialization roundtrip for mixed content parts.

### Adapters

1. OpenAI/Claude request mapping for text+audio, text+video, mixed media.
2. Response normalization for output audio/video parts.
3. Explicit unsupported-part error behavior.

### TUI

1. Mention + `/media attach` + paste parsing for media.
2. Message send builds canonical parts.
3. Media render parity checks (image/audio/video same row model + action hints).
4. `/media open` and `/media save` behavior and fallback coverage.

### End-to-end Smoke

1. Text-only regression.
2. Image/audio/video input-output roundtrip checks.

## Rollout

One-shot cutover, same as image phase:

1. No compatibility mode toggle.
2. No dual write/runtime fallback schema.
3. Full compile/test green gate before release.

## Acceptance Criteria

1. Image/audio/video follow the same behavioral model for input, mapping, output, and interactions.
2. OpenAI + Claude support baseline media paths (`input_audio`, `input_video`, `output_audio`, `output_video`) with explicit failure for unsupported provider/model variants.
3. TUI provides inline preview/playback where possible, fallback open/save otherwise.
4. TUI composer supports text paste and image paste flow; if image clipboard payload is unavailable in current terminal, user receives explicit fallback guidance to `@file` or `/media attach`.
5. `go test ./...` and `go build ./...` pass.

