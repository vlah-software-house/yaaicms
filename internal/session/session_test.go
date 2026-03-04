// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// testValkeyClient returns a Redis client connected to the test Valkey.
// Skips the test if Valkey is unavailable.
func testValkeyClient(t *testing.T) *redis.Client {
	t.Helper()

	host := envOr("VALKEY_HOST", "localhost")
	port := envOr("VALKEY_PORT", "6379")
	password := os.Getenv("VALKEY_PASSWORD")

	client := redis.NewClient(&redis.Options{
		Addr:     host + ":" + port,
		Password: password,
		DB:       15, // Use DB 15 for tests to isolate from dev data.
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Skipf("skipping integration test: Valkey not reachable: %v", err)
	}

	t.Cleanup(func() {
		// Clean up test keys.
		keys, _ := client.Keys(ctx, "session:*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		client.Close()
	})

	return client
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestSessionCreateAndGet(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	w := httptest.NewRecorder()
	ctx := context.Background()

	data := &Data{
		UserID:       uuid.New(),
		Email:        "test@session.local",
		DisplayName:  "Test User",
		IsSuperAdmin: true,
		TenantRole:   "admin",
		TwoFADone:    false,
	}

	sessionID, err := store.Create(ctx, w, data)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}

	// Verify cookie was set.
	resp := w.Result()
	cookies := resp.Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == CookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}
	if sessionCookie.HttpOnly != true {
		t.Error("expected HttpOnly cookie")
	}
	if sessionCookie.Secure != false {
		t.Error("expected Secure=false for non-secure store")
	}

	// Get the session back.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(sessionCookie)

	retrieved, err := store.Get(ctx, req)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected session data, got nil")
	}
	if retrieved.Email != "test@session.local" {
		t.Errorf("email: got %q, want %q", retrieved.Email, "test@session.local")
	}
	if retrieved.UserID != data.UserID {
		t.Errorf("userID: got %s, want %s", retrieved.UserID, data.UserID)
	}
	if retrieved.TenantRole != "admin" {
		t.Errorf("tenant role: got %q, want %q", retrieved.TenantRole, "admin")
	}
}

func TestSessionGetNoCookie(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	req := httptest.NewRequest("GET", "/", nil)
	data, err := store.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("Get (no cookie): %v", err)
	}
	if data != nil {
		t.Error("expected nil for request without session cookie")
	}
}

func TestSessionGetExpired(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	// Request with a cookie pointing to a nonexistent session.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "nonexistent-session-id"})

	data, err := store.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("Get (expired): %v", err)
	}
	if data != nil {
		t.Error("expected nil for expired/nonexistent session")
	}
}

func TestSessionUpdate(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	w := httptest.NewRecorder()
	ctx := context.Background()

	data := &Data{
		UserID:      uuid.New(),
		Email:       "update@session.local",
		DisplayName: "Update User",
		TenantRole:  "editor",
		TwoFADone:   false,
	}

	store.Create(ctx, w, data)
	cookie := w.Result().Cookies()[0]

	// Update: set 2FA done.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)

	data.TwoFADone = true
	if err := store.Update(ctx, req, data); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Verify the update persisted.
	retrieved, _ := store.Get(ctx, req)
	if retrieved == nil {
		t.Fatal("expected session after update")
	}
	if !retrieved.TwoFADone {
		t.Error("expected TwoFADone=true after update")
	}
}

func TestSessionUpdateNoCookie(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	req := httptest.NewRequest("GET", "/", nil)
	err := store.Update(context.Background(), req, &Data{})
	if err == nil {
		t.Error("expected error when updating without cookie")
	}
}

func TestSessionDestroy(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	w := httptest.NewRecorder()
	ctx := context.Background()

	data := &Data{
		UserID:      uuid.New(),
		Email:       "destroy@session.local",
		DisplayName: "Destroy User",
		TenantRole:  "admin",
	}

	store.Create(ctx, w, data)
	cookie := w.Result().Cookies()[0]

	// Destroy the session.
	w2 := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookie)

	if err := store.Destroy(ctx, w2, req); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	// Verify cookie is expired.
	resp := w2.Result()
	for _, c := range resp.Cookies() {
		if c.Name == CookieName && c.MaxAge != -1 {
			t.Error("expected MaxAge=-1 on destroyed cookie")
		}
	}

	// Verify session is gone from Valkey.
	retrieved, _ := store.Get(ctx, req)
	if retrieved != nil {
		t.Error("expected nil after destroy")
	}
}

func TestSessionDestroyNoCookie(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, false)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	// Should not error even without a cookie.
	err := store.Destroy(context.Background(), w, req)
	if err != nil {
		t.Errorf("Destroy (no cookie): %v", err)
	}
}

func TestSessionSecureCookie(t *testing.T) {
	client := testValkeyClient(t)
	store := NewStore(client, true) // secure = true

	w := httptest.NewRecorder()
	store.Create(context.Background(), w, &Data{
		UserID: uuid.New(), Email: "secure@test.local",
		DisplayName: "Secure", TenantRole: "admin",
	})

	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == CookieName {
			if !c.Secure {
				t.Error("expected Secure=true for secure store")
			}
			return
		}
	}
	t.Error("session cookie not found")
}
