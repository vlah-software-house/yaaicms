# Preview Content Type Filter

**Date:** 2026-03-08
**Branch:** fix/preview-content-type-filter
**Status:** Complete

## Problem

The AI template builder's "Preview Content" dropdown showed all content items (posts and pages) regardless of the selected template type. A page template should only show pages, and article_loop/author_page templates should only show posts.

## Solution

Added a `filteredPreviewContent()` method that filters items based on the active template type:
- `page` → pages only
- `article_loop` / `author_page` → posts only
- `header` / `footer` → all items (dropdown is hidden anyway)

Reset `previewContentID` when switching template type to avoid stale selections.

## Changes

### `internal/render/templates/admin/template_ai.html`
- Added `filteredPreviewContent()` method to filter preview content by template type
- Updated single-template dropdown to use `filteredPreviewContent()` instead of `previewContent`
- Changed type button click to reset `previewContentID` instead of re-fetching content
- Restyle section keeps showing all content (it processes all types at once)
