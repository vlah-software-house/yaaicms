# Menu Management System

**Date:** 2026-03-05
**Branch:** feat/menu-management

## Summary

Implemented a complete menu management system with 3 predefined locations (main, footer, footer_legal), admin UI, and dynamic template rendering.

## Changes

### New Files
- `internal/database/migrations/00022_create_menus.sql` — menus + menu_items tables
- `internal/models/menu.go` — Menu and MenuItem model structs
- `internal/store/menu.go` — MenuStore with CRUD, tree building, reorder
- `internal/handlers/admin_menu.go` — Admin handlers for menu CRUD + reorder + content list
- `internal/render/templates/admin/menus.html` — Admin UI with AlpineJS + SortableJS

### Modified Files
- `internal/engine/engine.go` — Added Menus type, FragmentData, updated RenderPage/RenderPostList signatures
- `internal/handlers/public.go` — Added menuStore dependency, loadMenus helper
- `internal/handlers/admin.go` — Added menuStore to Admin struct
- `internal/handlers/admin_ai.go` — Updated header/footer AI prompts with menu variables
- `internal/router/router.go` — Added /admin/menus route group
- `internal/render/templates/admin/base.html` — Added Menus sidebar nav item
- `internal/database/seed.go` — Seed menu locations for default tenant
- `cmd/yaaicms/main.go` — Wire MenuStore into Admin and Public handlers
- `internal/engine/engine_test.go` — Updated test calls for new signatures
- `internal/handlers/handler_test.go` — Updated test setup for new constructor args

## Key Decisions
- Menu items support both content links (auto-resolved slugs) and custom URLs
- One level of nesting for main nav only; footer menus are flat
- Cache invalidation on any menu change (InvalidateAll) since menus affect all pages
- AI template prompts now instruct LLMs to use `{{range .Menus.main}}` instead of hardcoding nav links
