# Multi-Tenancy Implementation

**Date:** 2026-03-04
**Branch:** feat/multi-tenancy

## Summary

Implemented full multi-tenancy support across all 6 planned phases, transforming SmartPress from a single-tenant to a multi-tenant platform.

## Changes

### Phase 1: Database Schema
- Created 4 new migrations (00015-00018): tenants table, user_tenants join table, super admin flag, tenant_id on all content tables
- Created `models/tenant.go` with Tenant, UserTenant, TenantMembership structs
- Updated `models/user.go`: removed Role field, added IsSuperAdmin

### Phase 2: Tenant Resolution Middleware
- Created `middleware/tenant.go`: subdomain-based tenant resolution with Valkey caching
- Updated `middleware/auth.go`: RequireAdmin checks TenantRole, added RequireSuperAdmin
- Updated `session/session.go`: replaced Role with IsSuperAdmin, TenantID, TenantRole
- Updated `config/config.go`: added BaseDomain

### Phase 3: Store Layer
- Created `store/tenant.go` with full CRUD
- Updated all stores (content, template, category, media, design_theme, site_setting, cache_log) to accept tenantID parameter
- Updated `store/user.go`: added ListByTenant, AddToTenant, RemoveFromTenant, GetTenants, GetTenantRole

### Phase 4: Handler Updates
- Updated admin.go, admin_ai.go, admin_media.go to pass sess.TenantID to all store calls
- Updated public.go to use middleware.TenantFromCtx for tenant-aware rendering
- Updated auth.go: login/2FA flow now handles tenant selection (auto-select for 1 tenant, picker for multiple)

### Phase 5: Cache & Engine
- Updated engine.go: RenderPage/RenderPostList now accept tenantID and siteName
- Updated cache/page.go: cache keys namespaced by tenant, added InvalidateAllForTenant

### Phase 6: Super-Admin & Tenant Management
- Created `handlers/admin_tenant.go`: TenantAdmin handler with CRUD, user management, tenant selection
- Created 4 templates: tenant_list, tenant_form, tenant_users, select_tenant
- Updated router.go: new signature with tenant deps, ResolveTenant on public routes, super-admin tenant routes
- Updated base.html sidebar: TenantRole checks, Tenants link for super admins
- Updated users_list.html for UserWithRole data structure
- Fixed all test files to match new API signatures
