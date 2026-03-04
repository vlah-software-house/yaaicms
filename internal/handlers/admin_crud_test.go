// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// --- Dashboard ---

func TestDashboard_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	sess := testSession(testAuthorID(t, env.DB), "admin@test.local", "admin", true)
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.Dashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Dashboard: got status %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Dashboard: Content-Type = %q, want text/html", ct)
	}
}

// --- Posts CRUD ---

func TestPostsList_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	rec := httptest.NewRecorder()
	env.Admin.PostsList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PostsList: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPostNew_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts/new", nil)
	rec := httptest.NewRecorder()
	env.Admin.PostNew(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PostNew: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPostCreate_ValidData_RedirectsToPosts(t *testing.T) {
	env := newTestEnv(t)

	testSlug := "test-post-create-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, testSlug) })

	form := url.Values{}
	form.Set("title", "Test Post Create")
	form.Set("slug", testSlug)
	form.Set("body", "This is the post body.")
	form.Set("status", "draft")

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	authorID := testAuthorID(t, env.DB)
	sess := testSession(authorID, "admin@test.local", "admin", true)
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.PostCreate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PostCreate valid: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/posts" {
		t.Errorf("PostCreate valid: redirect to %q, want /admin/posts", loc)
	}
}

func TestPostCreate_MissingTitle_ReRendersForm(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("title", "")
	form.Set("body", "Some body.")

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	authorID := testAuthorID(t, env.DB)
	sess := testSession(authorID, "admin@test.local", "admin", true)
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.PostCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PostCreate missing title: got status %d, want %d", rec.Code, http.StatusOK)
	}
	// The form should be re-rendered with an error message about the title.
	body := rec.Body.String()
	if !strings.Contains(body, "Title is required") {
		t.Errorf("PostCreate missing title: response body should contain validation error, got: %s", body[:min(len(body), 500)])
	}
}

