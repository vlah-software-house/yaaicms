// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package config handles application configuration loading from environment
// variables. It provides a centralized Config struct used across the application.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all application configuration values loaded from the environment.
type Config struct {
	// Server settings
	Host string
	Port string
	Env  string // "development", "production", "testing"

	// PostgreSQL connection
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string

	// Valkey (Redis-compatible cache)
	ValkeyHost     string
	ValkeyPort     string
	ValkeyPassword string

	// AI providers — keys for all supported providers; AIProvider selects
	// the default on startup. Switchable at runtime from admin Settings.
	AIProvider string // Default active: "openai", "gemini", "claude", "mistral"

	// Per-provider credentials and model tiers.
	// MODEL is the default (pro) model. MODEL_LIGHT is for cheap tasks
	// (titles, excerpts, SEO, tags). MODEL_CONTENT and MODEL_TEMPLATE
	// are optional overrides; they fall back to MODEL when unset.
	OpenAIKey          string
	OpenAIModel        string
	OpenAIModelLight   string
	OpenAIModelContent string
	OpenAIModelTemplate string
	OpenAIModelImage   string
	OpenAIBaseURL      string

	GeminiKey          string
	GeminiModel        string
	GeminiModelLight   string
	GeminiModelContent string
	GeminiModelTemplate string
	GeminiModelImage   string
	GeminiBaseURL      string

	ClaudeKey          string
	ClaudeModel        string
	ClaudeModelLight   string
	ClaudeModelContent string
	ClaudeModelTemplate string
	ClaudeBaseURL      string

	MistralKey          string
	MistralModel        string
	MistralModelLight   string
	MistralModelContent string
	MistralModelTemplate string
	MistralBaseURL      string

	// Multi-tenancy
	BaseDomain string // e.g. "smartpress.io" — tenants are {subdomain}.smartpress.io

	// Kubernetes integration (custom domain TLS provisioning)
	K8sEnabled   bool   // Enable K8s resource management for custom domains
	K8sNamespace string // K8s namespace for resources

	// S3-compatible object storage (Hetzner CEPH)
	S3Endpoint      string
	S3Region        string
	S3AccessKey     string
	S3SecretKey     string
	S3BucketPublic  string
	S3BucketPrivate string
	S3PublicURL     string // Optional CDN/public URL for serving files
}

// Load reads configuration from environment variables, applying defaults
// for development where appropriate. Returns an error if critical values
// are missing in production mode.
func Load() (*Config, error) {
	cfg := &Config{
		Host: envOrDefault("APP_HOST", "0.0.0.0"),
		Port: envOrDefault("APP_PORT", "8080"),
		Env:  envOrDefault("APP_ENV", "development"),

		DBHost:     envOrDefault("POSTGRES_HOST", "localhost"),
		DBPort:     envOrDefault("POSTGRES_PORT", "5432"),
		DBUser:     envOrDefault("POSTGRES_USER", "yaaicms"),
		DBPassword: envOrDefault("POSTGRES_PASSWORD", "changeme"),
		DBName:     envOrDefault("POSTGRES_DB", "yaaicms"),

		ValkeyHost:     envOrDefault("VALKEY_HOST", "localhost"),
		ValkeyPort:     envOrDefault("VALKEY_PORT", "6379"),
		ValkeyPassword: env("VALKEY_PASSWORD"),

		BaseDomain:   envOrDefault("BASE_DOMAIN", "localhost"),
		K8sEnabled:   env("K8S_ENABLED") == "true",
		K8sNamespace: envOrDefault("K8S_NAMESPACE", "yaaicms"),

		AIProvider: envOrDefault("AI_PROVIDER", "gemini"),

		OpenAIKey:          env("OPENAI_API_KEY"),
		OpenAIModel:        envOrDefault("OPENAI_MODEL", "gpt-4o"),
		OpenAIModelLight:   env("OPENAI_MODEL_LIGHT"),
		OpenAIModelContent: env("OPENAI_MODEL_CONTENT"),
		OpenAIModelTemplate: env("OPENAI_MODEL_TEMPLATE"),
		OpenAIModelImage:   envOrDefault("OPENAI_MODEL_IMAGE", "dall-e-3"),
		OpenAIBaseURL:      envOrDefault("OPENAI_BASE_URL", "https://api.openai.com/v1"),

		GeminiKey:          env("GEMINI_API_KEY"),
		GeminiModel:        envOrDefault("GEMINI_MODEL", "gemini-3.1-pro-preview"),
		GeminiModelLight:   env("GEMINI_MODEL_LIGHT"),
		GeminiModelContent: env("GEMINI_MODEL_CONTENT"),
		GeminiModelTemplate: env("GEMINI_MODEL_TEMPLATE"),
		GeminiModelImage:   env("GEMINI_MODEL_IMAGE"),
		GeminiBaseURL:      envOrDefault("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),

		ClaudeKey:          env("CLAUDE_API_KEY"),
		ClaudeModel:        envOrDefault("CLAUDE_MODEL", "claude-sonnet-4-6"),
		ClaudeModelLight:   env("CLAUDE_MODEL_LIGHT"),
		ClaudeModelContent: env("CLAUDE_MODEL_CONTENT"),
		ClaudeModelTemplate: env("CLAUDE_MODEL_TEMPLATE"),
		ClaudeBaseURL:      envOrDefault("CLAUDE_BASE_URL", "https://api.anthropic.com"),

		MistralKey:          env("MISTRAL_API_KEY"),
		MistralModel:        envOrDefault("MISTRAL_MODEL", "mistral-large-latest"),
		MistralModelLight:   env("MISTRAL_MODEL_LIGHT"),
		MistralModelContent: env("MISTRAL_MODEL_CONTENT"),
		MistralModelTemplate: env("MISTRAL_MODEL_TEMPLATE"),
		MistralBaseURL:      envOrDefault("MISTRAL_BASE_URL", "https://api.mistral.ai/v1"),

		S3Endpoint:      env("S3_ENDPOINT"),
		S3Region:        envOrDefault("S3_REGION", "fsn1"),
		S3AccessKey:     env("S3_ACCESS_KEY"),
		S3SecretKey:     env("S3_SECRET_KEY"),
		S3BucketPublic:  envOrDefault("S3_BUCKET_PUBLIC", "yaaicms-public"),
		S3BucketPrivate: envOrDefault("S3_BUCKET_PRIVATE", "yaaicms-private"),
		S3PublicURL:     env("S3_PUBLIC_URL"),
	}

	if cfg.Env == "production" {
		if cfg.DBPassword == "changeme" {
			return nil, fmt.Errorf("POSTGRES_PASSWORD must be set in production")
		}
	}

	return cfg, nil
}

// DSN returns the PostgreSQL connection string.
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName,
	)
}

// Addr returns the server listen address (host:port).
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// IsDev returns true if the application is running in development mode.
func (c *Config) IsDev() bool {
	return c.Env == "development"
}

// env reads an environment variable, trimming any surrounding whitespace.
// This prevents issues with K8s secrets or env files that include trailing spaces.
func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// envOrDefault reads an environment variable, returning a fallback if unset or empty.
func envOrDefault(key, fallback string) string {
	if v := env(key); v != "" {
		return v
	}
	return fallback
}
