# User Profile System

**Date:** 2026-03-07
**Branch:** feat/user-profiles

## Summary

Implemented a full user profile system that adds biographical data, social links,
and author pages to SmartPress. Users can edit their own profile from the admin panel,
and author data appears on public pages (post bylines, author pages).

## Changes

### New Files
- `internal/database/migrations/00023_create_user_profiles.sql` — Migration creating the `user_profiles` table (1:1 with users via user_id PK, CASCADE delete)
- `internal/models/user_profile.go` — UserProfile model struct
- `internal/store/user_profile.go` — UserProfileStore with FindByUserID, Upsert, FindByUserIDs, FindAuthorByUserID, FindAuthorBySlug
- `internal/render/templates/admin/profile.html` — Admin profile editing page (HTMX form)

### Modified Files
- `internal/store/user.go` — Added UpdateDisplayName method
- `internal/store/content.go` — Added ListPublishedByAuthor method
- `internal/engine/engine.go` — Added TemplateAuthor, AuthorPageData structs; added Author field to PageData; added AuthorName/AvatarURL/Slug to PostItem; updated RenderPage/RenderPostList signatures; added RenderAuthorPage method
- `internal/handlers/public.go` — Added userProfileStore dep, loadAuthor/loadAuthors helpers, AuthorPage handler; updated Page/Homepage to pass author data
- `internal/handlers/admin.go` — Added userProfileStore dep, ProfilePage/ProfileSave handlers
- `internal/handlers/admin_ai.go` — Added Author variables to page/article_loop template prompts; added author data to preview data (both static and real-content previews)
- `internal/router/router.go` — Added /admin/profile routes and /author/{slug} public route
- `internal/render/templates/admin/base.html` — Added "Edit Profile" link in user dropdown
- `internal/database/seed.go` — Added seedAdminProfile for admin user
- `cmd/yaaicms/main.go` — Wired UserProfileStore into Admin and Public handlers
- `internal/engine/engine_test.go` — Updated test calls for new RenderPage/RenderPostList signatures
- `internal/handlers/handler_test.go` — Updated NewAdmin/NewPublic calls for new parameter

## Architecture Decisions
- Separate `user_profiles` table (not columns on `users`) — keeps auth and presentation separate
- Lazy row creation via UPSERT — no profile row until user first saves
- Slug derived from display_name using existing slug.Generate()
- Author page reuses article_loop template with AuthorPageData
- ProfileSave invalidates full page cache since author data appears on public pages
- `is_published` boolean controls public visibility of the `/author/{slug}` page
  - `FindAuthorBySlug` filters `WHERE is_published = TRUE` — unpublished profiles return 404
  - `loadAuthor` (for bylines) only sets `Author.Slug` when `IsPublished` is true,
    so templates don't generate links to a 404 author page
  - Author name/avatar still appear in bylines even when unpublished — only the link is omitted
  - Admin previews always show the slug regardless of publish state
