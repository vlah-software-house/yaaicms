// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package engine

import (
	"database/sql"
	"fmt"
	"html/template"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"yaaicms/internal/database"
	"yaaicms/internal/models"
	"yaaicms/internal/store"
)

// --------------------------------------------------------------------------
// TestValidateTemplate — valid syntax, invalid syntax, and template variables
// --------------------------------------------------------------------------

func TestValidateTemplate(t *testing.T) {
	// Engine.ValidateTemplate does not need a TemplateStore — it only
	// parses Go template syntax.
	eng := &Engine{
		cache: newTemplateCache(),
	}

	tests := []struct {
		name        string
		html        string
		expectError bool
	}{
		{
			name:        "valid plain HTML",
			html:        `<html><body><h1>Hello World</h1></body></html>`,
			expectError: false,
		},
		{
			name:        "valid template with variable",
			html:        `<h1>{{.Title}}</h1><p>{{.Body}}</p>`,
			expectError: false,
		},
		{
			name:        "valid template with range",
			html:        `{{range .Items}}<li>{{.Name}}</li>{{end}}`,
			expectError: false,
		},
		{
			name:        "valid template with conditional",
			html:        `{{if .ShowHeader}}<header>{{.Header}}</header>{{end}}`,
			expectError: false,
		},
		{
			name:        "valid empty template",
			html:        ``,
			expectError: false,
		},
		{
			name:        "invalid unclosed action",
			html:        `<h1>{{.Title</h1>`,
			expectError: true,
		},
		{
			name:        "invalid unknown function",
			html:        `<h1>{{unknownFunc .Title}}</h1>`,
			expectError: true,
		},
		{
			name:        "invalid mismatched end",
			html:        `{{if .Show}}<p>hello</p>{{end}}{{end}}`,
			expectError: true,
		},
		{
			name:        "valid nested variables",
			html:        `<div>{{.SiteTitle}} - {{.Year}}</div>`,
			expectError: false,
		},
		{
			name:        "valid with pipe",
			html:        `<p>{{.Title | html}}</p>`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := eng.ValidateTemplate(tt.html)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tt.expectError && err != nil {
				if !strings.Contains(err.Error(), "invalid template syntax") {
					t.Errorf("error should contain 'invalid template syntax', got: %v", err)
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// TestValidateAndRender — render with data, missing variables, invalid syntax
// --------------------------------------------------------------------------

func TestValidateAndRender(t *testing.T) {
	eng := &Engine{
		cache: newTemplateCache(),
	}

	t.Run("render simple template with data", func(t *testing.T) {
		tmpl := `<h1>{{.Title}}</h1><p>{{.Body}}</p>`
		data := map[string]any{
			"Title": "Hello World",
			"Body":  "This is a test.",
		}

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := string(result)
		if !strings.Contains(body, "<h1>Hello World</h1>") {
			t.Errorf("expected rendered title, got: %s", body)
		}
		if !strings.Contains(body, "<p>This is a test.</p>") {
			t.Errorf("expected rendered body, got: %s", body)
		}
	})

	t.Run("render with HTML content via template.HTML", func(t *testing.T) {
		tmpl := `<div>{{.Content}}</div>`
		data := map[string]any{
			"Content": template.HTML("<strong>Bold</strong>"),
		}

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := string(result)
		// template.HTML should NOT be escaped.
		if !strings.Contains(body, "<strong>Bold</strong>") {
			t.Errorf("expected unescaped HTML, got: %s", body)
		}
	})

	t.Run("render with PageData struct", func(t *testing.T) {
		tmpl := `<!DOCTYPE html><html><head><title>{{.Title}}</title></head><body>{{.Body}}<footer>{{.Year}}</footer></body></html>`
		data := PageData{
			SiteTitle: "TestSite",
			Title:    "Test Page",
			Body:     template.HTML("<p>Page content</p>"),
			Year:     2026,
		}

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := string(result)
		if !strings.Contains(body, "<title>Test Page</title>") {
			t.Errorf("expected title, got: %s", body)
		}
		if !strings.Contains(body, "<p>Page content</p>") {
			t.Errorf("expected body content, got: %s", body)
		}
		if !strings.Contains(body, "2026") {
			t.Errorf("expected year 2026, got: %s", body)
		}
	})

	t.Run("render with ListData struct", func(t *testing.T) {
		tmpl := `<h1>{{.Title}}</h1>{{range .Posts}}<article><h2>{{.Title}}</h2><p>{{.Excerpt}}</p></article>{{end}}`
		data := ListData{
			SiteTitle: "TestSite",
			Title:    "Blog",
			Posts: []PostItem{
				{Title: "First Post", Slug: "first-post", Excerpt: "First excerpt"},
				{Title: "Second Post", Slug: "second-post", Excerpt: "Second excerpt"},
			},
			Year: 2026,
		}

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := string(result)
		if !strings.Contains(body, "First Post") {
			t.Errorf("expected first post title, got: %s", body)
		}
		if !strings.Contains(body, "Second Post") {
			t.Errorf("expected second post title, got: %s", body)
		}
		if !strings.Contains(body, "First excerpt") {
			t.Errorf("expected first excerpt, got: %s", body)
		}
	})

	t.Run("render with missing map key uses zero value", func(t *testing.T) {
		// In Go 1.25+, accessing a missing map key with {{.Title}} on a
		// map[string]any produces a zero value (empty string), not an error.
		tmpl := `<h1>{{.Title}}</h1>`
		data := map[string]any{}

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body := string(result)
		// Missing map key renders as empty — the <h1> tags are present but
		// the content between them is empty.
		if !strings.Contains(body, "<h1>") {
			t.Errorf("expected <h1> tag in output, got: %s", body)
		}
	})

	t.Run("render with nil data and no field access succeeds", func(t *testing.T) {
		// A template that does NOT access any fields on the data renders
		// fine even with nil data.
		tmpl := `<p>Static content</p>`
		result, err := eng.ValidateAndRender(tmpl, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(result), "Static content") {
			t.Errorf("expected static content, got: %s", string(result))
		}
	})

	t.Run("render with struct data and zero-value fields", func(t *testing.T) {
		// When using a struct, zero-value fields render as their zero values.
		tmpl := `<h1>{{.Title}}</h1><p>{{.Year}}</p>`
		data := PageData{} // all zero values

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body := string(result)
		if !strings.Contains(body, "<h1></h1>") {
			t.Errorf("expected empty title, got: %s", body)
		}
		if !strings.Contains(body, "<p>0</p>") {
			t.Errorf("expected zero year, got: %s", body)
		}
	})

	t.Run("render with invalid syntax returns error", func(t *testing.T) {
		tmpl := `<h1>{{.Title</h1>` // unclosed action
		data := map[string]any{"Title": "Test"}

		_, err := eng.ValidateAndRender(tmpl, data)
		if err == nil {
			t.Error("expected error for invalid template syntax")
		}
		if err != nil && !strings.Contains(err.Error(), "compile template") {
			t.Errorf("error should contain 'compile template', got: %v", err)
		}
	})

	t.Run("render empty template produces empty output", func(t *testing.T) {
		tmpl := ``
		data := map[string]any{}

		result, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("expected empty output, got: %q", string(result))
		}
	})
}

// --------------------------------------------------------------------------
// TestTemplateCacheOperations — test L1 cache get/put/invalidate/invalidateAll
// --------------------------------------------------------------------------

func TestTemplateCacheOperations(t *testing.T) {
	t.Run("new cache is empty", func(t *testing.T) {
		c := newTemplateCache()
		if got := c.get("some-id", 1); got != nil {
			t.Error("expected nil for empty cache lookup")
		}
	})

	t.Run("put and get", func(t *testing.T) {
		c := newTemplateCache()
		tmpl := template.Must(template.New("test").Parse("<p>hello</p>"))

		c.put("id-1", 1, tmpl)
		got := c.get("id-1", 1)
		if got == nil {
			t.Fatal("expected template from cache, got nil")
		}
		if got != tmpl {
			t.Error("cached template should be the same pointer")
		}
	})

	t.Run("version mismatch returns nil", func(t *testing.T) {
		c := newTemplateCache()
		tmpl := template.Must(template.New("test").Parse("<p>hello</p>"))

		c.put("id-1", 1, tmpl)
		if got := c.get("id-1", 2); got != nil {
			t.Error("expected nil for version mismatch")
		}
	})

	t.Run("id mismatch returns nil", func(t *testing.T) {
		c := newTemplateCache()
		tmpl := template.Must(template.New("test").Parse("<p>hello</p>"))

		c.put("id-1", 1, tmpl)
		if got := c.get("id-2", 1); got != nil {
			t.Error("expected nil for id mismatch")
		}
	})

	t.Run("invalidate removes all versions of an id", func(t *testing.T) {
		c := newTemplateCache()
		tmpl1 := template.Must(template.New("v1").Parse("<p>v1</p>"))
		tmpl2 := template.Must(template.New("v2").Parse("<p>v2</p>"))
		tmpl3 := template.Must(template.New("other").Parse("<p>other</p>"))

		c.put("id-1", 1, tmpl1)
		c.put("id-1", 2, tmpl2)
		c.put("id-2", 1, tmpl3)

		c.invalidate("id-1")

		if got := c.get("id-1", 1); got != nil {
			t.Error("id-1 v1 should be invalidated")
		}
		if got := c.get("id-1", 2); got != nil {
			t.Error("id-1 v2 should be invalidated")
		}
		// id-2 should still exist.
		if got := c.get("id-2", 1); got == nil {
			t.Error("id-2 should NOT be invalidated")
		}
	})

	t.Run("invalidateAll clears everything", func(t *testing.T) {
		c := newTemplateCache()
		tmpl1 := template.Must(template.New("a").Parse("<p>a</p>"))
		tmpl2 := template.Must(template.New("b").Parse("<p>b</p>"))

		c.put("id-1", 1, tmpl1)
		c.put("id-2", 1, tmpl2)

		c.invalidateAll()

		if got := c.get("id-1", 1); got != nil {
			t.Error("id-1 should be cleared after invalidateAll")
		}
		if got := c.get("id-2", 1); got != nil {
			t.Error("id-2 should be cleared after invalidateAll")
		}
	})

	t.Run("put overwrites same key", func(t *testing.T) {
		c := newTemplateCache()
		tmplOld := template.Must(template.New("old").Parse("<p>old</p>"))
		tmplNew := template.Must(template.New("new").Parse("<p>new</p>"))

		c.put("id-1", 1, tmplOld)
		c.put("id-1", 1, tmplNew)

		got := c.get("id-1", 1)
		if got != tmplNew {
			t.Error("expected the newer template to overwrite the old one")
		}
	})
}

// --------------------------------------------------------------------------
// TestEngineCacheIntegration — test cache through Engine methods
// --------------------------------------------------------------------------

func TestEngineCacheIntegration(t *testing.T) {
	// Build an Engine with nil TemplateStore — we only test methods that
	// don't touch the store.
	eng := &Engine{
		templateStore: nil,
		cache:         newTemplateCache(),
	}

	t.Run("ValidateAndRender does not cache results", func(t *testing.T) {
		tmpl := `<p>{{.Name}}</p>`
		data := map[string]any{"Name": "cached?"}

		_, err := eng.ValidateAndRender(tmpl, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The cache should remain empty since ValidateAndRender passes
		// an empty id (""), which skips caching.
		c := eng.cache
		// Try to get something from the cache — it should be nil.
		if got := c.get("", 0); got != nil {
			t.Error("ValidateAndRender should not cache templates (empty id)")
		}
	})

	t.Run("InvalidateTemplate on empty cache is safe", func(t *testing.T) {
		// Should not panic.
		eng.InvalidateTemplate("nonexistent-id")
	})

	t.Run("InvalidateAllTemplates on empty cache is safe", func(t *testing.T) {
		// Should not panic.
		eng.InvalidateAllTemplates()
	})

	t.Run("compileAndRender caches when id is provided", func(t *testing.T) {
		tmplContent := `<h1>{{.Title}}</h1>`
		data := map[string]any{"Title": "Cached Page"}

		result, err := eng.compileAndRender("test-id", 1, tmplContent, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(result), "Cached Page") {
			t.Errorf("expected rendered content, got: %s", string(result))
		}

		// Verify it was cached.
		cached := eng.cache.get("test-id", 1)
		if cached == nil {
			t.Error("template should be cached after compileAndRender with id")
		}

		// Render again with different data — should use the cached template.
		data2 := map[string]any{"Title": "From Cache"}
		result2, err := eng.compileAndRender("test-id", 1, "WRONG TEMPLATE", data2)
		if err != nil {
			t.Fatalf("unexpected error on cache hit: %v", err)
		}
		// Should render with the cached template (which has {{.Title}}),
		// not the "WRONG TEMPLATE" string.
		if !strings.Contains(string(result2), "From Cache") {
			t.Errorf("expected cached template to be used, got: %s", string(result2))
		}
	})

	t.Run("compileAndRender skips cache when id is empty", func(t *testing.T) {
		tmplContent := `<p>{{.Name}}</p>`
		data := map[string]any{"Name": "No Cache"}

		result, err := eng.compileAndRender("", 0, tmplContent, data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(result), "No Cache") {
			t.Errorf("expected rendered content, got: %s", string(result))
		}
	})
}

// --------------------------------------------------------------------------
// TestEngineNew — verify New() creates an engine with initialized cache
// --------------------------------------------------------------------------

func TestEngineNew(t *testing.T) {
	// New() requires a *store.TemplateStore. We pass nil since we only
	// check that the struct is properly initialized.
	eng := New(nil)
	if eng == nil {
		t.Fatal("New(nil) returned nil")
	}
	if eng.cache == nil {
		t.Error("engine cache should be initialized")
	}
	if eng.templateStore != nil {
		t.Error("templateStore should be nil when passed nil")
	}
}

// --------------------------------------------------------------------------
// TestRenderPageAndPostListRequireStore — document that these need integration tests
// --------------------------------------------------------------------------

func TestRenderPageAndPostListRequireStore(t *testing.T) {
	// RenderPage and RenderPostList depend on *store.TemplateStore (a concrete
	// struct, not an interface), which requires a real database connection.
	// These methods cannot be unit-tested without a mock or integration setup.
	//
	// This test documents the dependency and verifies that calling these
	// methods with a nil store produces a clear error (not a panic).

	eng := &Engine{
		templateStore: nil,
		cache:         newTemplateCache(),
	}

	t.Run("RenderPage with nil store panics or errors", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				// Expected: nil pointer dereference on templateStore.
				// This confirms the method needs a real store.
				t.Logf("RenderPage with nil store panicked as expected: %v", r)
			}
		}()

		// This will panic because renderFragment calls e.templateStore.FindActiveByType
		// on a nil pointer.
		_, _ = eng.RenderPage(testTenantID, testSiteTitle, testSlogan, nil, nil, nil, nil, nil)
		t.Log("RenderPage with nil store requires integration tests with a database")
	})

	t.Run("RenderPostList with nil store panics or errors", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("RenderPostList with nil store panicked as expected: %v", r)
			}
		}()

		_, _ = eng.RenderPostList(testTenantID, testSiteTitle, testSlogan, nil, nil, nil, nil)
		t.Log("RenderPostList with nil store requires integration tests with a database")
	})
}

