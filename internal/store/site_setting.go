// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// SiteSettingStore manages site configuration in the database.
type SiteSettingStore struct {
	db *sql.DB
}

// NewSiteSettingStore returns a new SiteSettingStore backed by the given database.
func NewSiteSettingStore(db *sql.DB) *SiteSettingStore {
	return &SiteSettingStore{db: db}
}

// All returns every setting as a convenience map.
func (s *SiteSettingStore) All(tenantID uuid.UUID) (models.SiteSettings, error) {
	rows, err := s.db.Query(`SELECT key, value FROM site_settings WHERE tenant_id = $1 ORDER BY key`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(models.SiteSettings)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

// Get returns a single setting by key, or the fallback if not found.
func (s *SiteSettingStore) Get(tenantID uuid.UUID, key, fallback string) (string, error) {
	var val string
	err := s.db.QueryRow(`SELECT value FROM site_settings WHERE key = $1 AND tenant_id = $2`, key, tenantID).Scan(&val)
	if err == sql.ErrNoRows {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	if val == "" {
		return fallback, nil
	}
	return val, nil
}

// Set upserts a single setting. Creates it if it doesn't exist.
func (s *SiteSettingStore) Set(tenantID uuid.UUID, key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO site_settings (tenant_id, key, value, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, key)
		DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		tenantID, key, value, time.Now(),
	)
	return err
}

// SetMany updates multiple settings in a single transaction.
func (s *SiteSettingStore) SetMany(tenantID uuid.UUID, settings map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO site_settings (tenant_id, key, value, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, key)
		DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for k, v := range settings {
		if _, err := stmt.Exec(tenantID, k, v, now); err != nil {
			return err
		}
	}

	return tx.Commit()
}
