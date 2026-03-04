// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// page.go provides a Valkey-backed full-page HTML cache (L2).
// When a public page is rendered by the template engine, the resulting HTML
// is stored in Valkey so subsequent requests skip the DB query and template
// execution entirely.
package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// pageKeyPrefix is the Valkey key prefix for cached pages.
	pageKeyPrefix = "page:"

	// DefaultPageTTL is how long a rendered page stays cached.
	DefaultPageTTL = 5 * time.Minute
)

// PageCache manages full-page HTML caching in Valkey.
type PageCache struct {
	client *redis.Client
	ttl    time.Duration
}

// NewPageCache creates a new page cache backed by the given Valkey client.
func NewPageCache(client *redis.Client, ttl time.Duration) *PageCache {
	if ttl == 0 {
		ttl = DefaultPageTTL
	}
	return &PageCache{client: client, ttl: ttl}
}

// Get retrieves cached HTML for a page key. Returns empty string on miss.
func (pc *PageCache) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := pc.client.Get(ctx, pageKeyPrefix+key).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		slog.Warn("page cache get error", "key", key, "error", err)
		return nil, false
	}
	slog.Debug("page cache hit", "key", key)
	return val, true
}

// Set stores rendered HTML for a page key with the configured TTL.
func (pc *PageCache) Set(ctx context.Context, key string, html []byte) {
	if err := pc.client.Set(ctx, pageKeyPrefix+key, html, pc.ttl).Err(); err != nil {
		slog.Warn("page cache set error", "key", key, "error", err)
	}
}

// InvalidatePage removes a single page from the cache by its slug.
func (pc *PageCache) InvalidatePage(ctx context.Context, slug string) {
	if err := pc.client.Del(ctx, pageKeyPrefix+slug).Err(); err != nil {
		slog.Warn("page cache invalidate error", "slug", slug, "error", err)
	}
	slog.Debug("page cache invalidated", "slug", slug)
}

// InvalidateHomepage removes the cached homepage.
func (pc *PageCache) InvalidateHomepage(ctx context.Context) {
	pc.InvalidatePage(ctx, "_homepage")
}

// InvalidateAll removes all cached pages by scanning for the prefix.
// Used when templates change, since any page could be affected.
func (pc *PageCache) InvalidateAll(ctx context.Context) {
	var cursor uint64
	var deleted int
	for {
		keys, nextCursor, err := pc.client.Scan(ctx, cursor, pageKeyPrefix+"*", 100).Result()
		if err != nil {
			slog.Warn("page cache scan error", "error", err)
			return
		}
		if len(keys) > 0 {
			if err := pc.client.Del(ctx, keys...).Err(); err != nil {
				slog.Warn("page cache bulk delete error", "error", err)
			}
			deleted += len(keys)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	if deleted > 0 {
		slog.Info("page cache fully cleared", "deleted", deleted)
	}
}

// HomepageKey returns the cache key for a tenant's homepage.
func HomepageKey(tenantID string) string {
	return tenantID + ":_homepage"
}

// SlugKey returns the cache key for a content slug scoped to a tenant.
func SlugKey(tenantID, slug string) string {
	return fmt.Sprintf("%s:%s", tenantID, slug)
}

// InvalidateAllForTenant removes all cached pages for a specific tenant.
func (pc *PageCache) InvalidateAllForTenant(ctx context.Context, tenantID string) {
	var cursor uint64
	var deleted int
	pattern := pageKeyPrefix + tenantID + ":*"
	for {
		keys, nextCursor, err := pc.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			slog.Warn("page cache tenant scan error", "error", err)
			return
		}
		if len(keys) > 0 {
			if err := pc.client.Del(ctx, keys...).Err(); err != nil {
				slog.Warn("page cache tenant bulk delete error", "error", err)
			}
			deleted += len(keys)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	if deleted > 0 {
		slog.Info("page cache cleared for tenant", "tenant_id", tenantID, "deleted", deleted)
	}
}