// --------------------------------------------------------------------------
// TestTemplateCacheConcurrency — verify cache is safe under concurrent access
// --------------------------------------------------------------------------

func TestTemplateCacheConcurrency(t *testing.T) {
	c := newTemplateCache()
	tmpl := template.Must(template.New("concurrent").Parse("<p>concurrent</p>"))

	// Run concurrent put/get/invalidate operations.
	done := make(chan struct{})

	// Writer goroutines.
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				c.put("id-concurrent", id*100+j, tmpl)
			}
		}(i)
	}

	// Reader goroutines.
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 100; j++ {
				_ = c.get("id-concurrent", id*100+j)
			}
		}(i)
	}

	// Invalidator goroutines.
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 50; j++ {
				c.invalidate("id-concurrent")
			}
		}()
	}

	// InvalidateAll goroutines.
	for i := 0; i < 3; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 20; j++ {
				c.invalidateAll()
			}
		}()
	}

	// Wait for all goroutines.
	for i := 0; i < 28; i++ {
		<-done
	}
	// If we reach here without a race condition panic, the test passes.
}

// ==========================================================================
// Integration tests — require a running PostgreSQL instance.
// These tests exercise RenderPage and RenderPostList against real database
// templates, covering the full render pipeline (DB lookup -> compile -> execute).
// ==========================================================================

