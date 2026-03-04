// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package middleware

import (
	"context"
	"net/http"

	"yaaicms/internal/session"
)

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey string

const (
	// SessionKey is the context key for the session data.
	SessionKey contextKey = "session"
)

// LoadSession retrieves the session from Valkey and stores it in the
// request context. Downstream handlers can access it via SessionFromCtx().
// This middleware does NOT enforce authentication — it just loads the
// session if one exists.
func LoadSession(store *session.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data, err := store.Get(r.Context(), r)
			if err != nil {
				// Log but don't block — treat as unauthenticated.
				next.ServeHTTP(w, r)
				return
			}

			if data != nil {
				ctx := context.WithValue(r.Context(), SessionKey, data)
				r = r.WithContext(ctx)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth redirects unauthenticated users to the login page.
// Must be applied after LoadSession in the middleware chain.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFromCtx(r.Context())
		if sess == nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Require2FA redirects users who haven't completed 2FA setup to the
// setup page. Must be applied after RequireAuth.
func Require2FA(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFromCtx(r.Context())
		if sess != nil && !sess.TwoFADone {
			// User is logged in but hasn't completed 2FA — redirect to setup.
			http.Redirect(w, r, "/admin/2fa/setup", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireAdmin returns 403 if the authenticated user is not a tenant admin.
// Must be applied after RequireAuth and Require2FA.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFromCtx(r.Context())
		if sess == nil || (sess.TenantRole != "admin" && !sess.IsSuperAdmin) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireSuperAdmin returns 403 if the authenticated user is not a platform super admin.
// Used for tenant management routes.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFromCtx(r.Context())
		if sess == nil || !sess.IsSuperAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SessionFromCtx extracts the session data from the request context.
// Returns nil if no session is loaded (user is not authenticated).
func SessionFromCtx(ctx context.Context) *session.Data {
	data, _ := ctx.Value(SessionKey).(*session.Data)
	return data
}
