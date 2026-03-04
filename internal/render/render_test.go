// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package render

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yaaicms/internal/middleware"
	"yaaicms/internal/session"

	"github.com/google/uuid"
)

// helperSession returns a session.Data suitable for rendering admin templates.
func helperSession() *session.Data {
	return &session.Data{
		UserID:       uuid.New(),
		Email:        "test@yaaicms.local",
		DisplayName:  "Test User",
		IsSuperAdmin: true,
		TenantRole:   "admin",
		TwoFADone:    true,
	}
}

// helperRequestWithContext builds an *http.Request whose context carries a
// session and a CSRF token, which the embedded templates expect.
func helperRequestWithContext(method, target string, sess *session.Data) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	ctx := req.Context()
	// Set session in context using the middleware's exported key.
	if sess != nil {
		ctx = context.WithValue(ctx, middleware.SessionKey, sess)
	}
	return req.WithContext(ctx)
}

// --------------------------------------------------------------------------
// TestNew — verify renderer creation in dev mode and prod mode
// --------------------------------------------------------------------------

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		devMode bool
	}{
		{"dev mode", true},
		{"prod mode", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rn, err := New(tt.devMode)
			if err != nil {
				t.Fatalf("New(devMode=%v) returned error: %v", tt.devMode, err)
			}
			if rn == nil {
				t.Fatal("New() returned nil renderer")
			}
			if len(rn.templates) == 0 {
				t.Error("renderer has no parsed templates")
			}

			// Verify well-known templates exist.
			for _, name := range []string{"dashboard", "login", "2fa_setup", "2fa_verify"} {
				if _, ok := rn.templates[name]; !ok {
					t.Errorf("expected template %q to be parsed", name)
				}
			}

			// base.html should NOT appear as a standalone template key.
			if _, ok := rn.templates["base"]; ok {
				t.Error("base.html should not be registered as a separate template")
			}
		})
	}
}

// --------------------------------------------------------------------------
// TestNewDevMode — verify isDev template function returns true
// --------------------------------------------------------------------------

func TestNewDevMode(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New(true) error: %v", err)
	}

	// Render login (standalone) and check for CDN URL present in dev mode.
	w := httptest.NewRecorder()
	req := helperRequestWithContext(http.MethodGet, "/admin/login", nil)
	rn.Page(w, req, "login", &PageData{Title: "Login"})

	body := w.Body.String()
	if !strings.Contains(body, "cdn.tailwindcss.com") {
		t.Error("dev mode: expected CDN tailwindcss URL in rendered output")
	}
	if strings.Contains(body, "/static/css/admin.css") {
		t.Error("dev mode: should NOT contain local static asset path")
	}
}

// --------------------------------------------------------------------------
// TestNewProdMode — verify isDev template function returns false
// --------------------------------------------------------------------------

func TestNewProdMode(t *testing.T) {
	rn, err := New(false)
	if err != nil {
		t.Fatalf("New(false) error: %v", err)
	}

	w := httptest.NewRecorder()
	req := helperRequestWithContext(http.MethodGet, "/admin/login", nil)
	rn.Page(w, req, "login", &PageData{Title: "Login"})

	body := w.Body.String()
	if strings.Contains(body, "cdn.tailwindcss.com") {
		t.Error("prod mode: should NOT contain CDN tailwindcss URL")
	}
	if !strings.Contains(body, "/static/css/admin.css") {
		t.Error("prod mode: expected local static asset path in rendered output")
	}
}

// --------------------------------------------------------------------------
// TestPageRendering — full page render of "dashboard" with session data
// --------------------------------------------------------------------------