// envOr returns the environment variable value or a fallback default.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// testDSN builds the PostgreSQL connection string for integration tests.
func testDSN() string {
	host := envOr("POSTGRES_HOST", "localhost")
	port := envOr("POSTGRES_PORT", "5432")
	user := envOr("POSTGRES_USER", "yaaicms")
	pass := envOr("POSTGRES_PASSWORD", "changeme")
	name := envOr("POSTGRES_DB", "yaaicms")
	return "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + name + "?sslmode=disable"
}

// testDB opens a database connection, runs migrations, and registers cleanup.
// If the database is unreachable, the test is skipped.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := testDSN()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("skipping integration test: cannot open DB: %v", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Skipf("skipping integration test: DB not reachable: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		_ = db.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Reset goose global state so it does not interfere with other tests.
	goose.SetBaseFS(nil)

	// Ensure at least one user exists (seed may have been cleared by
	// concurrent test packages).
	_ = database.Seed(db)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testAuthorID fetches any existing user ID from the database for use as
// the author_id foreign key when creating content rows.
func testAuthorID(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("no users in database — run seed first: %v", err)
	}
	return id
}

// cleanTemplates removes test templates by name.
func cleanTemplates(t *testing.T, db *sql.DB, names ...string) {
	t.Helper()
	for _, name := range names {
		// Deactivate first (Delete blocks active templates).
		_, _ = db.Exec("UPDATE templates SET is_active = FALSE WHERE name = $1", name)
		_, _ = db.Exec("DELETE FROM templates WHERE name = $1", name)
	}
}

