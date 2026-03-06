# Per-Tenant AI Provider Selection

**Date:** 2026-03-06
**Branch:** fix/per-tenant-ai-provider

## Problem

The `AI_PROVIDER` env var and `Registry.active` field were global — switching the
provider on one tenant's settings page affected all tenants. This is incorrect
in a multi-tenant CMS where each site should independently choose its AI provider.

## Solution

- Added `Registry.GenerateForTaskAs(providerName, ...)` — generates with a specific
  named provider without mutating global state.
- Added `tenantAIProvider(r)` helper in handlers — reads `ai_provider` from the
  tenant's `site_settings`, falls back to the env-var default.
- `AISetProvider` now persists to `site_settings` per-tenant instead of calling
  `SetActive()` globally.
- All 11 AI generation call sites updated to use `GenerateForTaskAs` with the
  tenant-resolved provider name.
- Settings page and provider selector UI now show per-tenant active state via
  `providerInfoForTenant(r)`.

## Files Changed

| File | Change |
|------|--------|
| `internal/ai/provider.go` | Added `GenerateForTaskAs()` method |
| `internal/ai/registry_test.go` | Added tests for `GenerateForTaskAs` |
| `internal/handlers/admin_ai.go` | `tenantAIProvider()`, `providerInfoForTenant()`, updated all AI calls and `AISetProvider` |
| `internal/handlers/admin.go` | Updated `SettingsPage`, `generateRevisionMeta`, `generateTemplateRevisionMeta` |
