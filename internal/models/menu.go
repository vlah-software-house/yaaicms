package models

import (
	"time"

	"github.com/google/uuid"
)

// Menu represents a navigation menu for a specific location (main, footer, footer_legal).
type Menu struct {
	ID        uuid.UUID  `json:"id"`
	TenantID  uuid.UUID  `json:"tenant_id"`
	Location  string     `json:"location"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Items     []MenuItem `json:"items,omitempty"`
}

// MenuItem represents a single link within a menu, optionally linked to content.
type MenuItem struct {
	ID        uuid.UUID  `json:"id"`
	MenuID    uuid.UUID  `json:"menu_id"`
	ParentID  *uuid.UUID `json:"parent_id"`
	Label     string     `json:"label"`
	URL       string     `json:"url"`
	ContentID *uuid.UUID `json:"content_id"`
	Target    string     `json:"target"`
	SortOrder int        `json:"sort_order"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Children  []MenuItem `json:"children,omitempty"`
}
