// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package cache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testValkeyClient returns a Redis client for tests.
// Skips if Valkey is unavailable.
func testValkeyClient(t *testing.T) *redis.Client {
	t.Helper()

	host := envOr("VALKEY_HOST", "localhost")
	port := envOr("VALKEY_PORT", "6379")
	password := os.Getenv("VALKEY_PASSWORD")

	client := redis.NewClient(&redis.Options{
		Addr:     host + ":" + port,
		Password: password,
		DB:       15, // Use DB 15 for tests.
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Skipf("skipping integration test: Valkey not reachable: %v", err)
	}

	t.Cleanup(func() {
		keys, _ := client.Keys(ctx, "page:*").Result()
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

func TestConnectValkey(t *testing.T) {
	host := envOr("VALKEY_HOST", "localhost")
	port := envOr("VALKEY_PORT", "6379")

	client, err := ConnectValkey(host, port, "")
	if err != nil {
		t.Skipf("skipping: Valkey not available: %v", err)
	}
	defer client.Close()

	// Verify connection.
	ctx := context.Background()
	pong, err := client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if pong != "PONG" {
		t.Errorf("expected PONG, got %q", pong)
	}
}

func TestPageCacheSetAndGet(t *testing.T) {
	client := testValkeyClient(t)
	pc := NewPageCache(client, 1*time.Minute)

	ctx := context.Background()

	// Miss.
	data, ok := pc.Get(ctx, "test-page")
	if ok {
		t.Error("expected cache miss")
	}
	if data != nil {
		t.Error("expected nil data on miss")
	}

	// Set.
	html := []byte("<html><body>Test Page</body></html>")
	pc.Set(ctx, "test-page", html)

	// Hit.
	data, ok = pc.Get(ctx, "test-page")
	if !ok {
		t.Error("expected cache hit")
	}
	if string(data) != string(html) {
		t.Errorf("data mismatch: got %q, want %q", data, html)
	}
}

func TestPageCacheInvalidatePage(t *testing.T) {
	client := testValkeyClient(t)
	pc := NewPageCache(client, 1*time.Minute)

	ctx := context.Background()

	pc.Set(ctx, "invalidate-me", []byte("cached"))

	// Verify it's cached.
	_, ok := pc.Get(ctx, "invalidate-me")
	if !ok {
		t.Fatal("expected cache hit before invalidation")
	}

	// Invalidate.
	pc.InvalidatePage(ctx, "invalidate-me")

	// Verify it's gone.
	_, ok = pc.Get(ctx, "invalidate-me")
	if ok {
		t.Error("expected cache miss after invalidation")
	}
}

func TestPageCacheInvalidateHomepage(t *testing.T) {
	client := testValkeyClient(t)
	pc := NewPageCache(client, 1*time.Minute)

	ctx := context.Background()

	testTenantID := "00000000-0000-0000-0000-000000000001"
	pc.Set(ctx, HomepageKey(testTenantID), []byte("homepage"))

	pc.InvalidateHomepage(ctx)

	_, ok := pc.Get(ctx, HomepageKey(testTenantID))
	if ok {
		t.Error("expected homepage cache miss after invalidation")
	}
}

func TestPageCacheInvalidateAll(t *testing.T) {
	client := testValkeyClient(t)
	pc := NewPageCache(client, 1*time.Minute)

	ctx := context.Background()

	// Set multiple pages.
	pc.Set(ctx, "page-a", []byte("a"))
	pc.Set(ctx, "page-b", []byte("b"))
	pc.Set(ctx, "page-c", []byte("c"))

	// Invalidate all.
	pc.InvalidateAll(ctx)

	// All should be gone.
	for _, key := range []string{"page-a", "page-b", "page-c"} {
		_, ok := pc.Get(ctx, key)
		if ok {
			t.Errorf("expected miss for %q after InvalidateAll", key)
		}
	}
}

func TestHomepageKey(t *testing.T) {
	testTenantID := "00000000-0000-0000-0000-000000000001"
	expected := testTenantID + ":_homepage"
	if HomepageKey(testTenantID) != expected {
		t.Errorf("HomepageKey: got %q, want %q", HomepageKey(testTenantID), expected)
	}
}

func TestSlugKey(t *testing.T) {
	testTenantID := "00000000-0000-0000-0000-000000000001"
	expected := testTenantID + ":about-us"
	if SlugKey(testTenantID, "about-us") != expected {
		t.Errorf("SlugKey: got %q, want %q", SlugKey(testTenantID, "about-us"), expected)
	}
}

func TestNewPageCacheDefaultTTL(t *testing.T) {
	client := testValkeyClient(t)

	// TTL = 0 should use default.
	pc := NewPageCache(client, 0)
	if pc.ttl != DefaultPageTTL {
		t.Errorf("expected DefaultPageTTL (%v), got %v", DefaultPageTTL, pc.ttl)
	}
}
