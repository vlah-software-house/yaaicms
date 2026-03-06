// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// TemplateStore handles all template-related database operations.
type TemplateStore struct {
	db *sql.DB
}

// NewTemplateStore creates a new TemplateStore with the given database connection.
func NewTemplateStore(db *sql.DB) *TemplateStore {
	return &TemplateStore{db: db}
}

// List returns all templates for a tenant, ordered by type and name.
func (s *TemplateStore) List(tenantID uuid.UUID) ([]models.Template, error) {
	rows, err := s.db.Query(`
		SELECT id, name, type, html_content, version, is_active, created_at, updated_at
		FROM templates
		WHERE tenant_id = $1
		ORDER BY name, type
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var templates []models.Template
	for rows.Next() {
		var t models.Template
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Type, &t.HTMLContent, &t.Version,
			&t.IsActive, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// FindByID retrieves a template by its UUID within a tenant. Returns nil if not found.
func (s *TemplateStore) FindByID(tenantID, id uuid.UUID) (*models.Template, error) {
	t := &models.Template{}
	err := s.db.QueryRow(`
		SELECT id, name, type, html_content, version, is_active, created_at, updated_at
		FROM templates WHERE id = $1 AND tenant_id = $2
	`, id, tenantID).Scan(
		&t.ID, &t.Name, &t.Type, &t.HTMLContent, &t.Version,
		&t.IsActive, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find template by id: %w", err)
	}
	return t, nil
}

// FindActiveByType returns the active template for the given type within a tenant.
// Only one template per type should be active at a time.
func (s *TemplateStore) FindActiveByType(tenantID uuid.UUID, tmplType models.TemplateType) (*models.Template, error) {
	t := &models.Template{}
	err := s.db.QueryRow(`
		SELECT id, name, type, html_content, version, is_active, created_at, updated_at
		FROM templates WHERE tenant_id = $1 AND type = $2 AND is_active = TRUE
		LIMIT 1
	`, tenantID, tmplType).Scan(
		&t.ID, &t.Name, &t.Type, &t.HTMLContent, &t.Version,
		&t.IsActive, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active template: %w", err)
	}
	return t, nil
}

// Create inserts a new template for a tenant. Does NOT activate it automatically.
func (s *TemplateStore) Create(tenantID uuid.UUID, t *models.Template) (*models.Template, error) {
	result := &models.Template{}
	err := s.db.QueryRow(`
		INSERT INTO templates (tenant_id, name, type, html_content, version, is_active)
		VALUES ($1, $2, $3, $4, 1, FALSE)
		RETURNING id, name, type, html_content, version, is_active, created_at, updated_at
	`, tenantID, t.Name, t.Type, t.HTMLContent).Scan(
		&result.ID, &result.Name, &result.Type, &result.HTMLContent,
		&result.Version, &result.IsActive, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return result, nil
}

// Update modifies a template and increments its version.
func (s *TemplateStore) Update(t *models.Template) error {
	_, err := s.db.Exec(`
		UPDATE templates SET
			name = $1, html_content = $2, version = version + 1, updated_at = NOW()
		WHERE id = $3
	`, t.Name, t.HTMLContent, t.ID)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	return nil
}

// Activate sets a template as the active one for its type within a tenant,
// deactivating any other template of the same type. Uses a transaction for atomicity.
func (s *TemplateStore) Activate(tenantID uuid.UUID, id uuid.UUID) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Get the template's type.
	var tmplType string
	err = tx.QueryRow(`SELECT type FROM templates WHERE id = $1`, id).Scan(&tmplType)
	if err != nil {
		return fmt.Errorf("get template type: %w", err)
	}

	// Deactivate all templates of this type within the tenant.
	_, err = tx.Exec(`UPDATE templates SET is_active = FALSE WHERE type = $1 AND tenant_id = $2`, tmplType, tenantID)
	if err != nil {
		return fmt.Errorf("deactivate templates: %w", err)
	}

	// Activate the target template.
	_, err = tx.Exec(`UPDATE templates SET is_active = TRUE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("activate template: %w", err)
	}

	return tx.Commit()
}

// Delete removes a template by ID. Cannot delete an active template.
func (s *TemplateStore) Delete(tenantID, id uuid.UUID) error {
	result, err := s.db.Exec(`DELETE FROM templates WHERE id = $1 AND tenant_id = $2 AND is_active = FALSE`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("cannot delete: template is active or not found")
	}
	return nil
}

// Count returns the total number of templates for a tenant.
func (s *TemplateStore) Count(tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM templates WHERE tenant_id = $1`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count templates: %w", err)
	}
	return count, nil
}