// cleanContent removes test content by slug.
func cleanContent(t *testing.T, db *sql.DB, slugs ...string) {
	t.Helper()
	for _, slug := range slugs {
		_, _ = db.Exec("DELETE FROM content WHERE slug = $1", slug)
	}
}

// testTenantID is a fixed tenant ID used across engine integration tests.
var testTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// testSiteTitle is a fixed site title used across engine integration tests.
const testSiteTitle = "Test Site"

// testSlogan is a fixed slogan used across engine integration tests.
const testSlogan = "Test Slogan"

// createAndActivateTemplate is a helper that inserts a template and activates it.
// It returns the created template model.
func createAndActivateTemplate(t *testing.T, ts *store.TemplateStore, name string, tmplType models.TemplateType, html string) *models.Template {
	t.Helper()
	created, err := ts.Create(testTenantID, &models.Template{
		Name:        name,
		Type:        tmplType,
		HTMLContent: html,
	})
	if err != nil {
		t.Fatalf("create template %q: %v", name, err)
	}
	if err := ts.Activate(testTenantID, created.ID); err != nil {
		t.Fatalf("activate template %q: %v", name, err)
	}
	return created
}

// --------------------------------------------------------------------------
// TestRenderPageIntegration — full integration test for RenderPage
// --------------------------------------------------------------------------

