// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// testContentTenantID is a fixed tenant ID used across content store tests.
var testContentTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

// testAuthorID returns a valid user ID for content creation.
func testAuthorID(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("no users in database — run seed first: %v", err)
	}
	return id
}

func TestContentStoreCreateAndFind(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug := "test-create-content-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanContent(t, db, slug) })

	content := &models.Content{
		Type:     models.ContentTypePost,
		Title:    "Test Post",
		Slug:     slug,
		Body:     "<p>Test body</p>",
		Status:   models.ContentStatusDraft,
		AuthorID: authorID,
	}

	created, err := s.Create(testContentTenantID, content)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if created.Title != "Test Post" {
		t.Errorf("title: got %q, want %q", created.Title, "Test Post")
	}
	if created.Status != models.ContentStatusDraft {
		t.Errorf("status: got %q, want %q", created.Status, models.ContentStatusDraft)
	}
	if created.PublishedAt != nil {
		t.Error("expected nil published_at for draft")
	}

	// FindByID.
	found, err := s.FindByID(testContentTenantID, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("expected content, got nil")
	}
	if found.Slug != slug {
		t.Errorf("slug: got %q, want %q", found.Slug, slug)
	}
}

func TestContentStoreCreatePublished(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug := "test-pub-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanContent(t, db, slug) })

	content := &models.Content{
		Type:     models.ContentTypePost,
		Title:    "Published Post",
		Slug:     slug,
		Body:     "<p>Published</p>",
		Status:   models.ContentStatusPublished,
		AuthorID: authorID,
	}

	created, err := s.Create(testContentTenantID, content)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.PublishedAt == nil {
		t.Error("expected non-nil published_at for published content")
	}
}

func TestContentStoreFindBySlug(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug := "test-slug-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanContent(t, db, slug) })

	// Create draft — should NOT be findable by slug.
	_, _ = s.Create(testContentTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Draft", Slug: slug,
		Body: "draft", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	found, err := s.FindBySlug(testContentTenantID, slug)
	if err != nil {
		t.Fatalf("FindBySlug (draft): %v", err)
	}
	if found != nil {
		t.Error("expected nil for draft content via FindBySlug")
	}

	// Update to published.
	_, _ = db.Exec("UPDATE content SET status = 'published', published_at = NOW() WHERE slug = $1", slug)

	found, err = s.FindBySlug(testContentTenantID, slug)
	if err != nil {
		t.Fatalf("FindBySlug (published): %v", err)
	}
	if found == nil {
		t.Fatal("expected content after publishing")
	}
	if found.Slug != slug {
		t.Errorf("slug: got %q, want %q", found.Slug, slug)
	}

	// Not found.
	found, _ = s.FindBySlug(testContentTenantID, "nonexistent-slug-xyz")
	if found != nil {
		t.Error("expected nil for nonexistent slug")
	}
}

func TestContentStoreListByType(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug1 := "test-list-post-" + uuid.NewString()[:8]
	slug2 := "test-list-page-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanContent(t, db, slug1, slug2) })

	_, _ = s.Create(testContentTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "List Post", Slug: slug1,
		Body: "body", Status: models.ContentStatusDraft, AuthorID: authorID,
	})
	_, _ = s.Create(testContentTenantID, &models.Content{
		Type: models.ContentTypePage, Title: "List Page", Slug: slug2,
		Body: "body", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	posts, err := s.ListByType(testContentTenantID, models.ContentTypePost)
	if err != nil {
		t.Fatalf("ListByType(post): %v", err)
	}
	pages, err := s.ListByType(testContentTenantID, models.ContentTypePage)
	if err != nil {
		t.Fatalf("ListByType(page): %v", err)
	}

	if len(posts) < 1 {
		t.Error("expected at least 1 post")
	}
	if len(pages) < 1 {
		t.Error("expected at least 1 page")
	}
}

func TestContentStoreUpdate(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug := "test-update-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanContent(t, db, slug) })

	created, _ := s.Create(testContentTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Original", Slug: slug,
		Body: "original", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	created.Title = "Updated Title"
	created.Body = "updated body"
	created.Status = models.ContentStatusPublished

	if err := s.Update(created); err != nil {
		t.Fatalf("Update: %v", err)
	}

	found, _ := s.FindByID(testContentTenantID, created.ID)
	if found.Title != "Updated Title" {
		t.Errorf("title: got %q, want %q", found.Title, "Updated Title")
	}
	if found.Status != models.ContentStatusPublished {
		t.Errorf("status: got %q, want %q", found.Status, models.ContentStatusPublished)
	}
	if found.PublishedAt == nil {
		t.Error("expected published_at set after publishing")
	}
}

func TestContentStoreDelete(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug := "test-delete-" + uuid.NewString()[:8]

	created, _ := s.Create(testContentTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Delete", Slug: slug,
		Body: "body", Status: models.ContentStatusDraft, AuthorID: authorID,
	})

	if err := s.Delete(testContentTenantID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	found, _ := s.FindByID(testContentTenantID, created.ID)
	if found != nil {
		t.Error("expected nil after delete")
	}
}

func TestContentStoreCountByType(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)

	count, err := s.CountByType(testContentTenantID, models.ContentTypePost)
	if err != nil {
		t.Fatalf("CountByType: %v", err)
	}
	if count < 0 {
		t.Error("expected non-negative count")
	}
}

func TestContentStoreListPublishedByType(t *testing.T) {
	db := testDB(t)
	s := NewContentStore(db)
	authorID := testAuthorID(t, db)

	slug := "test-publist-" + uuid.NewString()[:8]
	t.Cleanup(func() { cleanContent(t, db, slug) })

	// Create a published post.
	_, _ = s.Create(testContentTenantID, &models.Content{
		Type: models.ContentTypePost, Title: "Published", Slug: slug,
		Body: "body", Status: models.ContentStatusPublished, AuthorID: authorID,
	})

	published, err := s.ListPublishedByType(testContentTenantID, models.ContentTypePost)
	if err != nil {
		t.Fatalf("ListPublishedByType: %v", err)
	}

	found := false
	for _, p := range published {
		if p.Slug == slug {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected published post in list")
	}
}
