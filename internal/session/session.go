// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package session provides Valkey-backed HTTP session management.
// Sessions are identified by a secure cookie and stored as JSON in Valkey
// with automatic TTL expiry.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	// CookieName is the name of the session cookie sent to the browser.
	CookieName = "sp_session"

	// DefaultTTL is how long a session lives in Valkey before automatic expiry.
	DefaultTTL = 24 * time.Hour

	// keyPrefix namespaces session keys in Valkey to avoid collisions.
	keyPrefix = "session:"

	// idLength is the byte length of the random session ID (32 bytes = 64 hex chars).
	idLength = 32
)

// Data holds the session payload stored in Valkey. It contains the
// authenticated user's identity, tenant context, and 2FA completion status.
type Data struct {
	UserID       uuid.UUID `json:"user_id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"display_name"`
	IsSuperAdmin bool      `json:"is_super_admin"`
	TenantID     uuid.UUID `json:"tenant_id"`     // Active tenant (zero if not yet selected)
	TenantRole   string    `json:"tenant_role"`    // User's role in the active tenant
	TwoFADone    bool      `json:"two_fa_done"`
	CreatedAt    time.Time `json:"created_at"`
}

// Store manages session lifecycle in Valkey.
type Store struct {
	client     *redis.Client
	ttl        time.Duration
	secureCook bool // true when running behind TLS (non-development)
}

// NewStore creates a session store backed by the given Valkey client.
// Set secure to true in production/testing to mark cookies as Secure
// (browser will only send them over HTTPS).
func NewStore(client *redis.Client, secure bool) *Store {
	return &Store{
		client:     client,
		ttl:        DefaultTTL,
		secureCook: secure,
	}
}

// Create generates a new session, stores it in Valkey, and sets the
// session cookie on the response. Returns the session ID.
func (s *Store) Create(ctx context.Context, w http.ResponseWriter, data *Data) (string, error) {
	id, err := generateID()
	if err != nil {
		return "", fmt.Errorf("session create: %w", err)
	}

	data.CreatedAt = time.Now()

	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("session marshal: %w", err)
	}

	if err := s.client.Set(ctx, keyPrefix+id, payload, s.ttl).Err(); err != nil {
		return "", fmt.Errorf("session store: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCook,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(s.ttl.Seconds()),
	})

	return id, nil
}

// Get retrieves session data from Valkey using the session ID from the
// request cookie. Returns nil if no valid session exists.
func (s *Store) Get(ctx context.Context, r *http.Request) (*Data, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, nil // No cookie = no session (not an error)
	}

	payload, err := s.client.Get(ctx, keyPrefix+cookie.Value).Bytes()
	if err == redis.Nil {
		return nil, nil // Session expired or doesn't exist
	}
	if err != nil {
		return nil, fmt.Errorf("session get: %w", err)
	}

	var data Data
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("session unmarshal: %w", err)
	}

	return &data, nil
}

// Update replaces the session data in Valkey without changing the session
// ID or cookie. Resets the TTL.
func (s *Store) Update(ctx context.Context, r *http.Request, data *Data) error {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return fmt.Errorf("session update: no cookie")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("session marshal: %w", err)
	}

	if err := s.client.Set(ctx, keyPrefix+cookie.Value, payload, s.ttl).Err(); err != nil {
		return fmt.Errorf("session update: %w", err)
	}

	return nil
}

// Destroy removes the session from Valkey and clears the cookie.
func (s *Store) Destroy(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil // No cookie, nothing to destroy
	}

	s.client.Del(ctx, keyPrefix+cookie.Value)

	// Expire the cookie immediately — must match Create's attributes.
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCook,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	return nil
}

// generateID creates a cryptographically random session identifier.
func generateID() (string, error) {
	b := make([]byte, idLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