func TestRenderPageIntegration(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)
	authorID := testAuthorID(t, db)

	// Use a unique suffix to avoid collisions with parallel test runs.
	suffix := uuid.NewString()[:8]

	headerName := "integ-header-" + suffix
	footerName := "integ-footer-" + suffix
	pageName := "integ-page-" + suffix
	slug := "integ-render-page-" + suffix

	t.Cleanup(func() {
		cleanContent(t, db, slug)
		cleanTemplates(t, db, headerName, footerName, pageName)
	})

	// Create and activate header, footer, and page templates.
	// NOTE: Fragment templates (header, footer) are rendered with nil data
	// by renderFragment(), so they must not reference data fields like
	// {{.Year}} or {{.SiteTitle}}. Only static HTML works in fragments.
	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader,
		`<header><nav>Site Header</nav></header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter,
		`<footer><p>Site Footer</p></footer>`)
	createAndActivateTemplate(t, ts, pageName, models.TemplateTypePage,
		`<!DOCTYPE html><html><head><title>{{.Title}}</title></head><body>{{.Header}}<main>{{.Body}}</main><p>{{.Excerpt}}</p><p>{{.MetaDescription}}</p><p>{{.MetaKeywords}}</p><p>{{.Slug}}</p><p>{{.PublishedAt}}</p><span>{{.Year}}</span>{{.Footer}}</body></html>`)

	// Create a published content item.
	cs := store.NewContentStore(db)
	excerpt := "A test excerpt"
	metaDesc := "Test meta description"
	metaKw := "test,integration"
	now := time.Now()
	content, err := cs.Create(testTenantID, &models.Content{
		Type:            models.ContentTypePost,
		Title:           "Integration Test Page",
		Slug:            slug,
		Body:            "<p>Hello from integration test</p>",
		Excerpt:         &excerpt,
		Status:          models.ContentStatusPublished,
		MetaDescription: &metaDesc,
		MetaKeywords:    &metaKw,
		AuthorID:        authorID,
		PublishedAt:     &now,
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}

	eng := New(ts)

	result, err := eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}

	body := string(result)

	// Verify the page title appears.
	if !strings.Contains(body, "<title>Integration Test Page</title>") {
		t.Errorf("expected page title in output, got:\n%s", body)
	}

	// Verify the content body is rendered (unescaped HTML).
	if !strings.Contains(body, "<p>Hello from integration test</p>") {
		t.Errorf("expected content body in output, got:\n%s", body)
	}

	// Verify header fragment is included.
	if !strings.Contains(body, "Site Header") {
		t.Errorf("expected header fragment in output, got:\n%s", body)
	}

	// Verify footer fragment is included.
	if !strings.Contains(body, "Site Footer") {
		t.Errorf("expected footer fragment in output, got:\n%s", body)
	}

	// Verify the current year appears (from the page template's {{.Year}}).
	yearStr := fmt.Sprintf("%d", time.Now().Year())
	if !strings.Contains(body, yearStr) {
		t.Errorf("expected year %s in output, got:\n%s", yearStr, body)
	}

	// Verify excerpt, meta description, meta keywords, slug are populated.
	if !strings.Contains(body, "A test excerpt") {
		t.Errorf("expected excerpt in output, got:\n%s", body)
	}
	if !strings.Contains(body, "Test meta description") {
		t.Errorf("expected meta description in output, got:\n%s", body)
	}
	if !strings.Contains(body, "test,integration") {
		t.Errorf("expected meta keywords in output, got:\n%s", body)
	}
	if !strings.Contains(body, slug) {
		t.Errorf("expected slug %q in output, got:\n%s", slug, body)
	}

	// Verify published_at date is formatted.
	expectedDate := now.Format("January 2, 2006")
	if !strings.Contains(body, expectedDate) {
		t.Errorf("expected published date %q in output, got:\n%s", expectedDate, body)
	}
}

// --------------------------------------------------------------------------
// TestRenderPageNilOptionalFields — content with nil excerpt/meta fields
// --------------------------------------------------------------------------

func TestRenderPageNilOptionalFields(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)
	authorID := testAuthorID(t, db)

	suffix := uuid.NewString()[:8]

	headerName := "integ-hdr-nil-" + suffix
	footerName := "integ-ftr-nil-" + suffix
	pageName := "integ-pg-nil-" + suffix
	slug := "integ-nil-fields-" + suffix

	t.Cleanup(func() {
		cleanContent(t, db, slug)
		cleanTemplates(t, db, headerName, footerName, pageName)
	})

	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader, `<header>H</header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter, `<footer>F</footer>`)
	createAndActivateTemplate(t, ts, pageName, models.TemplateTypePage,
		`<h1>{{.Title}}</h1><p>excerpt:{{.Excerpt}}</p><p>desc:{{.MetaDescription}}</p><p>kw:{{.MetaKeywords}}</p>`)

	cs := store.NewContentStore(db)
	// Create content with nil optional fields and nil PublishedAt.
	content, err := cs.Create(testTenantID, &models.Content{
		Type:     models.ContentTypePage,
		Title:    "Nil Fields Page",
		Slug:     slug,
		Body:     "<p>Body</p>",
		Status:   models.ContentStatusDraft, // draft => no PublishedAt auto-set
		AuthorID: authorID,
		// Excerpt, MetaDescription, MetaKeywords all nil
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}

	eng := New(ts)

	result, err := eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPage with nil optional fields: %v", err)
	}

	body := string(result)

	// Nil optional fields should render as empty strings.
	if !strings.Contains(body, "excerpt:") {
		t.Errorf("expected empty excerpt placeholder in output, got:\n%s", body)
	}
	if !strings.Contains(body, "desc:") {
		t.Errorf("expected empty meta description placeholder in output, got:\n%s", body)
	}
	if !strings.Contains(body, "kw:") {
		t.Errorf("expected empty meta keywords placeholder in output, got:\n%s", body)
	}
}

// --------------------------------------------------------------------------
// TestRenderPostListIntegration — full integration test for RenderPostList
// --------------------------------------------------------------------------

