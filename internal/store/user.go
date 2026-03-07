// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package store provides database access methods for all YaaiCMS
// entities. Each store struct wraps a *sql.DB and exposes typed query methods.
package store

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"yaaicms/internal/models"
)

// UserStore handles all user-related database operations.
type UserStore struct {
	db *sql.DB
}

// NewUserStore creates a new UserStore with the given database connection.
func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

// userColumns lists the columns selected in user queries.
// Note: role is no longer on the users table (moved to user_tenants).
const userColumns = `id, email, password_hash, display_name, is_super_admin,
	totp_secret, totp_enabled, created_at, updated_at`

// scanUser scans a user row from the result set.
func scanUser(scanner interface{ Scan(...any) error }) (*models.User, error) {
	var u models.User
	err := scanner.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsSuperAdmin,
		&u.TOTPSecret, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByEmail retrieves a user by their email address. Returns nil if not found.
// Email is globally unique across all tenants.
func (s *UserStore) FindByEmail(email string) (*models.User, error) {
	row := s.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE email = $1`, email)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return u, nil
}

// FindByID retrieves a user by their UUID. Returns nil if not found.
func (s *UserStore) FindByID(id uuid.UUID) (*models.User, error) {
	row := s.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return u, nil
}

// List returns all users ordered by creation date. Super-admin only operation.
func (s *UserStore) List() ([]models.User, error) {
	rows, err := s.db.Query(`SELECT ` + userColumns + ` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []models.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

// ListByTenant returns all users belonging to a specific tenant, with their
// per-tenant role. Results are ordered by display name.
func (s *UserStore) ListByTenant(tenantID uuid.UUID) ([]UserWithRole, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.email, u.password_hash, u.display_name, u.is_super_admin,
			u.totp_secret, u.totp_enabled, u.created_at, u.updated_at, ut.role
		FROM users u
		JOIN user_tenants ut ON ut.user_id = u.id
		WHERE ut.tenant_id = $1
		ORDER BY u.display_name ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list users by tenant: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []UserWithRole
	for rows.Next() {
		var u models.User
		var role string
		if err := rows.Scan(
			&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.IsSuperAdmin,
			&u.TOTPSecret, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt, &role,
		); err != nil {
			return nil, fmt.Errorf("scan user with role: %w", err)
		}
		results = append(results, UserWithRole{User: u, TenantRole: models.Role(role)})
	}
	return results, rows.Err()
}

// UserWithRole pairs a user with their role in a specific tenant.
type UserWithRole struct {
	User       models.User
	TenantRole models.Role
}

// Create inserts a new user with a bcrypt-hashed password.
// The user is NOT automatically assigned to any tenant — call AddToTenant separately.
func (s *UserStore) Create(email, password, displayName string) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	row := s.db.QueryRow(`
		INSERT INTO users (email, password_hash, display_name)
		VALUES ($1, $2, $3)
		RETURNING `+userColumns,
		email, string(hash), displayName,
	)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// AddToTenant assigns a user to a tenant with the given role.
func (s *UserStore) AddToTenant(userID, tenantID uuid.UUID, role models.Role) error {
	_, err := s.db.Exec(`
		INSERT INTO user_tenants (user_id, tenant_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, tenant_id) DO UPDATE SET role = EXCLUDED.role
	`, userID, tenantID, role)
	if err != nil {
		return fmt.Errorf("add user to tenant: %w", err)
	}
	return nil
}

// RemoveFromTenant removes a user from a tenant.
func (s *UserStore) RemoveFromTenant(userID, tenantID uuid.UUID) error {
	_, err := s.db.Exec(`DELETE FROM user_tenants WHERE user_id = $1 AND tenant_id = $2`, userID, tenantID)
	if err != nil {
		return fmt.Errorf("remove user from tenant: %w", err)
	}
	return nil
}

// GetTenants returns all tenant memberships for a user (tenant info + role).
func (s *UserStore) GetTenants(userID uuid.UUID) ([]models.TenantMembership, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.name, t.subdomain, t.is_active, t.created_at, t.updated_at, ut.role
		FROM tenants t
		JOIN user_tenants ut ON ut.tenant_id = t.id
		WHERE ut.user_id = $1 AND t.is_active = TRUE
		ORDER BY t.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("get user tenants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var memberships []models.TenantMembership
	for rows.Next() {
		var m models.TenantMembership
		var role string
		if err := rows.Scan(
			&m.Tenant.ID, &m.Tenant.Name, &m.Tenant.Subdomain,
			&m.Tenant.IsActive, &m.Tenant.CreatedAt, &m.Tenant.UpdatedAt,
			&role,
		); err != nil {
			return nil, fmt.Errorf("scan tenant membership: %w", err)
		}
		m.Role = models.Role(role)
		memberships = append(memberships, m)
	}
	return memberships, rows.Err()
}

// GetTenantRole returns the user's role in a specific tenant.
// Returns empty string if the user is not a member.
func (s *UserStore) GetTenantRole(userID, tenantID uuid.UUID) (models.Role, error) {
	var role string
	err := s.db.QueryRow(`
		SELECT role FROM user_tenants WHERE user_id = $1 AND tenant_id = $2
	`, userID, tenantID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get tenant role: %w", err)
	}
	return models.Role(role), nil
}

// SetTOTPSecret saves the TOTP secret for a user (during 2FA setup).
func (s *UserStore) SetTOTPSecret(userID uuid.UUID, secret string) error {
	_, err := s.db.Exec(`
		UPDATE users SET totp_secret = $1, updated_at = NOW() WHERE id = $2
	`, secret, userID)
	if err != nil {
		return fmt.Errorf("set totp secret: %w", err)
	}
	return nil
}

// EnableTOTP marks 2FA as active for a user (after successful code verification).
func (s *UserStore) EnableTOTP(userID uuid.UUID) error {
	_, err := s.db.Exec(`
		UPDATE users SET totp_enabled = TRUE, updated_at = NOW() WHERE id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("enable totp: %w", err)
	}
	return nil
}

// ResetTOTP clears the TOTP secret and disables 2FA for a user.
// The user will be forced to set up 2FA again on their next login.
func (s *UserStore) ResetTOTP(userID uuid.UUID) error {
	_, err := s.db.Exec(`
		UPDATE users SET totp_secret = NULL, totp_enabled = FALSE, updated_at = NOW() WHERE id = $1
	`, userID)
	if err != nil {
		return fmt.Errorf("reset totp: %w", err)
	}
	return nil
}

// Delete removes a user by ID.
func (s *UserStore) Delete(userID uuid.UUID) error {
	_, err := s.db.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// UpdateDisplayName changes a user's display name.
func (s *UserStore) UpdateDisplayName(userID uuid.UUID, name string) error {
	_, err := s.db.Exec(`UPDATE users SET display_name = $1, updated_at = NOW() WHERE id = $2`, name, userID)
	if err != nil {
		return fmt.Errorf("update display name: %w", err)
	}
	return nil
}

// CheckPassword verifies a plaintext password against the user's stored hash.
func (s *UserStore) CheckPassword(user *models.User, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil
}
