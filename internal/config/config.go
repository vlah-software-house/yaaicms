// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package config handles application configuration loading from environment
// variables. It provides a centralized Config struct used across the application.
package config

import (
	"fmt"
	"os"
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
		ValkeyPassword: os.Getenv("VALKEY_PASSWORD"),

		BaseDomain: envOrDefault("BASE_DOMAIN", "localhost"),

		AIProvider: envOrDefault("AI_PROVIDER", "gemini"),

		OpenAIKey:          os.Getenv("OPENAI_API_KEY"),
		OpenAIModel:        envOrDefault("OPENAI_MODEL", "gpt-4o"),
		OpenAIModelLight:   os.Getenv("OPENAI_MODEL_LIGHT"),
		OpenAIModelContent: os.Getenv("OPENAI_MODEL_CONTENT"),
		OpenAIModelTemplate: os.Getenv("OPENAI_MODEL_TEMPLATE"),
		OpenAIModelImage:   envOrDefault("OPENAI_MODEL_IMAGE", "dall-e-3"),
		OpenAIBaseURL:      envOrDefault("OPENAI_BASE_URL", "https://api.openai.com/v1"),

		GeminiKey:          os.Getenv("GEMINI_API_KEY"),
		GeminiModel:        envOrDefault("GEMINI_MODEL", "gemini-3.1-pro-preview"),
		GeminiModelLight:   os.Getenv("GEMINI_MODEL_LIGHT"),
		GeminiModelContent: os.Getenv("GEMINI_MODEL_CONTENT"),
		GeminiModelTemplate: os.Getenv("GEMINI_MODEL_TEMPLATE"),
		GeminiModelImage:   os.Getenv("GEMINI_MODEL_IMAGE"),
		GeminiBaseURL:      envOrDefault("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com"),

		ClaudeKey:          os.Getenv("CLAUDE_API_KEY"),
		ClaudeModel:        envOrDefault("CLAUDE_MODEL", "claude-sonnet-4-6"),
		ClaudeModelLight:   os.Getenv("CLAUDE_MODEL_LIGHT"),
		ClaudeModelContent: os.Getenv("CLAUDE_MODEL_CONTENT"),
		ClaudeModelTemplate: os.Getenv("CLAUDE_MODEL_TEMPLATE"),
		ClaudeBaseURL:      envOrDefault("CLAUDE_BASE_URL", "https://api.anthropic.com"),

		MistralKey:          os.Getenv("MISTRAL_API_KEY"),
		MistralModel:        envOrDefault("MISTRAL_MODEL", "mistral-large-latest"),
		MistralModelLight:   os.Getenv("MISTRAL_MODEL_LIGHT"),
		MistralModelContent: os.Getenv("MISTRAL_MODEL_CONTENT"),
		MistralModelTemplate: os.Getenv("MISTRAL_MODEL_TEMPLATE"),
		MistralBaseURL:      envOrDefault("MISTRAL_BASE_URL", "https://api.mistral.ai/v1"),

		S3Endpoint:      os.Getenv("S3_ENDPOINT"),
		S3Region:        envOrDefault("S3_REGION", "fsn1"),
		S3AccessKey:     os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:     os.Getenv("S3_SECRET_KEY"),
		S3BucketPublic:  envOrDefault("S3_BUCKET_PUBLIC", "yaaicms-public"),
		S3BucketPrivate: envOrDefault("S3_BUCKET_PRIVATE", "yaaicms-private"),
		S3PublicURL:     os.Getenv("S3_PUBLIC_URL"),
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

// envOrDefault reads an environment variable, returning a fallback if unset or empty.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
