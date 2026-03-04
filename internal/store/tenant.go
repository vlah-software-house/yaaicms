// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// TenantStore handles all tenant-related database operations.
type TenantStore struct {
	db *sql.DB
}

// NewTenantStore creates a new TenantStore with the given database connection.
func NewTenantStore(db *sql.DB) *TenantStore {
	return &TenantStore{db: db}
}

// tenantColumns lists the columns selected in tenant queries.
const tenantColumns = `id, name, subdomain, is_active, created_at, updated_at`

// scanTenant scans a tenant row from the result set.
func scanTenant(scanner interface{ Scan(...any) error }) (*models.Tenant, error) {
	var t models.Tenant
	err := scanner.Scan(&t.ID, &t.Name, &t.Subdomain, &t.IsActive, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// FindBySubdomain retrieves a tenant by its subdomain. Returns nil if not found.
func (s *TenantStore) FindBySubdomain(subdomain string) (*models.Tenant, error) {
	row := s.db.QueryRow(`SELECT `+tenantColumns+` FROM tenants WHERE subdomain = $1`, subdomain)
	t, err := scanTenant(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find tenant by subdomain: %w", err)
	}
	return t, nil
}

// FindByID retrieves a tenant by its UUID. Returns nil if not found.
func (s *TenantStore) FindByID(id uuid.UUID) (*models.Tenant, error) {
	row := s.db.QueryRow(`SELECT `+tenantColumns+` FROM tenants WHERE id = $1`, id)
	t, err := scanTenant(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find tenant by id: %w", err)
	}
	return t, nil
}

// List returns all tenants ordered by name.
func (s *TenantStore) List() ([]models.Tenant, error) {
	rows, err := s.db.Query(`SELECT ` + tenantColumns + ` FROM tenants ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []models.Tenant
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, *t)
	}
	return tenants, rows.Err()
}

// Create inserts a new tenant and returns it with the generated ID.
func (s *TenantStore) Create(name, subdomain string) (*models.Tenant, error) {
	row := s.db.QueryRow(`
		INSERT INTO tenants (name, subdomain)
		VALUES ($1, $2)
		RETURNING `+tenantColumns,
		name, subdomain,
	)
	t, err := scanTenant(row)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	return t, nil
}

// Update modifies a tenant's name and active status.
func (s *TenantStore) Update(id uuid.UUID, name string, isActive bool) error {
	result, err := s.db.Exec(`
		UPDATE tenants SET name = $1, is_active = $2, updated_at = NOW()
		WHERE id = $3
	`, name, isActive, id)
	if err != nil {
		return fmt.Errorf("update tenant: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant not found")
	}
	return nil
}

// Delete removes a tenant by ID. This cascades to user_tenants.
func (s *TenantStore) Delete(id uuid.UUID) error {
	_, err := s.db.Exec(`DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete tenant: %w", err)
	}
	return nil
}
