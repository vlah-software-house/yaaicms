# Fix AI Template Max Tokens

**Date:** 2026-03-08
**Branch:** fix/ai-template-max-tokens
**Status:** Complete

## Problem

When generating author_page templates via AI (especially by refining an existing article_loop template), the AI output gets truncated mid-template. The truncated template has unclosed `{{if}}`, `{{range}}`, or HTML tags, causing Go's `template.Parse()` to fail with "unexpected EOF".

Root cause: The Claude provider hardcoded `MaxTokens: 4096`, which is too low for large templates (author profile hero + post loop). The OpenAI, Gemini, and Mistral providers didn't set max output tokens at all, relying on model defaults.

## Solution

### Token limit increase (all providers)
- **Claude:** `MaxTokens` 4096 → 16384
- **OpenAI:** Added `MaxTokens: 16384` to request struct (was omitted)
- **Gemini:** Added `GenerationConfig.MaxOutputTokens: 16384` (was omitted)
- **Mistral:** Added `MaxTokens: 16384` to request struct (reuses OpenAI types)

### Truncation detection (all providers)
- Added `ErrOutputTruncated` sentinel error to `ai` package
- **Claude:** Parse `stop_reason` from response; detect `"max_tokens"`
- **OpenAI/Mistral:** Parse `finish_reason` from response; detect `"length"`
- **Gemini:** Parse `finishReason` from response; detect `"MAX_TOKENS"`
- Handler shows clear message: "The generated template was too large and got cut off."

### Timeout increase
- All providers: HTTP client timeout 60s → 120s (larger outputs take longer)

## Changes

### `internal/ai/provider.go`
- Added `ErrOutputTruncated` sentinel error

### `internal/ai/claude.go`
- `MaxTokens`: 4096 → 16384
- Parse `stop_reason` from `claudeResponse`
- Return `ErrOutputTruncated` when `stop_reason == "max_tokens"`
- HTTP timeout: 60s → 120s

### `internal/ai/openai.go`
- Added `MaxTokens` field to `openAIRequest` (json: `max_tokens,omitempty`)
- Added `FinishReason` field to `openAIChoice` (json: `finish_reason`)
- Set `MaxTokens: 16384` in `GenerateWithModel`
- Return `ErrOutputTruncated` when `finish_reason == "length"`
- HTTP timeout: 60s → 120s

### `internal/ai/gemini.go`
- Added `geminiGenerationConfig` struct with `MaxOutputTokens`
- Added `GenerationConfig` to `geminiRequest`
- Added `FinishReason` to `geminiCandidate`
- Set `MaxOutputTokens: 16384` in `GenerateWithModel`
- Return `ErrOutputTruncated` when `finishReason == "MAX_TOKENS"`
- HTTP timeout: 60s → 120s

### `internal/ai/mistral.go`
- Set `MaxTokens: 16384` in request (reuses openAIRequest)

### `internal/handlers/admin_ai.go`
- Check for `ErrOutputTruncated` in `AITemplateGenerate` handler
- Show user-friendly message instead of cryptic "unexpected EOF"

### `internal/ai/provider_http_test.go`
- Updated max_tokens assertion: 4096 → 16384
- Added `TestClaudeGenerate_Truncated` test
- Updated `claudeSuccessBody` to include `stop_reason: "end_turn"`
