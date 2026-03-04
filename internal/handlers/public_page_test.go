// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yaaicms/internal/cache"
	"yaaicms/internal/models"
)

// TestHomepageDefault verifies that when no published content exists and no
// templates can render, the homepage handler returns 200 with the default
// YaaiCMS fallback HTML.
func TestHomepageDefault(t *testing.T) {
	env := newTestEnv(t)

	// Ensure no published posts or "home" page exist. The seeded database
	// may contain published content and active templates, so we must
	// temporarily hide all published content to reach the default fallback.
	cleanContent(t, env.DB, "home")

	// Unpublish all posts so ListPublishedByType returns empty.
	_, err := env.DB.Exec("UPDATE content SET status = 'draft' WHERE status = 'published'")
	if err != nil {
		t.Fatalf("unpublish all content: %v", err)
	}
	t.Cleanup(func() {
		// Restore published status for seeded content.
		env.DB.Exec("UPDATE content SET status = 'published', published_at = NOW() WHERE status = 'draft'")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Clear any cached homepage from previous test runs.
	env.PageCache.InvalidateHomepage(req.Context())

	env.Public.Homepage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "YaaiCMS") {
		t.Error("response body should contain 'YaaiCMS'")
	}
	if !strings.Contains(body, "Your site is running") {
		t.Error("response body should contain default setup message")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
}

// TestHomepageFallback verifies that when a published page with slug "home"
// exists but no active page template is installed, the homepage handler
// cannot render it via the engine (no active page template) and falls back
// to the static default page.
func TestHomepageFallback(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	// Clean up any residual test data.
	cleanContent(t, env.DB, "home")
	t.Cleanup(func() { cleanContent(t, env.DB, "home") })

	// Create a published page with slug "home".
	_, err := env.ContentStore.Create(testTenantID, &models.Content{
		Type:     models.ContentTypePage,
		Title:    "Home Page",
		Slug:     "home",
		Body:     "<p>Welcome to the homepage</p>",
		Status:   models.ContentStatusPublished,
		AuthorID: authorID,
	})
	if err != nil {
		t.Fatalf("create home page: %v", err)
	}

	// Ensure no active page template exists — deactivate all page templates.
	// Without an active page template, RenderPage will fail and the handler
	// falls back to the static default.
	cleanTemplates(t, env.DB, "__test_home_page_tmpl")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Clear cached homepage.
	env.PageCache.InvalidateHomepage(req.Context())

	env.Public.Homepage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	// Without an active page template, the engine.RenderPage call will fail
	// and the handler falls through to the static default.
	if !strings.Contains(body, "YaaiCMS") {
		t.Error("response body should contain 'YaaiCMS' fallback text")
	}
}

// TestPageNotFound verifies that requesting a nonexistent slug returns 404.
func TestPageNotFound(t *testing.T) {
	env := newTestEnv(t)

	slug := "__test_nonexistent_slug_12345"
	cleanContent(t, env.DB, slug)

	req := httptest.NewRequest(http.MethodGet, "/"+slug, nil)
	req = withChiURLParam(req, "slug", slug)
	rec := httptest.NewRecorder()

	// Ensure nothing is cached for this slug.
	env.PageCache.InvalidatePage(req.Context(), cache.SlugKey(testTenantID.String(), slug))

	env.Public.Page(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// TestPagePublished creates a published page and an active page template,
// then verifies the Page handler returns 200 with the rendered content.
func TestPagePublished(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	slug := "__test_published_page"
	tmplName := "__test_page_template"

	// Clean up before and after.
	cleanContent(t, env.DB, slug)
	cleanTemplates(t, env.DB, tmplName)
	t.Cleanup(func() {
		cleanContent(t, env.DB, slug)
		cleanTemplates(t, env.DB, tmplName)
	})

	// Create a published page.
	_, err := env.ContentStore.Create(testTenantID, &models.Content{
		Type:     models.ContentTypePage,
		Title:    "Test Published Page",
		Slug:     slug,
		Body:     "<p>Hello from the published page</p>",
		Status:   models.ContentStatusPublished,
		AuthorID: authorID,
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}

	// Create and activate a page template.
	tmpl, err := env.TemplateStore.Create(testTenantID, &models.Template{
		Name:        tmplName,
		Type:        models.TemplateTypePage,
		HTMLContent: `<html><head><title>{{.Title}}</title></head><body>{{.Body}}</body></html>`,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := env.TemplateStore.Activate(testTenantID, tmpl.ID); err != nil {
		t.Fatalf("activate template: %v", err)
	}

	// Invalidate the engine L1 cache so it picks up the new template.
	env.Engine.InvalidateAllTemplates()

	req := httptest.NewRequest(http.MethodGet, "/"+slug, nil)
	req = withChiURLParam(req, "slug", slug)
	rec := httptest.NewRecorder()

	// Ensure L2 cache is clear for this slug.
	env.PageCache.InvalidatePage(req.Context(), cache.SlugKey(testTenantID.String(), slug))

	env.Public.Page(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Test Published Page") {
		t.Error("response body should contain the page title")
	}
	if !strings.Contains(body, "Hello from the published page") {
		t.Error("response body should contain the page body")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
}

// TestPageDraftNotVisible verifies that a draft page is not visible on the
// public site — FindBySlug only returns published content, so drafts
// should result in a 404.
func TestPageDraftNotVisible(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	slug := "__test_draft_page"

	cleanContent(t, env.DB, slug)
	t.Cleanup(func() { cleanContent(t, env.DB, slug) })

	// Create a draft page.
	_, err := env.ContentStore.Create(testTenantID, &models.Content{
		Type:     models.ContentTypePage,
		Title:    "Draft Page Should Be Hidden",
		Slug:     slug,
		Body:     "<p>This is a draft and should not be visible</p>",
		Status:   models.ContentStatusDraft,
		AuthorID: authorID,
	})
	if err != nil {
		t.Fatalf("create draft content: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/"+slug, nil)
	req = withChiURLParam(req, "slug", slug)
	rec := httptest.NewRecorder()

	// Ensure L2 cache is clear for this slug.
	env.PageCache.InvalidatePage(req.Context(), cache.SlugKey(testTenantID.String(), slug))

	env.Public.Page(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want %d — drafts must not be publicly visible", rec.Code, http.StatusNotFound)
	}
}

// TestHomepageCacheHit verifies that when the page cache already contains
// HTML for the homepage key, the handler serves the cached content directly
// without querying the database or rendering templates.
func TestHomepageCacheHit(t *testing.T) {
	env := newTestEnv(t)

	cachedHTML := `<!DOCTYPE html><html><body><h1>Cached Homepage</h1></body></html>`

	ctx := context.Background()
	env.PageCache.Set(ctx, cache.HomepageKey(testTenantID.String()), []byte(cachedHTML))
	t.Cleanup(func() { env.PageCache.InvalidateHomepage(ctx) })

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	env.Public.Homepage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()
	if body != cachedHTML {
		t.Errorf("expected cached HTML to be served exactly.\ngot:  %q\nwant: %q", body, cachedHTML)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type: got %q, want %q", ct, "text/html; charset=utf-8")
	}
}