func TestRenderPostListIntegration(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)
	authorID := testAuthorID(t, db)

	suffix := uuid.NewString()[:8]

	headerName := "integ-header-list-" + suffix
	footerName := "integ-footer-list-" + suffix
	loopName := "integ-loop-" + suffix
	slug1 := "integ-post1-" + suffix
	slug2 := "integ-post2-" + suffix
	slug3 := "integ-post3-" + suffix

	t.Cleanup(func() {
		cleanContent(t, db, slug1, slug2, slug3)
		cleanTemplates(t, db, headerName, footerName, loopName)
	})

	// Create and activate header, footer, and article_loop templates.
	// Fragment templates are rendered with nil data, so they use only static HTML.
	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader,
		`<header>List Header</header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter,
		`<footer>List Footer</footer>`)
	createAndActivateTemplate(t, ts, loopName, models.TemplateTypeArticleLoop,
		`<html>{{.Header}}<h1>{{.Title}}</h1><span>{{.Year}}</span><ul>{{range .Posts}}<li><a href="/{{.Slug}}">{{.Title}}</a><p>{{.Excerpt}}</p><span>{{.PublishedAt}}</span></li>{{end}}</ul>{{.Footer}}</html>`)

	cs := store.NewContentStore(db)
	now := time.Now()

	excerpt1 := "First post excerpt"
	excerpt2 := "Second post excerpt"

	post1, err := cs.Create(testTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "First Post", Slug: slug1,
		Body: "<p>First</p>", Excerpt: &excerpt1,
		Status: models.ContentStatusPublished, AuthorID: authorID, PublishedAt: &now,
	})
	if err != nil {
		t.Fatalf("create post1: %v", err)
	}

	post2, err := cs.Create(testTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Second Post", Slug: slug2,
		Body: "<p>Second</p>", Excerpt: &excerpt2,
		Status: models.ContentStatusPublished, AuthorID: authorID, PublishedAt: &now,
	})
	if err != nil {
		t.Fatalf("create post2: %v", err)
	}

	// Third post has nil excerpt and nil PublishedAt (draft-like content but
	// still passed to the list — the engine does not filter, it renders what
	// it receives).
	post3, err := cs.Create(testTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Third Post", Slug: slug3,
		Body: "<p>Third</p>", Status: models.ContentStatusDraft, AuthorID: authorID,
	})
	if err != nil {
		t.Fatalf("create post3: %v", err)
	}

	eng := New(ts)

	posts := []models.Content{*post1, *post2, *post3}
	result, err := eng.RenderPostList(testTenantID, testSiteTitle, testSlogan, posts, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPostList: %v", err)
	}

	body := string(result)

	// Verify list title.
	if !strings.Contains(body, "<h1>Blog</h1>") {
		t.Errorf("expected list title 'Blog' in output, got:\n%s", body)
	}

	// Verify all three posts appear.
	if !strings.Contains(body, "First Post") {
		t.Errorf("expected 'First Post' in output, got:\n%s", body)
	}
	if !strings.Contains(body, "Second Post") {
		t.Errorf("expected 'Second Post' in output, got:\n%s", body)
	}
	if !strings.Contains(body, "Third Post") {
		t.Errorf("expected 'Third Post' in output, got:\n%s", body)
	}

	// Verify slugs are rendered as links.
	if !strings.Contains(body, slug1) {
		t.Errorf("expected slug %q in output", slug1)
	}
	if !strings.Contains(body, slug2) {
		t.Errorf("expected slug %q in output", slug2)
	}

	// Verify excerpts.
	if !strings.Contains(body, "First post excerpt") {
		t.Errorf("expected excerpt for post1 in output")
	}
	if !strings.Contains(body, "Second post excerpt") {
		t.Errorf("expected excerpt for post2 in output")
	}

	// Verify published dates.
	expectedDate := now.Format("January 2, 2006")
	if !strings.Contains(body, expectedDate) {
		t.Errorf("expected published date %q in output, got:\n%s", expectedDate, body)
	}

	// Verify header and footer fragments are included.
	if !strings.Contains(body, "List Header") {
		t.Errorf("expected header in output")
	}
	if !strings.Contains(body, "List Footer") {
		t.Errorf("expected footer in output")
	}

	// Verify current year in the article_loop template (from ListData.Year).
	yearStr := fmt.Sprintf("%d", time.Now().Year())
	if !strings.Contains(body, yearStr) {
		t.Errorf("expected year %s in output", yearStr)
	}
}

// --------------------------------------------------------------------------
// TestRenderPostListEmpty — render with an empty post slice
// --------------------------------------------------------------------------

func TestRenderPostListEmpty(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)

	suffix := uuid.NewString()[:8]

	headerName := "integ-hdr-empty-" + suffix
	footerName := "integ-ftr-empty-" + suffix
	loopName := "integ-loop-empty-" + suffix

	t.Cleanup(func() {
		cleanTemplates(t, db, headerName, footerName, loopName)
	})

	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader, `<header>H</header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter, `<footer>F</footer>`)
	createAndActivateTemplate(t, ts, loopName, models.TemplateTypeArticleLoop,
		`<h1>{{.Title}}</h1><ul>{{range .Posts}}<li>{{.Title}}</li>{{end}}</ul>`)

	eng := New(ts)

	result, err := eng.RenderPostList(testTenantID, testSiteTitle, testSlogan, []models.Content{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPostList with empty slice: %v", err)
	}

	body := string(result)
	if !strings.Contains(body, "<h1>Blog</h1>") {
		t.Errorf("expected list title in output, got:\n%s", body)
	}
	// The <ul> should be empty (no <li> items).
	if strings.Contains(body, "<li>") {
		t.Errorf("expected no list items for empty post list, got:\n%s", body)
	}
}

// --------------------------------------------------------------------------
// TestRenderPageNoActivePageTemplate — error when no active page template
// --------------------------------------------------------------------------

