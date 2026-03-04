// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"yaaicms/internal/models"
)

const (
	// TenantKey is the context key for the resolved tenant.
	TenantKey contextKey = "tenant"

	// tenantCachePrefix is the Valkey key prefix for cached tenant lookups.
	tenantCachePrefix = "tenant:"

	// tenantCacheTTL is how long tenant lookups are cached in Valkey.
	tenantCacheTTL = 5 * time.Minute
)

// TenantFinder is the interface needed by the tenant middleware to look up
// tenants by subdomain. Satisfied by store.TenantStore.
type TenantFinder interface {
	FindBySubdomain(subdomain string) (*models.Tenant, error)
}

// ResolveTenant extracts the subdomain from the Host header, looks up the
// tenant in the database (with Valkey caching), and injects it into the
// request context. Returns 404 for unknown or inactive tenants.
//
// baseDomain is the platform's base domain (e.g., "smartpress.io").
// Requests to "blog1.smartpress.io" resolve subdomain "blog1".
func ResolveTenant(finder TenantFinder, cache *redis.Client, baseDomain string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			subdomain := extractSubdomain(r.Host, baseDomain)
			if subdomain == "" {
				// No subdomain — treat as the default tenant.
				// In development, localhost requests use "default" tenant.
				subdomain = "default"
			}

			// Try Valkey cache first.
			tenant := getCachedTenant(r.Context(), cache, subdomain)

			// Cache miss — hit the database.
			if tenant == nil {
				var err error
				tenant, err = finder.FindBySubdomain(subdomain)
				if err != nil {
					slog.Error("tenant lookup failed", "subdomain", subdomain, "error", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				if tenant == nil || !tenant.IsActive {
					http.NotFound(w, r)
					return
				}
				// Cache the result.
				cacheTenant(r.Context(), cache, subdomain, tenant)
			}

			ctx := context.WithValue(r.Context(), TenantKey, tenant)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantFromCtx extracts the tenant from the request context.
// Returns nil if no tenant was resolved (e.g., health check routes).
func TenantFromCtx(ctx context.Context) *models.Tenant {
	t, _ := ctx.Value(TenantKey).(*models.Tenant)
	return t
}

// extractSubdomain extracts the subdomain from a host string given a base domain.
// For "blog1.smartpress.io" with baseDomain "smartpress.io", returns "blog1".
// For "localhost:8080" with baseDomain "localhost", returns "".
func extractSubdomain(host, baseDomain string) string {
	// Strip port if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Strip the base domain suffix.
	if !strings.HasSuffix(host, baseDomain) {
		return ""
	}

	prefix := strings.TrimSuffix(host, baseDomain)
	prefix = strings.TrimSuffix(prefix, ".")

	// No subdomain — bare domain.
	if prefix == "" {
		return ""
	}

	// Return the last component (handles nested subdomains).
	parts := strings.Split(prefix, ".")
	return parts[len(parts)-1]
}

// getCachedTenant retrieves a tenant from Valkey cache by subdomain.
// Returns nil on cache miss or error.
func getCachedTenant(ctx context.Context, cache *redis.Client, subdomain string) *models.Tenant {
	if cache == nil {
		return nil
	}

	// We store a simple serialized format: "id|name|subdomain|active"
	val, err := cache.Get(ctx, tenantCachePrefix+subdomain).Result()
	if err != nil {
		return nil
	}

	parts := strings.SplitN(val, "|", 4)
	if len(parts) != 4 {
		return nil
	}

	id, err := uuid.Parse(parts[0])
	if err != nil {
		return nil
	}

	isActive := parts[3] == "1"
	if !isActive {
		return nil
	}

	return &models.Tenant{
		ID:        id,
		Name:      parts[1],
		Subdomain: parts[2],
		IsActive:  isActive,
	}
}

// cacheTenant stores a tenant in the Valkey cache.
func cacheTenant(ctx context.Context, cache *redis.Client, subdomain string, t *models.Tenant) {
	if cache == nil {
		return
	}

	active := "0"
	if t.IsActive {
		active = "1"
	}

	val := fmt.Sprintf("%s|%s|%s|%s", t.ID.String(), t.Name, t.Subdomain, active)
	cache.Set(ctx, tenantCachePrefix+subdomain, val, tenantCacheTTL)
}
