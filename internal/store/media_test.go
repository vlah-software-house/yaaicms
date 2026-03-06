// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"testing"

	"github.com/google/uuid"

	"yaaicms/internal/models"
)

// testMediaTenantID is a fixed tenant ID used across media store tests.
var testMediaTenantID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func TestMediaStoreCreateAndFind(t *testing.T) {
	db := testDB(t)
	s := NewMediaStore(db)

	// Need a valid uploader (user) ID.
	var uploaderID uuid.UUID
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&uploaderID); err != nil {
		t.Skip("no users in database")
	}

	s3Key := "media/test/" + uuid.NewString()[:8] + ".jpg"
	t.Cleanup(func() { cleanMediaByKey(t, db, s3Key) })

	media := &models.Media{
		Filename:     "test.jpg",
		OriginalName: "original.jpg",
		ContentType:  "image/jpeg",
		SizeBytes:    1024,
		Bucket:       "public",
		S3Key:        s3Key,
		UploaderID:   uploaderID,
	}

	created, err := s.Create(testMediaTenantID, media)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if created.Filename != "test.jpg" {
		t.Errorf("filename: got %q, want %q", created.Filename, "test.jpg")
	}
	if created.SizeBytes != 1024 {
		t.Errorf("size: got %d, want 1024", created.SizeBytes)
	}

	// FindByID.
	found, err := s.FindByID(testMediaTenantID, created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil {
		t.Fatal("expected media, got nil")
	}
	if found.S3Key != s3Key {
		t.Errorf("s3_key: got %q, want %q", found.S3Key, s3Key)
	}

	// Not found.
	found, _ = s.FindByID(testMediaTenantID, uuid.New())
	if found != nil {
		t.Error("expected nil for random UUID")
	}
}

func TestMediaStoreList(t *testing.T) {
	db := testDB(t)
	s := NewMediaStore(db)

	var uploaderID uuid.UUID
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&uploaderID); err != nil {
		t.Skip("no users in database")
	}

	key1 := "media/test/list-" + uuid.NewString()[:8] + ".jpg"
	key2 := "media/test/list-" + uuid.NewString()[:8] + ".png"
	t.Cleanup(func() { cleanMediaByKey(t, db, key1, key2) })

	_, _ = s.Create(testMediaTenantID, &models.Media{
		Filename: "a.jpg", OriginalName: "a.jpg", ContentType: "image/jpeg",
		SizeBytes: 100, Bucket: "public", S3Key: key1, UploaderID: uploaderID,
	})
	_, _ = s.Create(testMediaTenantID, &models.Media{
		Filename: "b.png", OriginalName: "b.png", ContentType: "image/png",
		SizeBytes: 200, Bucket: "public", S3Key: key2, UploaderID: uploaderID,
	})

	items, err := s.List(testMediaTenantID, 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) < 2 {
		t.Errorf("expected at least 2 items, got %d", len(items))
	}

	// Pagination: limit 1.
	items, err = s.List(testMediaTenantID, 1, 0)
	if err != nil {
		t.Fatalf("List(1,0): %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item with limit=1, got %d", len(items))
	}
}

func TestMediaStoreDelete(t *testing.T) {
	db := testDB(t)
	s := NewMediaStore(db)

	var uploaderID uuid.UUID
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&uploaderID); err != nil {
		t.Skip("no users in database")
	}

	key := "media/test/del-" + uuid.NewString()[:8] + ".jpg"

	_, _ = s.Create(testMediaTenantID, &models.Media{
		Filename: "del.jpg", OriginalName: "del.jpg", ContentType: "image/jpeg",
		SizeBytes: 100, Bucket: "public", S3Key: key, UploaderID: uploaderID,
	})

	// Get the ID from the DB.
	var id uuid.UUID
	_ = db.QueryRow("SELECT id FROM media WHERE s3_key = $1", key).Scan(&id)

	deleted, err := s.Delete(testMediaTenantID, id)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted == nil {
		t.Fatal("expected deleted media record returned")
	}
	if deleted.S3Key != key {
		t.Errorf("deleted s3_key: got %q, want %q", deleted.S3Key, key)
	}

	// Verify gone.
	found, _ := s.FindByID(testMediaTenantID, id)
	if found != nil {
		t.Error("expected nil after delete")
	}

	// Delete nonexistent — should return nil.
	deleted, _ = s.Delete(testMediaTenantID, uuid.New())
	if deleted != nil {
		t.Error("expected nil for nonexistent delete")
	}
}

func TestMediaStoreCount(t *testing.T) {
	db := testDB(t)
	s := NewMediaStore(db)

	count, err := s.Count(testMediaTenantID)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count < 0 {
		t.Error("expected non-negative count")
	}
}
