// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package models

import (
	"time"

	"github.com/google/uuid"
)

// UserProfile holds presentation and biographical data for a user.
// It lives in a separate table (user_profiles) with a 1:1 relationship
// to users via user_id. Rows are created lazily on first profile save.
type UserProfile struct {
	UserID    uuid.UUID `json:"user_id"`
	Slug      string    `json:"slug"`
	Bio       string    `json:"bio"`
	AvatarURL string    `json:"avatar_url"`
	Website   string    `json:"website"`
	Location  string    `json:"location"`
	JobTitle  string    `json:"job_title"`
	Pronouns  string    `json:"pronouns"`
	Twitter   string    `json:"twitter"`
	GitHub    string    `json:"github"`
	LinkedIn  string    `json:"linkedin"`
	Instagram   string    `json:"instagram"`
	IsPublished bool      `json:"is_published"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
