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

// templateRevisionColumns lists all columns for template_revisions SELECTs.
const templateRevisionColumns = `id, template_id, name, html_content,
	revision_title, revision_log, created_by, created_at`

// TemplateRevisionStore provides access to template revision data in PostgreSQL.
type TemplateRevisionStore struct {
	db *sql.DB
}

// NewTemplateRevisionStore creates a new TemplateRevisionStore backed by the given database.
func NewTemplateRevisionStore(db *sql.DB) *TemplateRevisionStore {
	return &TemplateRevisionStore{db: db}
}

// scanTemplateRevision scans a single template_revisions row into a TemplateRevision.
func scanTemplateRevision(scanner interface{ Scan(...any) error }) (*models.TemplateRevision, error) {
	var r models.TemplateRevision
	err := scanner.Scan(
		&r.ID, &r.TemplateID, &r.Name, &r.HTMLContent,
		&r.RevisionTitle, &r.RevisionLog, &r.CreatedBy, &r.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Create inserts a new template revision and returns it with the generated ID.
func (s *TemplateRevisionStore) Create(rev *models.TemplateRevision) (*models.TemplateRevision, error) {
	row := s.db.QueryRow(`
		INSERT INTO template_revisions (
			template_id, name, html_content,
			revision_title, revision_log, created_by
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+templateRevisionColumns,
		rev.TemplateID, rev.Name, rev.HTMLContent,
		rev.RevisionTitle, rev.RevisionLog, rev.CreatedBy,
	)
	return scanTemplateRevision(row)
}

// ListByTemplateID returns all revisions for a template, newest first.
func (s *TemplateRevisionStore) ListByTemplateID(templateID uuid.UUID) ([]*models.TemplateRevision, error) {
	rows, err := s.db.Query(`
		SELECT `+templateRevisionColumns+`
		FROM template_revisions
		WHERE template_id = $1
		ORDER BY created_at DESC
	`, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template revisions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var revisions []*models.TemplateRevision
	for rows.Next() {
		r, err := scanTemplateRevision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan template revision: %w", err)
		}
		revisions = append(revisions, r)
	}
	return revisions, rows.Err()
}

// FindByID returns a single template revision by its ID, scoped to a tenant
// via the parent template's tenant_id.
func (s *TemplateRevisionStore) FindByID(tenantID, id uuid.UUID) (*models.TemplateRevision, error) {
	row := s.db.QueryRow(`
		SELECT `+templateRevisionColumns+`
		FROM template_revisions
		WHERE id = $1
		  AND template_id IN (SELECT id FROM templates WHERE tenant_id = $2)
	`, id, tenantID)
	r, err := scanTemplateRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// UpdateMeta updates the revision title and changelog for a given template revision.
func (s *TemplateRevisionStore) UpdateMeta(id uuid.UUID, title, changelog string) error {
	_, err := s.db.Exec(`
		UPDATE template_revisions
		SET revision_title = $1, revision_log = $2
		WHERE id = $3
	`, title, changelog, id)
	if err != nil {
		return fmt.Errorf("update template revision meta: %w", err)
	}
	return nil
}
