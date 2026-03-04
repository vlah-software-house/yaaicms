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

// MediaStore handles all media-related database operations.
type MediaStore struct {
	db *sql.DB
}

// NewMediaStore creates a new MediaStore with the given database connection.
func NewMediaStore(db *sql.DB) *MediaStore {
	return &MediaStore{db: db}
}

// mediaColumns lists the columns selected in media queries.
const mediaColumns = `id, filename, original_name, content_type, size_bytes,
	bucket, s3_key, thumb_s3_key, alt_text, uploader_id, created_at`

// scanMedia scans a media row from the result set.
func scanMedia(scanner interface{ Scan(...any) error }) (*models.Media, error) {
	var m models.Media
	err := scanner.Scan(
		&m.ID, &m.Filename, &m.OriginalName, &m.ContentType, &m.SizeBytes,
		&m.Bucket, &m.S3Key, &m.ThumbS3Key, &m.AltText, &m.UploaderID, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Create inserts a new media record and returns it with the generated ID.
func (s *MediaStore) Create(tenantID uuid.UUID, m *models.Media) (*models.Media, error) {
	err := s.db.QueryRow(`
		INSERT INTO media (tenant_id, filename, original_name, content_type, size_bytes,
			bucket, s3_key, thumb_s3_key, alt_text, uploader_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+mediaColumns,
		tenantID, m.Filename, m.OriginalName, m.ContentType, m.SizeBytes,
		m.Bucket, m.S3Key, m.ThumbS3Key, m.AltText, m.UploaderID,
	).Scan(
		&m.ID, &m.Filename, &m.OriginalName, &m.ContentType, &m.SizeBytes,
		&m.Bucket, &m.S3Key, &m.ThumbS3Key, &m.AltText, &m.UploaderID, &m.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create media: %w", err)
	}
	return m, nil
}

// FindByID retrieves a single media record by its UUID.
func (s *MediaStore) FindByID(id uuid.UUID) (*models.Media, error) {
	row := s.db.QueryRow(`SELECT `+mediaColumns+` FROM media WHERE id = $1`, id)
	m, err := scanMedia(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find media by id: %w", err)
	}
	return m, nil
}

// List returns media items ordered by creation date, with pagination.
func (s *MediaStore) List(tenantID uuid.UUID, limit, offset int) ([]models.Media, error) {
	rows, err := s.db.Query(`
		SELECT `+mediaColumns+`
		FROM media
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list media: %w", err)
	}
	defer rows.Close()

	var items []models.Media
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		items = append(items, *m)
	}
	return items, rows.Err()
}

// Delete removes a media record and returns it so the caller can clean
// up the corresponding S3 objects.
func (s *MediaStore) Delete(id uuid.UUID) (*models.Media, error) {
	row := s.db.QueryRow(`
		DELETE FROM media WHERE id = $1
		RETURNING `+mediaColumns, id)
	m, err := scanMedia(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("delete media: %w", err)
	}
	return m, nil
}

// UpdateThumbKey updates the thumbnail S3 key for a media item.
// Used when regenerating variants to point to the new thumb.
func (s *MediaStore) UpdateThumbKey(id uuid.UUID, thumbKey *string) error {
	_, err := s.db.Exec(`UPDATE media SET thumb_s3_key = $1 WHERE id = $2`, thumbKey, id)
	if err != nil {
		return fmt.Errorf("update thumb key: %w", err)
	}
	return nil
}

// FindByS3Keys returns media items matching the given S3 keys, keyed by s3_key.
// Used for batch-resolving inline content images to their responsive variants.
func (s *MediaStore) FindByS3Keys(keys []string) (map[string]*models.Media, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	placeholders := ""
	args := make([]any, len(keys))
	for i, k := range keys {
		if i > 0 {
			placeholders += ", "
		}
		placeholders += fmt.Sprintf("$%d", i+1)
		args[i] = k
	}

	rows, err := s.db.Query(`
		SELECT `+mediaColumns+`
		FROM media
		WHERE s3_key IN (`+placeholders+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("find media by s3 keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string]*models.Media)
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, fmt.Errorf("scan media: %w", err)
		}
		result[m.S3Key] = m
	}
	return result, rows.Err()
}

// Count returns the total number of media items.
func (s *MediaStore) Count(tenantID uuid.UUID) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM media WHERE tenant_id = $1`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count media: %w", err)
	}
	return count, nil
}
