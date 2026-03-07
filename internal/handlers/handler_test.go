// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// handler_test.go provides shared test infrastructure for handler integration
// tests. Tests are skipped when PostgreSQL or Valkey are unavailable.
package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"

	"yaaicms/internal/ai"
	"yaaicms/internal/cache"
	"yaaicms/internal/database"
	"yaaicms/internal/engine"
	"yaaicms/internal/middleware"
	"yaaicms/internal/render"
	"yaaicms/internal/session"
	"yaaicms/internal/store"
)

// mockAIProvider implements ai.Provider for handler tests.
type mockAIProvider struct {
	name     string
	response string
	err      error
}

func (m *mockAIProvider) Name() string { return m.name }
func (m *mockAIProvider) Generate(_ context.Context, _, _ string) (string, error) {
	return m.response, m.err
}
func (m *mockAIProvider) GenerateWithModel(_ context.Context, _, _, _ string) (string, error) {
	return m.response, m.err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// testDB opens a connection to the test PostgreSQL and runs migrations.
func testDB(t *testing.T) *sql.DB {
	t.Helper()

	host := envOr("POSTGRES_HOST", "localhost")
	port := envOr("POSTGRES_PORT", "5432")
	user := envOr("POSTGRES_USER", "yaaicms")
	pass := envOr("POSTGRES_PASSWORD", "changeme")
	name := envOr("POSTGRES_DB", "yaaicms")
	dsn := "postgres://" + user + ":" + pass + "@" + host + ":" + port + "/" + name + "?sslmode=disable"

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("skipping: cannot open DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Skipf("skipping: DB not reachable: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		_ = db.Close()
		t.Fatalf("migrate: %v", err)
	}
	goose.SetBaseFS(nil)

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// testValkeyClient returns a Redis client for handler tests on DB 15.
func testValkeyClient(t *testing.T) *redis.Client {
	t.Helper()

	host := envOr("VALKEY_HOST", "localhost")
	port := envOr("VALKEY_PORT", "6379")
	password := os.Getenv("VALKEY_PASSWORD")

	client := redis.NewClient(&redis.Options{
		Addr:     host + ":" + port,
		Password: password,
		DB:       15,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("skipping: Valkey not reachable: %v", err)
	}

	t.Cleanup(func() {
		// Clean up test session and cache keys.
		for _, pattern := range []string{"session:*", "page:*"} {
			keys, _ := client.Keys(ctx, pattern).Result()
			if len(keys) > 0 {
				client.Del(ctx, keys...)
			}
		}
		_ = client.Close()
	})

	return client
}

// testEnv holds all dependencies for handler integration tests.
type testEnv struct {
	DB            *sql.DB
	Valkey        *redis.Client
	Renderer      *render.Renderer
	Sessions      *session.Store
	ContentStore  *store.ContentStore
	UserStore     *store.UserStore
	TemplateStore *store.TemplateStore
	MediaStore    *store.MediaStore
	CacheLog      *store.CacheLogStore
	Engine        *engine.Engine
	PageCache     *cache.PageCache
	AIRegistry    *ai.Registry
	AIConfig      *AIConfig
	Admin         *Admin
	Auth          *Auth
	Public        *Public
}

// newTestEnv creates a complete test environment with all handler dependencies.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	db := testDB(t)
	vk := testValkeyClient(t)

	renderer, err := render.New(true)
	if err != nil {
		t.Fatalf("render.New: %v", err)
	}

	sessions := session.NewStore(vk, false)
	contentStore := store.NewContentStore(db)
	userStore := store.NewUserStore(db)
	templateStore := store.NewTemplateStore(db)
	mediaStore := store.NewMediaStore(db)
	cacheLogStore := store.NewCacheLogStore(db)
	eng := engine.New(templateStore)
	pageCache := cache.NewPageCache(vk, 1*time.Minute)

	// Create a mock AI registry with a test provider.
	aiRegistry := ai.NewRegistry("test", map[string]ai.ProviderConfig{})
	aiRegistry.Register("test", &mockAIProvider{
		name:     "test",
		response: "mock AI response",
	})

	aiCfg := &AIConfig{
		ActiveProvider: "test",
		Providers: []AIProviderInfo{
			{Name: "test", Label: "Test Provider", HasKey: true, Active: true, Model: "test-model"},
		},
	}

	siteSettingStore := store.NewSiteSettingStore(db)
	categoryStore := store.NewCategoryStore(db)
	menuStore := store.NewMenuStore(db)
	userProfileStore := store.NewUserProfileStore(db)
	admin := NewAdmin(renderer, sessions, contentStore, userStore, templateStore,
		mediaStore, nil, nil, nil, nil, siteSettingStore, categoryStore, menuStore, userProfileStore, nil, eng, pageCache, cacheLogStore, aiRegistry, aiCfg)
	auth := NewAuth(renderer, sessions, userStore)
	public := NewPublic(eng, contentStore, siteSettingStore, menuStore, nil, nil, userProfileStore, nil, pageCache, nil, "localhost")

	return &testEnv{
		DB:            db,
		Valkey:        vk,
		Renderer:      renderer,
		Sessions:      sessions,
		ContentStore:  contentStore,
		UserStore:     userStore,
		TemplateStore: templateStore,
		MediaStore:    mediaStore,
		CacheLog:      cacheLogStore,
		Engine:        eng,
		PageCache:     pageCache,
		AIRegistry:    aiRegistry,
		AIConfig:      aiCfg,
		Admin:         admin,
		Auth:          auth,
		Public:        public,
	}
}

// ctxWithSession adds session data to a context using the middleware key.
func ctxWithSession(ctx context.Context, data *session.Data) context.Context {
	return context.WithValue(ctx, middleware.SessionKey, data)
}

// testSession creates a session.Data for testing.
// The role parameter is used as TenantRole. Pass "admin" for admin tests.
func testSession(userID uuid.UUID, email, role string, twoFADone bool) *session.Data {
	return &session.Data{
		UserID:       userID,
		Email:        email,
		DisplayName:  "Test User",
		IsSuperAdmin: role == "admin",
		TenantRole:   role,
		TwoFADone:    twoFADone,
	}
}

// withChiURLParam adds a chi URL parameter to a request.
func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// withChiURLParamAndSession adds both chi URL param and session to a request.
func withChiURLParamAndSession(r *http.Request, key, value string, sess *session.Data) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, middleware.SessionKey, sess)
	return r.WithContext(ctx)
}

// testAuthorID returns a valid user ID for content creation.
func testAuthorID(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("no users in database — run seed first: %v", err)
	}
	return id
}

// cleanContent removes test content by slug.
func cleanContent(t *testing.T, db *sql.DB, slugs ...string) {
	t.Helper()
	for _, s := range slugs {
		_, _ = db.Exec("DELETE FROM content WHERE slug = $1", s)
	}
}

// cleanTemplates removes test templates by name.
func cleanTemplates(t *testing.T, db *sql.DB, names ...string) {
	t.Helper()
	for _, n := range names {
		_, _ = db.Exec("DELETE FROM templates WHERE name = $1", n)
	}
}
