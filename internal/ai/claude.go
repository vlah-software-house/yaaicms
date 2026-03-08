// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// claudeProvider implements the Provider interface using the Anthropic
// Messages API (POST /v1/messages).
type claudeProvider struct {
	config ProviderConfig
	client *http.Client
}

// newClaude creates a new Anthropic Claude provider.
func newClaude(cfg ProviderConfig) *claudeProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.anthropic.com"
	}
	return &claudeProvider{
		config: cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *claudeProvider) Name() string { return "claude" }

// Generate sends a message to the Anthropic Messages API using the default model.
func (p *claudeProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return p.GenerateWithModel(ctx, "", systemPrompt, userPrompt)
}

// GenerateWithModel sends a message using a specific model.
// If model is empty, the provider's default model is used.
func (p *claudeProvider) GenerateWithModel(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	if model == "" {
		model = p.config.Model
	}
	body := claudeRequest{
		Model:     model,
		MaxTokens: 16384,
		System:    systemPrompt,
		Messages: []claudeMessage{
			{Role: "user", Content: userPrompt},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("claude marshal: %w", err)
	}

	url := p.config.BaseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("claude request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("claude http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("claude read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("claude API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result claudeResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("claude unmarshal: %w", err)
	}

	// Detect output truncation: if stop_reason is "max_tokens", the model
	// ran out of output space and the response is incomplete.
	if result.StopReason == "max_tokens" {
		return "", fmt.Errorf("%w: claude response was truncated (output exceeded token limit)", ErrOutputTruncated)
	}

	// Extract text from the first content block.
	for _, block := range result.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}

	return "", fmt.Errorf("claude: no text content in response")
}

// --- Anthropic Messages API types ---

type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system,omitempty"`
	Messages  []claudeMessage `json:"messages"`
}

type claudeContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type claudeResponse struct {
	Content    []claudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
}
