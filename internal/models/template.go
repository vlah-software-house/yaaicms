// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package models

import (
	"time"

	"github.com/google/uuid"
)

// TemplateType categorizes templates by their role in page composition.
type TemplateType string

const (
	TemplateTypeHeader      TemplateType = "header"
	TemplateTypeFooter      TemplateType = "footer"
	TemplateTypePage        TemplateType = "page"
	TemplateTypeArticleLoop TemplateType = "article_loop"
	TemplateTypeAuthorPage  TemplateType = "author_page"
)

// Template represents an AI-generated HTML+TailwindCSS template stored in
// the database. Templates contain Go template variables (e.g., {{.Title}})
// and are compiled at runtime by the rendering engine.
type Template struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Type        TemplateType `json:"type"`
	HTMLContent string       `json:"html_content"`
	Version     int          `json:"version"`
	IsActive    bool         `json:"is_active"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// TemplateRevision stores a snapshot of a template's state before an update,
// enabling undo/restore. Follows the same pattern as ContentRevision.
type TemplateRevision struct {
	ID            uuid.UUID `json:"id"`
	TemplateID    uuid.UUID `json:"template_id"`
	Name          string    `json:"name"`
	HTMLContent   string    `json:"html_content"`
	RevisionTitle string    `json:"revision_title"`
	RevisionLog   string    `json:"revision_log"`
	CreatedBy     uuid.UUID `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}
