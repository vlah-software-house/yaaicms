// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestCacheLogStoreLog(t *testing.T) {
	db := testDB(t)
	s := NewCacheLogStore(db)

	// Log should not error (best-effort).
	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	entityID := uuid.New()
	s.Log(tenantID, "content", entityID, "update")

	// Clean up.
	t.Cleanup(func() {
		db.Exec("DELETE FROM cache_invalidation_log WHERE entity_id = $1", entityID)
	})

	// Verify entry was written.
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM cache_invalidation_log WHERE entity_id = $1", entityID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 log entry, got %d", count)
	}
}

func TestCacheLogStoreRecentEntries(t *testing.T) {
	db := testDB(t)
	s := NewCacheLogStore(db)

	// Insert a few log entries.
	tenantID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id1 := uuid.New()
	id2 := uuid.New()
	s.Log(tenantID, "content", id1, "create")
	s.Log(tenantID, "template", id2, "delete")

	t.Cleanup(func() {
		db.Exec("DELETE FROM cache_invalidation_log WHERE entity_id IN ($1, $2)", id1, id2)
	})

	entries, err := s.RecentEntries(tenantID, 10)
	if err != nil {
		t.Fatalf("RecentEntries: %v", err)
	}

	if len(entries) < 2 {
		t.Errorf("expected at least 2 entries, got %d", len(entries))
	}

	// Most recent should be first.
	if len(entries) >= 2 {
		if entries[0].InvalidatedAt < entries[1].InvalidatedAt {
			t.Error("expected entries ordered by invalidated_at DESC")
		}
	}
}