func TestPostEdit_ValidUUID_Returns200(t *testing.T) {
	env := newTestEnv(t)

	// Create a post to edit.
	testSlug := "test-post-edit-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, testSlug) })

	authorID := testAuthorID(t, env.DB)
	created := createTestPost(t, env, authorID, "Editable Post", testSlug)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts/"+created.ID.String()+"/edit", nil)
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.PostEdit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PostEdit valid: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPostEdit_InvalidUUID_Returns400(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts/not-a-uuid/edit", nil)
	req = withChiURLParam(req, "id", "not-a-uuid")

	rec := httptest.NewRecorder()
	env.Admin.PostEdit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PostEdit invalid UUID: got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPostUpdate_ValidData_Redirects(t *testing.T) {
	env := newTestEnv(t)

	testSlug := "test-post-update-" + uuid.New().String()[:8]
	updatedSlug := "test-post-updated-" + uuid.New().String()[:8]
	t.Cleanup(func() {
		cleanContent(t, env.DB, testSlug)
		cleanContent(t, env.DB, updatedSlug)
	})

	authorID := testAuthorID(t, env.DB)
	created := createTestPost(t, env, authorID, "Post to Update", testSlug)

	form := url.Values{}
	form.Set("title", "Updated Post Title")
	form.Set("slug", updatedSlug)
	form.Set("body", "Updated body content.")
	form.Set("status", "draft")

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/"+created.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.PostUpdate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PostUpdate: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/posts" {
		t.Errorf("PostUpdate: redirect to %q, want /admin/posts", loc)
	}
}

func TestPostDelete_Redirects(t *testing.T) {
	env := newTestEnv(t)

	testSlug := "test-post-delete-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, testSlug) })

	authorID := testAuthorID(t, env.DB)
	created := createTestPost(t, env, authorID, "Post to Delete", testSlug)

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/"+created.ID.String()+"/delete", nil)
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.PostDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PostDelete: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/posts" {
		t.Errorf("PostDelete: redirect to %q, want /admin/posts", loc)
	}

	// Verify the post was actually deleted.
	item, err := env.ContentStore.FindByID(created.ID)
	if err != nil {
		t.Fatalf("FindByID after delete: %v", err)
	}
	if item != nil {
		t.Error("PostDelete: post should have been deleted but still exists")
	}
}

// --- Pages CRUD ---

func TestPagesList_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/pages", nil)
	rec := httptest.NewRecorder()
	env.Admin.PagesList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PagesList: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPageNew_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/pages/new", nil)
	rec := httptest.NewRecorder()
	env.Admin.PageNew(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PageNew: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPageCreate_ValidData_RedirectsToPages(t *testing.T) {
	env := newTestEnv(t)

	testSlug := "test-page-create-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, testSlug) })

	form := url.Values{}
	form.Set("title", "Test Page Create")
	form.Set("slug", testSlug)
	form.Set("body", "This is the page body.")
	form.Set("status", "draft")

	req := httptest.NewRequest(http.MethodPost, "/admin/pages/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	authorID := testAuthorID(t, env.DB)
	sess := testSession(authorID, "admin@test.local", "admin", true)
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.PageCreate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PageCreate valid: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/pages" {
		t.Errorf("PageCreate valid: redirect to %q, want /admin/pages", loc)
	}
}

func TestPageCreate_MissingTitle_ReRendersForm(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("title", "")
	form.Set("body", "Page body without title.")

	req := httptest.NewRequest(http.MethodPost, "/admin/pages/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	authorID := testAuthorID(t, env.DB)
	sess := testSession(authorID, "admin@test.local", "admin", true)
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.PageCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PageCreate missing title: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Title is required") {
		t.Errorf("PageCreate missing title: response should contain validation error")
	}
}

func TestPageEdit_ValidUUID_Returns200(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	slug := "test-page-edit-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, slug) })

	page, _ := env.ContentStore.Create(testTenantID, &models.Content{
		Type: models.ContentTypePage, Title: "Page To Edit", Slug: slug,
		Body: "body", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/pages/"+page.ID.String()+"/edit", nil)
	req = withChiURLParam(req, "id", page.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.PageEdit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PageEdit: got %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPageEdit_InvalidUUID_Returns400(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/pages/bad-uuid/edit", nil)
	req = withChiURLParam(req, "id", "bad-uuid")

	rec := httptest.NewRecorder()
	env.Admin.PageEdit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PageEdit invalid UUID: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPageUpdate_ValidData_Redirects(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	slug := "test-page-update-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, slug) })

	page, _ := env.ContentStore.Create(testTenantID, &models.Content{
		Type: models.ContentTypePage, Title: "Original Page", Slug: slug,
		Body: "original", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	form := url.Values{}
	form.Set("title", "Updated Page Title")
	form.Set("slug", slug)
	form.Set("body", "updated body")
	form.Set("status", "published")

	req := httptest.NewRequest(http.MethodPost, "/admin/pages/"+page.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiURLParamAndSession(req, "id", page.ID.String(),
		testSession(authorID, "admin@test.local", "admin", true))

	rec := httptest.NewRecorder()
	env.Admin.PageUpdate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PageUpdate: got %d, want %d; body: %s", rec.Code, http.StatusSeeOther,
			rec.Body.String()[:min(rec.Body.Len(), 300)])
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/pages" {
		t.Errorf("PageUpdate: redirect to %q, want /admin/pages", loc)
	}
}

func TestPageUpdate_MissingTitle_ReRendersForm(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	slug := "test-page-upd-bad-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, slug) })

	page, _ := env.ContentStore.Create(testTenantID, &models.Content{
		Type: models.ContentTypePage, Title: "Page Title", Slug: slug,
		Body: "body", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	form := url.Values{}
	form.Set("title", "")
	form.Set("slug", slug)
	form.Set("body", "body")

	req := httptest.NewRequest(http.MethodPost, "/admin/pages/"+page.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiURLParamAndSession(req, "id", page.ID.String(),
		testSession(authorID, "admin@test.local", "admin", true))

	rec := httptest.NewRecorder()
	env.Admin.PageUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PageUpdate missing title: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Title is required") {
		t.Error("expected validation error for missing title")
	}
}

func TestPageDelete_Redirects(t *testing.T) {
	env := newTestEnv(t)
	authorID := testAuthorID(t, env.DB)

	slug := "test-page-delete-" + uuid.New().String()[:8]

	page, _ := env.ContentStore.Create(testTenantID, &models.Content{
		Type: models.ContentTypePage, Title: "Page To Delete", Slug: slug,
		Body: "body", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/pages/"+page.ID.String()+"/delete", nil)
	req = withChiURLParam(req, "id", page.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.PageDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PageDelete: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/pages" {
		t.Errorf("PageDelete: redirect to %q, want /admin/pages", loc)
	}

	// Verify deletion.
	found, _ := env.ContentStore.FindByID(page.ID)
	if found != nil {
		t.Error("expected page to be deleted")
	}
}

// --- Templates CRUD ---

func TestTemplatesList_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/templates", nil)
	rec := httptest.NewRecorder()
	env.Admin.TemplatesList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplatesList: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestTemplateNew_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/templates/new", nil)
	rec := httptest.NewRecorder()
	env.Admin.TemplateNew(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplateNew: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestTemplateCreate_ValidData_Redirects(t *testing.T) {
	env := newTestEnv(t)

	tmplName := "Test Template " + uuid.New().String()[:8]
	t.Cleanup(func() { cleanTemplates(t, env.DB, tmplName) })

	form := url.Values{}
	form.Set("name", tmplName)
	form.Set("type", "header")
	form.Set("html_content", "<header><nav>{{.SiteName}}</nav></header>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.TemplateCreate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("TemplateCreate valid: got status %d, want %d; body: %s",
			rec.Code, http.StatusSeeOther, rec.Body.String()[:min(rec.Body.Len(), 500)])
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/templates" {
		t.Errorf("TemplateCreate valid: redirect to %q, want /admin/templates", loc)
	}
}

func TestTemplateCreate_MissingName_ReRendersForm(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("name", "")
	form.Set("type", "header")
	form.Set("html_content", "<header>Test</header>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.TemplateCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplateCreate missing name: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Template name is required") {
		t.Errorf("TemplateCreate missing name: response should contain validation error")
	}
}

func TestTemplateCreate_InvalidSyntax_ReRendersFormWithError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("name", "Bad Template")
	form.Set("type", "header")
	// Intentionally broken Go template syntax.
	form.Set("html_content", "<header>{{.Unclosed</header>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.TemplateCreate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplateCreate invalid syntax: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Template syntax error") {
		t.Errorf("TemplateCreate invalid syntax: response should contain syntax error, got: %s",
			body[:min(len(body), 500)])
	}
}

func TestTemplateEdit_ValidUUID_Returns200(t *testing.T) {
	env := newTestEnv(t)

	tmplName := "Editable Template " + uuid.New().String()[:8]
	t.Cleanup(func() { cleanTemplates(t, env.DB, tmplName) })

	created := createTestTemplate(t, env, tmplName, "header", "<header>Test</header>")

	req := httptest.NewRequest(http.MethodGet, "/admin/templates/"+created.ID.String()+"/edit", nil)
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.TemplateEdit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplateEdit valid: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestTemplateEdit_InvalidUUID_Returns400(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/templates/not-a-uuid/edit", nil)
	req = withChiURLParam(req, "id", "not-a-uuid")

	rec := httptest.NewRecorder()
	env.Admin.TemplateEdit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("TemplateEdit invalid UUID: got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestTemplateUpdate_ValidData_Redirects(t *testing.T) {
	env := newTestEnv(t)

	tmplName := "Updatable Template " + uuid.New().String()[:8]
	t.Cleanup(func() { cleanTemplates(t, env.DB, tmplName) })

	created := createTestTemplate(t, env, tmplName, "footer", "<footer>Original</footer>")

	form := url.Values{}
	form.Set("name", tmplName)
	form.Set("html_content", "<footer>Updated Content</footer>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/"+created.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.TemplateUpdate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("TemplateUpdate: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/templates" {
		t.Errorf("TemplateUpdate: redirect to %q, want /admin/templates", loc)
	}
}

func TestTemplateActivate_Redirects(t *testing.T) {
	env := newTestEnv(t)

	tmplName := "Activatable Template " + uuid.New().String()[:8]
	t.Cleanup(func() { cleanTemplates(t, env.DB, tmplName) })

	created := createTestTemplate(t, env, tmplName, "header", "<header>Activate Me</header>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/"+created.ID.String()+"/activate", nil)
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.TemplateActivate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("TemplateActivate: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/templates" {
		t.Errorf("TemplateActivate: redirect to %q, want /admin/templates", loc)
	}
}

func TestTemplateDelete_Redirects(t *testing.T) {
	env := newTestEnv(t)

	tmplName := "Deletable Template " + uuid.New().String()[:8]
	t.Cleanup(func() { cleanTemplates(t, env.DB, tmplName) })

	created := createTestTemplate(t, env, tmplName, "footer", "<footer>Delete Me</footer>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/"+created.ID.String()+"/delete", nil)
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.TemplateDelete(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("TemplateDelete: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/templates" {
		t.Errorf("TemplateDelete: redirect to %q, want /admin/templates", loc)
	}
}

func TestTemplatePreview_Returns200(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("html_content", "<div><h1>{{.Title}}</h1><p>Preview content</p></div>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.TemplatePreview(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplatePreview: got status %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("TemplatePreview: Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Preview Page Title") {
		t.Errorf("TemplatePreview: response should contain rendered title, got: %s",
			body[:min(len(body), 500)])
	}
}

func TestTemplatePreview_EmptyContent_Returns400(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("html_content", "")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.TemplatePreview(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("TemplatePreview empty: got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestTemplatePreview_InvalidSyntax_Returns200WithError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("html_content", "<div>{{.Broken</div>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.TemplatePreview(rec, req)

	// TemplatePreview returns 200 with an error div even for invalid syntax.
	if rec.Code != http.StatusOK {
		t.Fatalf("TemplatePreview invalid syntax: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Template error") {
		t.Errorf("TemplatePreview invalid syntax: response should contain error message")
	}
}

// --- Users ---

func TestUsersList_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	rec := httptest.NewRecorder()
	env.Admin.UsersList(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("UsersList: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestUserResetTwoFA_OwnUser_Returns403(t *testing.T) {
	env := newTestEnv(t)

	userID := testAuthorID(t, env.DB)
	sess := testSession(userID, "admin@test.local", "admin", true)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+userID.String()+"/reset-2fa", nil)
	req = withChiURLParamAndSession(req, "id", userID.String(), sess)

	rec := httptest.NewRecorder()
	env.Admin.UserResetTwoFA(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("UserResetTwoFA own user: got status %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestUserResetTwoFA_OtherUser_Redirects(t *testing.T) {
	env := newTestEnv(t)

	// The admin performing the reset.
	adminID := testAuthorID(t, env.DB)
	sess := testSession(adminID, "admin@test.local", "admin", true)

	// Create a second user to reset. Use a unique email to avoid conflicts.
	targetEmail := "reset-target-" + uuid.New().String()[:8] + "@test.local"
	targetUser, err := env.UserStore.Create(targetEmail, "password123", "Target User")
	if err != nil {
		t.Fatalf("create target user: %v", err)
	}
	t.Cleanup(func() { env.UserStore.Delete(targetUser.ID) })

	req := httptest.NewRequest(http.MethodPost, "/admin/users/"+targetUser.ID.String()+"/reset-2fa", nil)
	req = withChiURLParamAndSession(req, "id", targetUser.ID.String(), sess)

	rec := httptest.NewRecorder()
	env.Admin.UserResetTwoFA(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("UserResetTwoFA other user: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/users" {
		t.Errorf("UserResetTwoFA other user: redirect to %q, want /admin/users", loc)
	}
}

func TestUserResetTwoFA_InvalidUUID_Returns400(t *testing.T) {
	env := newTestEnv(t)

	userID := testAuthorID(t, env.DB)
	sess := testSession(userID, "admin@test.local", "admin", true)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/bad-id/reset-2fa", nil)
	req = withChiURLParamAndSession(req, "id", "bad-id", sess)

	rec := httptest.NewRecorder()
	env.Admin.UserResetTwoFA(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("UserResetTwoFA invalid UUID: got status %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// --- Settings ---

func TestSettingsPage_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/settings", nil)
	rec := httptest.NewRecorder()
	env.Admin.SettingsPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("SettingsPage: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- Post Create edge cases ---

func TestPostCreate_DuplicateSlug_ReRendersFormWithError(t *testing.T) {
	env := newTestEnv(t)

	testSlug := "test-dup-slug-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, testSlug) })

	authorID := testAuthorID(t, env.DB)
	sess := testSession(authorID, "admin@test.local", "admin", true)

	// Create first post with the slug.
	createTestPost(t, env, authorID, "First Post", testSlug)

	// Attempt to create second post with the same slug.
	form := url.Values{}
	form.Set("title", "Second Post Same Slug")
	form.Set("slug", testSlug)
	form.Set("body", "Duplicate slug body.")
	form.Set("status", "draft")

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.PostCreate(rec, req)

	// Should re-render the form with a duplicate slug error (status 200).
	if rec.Code != http.StatusOK {
		t.Fatalf("PostCreate duplicate slug: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "slug may already exist") {
		t.Errorf("PostCreate duplicate slug: response should mention slug conflict, got: %s",
			body[:min(len(body), 500)])
	}
}

func TestPostCreate_AutoGeneratesSlugFromTitle(t *testing.T) {
	env := newTestEnv(t)

	// The slug field is left empty; the handler auto-generates from title.
	expectedSlug := "auto-slug-test-post"
	t.Cleanup(func() { cleanContent(t, env.DB, expectedSlug) })

	authorID := testAuthorID(t, env.DB)
	sess := testSession(authorID, "admin@test.local", "admin", true)

	form := url.Values{}
	form.Set("title", "Auto Slug Test Post")
	form.Set("slug", "")
	form.Set("body", "Body for auto-slug test.")
	form.Set("status", "draft")

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Admin.PostCreate(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("PostCreate auto-slug: got status %d, want %d", rec.Code, http.StatusSeeOther)
	}

	// Verify the post was created with the auto-generated slug.
	items, err := env.ContentStore.ListByType(testTenantID, "post")
	if err != nil {
		t.Fatalf("ListByType: %v", err)
	}
	found := false
	for _, item := range items {
		if item.Slug == expectedSlug {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("PostCreate auto-slug: expected post with slug %q to exist", expectedSlug)
	}
}

func TestPostEdit_NonExistentUUID_Returns404(t *testing.T) {
	env := newTestEnv(t)

	fakeID := uuid.New().String()
	req := httptest.NewRequest(http.MethodGet, "/admin/posts/"+fakeID+"/edit", nil)
	req = withChiURLParam(req, "id", fakeID)

	rec := httptest.NewRecorder()
	env.Admin.PostEdit(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("PostEdit non-existent: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestPostUpdate_MissingTitle_ReRendersForm(t *testing.T) {
	env := newTestEnv(t)

	testSlug := "test-post-update-notitle-" + uuid.New().String()[:8]
	t.Cleanup(func() { cleanContent(t, env.DB, testSlug) })

	authorID := testAuthorID(t, env.DB)
	created := createTestPost(t, env, authorID, "Post Update No Title", testSlug)

	form := url.Values{}
	form.Set("title", "")
	form.Set("slug", testSlug)
	form.Set("body", "Updated body.")
	form.Set("status", "draft")

	req := httptest.NewRequest(http.MethodPost, "/admin/posts/"+created.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.PostUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PostUpdate missing title: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Title is required") {
		t.Errorf("PostUpdate missing title: response should contain validation error")
	}
}

func TestTemplateUpdate_InvalidSyntax_ReRendersForm(t *testing.T) {
	env := newTestEnv(t)

	tmplName := "Template Update Bad Syntax " + uuid.New().String()[:8]
	t.Cleanup(func() { cleanTemplates(t, env.DB, tmplName) })

	created := createTestTemplate(t, env, tmplName, "header", "<header>Valid</header>")

	form := url.Values{}
	form.Set("name", tmplName)
	form.Set("html_content", "<header>{{.Broken</header>")

	req := httptest.NewRequest(http.MethodPost, "/admin/templates/"+created.ID.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withChiURLParam(req, "id", created.ID.String())

	rec := httptest.NewRecorder()
	env.Admin.TemplateUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("TemplateUpdate invalid syntax: got status %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Template syntax error") {
		t.Errorf("TemplateUpdate invalid syntax: response should contain syntax error, got: %s",
			body[:min(len(body), 500)])
	}
}

// --- Test helpers ---

// createTestPost inserts a test post directly through the content store and
// returns the created item. The caller is responsible for cleanup.
// testTenantID is a fixed tenant ID used across handler integration tests.
var testTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func createTestPost(t *testing.T, env *testEnv, authorID uuid.UUID, title, slug string) *models.Content {
	t.Helper()
	c := &models.Content{
		Type:     models.ContentTypePost,
		Title:    title,
		Slug:     slug,
		Body:     "Test body for " + title,
		Status:   models.ContentStatusDraft,
		AuthorID: authorID,
	}
	created, err := env.ContentStore.Create(testTenantID, c)
	if err != nil {
		t.Fatalf("createTestPost: %v", err)
	}
	return created
}

// createTestTemplate inserts a test template directly through the template
// store and returns the created item. The caller is responsible for cleanup.
func createTestTemplate(t *testing.T, env *testEnv, name, tmplType, htmlContent string) *models.Template {
	t.Helper()
	tmpl := &models.Template{
		Name:        name,
		Type:        models.TemplateType(tmplType),
		HTMLContent: htmlContent,
	}
	created, err := env.TemplateStore.Create(testTenantID, tmpl)
	if err != nil {
		t.Fatalf("createTestTemplate: %v", err)
	}
	return created
}

