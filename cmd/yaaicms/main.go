// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package main is the entry point for the YaaiCMS CMS server.
// It loads configuration, connects to services, sets up routing, and starts
// the HTTP server with graceful shutdown support.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"yaaicms/internal/ai"
	"yaaicms/internal/cache"
	"yaaicms/internal/config"
	"yaaicms/internal/database"
	"yaaicms/internal/engine"
	"yaaicms/internal/handlers"
	"yaaicms/internal/imaging"
	"yaaicms/internal/k8s"
	"yaaicms/internal/render"
	"yaaicms/internal/router"
	"yaaicms/internal/session"
	"yaaicms/internal/storage"
	"yaaicms/internal/store"
)

func main() {
	// Structured logger — outputs JSON in production, text in development.
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// Load configuration from environment variables.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	slog.Info("configuration loaded",
		"env", cfg.Env,
		"addr", cfg.Addr(),
	)

	// Connect to PostgreSQL.
	db, err := database.Connect(cfg.DSN())
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	// Run pending migrations.
	if err := database.Migrate(db); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Seed initial data in development and testing environments
	// (no-op if data already exists).
	if cfg.IsDev() || cfg.Env == "testing" {
		if err := database.Seed(db); err != nil {
			slog.Error("failed to seed database", "error", err)
			os.Exit(1)
		}
	}

	// Connect to Valkey (Redis-compatible cache + session store).
	valkeyClient, err := cache.ConnectValkey(cfg.ValkeyHost, cfg.ValkeyPort, cfg.ValkeyPassword)
	if err != nil {
		slog.Error("failed to connect to valkey", "error", err)
		os.Exit(1)
	}
	defer valkeyClient.Close()

	// Initialize session store backed by Valkey.
	// In non-development environments, mark session cookies as Secure (HTTPS-only).
	secureCookies := !cfg.IsDev()
	sessionStore := session.NewStore(valkeyClient, secureCookies)

	// Initialize the HTML template renderer for admin pages.
	// In dev mode, templates load assets from CDN; in production they use
	// compiled local files embedded in the binary.
	renderer, err := render.New(cfg.IsDev())
	if err != nil {
		slog.Error("failed to initialize template renderer", "error", err)
		os.Exit(1)
	}

	// Initialize data stores.
	tenantStore := store.NewTenantStore(db)
	domainStore := store.NewTenantDomainStore(db)
	tenantResolver := store.NewTenantResolver(tenantStore, domainStore)
	userStore := store.NewUserStore(db)
	contentStore := store.NewContentStore(db)
	templateStore := store.NewTemplateStore(db)
	cacheLogStore := store.NewCacheLogStore(db)
	mediaStore := store.NewMediaStore(db)
	variantStore := store.NewVariantStore(db)
	revisionStore := store.NewRevisionStore(db)
	templateRevisionStore := store.NewTemplateRevisionStore(db)
	themeStore := store.NewDesignThemeStore(db)
	siteSettingStore := store.NewSiteSettingStore(db)
	categoryStore := store.NewCategoryStore(db)
	menuStore := store.NewMenuStore(db)
	userProfileStore := store.NewUserProfileStore(db)

	// Connect to S3-compatible object storage (optional — app works without it).
	var storageClient *storage.Client
	if cfg.S3Endpoint != "" && cfg.S3AccessKey != "" {
		storageClient, err = storage.New(
			cfg.S3Endpoint, cfg.S3Region, cfg.S3AccessKey, cfg.S3SecretKey,
			cfg.S3BucketPublic, cfg.S3BucketPrivate, cfg.S3PublicURL,
		)
		if err != nil {
			slog.Error("failed to initialize S3 storage", "error", err)
			os.Exit(1)
		}
		if storageClient != nil {
			slog.Info("s3 storage connected",
				"endpoint", cfg.S3Endpoint,
				"public_bucket", cfg.S3BucketPublic,
				"private_bucket", cfg.S3BucketPrivate,
			)
		}
	} else {
		slog.Warn("s3 storage not configured — media uploads disabled")
	}

	// Initialize libvips for responsive image variant generation.
	// Concurrency 0 lets libvips auto-detect based on CPU cores.
	imaging.Startup(0)
	defer imaging.Shutdown()

	// Initialize the dynamic template engine for public page rendering.
	eng := engine.New(templateStore)

	// Enable responsive srcset rewriting for inline content images when S3 is available.
	if storageClient != nil {
		eng.SetMediaDeps(mediaStore, variantStore, storageClient)
	}

	// Initialize the L2 page cache (full-page HTML in Valkey).
	pageCache := cache.NewPageCache(valkeyClient, cache.DefaultPageTTL)

	// Initialize the AI provider registry with all configured providers.
	aiRegistry := ai.NewRegistry(cfg.AIProvider, map[string]ai.ProviderConfig{
		"openai":  {APIKey: cfg.OpenAIKey, Model: cfg.OpenAIModel, ModelLight: cfg.OpenAIModelLight, ModelContent: cfg.OpenAIModelContent, ModelTemplate: cfg.OpenAIModelTemplate, ModelImage: cfg.OpenAIModelImage, BaseURL: cfg.OpenAIBaseURL},
		"gemini":  {APIKey: cfg.GeminiKey, Model: cfg.GeminiModel, ModelLight: cfg.GeminiModelLight, ModelContent: cfg.GeminiModelContent, ModelTemplate: cfg.GeminiModelTemplate, ModelImage: cfg.GeminiModelImage, BaseURL: cfg.GeminiBaseURL},
		"claude":  {APIKey: cfg.ClaudeKey, Model: cfg.ClaudeModel, ModelLight: cfg.ClaudeModelLight, ModelContent: cfg.ClaudeModelContent, ModelTemplate: cfg.ClaudeModelTemplate, BaseURL: cfg.ClaudeBaseURL},
		"mistral": {APIKey: cfg.MistralKey, Model: cfg.MistralModel, ModelLight: cfg.MistralModelLight, ModelContent: cfg.MistralModelContent, ModelTemplate: cfg.MistralModelTemplate, BaseURL: cfg.MistralBaseURL},
	})

	slog.Info("ai providers initialized",
		"active", aiRegistry.ActiveName(),
		"available", aiRegistry.Available(),
	)

	// Build AI provider config for the admin settings page.
	aiCfg := &handlers.AIConfig{
		ActiveProvider: cfg.AIProvider,
		Providers: []handlers.AIProviderInfo{
			{Name: "openai", Label: "OpenAI", HasKey: cfg.OpenAIKey != "", Active: cfg.AIProvider == "openai", Model: cfg.OpenAIModel, KeyEnvVar: "OPENAI_API_KEY"},
			{Name: "gemini", Label: "Google Gemini", HasKey: cfg.GeminiKey != "", Active: cfg.AIProvider == "gemini", Model: cfg.GeminiModel, KeyEnvVar: "GEMINI_API_KEY"},
			{Name: "claude", Label: "Anthropic Claude", HasKey: cfg.ClaudeKey != "", Active: cfg.AIProvider == "claude", Model: cfg.ClaudeModel, KeyEnvVar: "CLAUDE_API_KEY"},
			{Name: "mistral", Label: "Mistral", HasKey: cfg.MistralKey != "", Active: cfg.AIProvider == "mistral", Model: cfg.MistralModel, KeyEnvVar: "MISTRAL_API_KEY"},
		},
	}

	// Initialize the K8s resource manager for custom domain TLS provisioning.
	k8sManager := k8s.NewManager(cfg.K8sNamespace, cfg.K8sEnabled)

	// Create handler groups with their dependencies.
	adminHandlers := handlers.NewAdmin(renderer, sessionStore, contentStore, userStore, templateStore, mediaStore, variantStore, revisionStore, templateRevisionStore, themeStore, siteSettingStore, categoryStore, menuStore, userProfileStore, storageClient, eng, pageCache, cacheLogStore, aiRegistry, aiCfg)
	authHandlers := handlers.NewAuth(renderer, sessionStore, userStore)
	publicHandlers := handlers.NewPublic(eng, contentStore, siteSettingStore, menuStore, mediaStore, variantStore, userProfileStore, storageClient, pageCache, tenantResolver, cfg.BaseDomain)
	tenantHandlers := handlers.NewTenantAdmin(renderer, sessionStore, tenantStore, userStore, domainStore, k8sManager, valkeyClient, cfg.BaseDomain)

	// Set up the Chi router with all middleware and routes.
	r := router.New(sessionStore, adminHandlers, authHandlers, publicHandlers, tenantHandlers, tenantStore, tenantResolver, valkeyClient, cfg.BaseDomain, secureCookies)

	// Create the HTTP server with sensible timeouts.
	// WriteTimeout must accommodate AI endpoints that wait on LLM responses
	// (typically 10-30s, up to 60s for complex prompts).
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      r,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 90 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start the background domain verifier if K8s is enabled.
	// It periodically checks DNS and certificate status for custom domains.
	ctx, cancelVerifier := context.WithCancel(context.Background())
	defer cancelVerifier()
	if cfg.K8sEnabled {
		verifier := k8s.NewVerifier(domainStore, k8sManager, valkeyClient, cfg.BaseDomain)
		go verifier.Run(ctx)
	}

	// Start the server in a goroutine so we can listen for shutdown signals.
	go func() {
		slog.Info("server starting", "addr", cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown: wait for SIGINT or SIGTERM, then drain connections.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutdown signal received", "signal", sig)

	// Give active requests up to 30 seconds to complete.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}
