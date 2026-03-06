// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package ai provides a unified interface for interacting with multiple
// LLM providers (OpenAI, Gemini, Claude, Mistral). Each provider implements
// the Provider interface, and the Registry selects the active one by name.
package ai

import (
	"context"
	"fmt"
	"sync"
)

// Provider defines the interface that all AI providers must implement.
// Each provider handles its own HTTP communication and response parsing.
type Provider interface {
	// Generate sends a prompt to the LLM using the default model and
	// returns the generated text. systemPrompt sets the model's behaviour;
	// userPrompt is the user's request.
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)

	// GenerateWithModel is like Generate but uses a specific model instead
	// of the provider's default. If model is empty, falls back to the default.
	GenerateWithModel(ctx context.Context, model, systemPrompt, userPrompt string) (string, error)

	// Name returns the provider identifier (e.g., "openai", "gemini").
	Name() string
}

// TaskType classifies AI tasks for model tier selection.
type TaskType int

const (
	// TaskContent is for heavy tasks: article generation, rewriting.
	TaskContent TaskType = iota
	// TaskTemplate is for template generation (full HTML+CSS).
	TaskTemplate
	// TaskLight is for cheap tasks: titles, excerpts, SEO, tags.
	TaskLight
)

// ProviderConfig holds the credentials and model tier settings for a single provider.
type ProviderConfig struct {
	APIKey       string
	Model        string // Default (pro) model — used for content and templates.
	ModelLight   string // Cheaper/faster model for lightweight tasks.
	ModelContent string // Optional override for content generation + rewrite.
	ModelTemplate string // Optional override for template generation.
	ModelImage   string // Image generation model (e.g., "dall-e-3", "gemini-2.5-flash-image").
	BaseURL      string
}

// ModelForTask resolves the best model for a given task type.
// Resolution order: task-specific override → tier default → Model.
func (c ProviderConfig) ModelForTask(task TaskType) string {
	switch task {
	case TaskContent:
		if c.ModelContent != "" {
			return c.ModelContent
		}
		return c.Model
	case TaskTemplate:
		if c.ModelTemplate != "" {
			return c.ModelTemplate
		}
		return c.Model
	case TaskLight:
		if c.ModelLight != "" {
			return c.ModelLight
		}
		return c.Model
	default:
		return c.Model
	}
}

// Registry manages available AI providers and selects the active one.
// It supports runtime switching by changing the active provider name.
// All methods are safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	configs   map[string]ProviderConfig // stored for model tier resolution
	active    string
	moderator Moderator // may be nil if no moderation API is available
}

// NewRegistry creates a registry and initialises providers for every config
// that has a non-empty API key. Providers without keys are silently skipped.
// A Moderator is automatically configured: OpenAI's free moderation API is
// preferred; Mistral's paid endpoint is used as fallback.
func NewRegistry(active string, configs map[string]ProviderConfig) *Registry {
	r := &Registry{
		providers: make(map[string]Provider),
		configs:   configs,
		active:    active,
	}

	for name, cfg := range configs {
		if cfg.APIKey == "" {
			continue
		}
		switch name {
		case "openai":
			r.providers[name] = newOpenAI(cfg)
		case "gemini":
			r.providers[name] = newGemini(cfg)
		case "claude":
			r.providers[name] = newClaude(cfg)
		case "mistral":
			r.providers[name] = newMistral(cfg)
		}
	}

	// Set up prompt moderation: prefer OpenAI (free), fall back to Mistral.
	// When both keys are available, use a fallback moderator that automatically
	// switches from OpenAI to Mistral on auth errors (e.g. project-scoped keys).
	openaiCfg, hasOpenAI := configs["openai"]
	hasOpenAI = hasOpenAI && openaiCfg.APIKey != ""
	mistralCfg, hasMistral := configs["mistral"]
	hasMistral = hasMistral && mistralCfg.APIKey != ""

	if hasOpenAI && hasMistral {
		r.moderator = newFallbackModerator(
			newOpenAIModerator(openaiCfg.APIKey, openaiCfg.BaseURL),
			newMistralModerator(mistralCfg.APIKey, mistralCfg.BaseURL),
		)
	} else if hasOpenAI {
		r.moderator = newOpenAIModerator(openaiCfg.APIKey, openaiCfg.BaseURL)
	} else if hasMistral {
		r.moderator = newMistralModerator(mistralCfg.APIKey, mistralCfg.BaseURL)
	}

	return r
}

// Generate calls the active provider's Generate method using the default model.
func (r *Registry) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	p, err := r.Active()
	if err != nil {
		return "", err
	}
	return p.Generate(ctx, systemPrompt, userPrompt)
}

// GenerateForTask calls the active provider using the model tier appropriate
// for the given task type (e.g., light model for titles, pro model for content).
func (r *Registry) GenerateForTask(ctx context.Context, task TaskType, systemPrompt, userPrompt string) (string, error) {
	r.mu.RLock()
	p, ok := r.providers[r.active]
	cfg := r.configs[r.active]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("ai: no provider configured for %q", r.active)
	}

	model := cfg.ModelForTask(task)
	return p.GenerateWithModel(ctx, model, systemPrompt, userPrompt)
}

// GenerateForTaskAs is like GenerateForTask but uses a specific named provider
// instead of the globally active one. This enables per-tenant provider selection
// without mutating global state.
func (r *Registry) GenerateForTaskAs(ctx context.Context, providerName string, task TaskType, systemPrompt, userPrompt string) (string, error) {
	r.mu.RLock()
	p, ok := r.providers[providerName]
	cfg := r.configs[providerName]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("ai: no provider configured for %q", providerName)
	}

	model := cfg.ModelForTask(task)
	return p.GenerateWithModel(ctx, model, systemPrompt, userPrompt)
}

// Active returns the currently active provider.
func (r *Registry) Active() (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[r.active]
	if !ok {
		return nil, fmt.Errorf("ai: no provider configured for %q", r.active)
	}
	return p, nil
}

// SetActive switches the active provider at runtime. Returns an error if
// the named provider has no API key configured.
func (r *Registry) SetActive(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.providers[name]; !ok {
		return fmt.Errorf("ai: provider %q is not available (no API key?)", name)
	}
	r.active = name
	return nil
}

// ActiveName returns the name of the currently active provider.
func (r *Registry) ActiveName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.active
}

// Available returns the names of all providers that have valid API keys.
func (r *Registry) Available() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var names []string
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Register adds or replaces a provider in the registry. This allows injecting
// custom providers at runtime (e.g. for testing or plugin-based providers).
func (r *Registry) Register(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
}

// CheckPrompt runs the user prompt through the moderation API before
// generation. Returns nil if the prompt is safe or if no moderator is
// configured (graceful degradation — providers still have their own
// built-in safety filters). Returns a *ModerationResult with Safe=false
// and flagged Categories if the prompt violates policies.
func (r *Registry) CheckPrompt(ctx context.Context, prompt string) (*ModerationResult, error) {
	if r.moderator == nil {
		return &ModerationResult{Safe: true}, nil
	}
	return r.moderator.CheckSafety(ctx, prompt)
}

// HasProvider checks whether a named provider is configured and available.
func (r *Registry) HasProvider(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.providers[name]
	return ok
}
