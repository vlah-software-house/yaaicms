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

// CategoryStore manages categories in the database.
type CategoryStore struct {
	db *sql.DB
}

// NewCategoryStore returns a new CategoryStore.
func NewCategoryStore(db *sql.DB) *CategoryStore {
	return &CategoryStore{db: db}
}

const categoryColumns = `id, name, slug, description, parent_id, sort_order, created_at, updated_at`

// scanCategory scans a row into a Category struct.
func scanCategory(scanner interface{ Scan(...any) error }) (*models.Category, error) {
	var c models.Category
	err := scanner.Scan(
		&c.ID, &c.Name, &c.Slug, &c.Description,
		&c.ParentID, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// List returns all categories ordered by sort_order, with post counts.
func (s *CategoryStore) List(tenantID uuid.UUID) ([]models.Category, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.slug, c.description, c.parent_id, c.sort_order,
		       c.created_at, c.updated_at,
		       COUNT(ct.id) AS post_count
		FROM categories c
		LEFT JOIN content ct ON ct.category_id = c.id AND ct.type = 'post'
		WHERE c.tenant_id = $1
		GROUP BY c.id
		ORDER BY c.sort_order, c.name
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []models.Category
	for rows.Next() {
		var c models.Category
		err := rows.Scan(
			&c.ID, &c.Name, &c.Slug, &c.Description,
			&c.ParentID, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt,
			&c.PostCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// Tree returns categories as a nested tree structure.
func (s *CategoryStore) Tree(tenantID uuid.UUID) ([]models.Category, error) {
	flat, err := s.List(tenantID)
	if err != nil {
		return nil, err
	}
	return buildTree(flat, nil, 0), nil
}

// buildTree recursively builds a tree from a flat list.
func buildTree(flat []models.Category, parentID *uuid.UUID, depth int) []models.Category {
	var result []models.Category
	for _, c := range flat {
		if ptrEqual(c.ParentID, parentID) {
			c.Depth = depth
			c.Children = buildTree(flat, &c.ID, depth+1)
			result = append(result, c)
		}
	}
	return result
}

// ptrEqual compares two *uuid.UUID for equality (both nil or same value).
func ptrEqual(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// FlatTree returns categories as a flat list ordered for display,
// with Depth set for indentation. Useful for <select> dropdowns.
func (s *CategoryStore) FlatTree(tenantID uuid.UUID) ([]models.Category, error) {
	tree, err := s.Tree(tenantID)
	if err != nil {
		return nil, err
	}
	var result []models.Category
	flattenTree(tree, &result)
	return result, nil
}

// flattenTree walks a category tree depth-first, appending to result.
func flattenTree(cats []models.Category, result *[]models.Category) {
	for _, c := range cats {
		*result = append(*result, c)
		if len(c.Children) > 0 {
			flattenTree(c.Children, result)
		}
	}
}

// FindByID retrieves a category by ID within a tenant. Returns nil if not found.
func (s *CategoryStore) FindByID(tenantID, id uuid.UUID) (*models.Category, error) {
	row := s.db.QueryRow(`SELECT `+categoryColumns+` FROM categories WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	c, err := scanCategory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find category by id: %w", err)
	}
	return c, nil
}

// Create inserts a new category and returns it.
func (s *CategoryStore) Create(tenantID uuid.UUID, c *models.Category) (*models.Category, error) {
	row := s.db.QueryRow(`
		INSERT INTO categories (tenant_id, name, slug, description, parent_id, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+categoryColumns,
		tenantID, c.Name, c.Slug, c.Description, c.ParentID, c.SortOrder,
	)
	result, err := scanCategory(row)
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return result, nil
}

// Update modifies an existing category.
func (s *CategoryStore) Update(c *models.Category) error {
	_, err := s.db.Exec(`
		UPDATE categories SET
			name = $1, slug = $2, description = $3, parent_id = $4,
			sort_order = $5, updated_at = NOW()
		WHERE id = $6
	`, c.Name, c.Slug, c.Description, c.ParentID, c.SortOrder, c.ID)
	if err != nil {
		return fmt.Errorf("update category: %w", err)
	}
	return nil
}

// Delete removes a category by ID. Children are re-parented (ON DELETE SET NULL).
func (s *CategoryStore) Delete(tenantID, id uuid.UUID) error {
	_, err := s.db.Exec(`DELETE FROM categories WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return nil
}

// ReorderItem represents a single item in a reorder request.
type ReorderItem struct {
	ID       uuid.UUID  `json:"id"`
	ParentID *uuid.UUID `json:"parent_id"`
	Order    int        `json:"order"`
}

// Reorder updates sort_order and parent_id for multiple categories in a transaction.
func (s *CategoryStore) Reorder(items []ReorderItem) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		UPDATE categories SET parent_id = $1, sort_order = $2, updated_at = $3
		WHERE id = $4`)
	if err != nil {
		return fmt.Errorf("prepare reorder: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now()
	for _, item := range items {
		if _, err := stmt.Exec(item.ParentID, item.Order, now, item.ID); err != nil {
			return fmt.Errorf("reorder category %s: %w", item.ID, err)
		}
	}

	return tx.Commit()
}

// NextSortOrder returns the next sort_order value for a given parent.
func (s *CategoryStore) NextSortOrder(tenantID uuid.UUID, parentID *uuid.UUID) (int, error) {
	var maxOrder sql.NullInt64
	var err error
	if parentID == nil {
		err = s.db.QueryRow(`SELECT MAX(sort_order) FROM categories WHERE tenant_id = $1 AND parent_id IS NULL`, tenantID).Scan(&maxOrder)
	} else {
		err = s.db.QueryRow(`SELECT MAX(sort_order) FROM categories WHERE tenant_id = $1 AND parent_id = $2`, tenantID, *parentID).Scan(&maxOrder)
	}
	if err != nil {
		return 0, err
	}
	if maxOrder.Valid {
		return int(maxOrder.Int64) + 1, nil
	}
	return 0, nil
}
