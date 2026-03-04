// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package store

import (
	"testing"

	"github.com/google/uuid"
)

func TestUserStoreCreate(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-create@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email) })

	user, err := s.Create(email, "testpass123", "Test User")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if user.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if user.Email != email {
		t.Errorf("email: got %q, want %q", user.Email, email)
	}
	if user.DisplayName != "Test User" {
		t.Errorf("display name: got %q, want %q", user.DisplayName, "Test User")
	}
	if user.TOTPEnabled {
		t.Error("expected totp_enabled=false for new user")
	}
	if user.PasswordHash == "" {
		t.Error("expected non-empty password hash")
	}
	if user.PasswordHash == "testpass123" {
		t.Error("password hash must not be plaintext")
	}
}

func TestUserStoreFindByEmail(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-findbyemail@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email) })

	// Not found case.
	user, err := s.FindByEmail(email)
	if err != nil {
		t.Fatalf("FindByEmail (not found): %v", err)
	}
	if user != nil {
		t.Error("expected nil for non-existent user")
	}

	// Create and find.
	created, err := s.Create(email, "pass", "Find Me")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	user, err = s.FindByEmail(email)
	if err != nil {
		t.Fatalf("FindByEmail: %v", err)
	}
	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.ID != created.ID {
		t.Errorf("ID mismatch: got %s, want %s", user.ID, created.ID)
	}
}

func TestUserStoreFindByID(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-findbyid@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email) })

	// Not found.
	user, err := s.FindByID(uuid.New())
	if err != nil {
		t.Fatalf("FindByID (not found): %v", err)
	}
	if user != nil {
		t.Error("expected nil for random UUID")
	}

	// Create and find.
	created, _ := s.Create(email, "pass", "By ID")
	user, err = s.FindByID(created.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.Email != email {
		t.Errorf("email: got %q, want %q", user.Email, email)
	}
}

func TestUserStoreList(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email1 := "test-list-a@store-test.local"
	email2 := "test-list-b@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email1, email2) })

	s.Create(email1, "pass", "A")
	s.Create(email2, "pass", "B")

	users, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	// Should contain at least our 2 test users (plus any existing seed data).
	if len(users) < 2 {
		t.Errorf("expected at least 2 users, got %d", len(users))
	}
}

func TestUserStoreCheckPassword(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-checkpass@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email) })

	user, _ := s.Create(email, "correct-password", "PW Check")

	if !s.CheckPassword(user, "correct-password") {
		t.Error("expected CheckPassword to return true for correct password")
	}
	if s.CheckPassword(user, "wrong-password") {
		t.Error("expected CheckPassword to return false for wrong password")
	}
	if s.CheckPassword(user, "") {
		t.Error("expected CheckPassword to return false for empty password")
	}
}

func TestUserStoreTOTPLifecycle(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-totp@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email) })

	user, _ := s.Create(email, "pass", "TOTP User")

	// Initially no TOTP.
	if user.TOTPSecret != nil {
		t.Error("expected nil TOTP secret initially")
	}
	if user.TOTPEnabled {
		t.Error("expected TOTP disabled initially")
	}

	// Set TOTP secret.
	if err := s.SetTOTPSecret(user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("SetTOTPSecret: %v", err)
	}

	user, _ = s.FindByID(user.ID)
	if user.TOTPSecret == nil || *user.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("expected TOTP secret set, got %v", user.TOTPSecret)
	}
	if user.TOTPEnabled {
		t.Error("TOTP should not be enabled yet (just set secret)")
	}

	// Enable TOTP.
	if err := s.EnableTOTP(user.ID); err != nil {
		t.Fatalf("EnableTOTP: %v", err)
	}

	user, _ = s.FindByID(user.ID)
	if !user.TOTPEnabled {
		t.Error("expected TOTP enabled after EnableTOTP")
	}

	// Reset TOTP.
	if err := s.ResetTOTP(user.ID); err != nil {
		t.Fatalf("ResetTOTP: %v", err)
	}

	user, _ = s.FindByID(user.ID)
	if user.TOTPSecret != nil {
		t.Error("expected nil TOTP secret after reset")
	}
	if user.TOTPEnabled {
		t.Error("expected TOTP disabled after reset")
	}
}

func TestUserStoreDelete(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-delete@store-test.local"
	// No cleanup needed since we're deleting.

	user, _ := s.Create(email, "pass", "Delete Me")

	if err := s.Delete(user.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	found, _ := s.FindByID(user.ID)
	if found != nil {
		t.Error("expected nil after delete")
	}
}

func TestUserStoreDuplicateEmail(t *testing.T) {
	db := testDB(t)
	s := NewUserStore(db)

	email := "test-dupe@store-test.local"
	t.Cleanup(func() { cleanUsers(t, db, email) })

	_, err := s.Create(email, "pass", "First")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = s.Create(email, "pass", "Second")
	if err == nil {
		t.Error("expected error for duplicate email, got nil")
	}
}
