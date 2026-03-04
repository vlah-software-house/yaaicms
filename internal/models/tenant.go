// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package models defines the data structures that map to database tables
// and provides the core types used throughout the application.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Tenant represents an isolated blog/site in the multi-tenant platform.
// Each tenant is identified by a unique subdomain (e.g., "blog1" → blog1.smartpress.io).
type Tenant struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Subdomain string    `json:"subdomain"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserTenant represents a user's membership in a tenant with a specific role.
type UserTenant struct {
	UserID    uuid.UUID `json:"user_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantMembership bundles tenant info with the user's role in that tenant.
// Used when listing which tenants a user belongs to.
type TenantMembership struct {
	Tenant Tenant `json:"tenant"`
	Role   Role   `json:"role"`
}
