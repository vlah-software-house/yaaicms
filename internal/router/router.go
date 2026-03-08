// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package router sets up all HTTP routes and middleware chains for the
// YaaiCMS CMS. It organizes routes into public and admin groups with
// appropriate middleware stacks.
package router

import (
	"io/fs"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"yaaicms/internal/handlers"
	"yaaicms/internal/middleware"
	"yaaicms/internal/session"
	"yaaicms/internal/store"
	"yaaicms/web"
)

// New creates and returns the configured Chi router with all middleware
// and route groups wired up. Set secureCookies to true in production to
// mark CSRF cookies as Secure (HTTPS-only).
func New(sessionStore *session.Store, admin *handlers.Admin, auth *handlers.Auth, public *handlers.Public, tenant *handlers.TenantAdmin, tenantStore *store.TenantStore, domainResolver middleware.DomainResolver, valkeyClient *redis.Client, baseDomain string, secureCookies bool) chi.Router {
	r := chi.NewRouter()

	// Rate limiters: auth endpoints are tightly limited (brute-force protection),
	// AI endpoints get a generous limit (authenticated users, slow operations).
	authLimiter := middleware.NewRateLimiter(10, 1*time.Minute)
	aiLimiter := middleware.NewRateLimiter(30, 1*time.Minute)

	// Global middleware — applied to every request.
	r.Use(middleware.Recoverer)
	r.Use(middleware.SecureHeaders)
	r.Use(middleware.Logger)
	r.Use(middleware.LoadSession(sessionStore))

	// Static assets (compiled CSS, vendored JS) — served from the embedded FS.
	// In production the Docker build populates these; in development the
	// templates use CDN instead, so 404s on /static/ are harmless.
	staticFS, _ := fs.Sub(web.StaticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health check — no auth, no CSRF.
	r.Get("/health", healthHandler)

	// Admin routes — require authentication and CSRF protection.
	r.Route("/admin", func(r chi.Router) {
		r.Use(middleware.NewCSRF(secureCookies))

		// Auth pages — rate-limited to prevent brute force.
		r.Group(func(r chi.Router) {
			r.Use(authLimiter.Middleware)
			r.Get("/login", auth.LoginPage)
			r.Post("/login", auth.LoginSubmit)
			r.Post("/logout", auth.Logout)
		})

		// 2FA — requires auth but NOT completed 2FA. Rate-limited.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Use(authLimiter.Middleware)
			r.Get("/2fa/setup", auth.TwoFASetupPage)
			r.Get("/2fa/verify", auth.TwoFAVerifyPage)
			r.Post("/2fa/verify", auth.TwoFAVerifySubmit)
		})

		// Tenant selection — requires auth + 2FA but no tenant context yet.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Use(middleware.Require2FA)
			r.Get("/select-tenant", tenant.SelectTenantPage)
			r.Post("/select-tenant", tenant.SelectTenantSubmit)
		})

		// Authenticated + 2FA-verified admin area.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)
			r.Use(middleware.Require2FA)

			// Dashboard
			r.Get("/", admin.Dashboard)
			r.Get("/dashboard", admin.Dashboard)

			// Profile (self-service, any authenticated user)
			r.Get("/profile", admin.ProfilePage)
			r.Post("/profile", admin.ProfileSave)
			r.Post("/profile/avatar", admin.AvatarUpload)

			// Posts
			r.Route("/posts", func(r chi.Router) {
				r.Get("/", admin.PostsList)
				r.Get("/new", admin.PostNew)
				r.Post("/", admin.PostCreate)
				r.Get("/{id}", admin.PostEdit)
				r.Put("/{id}", admin.PostUpdate)
				r.Delete("/{id}", admin.PostDelete)
			})

			// Pages
			r.Route("/pages", func(r chi.Router) {
				r.Get("/", admin.PagesList)
				r.Get("/new", admin.PageNew)
				r.Post("/", admin.PageCreate)
				r.Get("/{id}", admin.PageEdit)
				r.Put("/{id}", admin.PageUpdate)
				r.Delete("/{id}", admin.PageDelete)
			})

			// Templates (AI Design)
			r.Route("/templates", func(r chi.Router) {
				r.Get("/", admin.TemplatesList)
				r.Get("/ai", admin.AITemplatePage)
				r.Get("/new", admin.TemplateNew)
				r.Get("/prefixes", admin.TemplatePrefixes)
				r.Post("/", admin.TemplateCreate)
				r.Post("/preview", admin.TemplatePreview)
				r.Get("/{id}", admin.TemplateEdit)
				r.Put("/{id}", admin.TemplateUpdate)
				r.Delete("/{id}", admin.TemplateDelete)
				r.Post("/{id}/activate", admin.TemplateActivate)
			})

			// Media Library
			r.Route("/media", func(r chi.Router) {
				r.Get("/", admin.MediaLibrary)
				r.Get("/json", admin.MediaListJSON)
				r.Post("/", admin.MediaUpload)
				r.Delete("/{id}", admin.MediaDelete)
				r.Get("/{id}/url", admin.MediaServe)
				r.Post("/{id}/regenerate", admin.MediaRegenerateVariants)
				r.Post("/regenerate-all", admin.MediaRegenerateBulk)
			})

			// Content Revisions
			r.Post("/revisions/{revisionID}/restore", admin.RevisionRestore)
			r.Put("/revisions/{revisionID}/title", admin.RevisionUpdateTitle)

			// Template Revisions
			r.Post("/template-revisions/{revisionID}/restore", admin.TemplateRevisionRestore)
			r.Put("/template-revisions/{revisionID}/title", admin.TemplateRevisionUpdateTitle)

			// Categories
			r.Route("/categories", func(r chi.Router) {
				r.Get("/", admin.CategoriesList)
				r.Post("/", admin.CategoryCreate)
				r.Put("/{id}", admin.CategoryUpdate)
				r.Delete("/{id}", admin.CategoryDelete)
				r.Post("/reorder", admin.CategoryReorder)
			})

			// Menus
			r.Route("/menus", func(r chi.Router) {
				r.Get("/", admin.MenusPage)
				r.Post("/items", admin.MenuItemCreate)
				r.Put("/items/{id}", admin.MenuItemUpdate)
				r.Delete("/items/{id}", admin.MenuItemDelete)
				r.Post("/items/reorder", admin.MenuItemReorder)
				r.Get("/content-list", admin.MenuContentList)
			})

			// User management — admin only
			r.Route("/users", func(r chi.Router) {
				r.Use(middleware.RequireAdmin)
				r.Get("/", admin.UsersList)
				r.Get("/new", admin.UserNew)
				r.Post("/", admin.UserCreate)
				r.Post("/{id}/reset-2fa", admin.UserResetTwoFA)
				r.With(middleware.RequireSuperAdmin).Delete("/{id}", admin.UserDelete)
			})

			// AI Assistant (content editor helpers + template builder)
			r.Route("/ai", func(r chi.Router) {
				r.Use(aiLimiter.Middleware)
				r.Post("/set-provider", admin.AISetProvider)
				r.Get("/provider-status", admin.AIProviderStatus)
				r.Get("/image-providers", admin.AIImageProviders)
				r.Post("/generate-content", admin.AIGenerateContent)
				r.Post("/generate-image", admin.AIGenerateImage)
				r.Post("/suggest-title", admin.AISuggestTitle)
				r.Post("/generate-excerpt", admin.AIGenerateExcerpt)
				r.Post("/seo-metadata", admin.AISEOMetadata)
				r.Post("/rewrite", admin.AIRewrite)
				r.Post("/extract-tags", admin.AIExtractTags)
				r.Post("/generate-template", admin.AITemplateGenerate)
				r.Post("/save-template", admin.AITemplateSave)
				r.Get("/preview-content", admin.AIPreviewContentList)

				// Design themes (style briefs for visual consistency)
				r.Get("/themes", admin.AIThemeList)
				r.Post("/themes", admin.AIThemeCreate)
				r.Get("/active-theme", admin.AIActiveTheme)
				r.Put("/themes/{id}", admin.AIThemeUpdate)
				r.Post("/themes/{id}/activate", admin.AIThemeActivate)
				r.Post("/themes/{id}/deactivate", admin.AIThemeDeactivate)
				r.Delete("/themes/{id}", admin.AIThemeDelete)
				r.Post("/restyle-preview", admin.AIRestylePreview)
			})

			// Settings
			r.Get("/settings", admin.SettingsPage)
			r.Post("/settings", admin.SettingsSave)

			// Help
			r.Get("/help", admin.HelpPage)

			// Tenant management — super-admin only
			r.Route("/tenants", func(r chi.Router) {
				r.Use(middleware.RequireSuperAdmin)
				r.Get("/", tenant.TenantList)
				r.Get("/new", tenant.TenantNew)
				r.Post("/", tenant.TenantCreate)
				r.Get("/{id}", tenant.TenantEdit)
				r.Put("/{id}", tenant.TenantUpdate)
				r.Delete("/{id}", tenant.TenantDelete)
				r.Get("/{id}/users", tenant.TenantUsers)
				r.Post("/{id}/users", tenant.TenantAddUser)
				r.Delete("/{id}/users/{uid}", tenant.TenantRemoveUser)
				r.Get("/{id}/domains", tenant.TenantDomains)
				r.Post("/{id}/domains", tenant.TenantAddDomain)
				r.Delete("/{id}/domains/{did}", tenant.TenantDeleteDomain)
				r.Post("/{id}/domains/{did}/verify", tenant.TenantVerifyDomain)
				r.Post("/{id}/domains/{did}/primary", tenant.TenantSetPrimaryDomain)
				r.Delete("/{id}/domains/{did}/primary", tenant.TenantUnsetPrimaryDomain)
			})
		})
	})

	// Public routes — served by the dynamic template engine.
	// Tenant resolution middleware identifies which tenant to serve based on subdomain.
	// CanonicalRedirect ensures SEO-friendly 301 redirects to the primary domain.
	r.Group(func(r chi.Router) {
		r.Use(middleware.ResolveTenant(tenantStore, domainResolver, valkeyClient, baseDomain))
		r.Use(middleware.CanonicalRedirect(domainResolver, valkeyClient, baseDomain))
		r.Get("/", public.Homepage)
		r.Get("/author/{slug}", public.AuthorPage)
		r.Get("/{slug}", public.Page)
	})

	return r
}

// healthHandler returns a simple JSON health check response.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
