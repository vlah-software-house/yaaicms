// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// ContentStore handles all content-related database operations.
// It serves both posts and pages through the unified content table.
type ContentStore struct {
	db *sql.DB
}

// NewContentStore creates a new ContentStore with the given database connection.
func NewContentStore(db *sql.DB) *ContentStore {
	return &ContentStore{db: db}
}

// contentColumns lists the columns selected in content queries.
const contentColumns = `id, type, title, slug, body, body_format, excerpt, status,
	meta_description, meta_keywords, featured_image_id, category_id, author_id,
	published_at, created_at, updated_at`

// scanContent scans a content row into a Content struct.
func scanContent(scanner interface{ Scan(...any) error }) (*models.Content, error) {
	var c models.Content
	err := scanner.Scan(
		&c.ID, &c.Type, &c.Title, &c.Slug, &c.Body, &c.BodyFormat,
		&c.Excerpt, &c.Status, &c.MetaDescription, &c.MetaKeywords,
		&c.FeaturedImageID, &c.CategoryID, &c.AuthorID, &c.PublishedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListByType returns all content items of the given type, ordered by creation date descending.
func (s *ContentStore) ListByType(tenantID uuid.UUID, contentType models.ContentType) ([]models.Content, error) {
	rows, err := s.db.Query(`
		SELECT `+contentColumns+`
		FROM content
		WHERE tenant_id = $1 AND type = $2
		ORDER BY created_at DESC
	`, tenantID, contentType)
	if err != nil {
		return nil, fmt.Errorf("list content by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []models.Content
	for rows.Next() {
		c, err := scanContent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan content: %w", err)
		}
		items = append(items, *c)
	}
	return items, rows.Err()
}

// FindByID retrieves a content item by its UUID within a tenant. Returns nil if not found.
func (s *ContentStore) FindByID(tenantID, id uuid.UUID) (*models.Content, error) {
	row := s.db.QueryRow(`SELECT `+contentColumns+` FROM content WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	c, err := scanContent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find content by id: %w", err)
	}
	return c, nil
}

// FindBySlug retrieves a published content item by its slug. Used for public page rendering.
func (s *ContentStore) FindBySlug(tenantID uuid.UUID, slug string) (*models.Content, error) {
	row := s.db.QueryRow(`
		SELECT `+contentColumns+`
		FROM content WHERE tenant_id = $1 AND slug = $2 AND status = 'published'
	`, tenantID, slug)
	c, err := scanContent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find content by slug: %w", err)
	}
	return c, nil
}

// Create inserts a new content item and returns it with the generated ID.
func (s *ContentStore) Create(tenantID uuid.UUID, c *models.Content) (*models.Content, error) {
	// If publishing, set the published_at timestamp.
	if c.Status == models.ContentStatusPublished && c.PublishedAt == nil {
		now := time.Now()
		c.PublishedAt = &now
	}

	row := s.db.QueryRow(`
		INSERT INTO content (tenant_id, type, title, slug, body, body_format, excerpt, status,
		                     meta_description, meta_keywords, featured_image_id,
		                     category_id, author_id, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+contentColumns,
		tenantID, c.Type, c.Title, c.Slug, c.Body, c.BodyFormat, c.Excerpt, c.Status,
		c.MetaDescription, c.MetaKeywords, c.FeaturedImageID,
		c.CategoryID, c.AuthorID, c.PublishedAt,
	)
	result, err := scanContent(row)
	if err != nil {
		return nil, fmt.Errorf("create content: %w", err)
	}
	return result, nil
}

// Update modifies an existing content item.
func (s *ContentStore) Update(c *models.Content) error {
	// If transitioning to published and no published_at set, set it now.
	if c.Status == models.ContentStatusPublished && c.PublishedAt == nil {
		now := time.Now()
		c.PublishedAt = &now
	}

	_, err := s.db.Exec(`
		UPDATE content SET
			title = $1, slug = $2, body = $3, body_format = $4, excerpt = $5,
			status = $6, meta_description = $7, meta_keywords = $8,
			featured_image_id = $9, category_id = $10, published_at = $11,
			updated_at = NOW()
		WHERE id = $12
	`, c.Title, c.Slug, c.Body, c.BodyFormat, c.Excerpt, c.Status,
		c.MetaDescription, c.MetaKeywords, c.FeaturedImageID,
		c.CategoryID, c.PublishedAt, c.ID,
	)
	if err != nil {
		return fmt.Errorf("update content: %w", err)
	}
	return nil
}

// Delete removes a content item by ID.
func (s *ContentStore) Delete(tenantID, id uuid.UUID) error {
	_, err := s.db.Exec(`DELETE FROM content WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete content: %w", err)
	}
	return nil
}

// ListPublishedByType returns all published content of the given type,
// ordered by published date descending. Used for public page rendering.
func (s *ContentStore) ListPublishedByType(tenantID uuid.UUID, contentType models.ContentType) ([]models.Content, error) {
	rows, err := s.db.Query(`
		SELECT `+contentColumns+`
		FROM content
		WHERE tenant_id = $1 AND type = $2 AND status = 'published'
		ORDER BY published_at DESC NULLS LAST
	`, tenantID, contentType)
	if err != nil {
		return nil, fmt.Errorf("list published content: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []models.Content
	for rows.Next() {
		c, err := scanContent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan content: %w", err)
		}
		items = append(items, *c)
	}
	return items, rows.Err()
}

// ListPublishedByAuthor returns all published content by a specific author.
func (s *ContentStore) ListPublishedByAuthor(tenantID, authorID uuid.UUID) ([]models.Content, error) {
	rows, err := s.db.Query(`
		SELECT `+contentColumns+`
		FROM content
		WHERE tenant_id = $1 AND author_id = $2 AND status = 'published'
		ORDER BY published_at DESC NULLS LAST
	`, tenantID, authorID)
	if err != nil {
		return nil, fmt.Errorf("list published by author: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []models.Content
	for rows.Next() {
		c, err := scanContent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan content: %w", err)
		}
		items = append(items, *c)
	}
	return items, rows.Err()
}

// CountByType returns the number of content items of the given type.
func (s *ContentStore) CountByType(tenantID uuid.UUID, contentType models.ContentType) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM content WHERE tenant_id = $1 AND type = $2`, tenantID, contentType).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count content: %w", err)
	}
	return count, nil
}
