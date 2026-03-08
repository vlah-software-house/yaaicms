// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// openAIProvider implements the Provider interface using the OpenAI
// chat completions API (POST /v1/chat/completions).
type openAIProvider struct {
	config ProviderConfig
	client *http.Client
}

// newOpenAI creates a new OpenAI provider.
func newOpenAI(cfg ProviderConfig) *openAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	return &openAIProvider{
		config: cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *openAIProvider) Name() string { return "openai" }

// Generate sends a chat completion request to OpenAI using the default model.
func (p *openAIProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return p.GenerateWithModel(ctx, "", systemPrompt, userPrompt)
}

// GenerateWithModel sends a chat completion request using a specific model.
// If model is empty, the provider's default model is used.
func (p *openAIProvider) GenerateWithModel(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	if model == "" {
		model = p.config.Model
	}

	messages := []openAIMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	body := openAIRequest{
		Model:     model,
		Messages:  messages,
		MaxTokens: 16384,
	}

	return p.doChat(ctx, body)
}

// doChat performs the HTTP call to the chat completions endpoint.
// Shared between OpenAI and Mistral (same API format).
func (p *openAIProvider) doChat(ctx context.Context, body openAIRequest) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("openai marshal: %w", err)
	}

	url := p.config.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("openai request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("openai http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("openai read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result openAIResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("openai unmarshal: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices returned")
	}

	// Detect output truncation: "length" means the model hit the token limit.
	if result.Choices[0].FinishReason == "length" {
		return "", fmt.Errorf("%w: openai response was truncated (output exceeded token limit)", ErrOutputTruncated)
	}

	return result.Choices[0].Message.Content, nil
}

// GenerateImage creates an image using the OpenAI DALL-E API.
// Returns PNG image bytes and the content type. Uses ModelImage from
// config (defaults to "dall-e-3").
func (p *openAIProvider) GenerateImage(ctx context.Context, prompt string) ([]byte, string, error) {
	model := p.config.ModelImage
	if model == "" {
		model = "dall-e-3"
	}
	body := openAIImageRequest{
		Model:          model,
		Prompt:         prompt,
		N:              1,
		Size:           "1792x1024",
		ResponseFormat: "b64_json",
		Quality:        "standard",
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("openai image marshal: %w", err)
	}

	url := p.config.BaseURL + "/images/generations"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, "", fmt.Errorf("openai image request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.config.APIKey)

	// Image generation can take up to 60 seconds; use a dedicated client
	// with a longer timeout.
	imgClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := imgClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("openai image http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("openai image read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("openai image API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result openAIImageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, "", fmt.Errorf("openai image unmarshal: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, "", fmt.Errorf("openai image: no images returned")
	}

	imgBytes, err := base64.StdEncoding.DecodeString(result.Data[0].B64JSON)
	if err != nil {
		return nil, "", fmt.Errorf("openai image decode base64: %w", err)
	}

	return imgBytes, "image/png", nil
}

// --- OpenAI-compatible request/response types ---
// Used by both OpenAI and Mistral providers.

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRequest struct {
	Model     string          `json:"model"`
	Messages  []openAIMessage `json:"messages"`
	MaxTokens int             `json:"max_tokens,omitempty"`
}

type openAIResponse struct {
	Choices []openAIChoice `json:"choices"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

// --- OpenAI image generation types ---

type openAIImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
	Quality        string `json:"quality"`
}

type openAIImageResponse struct {
	Data []openAIImageData `json:"data"`
}

type openAIImageData struct {
	B64JSON string `json:"b64_json"`
}
