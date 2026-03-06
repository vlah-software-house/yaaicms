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

// TenantDomainStore handles custom domain database operations.
type TenantDomainStore struct {
	db *sql.DB
}

// NewTenantDomainStore creates a new TenantDomainStore.
func NewTenantDomainStore(db *sql.DB) *TenantDomainStore {
	return &TenantDomainStore{db: db}
}

// tenantDomainColumns lists the columns selected in tenant domain queries.
const tenantDomainColumns = `id, tenant_id, domain, status, is_primary, verified_at, created_at, updated_at`

// scanTenantDomain scans a tenant domain row from the result set.
func scanTenantDomain(scanner interface{ Scan(...any) error }) (*models.TenantDomain, error) {
	var d models.TenantDomain
	err := scanner.Scan(&d.ID, &d.TenantID, &d.Domain, &d.Status, &d.IsPrimary, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// FindByDomain retrieves a tenant domain by exact domain match. Returns nil if not found.
func (s *TenantDomainStore) FindByDomain(domain string) (*models.TenantDomain, error) {
	row := s.db.QueryRow(`SELECT `+tenantDomainColumns+` FROM tenant_domains WHERE domain = $1`, domain)
	d, err := scanTenantDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find tenant domain by domain: %w", err)
	}
	return d, nil
}

// FindByDomainWithTenant retrieves an active custom domain and its associated tenant.
// Only returns results where the domain status is 'active' and the tenant is active.
// Returns (nil, nil, nil) if not found or not active.
func (s *TenantDomainStore) FindByDomainWithTenant(domain string) (*models.TenantDomain, *models.Tenant, error) {
	row := s.db.QueryRow(`
		SELECT td.id, td.tenant_id, td.domain, td.status, td.is_primary, td.verified_at, td.created_at, td.updated_at,
		       t.id, t.name, t.subdomain, t.is_active, t.created_at, t.updated_at
		FROM tenant_domains td
		JOIN tenants t ON t.id = td.tenant_id
		WHERE td.domain = $1 AND td.status = 'active' AND t.is_active = TRUE
	`, domain)

	var d models.TenantDomain
	var t models.Tenant
	err := row.Scan(
		&d.ID, &d.TenantID, &d.Domain, &d.Status, &d.IsPrimary, &d.VerifiedAt, &d.CreatedAt, &d.UpdatedAt,
		&t.ID, &t.Name, &t.Subdomain, &t.IsActive, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("find tenant domain with tenant: %w", err)
	}
	return &d, &t, nil
}

// ListByTenant returns all custom domains for a given tenant.
func (s *TenantDomainStore) ListByTenant(tenantID uuid.UUID) ([]models.TenantDomain, error) {
	rows, err := s.db.Query(
		`SELECT `+tenantDomainColumns+` FROM tenant_domains WHERE tenant_id = $1 ORDER BY domain`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenant domains: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []models.TenantDomain
	for rows.Next() {
		d, err := scanTenantDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant domain: %w", err)
		}
		domains = append(domains, *d)
	}
	return domains, rows.Err()
}

// ListByStatus returns all domains with the given status.
func (s *TenantDomainStore) ListByStatus(status string) ([]models.TenantDomain, error) {
	rows, err := s.db.Query(
		`SELECT `+tenantDomainColumns+` FROM tenant_domains WHERE status = $1 ORDER BY created_at`,
		status,
	)
	if err != nil {
		return nil, fmt.Errorf("list tenant domains by status: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var domains []models.TenantDomain
	for rows.Next() {
		d, err := scanTenantDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant domain: %w", err)
		}
		domains = append(domains, *d)
	}
	return domains, rows.Err()
}

// Create inserts a new custom domain mapping for a tenant.
func (s *TenantDomainStore) Create(tenantID uuid.UUID, domain string) (*models.TenantDomain, error) {
	row := s.db.QueryRow(`
		INSERT INTO tenant_domains (tenant_id, domain)
		VALUES ($1, $2)
		RETURNING `+tenantDomainColumns,
		tenantID, domain,
	)
	d, err := scanTenantDomain(row)
	if err != nil {
		return nil, fmt.Errorf("create tenant domain: %w", err)
	}
	return d, nil
}

// UpdateStatus changes the status of a domain record.
func (s *TenantDomainStore) UpdateStatus(id uuid.UUID, status string) error {
	result, err := s.db.Exec(`
		UPDATE tenant_domains SET status = $1, updated_at = NOW()
		WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("update tenant domain status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant domain not found")
	}
	return nil
}

// SetVerified marks a domain as verified and records the verification timestamp.
func (s *TenantDomainStore) SetVerified(id uuid.UUID) error {
	result, err := s.db.Exec(`
		UPDATE tenant_domains SET status = $1, verified_at = $2, updated_at = NOW()
		WHERE id = $3
	`, models.DomainStatusVerified, time.Now(), id)
	if err != nil {
		return fmt.Errorf("set tenant domain verified: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("tenant domain not found")
	}
	return nil
}

// Delete removes a custom domain mapping.
func (s *TenantDomainStore) Delete(tenantID, id uuid.UUID) error {
	_, err := s.db.Exec(`DELETE FROM tenant_domains WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("delete tenant domain: %w", err)
	}
	return nil
}

// FindByID retrieves a tenant domain by its UUID within a tenant. Returns nil if not found.
func (s *TenantDomainStore) FindByID(tenantID, id uuid.UUID) (*models.TenantDomain, error) {
	row := s.db.QueryRow(`SELECT `+tenantDomainColumns+` FROM tenant_domains WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	d, err := scanTenantDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find tenant domain by id: %w", err)
	}
	return d, nil
}

// SetPrimary designates a domain as the primary for its tenant. The domain must
// be active. Uses a transaction to unset any existing primary first.
func (s *TenantDomainStore) SetPrimary(tenantID, domainID uuid.UUID) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin set primary tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Unset any existing primary for this tenant.
	if _, err := tx.Exec(`UPDATE tenant_domains SET is_primary = false WHERE tenant_id = $1 AND is_primary = true`, tenantID); err != nil {
		return fmt.Errorf("unset existing primary: %w", err)
	}

	// Set the new primary (only if active).
	result, err := tx.Exec(`UPDATE tenant_domains SET is_primary = true, updated_at = NOW() WHERE id = $1 AND tenant_id = $2 AND status = 'active'`, domainID, tenantID)
	if err != nil {
		return fmt.Errorf("set primary: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("domain not found or not active")
	}

	return tx.Commit()
}

// UnsetPrimary clears the primary flag for all domains of a tenant.
func (s *TenantDomainStore) UnsetPrimary(tenantID uuid.UUID) error {
	_, err := s.db.Exec(`UPDATE tenant_domains SET is_primary = false, updated_at = NOW() WHERE tenant_id = $1 AND is_primary = true`, tenantID)
	if err != nil {
		return fmt.Errorf("unset primary: %w", err)
	}
	return nil
}

// FindPrimaryByTenantID returns the primary domain for a tenant, or nil if none is set.
func (s *TenantDomainStore) FindPrimaryByTenantID(tenantID uuid.UUID) (*models.TenantDomain, error) {
	row := s.db.QueryRow(`SELECT `+tenantDomainColumns+` FROM tenant_domains WHERE tenant_id = $1 AND is_primary = true`, tenantID)
	d, err := scanTenantDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find primary domain: %w", err)
	}
	return d, nil
}