func TestPageRendering(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sess := helperSession()
	req := helperRequestWithContext(http.MethodGet, "/admin/dashboard", sess)
	w := httptest.NewRecorder()

	rn.Page(w, req, "dashboard", &PageData{
		Title:   "Dashboard",
		Section: "dashboard",
		Session: sess,
		Data:    map[string]any{"PostCount": 5, "PageCount": 3, "MediaCount": 10, "UserCount": 2},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// Full page render should contain the base layout HTML structure.
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("full page render should contain <!DOCTYPE html>")
	}
	if !strings.Contains(body, "YaaiCMS") {
		t.Error("full page render should contain YaaiCMS branding")
	}
	// Dashboard content should be present.
	if !strings.Contains(body, "Welcome back") {
		t.Error("full page render should contain dashboard content")
	}
	// Content-Type header check.
	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
}

// --------------------------------------------------------------------------
// TestHTMXPartialRendering — HTMX requests only render the content block
// --------------------------------------------------------------------------

func TestHTMXPartialRendering(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sess := helperSession()
	req := helperRequestWithContext(http.MethodGet, "/admin/dashboard", sess)
	// Set the HX-Request header to trigger partial rendering.
	req.Header.Set("HX-Request", "true")

	w := httptest.NewRecorder()
	rn.Page(w, req, "dashboard", &PageData{
		Title:   "Dashboard",
		Section: "dashboard",
		Session: sess,
		Data:    map[string]any{"PostCount": 1, "PageCount": 0, "MediaCount": 0, "UserCount": 1},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()

	// HTMX partial should NOT contain full HTML layout.
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Error("HTMX partial should NOT contain <!DOCTYPE html>")
	}
	if strings.Contains(body, "<head>") {
		t.Error("HTMX partial should NOT contain <head> tag")
	}

	// But it should still contain the dashboard content.
	if !strings.Contains(body, "Welcome back") {
		t.Error("HTMX partial should contain dashboard content block")
	}
}

// --------------------------------------------------------------------------
// TestStandaloneTemplates — login, 2fa_setup, 2fa_verify render standalone
// --------------------------------------------------------------------------

func TestStandaloneTemplates(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	standaloneNames := []struct {
		name          string
		expectedTitle string
	}{
		{"login", "Sign In"},
		{"2fa_setup", "Two-Factor"},
		{"2fa_verify", "Two-Factor"},
	}

	for _, tt := range standaloneNames {
		t.Run(tt.name, func(t *testing.T) {
			req := helperRequestWithContext(http.MethodGet, "/admin/"+tt.name, nil)
			w := httptest.NewRecorder()

			rn.Page(w, req, tt.name, &PageData{
				Title: tt.name,
				Data:  map[string]any{},
			})

			if w.Code != http.StatusOK {
				t.Fatalf("template %q: expected 200, got %d", tt.name, w.Code)
			}

			body := w.Body.String()

			// Standalone templates should contain their own <!DOCTYPE html>.
			if !strings.Contains(body, "<!DOCTYPE html>") {
				t.Errorf("template %q: expected standalone HTML with <!DOCTYPE html>", tt.name)
			}

			// Standalone templates should NOT contain sidebar navigation
			// from base.html (the sidebar has a unique class).
			if strings.Contains(body, "lg:flex-shrink-0") {
				t.Errorf("template %q: should NOT contain base layout sidebar", tt.name)
			}
		})
	}
}

// --------------------------------------------------------------------------
// TestMissingTemplate — Page() with nonexistent template returns 500
// --------------------------------------------------------------------------

func TestMissingTemplate(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	req := helperRequestWithContext(http.MethodGet, "/admin/nonexistent", nil)
	w := httptest.NewRecorder()

	rn.Page(w, req, "nonexistent_template", &PageData{
		Title: "Not Found",
	})

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "not found") {
		t.Error("error response should mention template not found")
	}
}

// --------------------------------------------------------------------------
// TestPageDataCSRFInjection — verify CSRF token is injected from context
// --------------------------------------------------------------------------

func TestPageDataCSRFInjection(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Build a request with a known CSRF token in context.
	// The CSRF middleware stores it with an unexported key, but we can use
	// the NewCSRF middleware to set it up, then pass the request through.
	csrfMiddleware := middleware.NewCSRF(false)
	var capturedReq *http.Request
	inner := csrfMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
	}))

	setupReq := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	setupRR := httptest.NewRecorder()
	inner.ServeHTTP(setupRR, setupReq)

	if capturedReq == nil {
		t.Fatal("CSRF middleware did not call inner handler")
	}

	// Extract the CSRF token from the context.
	csrfToken := middleware.CSRFTokenFromCtx(capturedReq.Context())
	if csrfToken == "" {
		t.Fatal("CSRF token not found in context")
	}

	// Now render a standalone template with that context.
	w := httptest.NewRecorder()
	data := &PageData{Title: "Login"}
	rn.Page(w, capturedReq, "login", data)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// The CSRF token should appear in the rendered output (used in hx-headers).
	body := w.Body.String()
	if !strings.Contains(body, csrfToken) {
		t.Error("rendered output should contain the CSRF token from context")
	}

	// Also verify it was injected into the PageData struct.
	if data.CSRFToken != csrfToken {
		t.Errorf("PageData.CSRFToken: got %q, want %q", data.CSRFToken, csrfToken)
	}
}

// --------------------------------------------------------------------------
// TestSessionInjectionFromContext — verify session is injected from context
// --------------------------------------------------------------------------

func TestSessionInjectionFromContext(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sess := helperSession()
	req := helperRequestWithContext(http.MethodGet, "/admin/dashboard", sess)
	w := httptest.NewRecorder()

	// Pass PageData WITHOUT setting Session — it should be injected from context.
	data := &PageData{
		Title:   "Dashboard",
		Section: "dashboard",
		Data:    map[string]any{"PostCount": 0, "PageCount": 0, "MediaCount": 0, "UserCount": 0},
	}
	rn.Page(w, req, "dashboard", data)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Session should have been injected.
	if data.Session == nil {
		t.Error("expected Session to be injected from context")
	}
	if data.Session != nil && data.Session.DisplayName != "Test User" {
		t.Errorf("Session.DisplayName: got %q, want %q", data.Session.DisplayName, "Test User")
	}

	// The rendered body should contain the user's display name.
	body := w.Body.String()
	if !strings.Contains(body, "Test User") {
		t.Error("rendered output should contain session DisplayName")
	}
}

// --------------------------------------------------------------------------
// TestIsHTMXHelper — internal helper detects HX-Request header
// --------------------------------------------------------------------------

func TestIsHTMXHelper(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected bool
	}{
		{"no header", "", false},
		{"header true", "true", true},
		{"header false", "false", false},
		{"header random", "yes", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("HX-Request", tt.header)
			}
			if got := isHTMX(req); got != tt.expected {
				t.Errorf("isHTMX(): got %v, want %v", got, tt.expected)
			}
		})
	}
}

// --------------------------------------------------------------------------
// TestRendererTemplateCount — verify we have the expected number of templates
// --------------------------------------------------------------------------

func TestRendererTemplateCount(t *testing.T) {
	rn, err := New(true)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// We expect all templates except base.html to be registered.
	// Known templates: dashboard, login, 2fa_setup, 2fa_verify, posts_list,
	// pages_list, users_list, content_form, templates_list, template_form,
	// template_ai, settings, media_library
	// That's 13 templates (base.html is excluded).
	expectedMin := 13
	if len(rn.templates) < expectedMin {
		t.Errorf("expected at least %d templates, got %d", expectedMin, len(rn.templates))
	}
}
