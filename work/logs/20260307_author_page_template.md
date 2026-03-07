# Author Page Template Type

**Date:** 2026-03-07
**Branch:** feat/author-page-template

## Summary

Added a dedicated `author_page` template type so the AI template builder can generate
author-specific page designs (hero bio section, social links, post listing) independently
from the generic `article_loop` template. The `RenderAuthorPage` engine method prefers
`author_page` templates and falls back to `article_loop` for backward compatibility.

## Changes

### Modified Files
- `internal/models/template.go` — Added `TemplateTypeAuthorPage = "author_page"` constant
- `internal/engine/engine.go` — Updated `RenderAuthorPage` to prefer `author_page` template with `article_loop` fallback
- `internal/handlers/admin_ai.go` — Added `case "author_page"` to `buildTemplateSystemPrompt()`, `buildPreviewData()`, `buildRealPreviewData()`; new `buildRealAuthorPagePreview()` method; extended restyle preview structs and `AIRestylePreview` handler for 5-template flow
- `internal/render/templates/admin/template_form.html` — Added `author_page` option to type dropdown; added variable help text for author page templates
- `internal/render/templates/admin/template_ai.html` — Extended restyle flow to 5 steps (header → footer → page → article_loop → author_page); added Author View preview tab; updated all JS data objects, order arrays, save loop, and preview response handling
- `internal/database/seed.go` — Added default `author_page` seed template with author bio, avatar, job title, and post listing
- `internal/models/template_test.go` — Added `TemplateTypeAuthorPage` to all three test functions

## Architecture Decisions
- Fallback strategy: `RenderAuthorPage` tries `author_page` first, falls back to `article_loop` — existing sites work without a dedicated author template
- Restyle flow generates all 5 types sequentially for visual consistency
- Author page preview uses `AuthorPageData` (not `ListData`) with full `TemplateAuthor` profile including social links
- Real preview data picks the first post author and filters their posts
