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

// DesignThemeStore handles all design theme database operations.
type DesignThemeStore struct {
	db *sql.DB
}

// NewDesignThemeStore creates a new DesignThemeStore.
func NewDesignThemeStore(db *sql.DB) *DesignThemeStore {
	return &DesignThemeStore{db: db}
}

// themeColumns lists the columns selected in design theme queries.
const themeColumns = `id, name, style_prompt, is_active, created_at, updated_at`

// scanTheme scans a design theme row from the result set.
func scanTheme(scanner interface{ Scan(...any) error }) (*models.DesignTheme, error) {
	var t models.DesignTheme
	err := scanner.Scan(&t.ID, &t.Name, &t.StylePrompt, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List returns all design themes ordered by creation date descending.
func (s *DesignThemeStore) List(tenantID uuid.UUID) ([]models.DesignTheme, error) {
	rows, err := s.db.Query(`
		SELECT `+themeColumns+`
		FROM design_themes
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list design themes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []models.DesignTheme
	for rows.Next() {
		t, err := scanTheme(rows)
		if err != nil {
			return nil, fmt.Errorf("scan design theme: %w", err)
		}
		items = append(items, *t)
	}
	return items, rows.Err()
}

// FindByID retrieves a design theme by its UUID within a tenant. Returns nil if not found.
func (s *DesignThemeStore) FindByID(tenantID, id uuid.UUID) (*models.DesignTheme, error) {
	row := s.db.QueryRow(`SELECT `+themeColumns+` FROM design_themes WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	t, err := scanTheme(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find design theme by id: %w", err)
	}
	return t, nil
}

// FindActive returns the currently active design theme, or nil if none is active.
func (s *DesignThemeStore) FindActive(tenantID uuid.UUID) (*models.DesignTheme, error) {
	row := s.db.QueryRow(`SELECT `+themeColumns+` FROM design_themes WHERE is_active = TRUE AND tenant_id = $1 LIMIT 1`, tenantID)
	t, err := scanTheme(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find active design theme: %w", err)
	}
	return t, nil
}

// Create inserts a new design theme and returns it with the generated ID.
func (s *DesignThemeStore) Create(tenantID uuid.UUID, t *models.DesignTheme) (*models.DesignTheme, error) {
	err := s.db.QueryRow(`
		INSERT INTO design_themes (tenant_id, name, style_prompt)
		VALUES ($1, $2, $3)
		RETURNING `+themeColumns,
		tenantID, t.Name, t.StylePrompt,
	).Scan(&t.ID, &t.Name, &t.StylePrompt, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create design theme: %w", err)
	}
	return t, nil
}

// Update modifies a design theme's name and style prompt.
func (s *DesignThemeStore) Update(id uuid.UUID, name, stylePrompt string) error {
	result, err := s.db.Exec(`
		UPDATE design_themes SET name = $1, style_prompt = $2, updated_at = NOW()
		WHERE id = $3
	`, name, stylePrompt, id)
	if err != nil {
		return fmt.Errorf("update design theme: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("design theme not found")
	}
	return nil
}

// Activate sets a theme as active and deactivates all others within the tenant.
// Uses a transaction to ensure atomicity.
func (s *DesignThemeStore) Activate(tenantID uuid.UUID, id uuid.UUID) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Deactivate all themes for this tenant.
	if _, err := tx.Exec(`UPDATE design_themes SET is_active = FALSE WHERE is_active = TRUE AND tenant_id = $1`, tenantID); err != nil {
		return fmt.Errorf("deactivate themes: %w", err)
	}

	// Activate the target theme.
	result, err := tx.Exec(`UPDATE design_themes SET is_active = TRUE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("activate theme: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("design theme not found")
	}

	return tx.Commit()
}

// Deactivate sets a specific theme as inactive. Used when the user wants
// to disable the active theme without activating another.
func (s *DesignThemeStore) Deactivate(id uuid.UUID) error {
	_, err := s.db.Exec(`UPDATE design_themes SET is_active = FALSE, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deactivate theme: %w", err)
	}
	return nil
}

// Delete removes a design theme. Cannot delete the active theme.
func (s *DesignThemeStore) Delete(tenantID, id uuid.UUID) error {
	result, err := s.db.Exec(`DELETE FROM design_themes WHERE id = $1 AND tenant_id = $2 AND is_active = FALSE`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete design theme: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("design theme not found or is currently active")
	}
	return nil
}
