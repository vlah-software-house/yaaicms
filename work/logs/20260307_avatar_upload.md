# Avatar Upload

**Date:** 2026-03-07
**Branch:** feat/avatar-upload

## Summary

Added avatar file upload support to the user profile page. Users can upload a
JPEG, PNG, or WebP image which is automatically cropped to a 512x512 square
(using libvips attention-based smart cropping) and converted to optimised WebP.
The processed avatar is stored in S3 and the profile's avatar_url is updated.

## Changes

### Modified Files
- `internal/imaging/imaging.go` — Added `ProcessAvatar()` function: crops to 512x512 square using `vips.InterestingAttention` for smart face/feature detection, exports as WebP at quality 80
- `internal/handlers/admin.go` — Added `AvatarUpload` handler (POST /admin/profile/avatar): validates image type, processes with libvips, uploads to S3 at `avatars/{user_id}.webp`, updates profile avatar_url
- `internal/store/user_profile.go` — Added `UpdateAvatarURL()` method with UPSERT for lazy profile creation
- `internal/router/router.go` — Added `POST /admin/profile/avatar` route
- `internal/render/templates/admin/profile.html` — Replaced text URL input with file upload UI using AlpineJS: file picker button, live preview update, client-side validation, hidden field keeps avatar_url in sync with the main form

## Architecture Decisions
- 512px square: covers 3x Retina at typical 160px CSS display sizes
- Smart crop via `vips.InterestingAttention`: detects faces/salient features for crop center
- S3 key `avatars/{user_id}.webp`: one avatar per user, overwrites on re-upload
- Not tracked in media table: avatars are user assets, not content media items
- AlpineJS for client-side upload UX: async fetch to dedicated endpoint, live preview update without page reload
- Hidden `avatar_url` input keeps the value in sync so the main profile form submit still works
