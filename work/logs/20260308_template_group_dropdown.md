# Template Group Dropdown

**Date:** 2026-03-08
**Branch:** feat/template-group-dropdown
**Status:** Complete

## Problem

Templates are grouped by name prefix (text before " — " em dash). When users create templates manually, they may use a regular hyphen (" - ") instead, breaking the grouping. Example: "web of the 60s — Header" and "web of the 60s - Author Page" appeared as separate groups.

## Solution

Added a group dropdown to the template form (both manual and AI builder) that shows existing group prefixes. Selecting a group auto-composes the name as "Group — TypeLabel" with the correct em dash separator.

## Changes

### `internal/store/template.go`
- Added `ListDistinctPrefixes(tenantID)` — SQL query extracting distinct name prefixes, normalizing both em dash and hyphen separators.

### `internal/handlers/admin.go`
- Added helper functions: `templateNamePrefix()`, `templateNameSuffix()`, `composeTemplateName()`
- Updated `TemplatesList` grouping to use `templateNamePrefix()` (now supports both separators)
- Updated `TemplateNew` and `TemplateEdit` to pass `Prefixes` and `CurrentGroup` to template
- Updated `TemplateCreate` and `TemplateUpdate` to compose name from `group` form field
- Added `TemplatePrefixes` JSON endpoint for AJAX loading

### `internal/handlers/admin_ai.go`
- Updated `AITemplateSave` to support `group` form field (backward compatible — restyle flow still uses pre-composed `name`)

### `internal/router/router.go`
- Added `GET /admin/templates/prefixes` route (before `/{id}` to avoid catch-all)

### `internal/render/templates/admin/template_form.html`
- Replaced single "Template Name" field with: Group dropdown + "No group" option + "+ New group..." option + composed name preview
- Hidden `<input name="group">` sends the effective group value via AlpineJS binding
- When no group selected, falls back to manual name input

### `internal/render/templates/admin/template_ai.html`
- Added group dropdown to "Save as Template" section (single template save)
- Added `loadPrefixes()`, `saveEffectiveGroup()`, `saveComposedName()` methods
- Updated `saveTemplate()` to send group field when a group is selected
- Reloads prefixes after successful save so new groups appear immediately

### `internal/render/templates/admin/templates_list.html`
- Added `author_page` badge (teal color) to type column