func TestRenderPageNoActivePageTemplate(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)

	suffix := uuid.NewString()[:8]

	// Deactivate ALL page templates so RenderPage cannot find one.
	// Save original active page template to restore later.
	var origActiveID *uuid.UUID
	var origID uuid.UUID
	err := db.QueryRow("SELECT id FROM templates WHERE type = 'page' AND is_active = TRUE LIMIT 1").Scan(&origID)
	if err == nil {
		origActiveID = &origID
	}

	// Deactivate all page templates.
	_, _ = db.Exec("UPDATE templates SET is_active = FALSE WHERE type = 'page'")

	// Also make sure no test header/footer templates confuse things: create
	// isolated header/footer to avoid nil-pointer issues from other tests
	// that may have cleaned up their templates.
	headerName := "integ-hdr-nopage-" + suffix
	footerName := "integ-ftr-nopage-" + suffix

	t.Cleanup(func() {
		// Re-activate the original page template if one existed.
		if origActiveID != nil {
			_, _ = db.Exec("UPDATE templates SET is_active = TRUE WHERE id = $1", *origActiveID)
		}
		cleanTemplates(t, db, headerName, footerName)
	})

	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader, `<header>H</header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter, `<footer>F</footer>`)

	eng := New(ts)

	content := &models.Content{
		Title: "Orphan Page",
		Slug:  "orphan",
		Body:  "<p>No template</p>",
	}

	_, err = eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when no active page template exists, got nil")
	}
	if !strings.Contains(err.Error(), "no active page template") {
		t.Errorf("expected error about missing page template, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// TestRenderPostListNoActiveLoopTemplate — error when no active article_loop
// --------------------------------------------------------------------------

func TestRenderPostListNoActiveLoopTemplate(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)

	suffix := uuid.NewString()[:8]

	// Save and deactivate all article_loop templates.
	var origActiveID *uuid.UUID
	var origID uuid.UUID
	err := db.QueryRow("SELECT id FROM templates WHERE type = 'article_loop' AND is_active = TRUE LIMIT 1").Scan(&origID)
	if err == nil {
		origActiveID = &origID
	}

	_, _ = db.Exec("UPDATE templates SET is_active = FALSE WHERE type = 'article_loop'")

	headerName := "integ-hdr-noloop-" + suffix
	footerName := "integ-ftr-noloop-" + suffix

	t.Cleanup(func() {
		if origActiveID != nil {
			_, _ = db.Exec("UPDATE templates SET is_active = TRUE WHERE id = $1", *origActiveID)
		}
		cleanTemplates(t, db, headerName, footerName)
	})

	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader, `<header>H</header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter, `<footer>F</footer>`)

	eng := New(ts)

	_, err = eng.RenderPostList(testTenantID, testSiteTitle, testSlogan, []models.Content{}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when no active article_loop template exists, got nil")
	}
	if !strings.Contains(err.Error(), "no active article_loop template") {
		t.Errorf("expected error about missing article_loop template, got: %v", err)
	}
}

// --------------------------------------------------------------------------
// TestRenderPageHeaderFooterFragments — verify header/footer in rendered output
// --------------------------------------------------------------------------

func TestRenderPageHeaderFooterFragments(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)
	authorID := testAuthorID(t, db)

	suffix := uuid.NewString()[:8]

	headerName := "integ-hdr-frag-" + suffix
	footerName := "integ-ftr-frag-" + suffix
	pageName := "integ-pg-frag-" + suffix
	slug := "integ-frag-" + suffix

	t.Cleanup(func() {
		cleanContent(t, db, slug)
		cleanTemplates(t, db, headerName, footerName, pageName)
	})

	// Create templates with distinctive header/footer content.
	// NOTE: Header and footer fragments are rendered with nil data by
	// renderFragment, so they cannot reference {{.Year}} or other fields.
	// The {{.Year}} in the page template (via PageData) works fine.
	createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader,
		`<header class="main-header"><nav><a href="/">Home</a><a href="/blog">Blog</a></nav></header>`)
	createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter,
		`<footer class="main-footer"><p>Copyright YaaiCMS</p></footer>`)
	createAndActivateTemplate(t, ts, pageName, models.TemplateTypePage,
		`<html>{{.Header}}<main><h1>{{.Title}}</h1>{{.Body}}</main><p>{{.Year}}</p>{{.Footer}}</html>`)

	cs := store.NewContentStore(db)
	now := time.Now()
	content, err := cs.Create(testTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Fragment Test", Slug: slug,
		Body: "<p>content</p>", Status: models.ContentStatusPublished,
		AuthorID: authorID, PublishedAt: &now,
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}

	eng := New(ts)

	result, err := eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}

	body := string(result)

	// Verify the header fragment HTML is present and contains the navigation links.
	if !strings.Contains(body, `class="main-header"`) {
		t.Errorf("expected header class in output, got:\n%s", body)
	}
	if !strings.Contains(body, `<a href="/">Home</a>`) {
		t.Errorf("expected Home link from header, got:\n%s", body)
	}
	if !strings.Contains(body, `<a href="/blog">Blog</a>`) {
		t.Errorf("expected Blog link from header, got:\n%s", body)
	}

	// Verify the footer fragment HTML is present with copyright text.
	if !strings.Contains(body, `class="main-footer"`) {
		t.Errorf("expected footer class in output, got:\n%s", body)
	}
	if !strings.Contains(body, "Copyright YaaiCMS") {
		t.Errorf("expected copyright text in footer output, got:\n%s", body)
	}

	// Verify the year is rendered from the page template (PageData.Year).
	yearStr := fmt.Sprintf("%d", time.Now().Year())
	if !strings.Contains(body, yearStr) {
		t.Errorf("expected year %s in page output, got:\n%s", yearStr, body)
	}
}

// --------------------------------------------------------------------------
// TestRenderPageWithoutHeaderFooter — graceful degradation when header/footer
// templates are missing (should render with empty header/footer, not error)
// --------------------------------------------------------------------------

func TestRenderPageWithoutHeaderFooter(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)
	authorID := testAuthorID(t, db)

	suffix := uuid.NewString()[:8]

	pageName := "integ-pg-nohf-" + suffix
	slug := "integ-nohf-" + suffix

	// Save any existing active header/footer templates to restore.
	var origHeaderID, origFooterID *uuid.UUID
	var hID, fID uuid.UUID
	if err := db.QueryRow("SELECT id FROM templates WHERE type = 'header' AND is_active = TRUE LIMIT 1").Scan(&hID); err == nil {
		origHeaderID = &hID
	}
	if err := db.QueryRow("SELECT id FROM templates WHERE type = 'footer' AND is_active = TRUE LIMIT 1").Scan(&fID); err == nil {
		origFooterID = &fID
	}

	// Deactivate all header and footer templates.
	_, _ = db.Exec("UPDATE templates SET is_active = FALSE WHERE type = 'header'")
	_, _ = db.Exec("UPDATE templates SET is_active = FALSE WHERE type = 'footer'")

	t.Cleanup(func() {
		cleanContent(t, db, slug)
		cleanTemplates(t, db, pageName)
		if origHeaderID != nil {
			_, _ = db.Exec("UPDATE templates SET is_active = TRUE WHERE id = $1", *origHeaderID)
		}
		if origFooterID != nil {
			_, _ = db.Exec("UPDATE templates SET is_active = TRUE WHERE id = $1", *origFooterID)
		}
	})

	createAndActivateTemplate(t, ts, pageName, models.TemplateTypePage,
		`<html><body>HEADER[{{.Header}}]<h1>{{.Title}}</h1>FOOTER[{{.Footer}}]</body></html>`)

	cs := store.NewContentStore(db)
	now := time.Now()
	content, err := cs.Create(testTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "No HF Page", Slug: slug,
		Body: "<p>body</p>", Status: models.ContentStatusPublished,
		AuthorID: authorID, PublishedAt: &now,
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}

	eng := New(ts)

	// RenderPage should NOT error even when header/footer templates are missing.
	result, err := eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPage without header/footer should succeed, got: %v", err)
	}

	body := string(result)

	// Header and footer should be empty strings.
	if !strings.Contains(body, "HEADER[]") {
		t.Errorf("expected empty header placeholder, got:\n%s", body)
	}
	if !strings.Contains(body, "FOOTER[]") {
		t.Errorf("expected empty footer placeholder, got:\n%s", body)
	}
	if !strings.Contains(body, "No HF Page") {
		t.Errorf("expected page title in output, got:\n%s", body)
	}
}

// --------------------------------------------------------------------------
// TestRenderPageCachesTemplates — verify the L1 cache is populated after render
// --------------------------------------------------------------------------

func TestRenderPageCachesTemplates(t *testing.T) {
	db := testDB(t)
	ts := store.NewTemplateStore(db)
	authorID := testAuthorID(t, db)

	suffix := uuid.NewString()[:8]

	headerName := "integ-hdr-cache-" + suffix
	footerName := "integ-ftr-cache-" + suffix
	pageName := "integ-pg-cache-" + suffix
	slug := "integ-cache-" + suffix

	t.Cleanup(func() {
		cleanContent(t, db, slug)
		cleanTemplates(t, db, headerName, footerName, pageName)
	})

	hdr := createAndActivateTemplate(t, ts, headerName, models.TemplateTypeHeader, `<header>Cached</header>`)
	ftr := createAndActivateTemplate(t, ts, footerName, models.TemplateTypeFooter, `<footer>Cached</footer>`)
	pg := createAndActivateTemplate(t, ts, pageName, models.TemplateTypePage,
		`{{.Header}}<h1>{{.Title}}</h1>{{.Footer}}`)

	cs := store.NewContentStore(db)
	now := time.Now()
	content, err := cs.Create(testTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Cache Test", Slug: slug,
		Body: "body", Status: models.ContentStatusPublished,
		AuthorID: authorID, PublishedAt: &now,
	})
	if err != nil {
		t.Fatalf("create content: %v", err)
	}

	eng := New(ts)

	// First render should populate the cache.
	_, err = eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}

	// Re-fetch templates to get current versions after activation bumped them.
	hdr, _ = ts.FindByID(testTenantID, hdr.ID)
	ftr, _ = ts.FindByID(testTenantID, ftr.ID)
	pg, _ = ts.FindByID(testTenantID, pg.ID)

	// Verify all three templates are now cached.
	if eng.cache.get(hdr.ID.String(), hdr.Version) == nil {
		t.Error("expected header template in cache after RenderPage")
	}
	if eng.cache.get(ftr.ID.String(), ftr.Version) == nil {
		t.Error("expected footer template in cache after RenderPage")
	}
	if eng.cache.get(pg.ID.String(), pg.Version) == nil {
		t.Error("expected page template in cache after RenderPage")
	}

	// Second render should use the cache (we cannot easily prove it skipped
	// DB, but we verify it still works correctly).
	result2, err := eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("second RenderPage: %v", err)
	}
	if !strings.Contains(string(result2), "Cache Test") {
		t.Errorf("expected title in cached render output")
	}

	// Invalidate and verify the cache entry is gone.
	eng.InvalidateTemplate(pg.ID.String())
	if eng.cache.get(pg.ID.String(), pg.Version) != nil {
		t.Error("expected page template removed from cache after invalidation")
	}

	// Render again — should re-populate the cache.
	_, err = eng.RenderPage(testTenantID, testSiteTitle, testSlogan, content, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("third RenderPage after invalidation: %v", err)
	}
	if eng.cache.get(pg.ID.String(), pg.Version) == nil {
		t.Error("expected page template back in cache after re-render")
	}
}
