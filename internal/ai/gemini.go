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

// geminiProvider implements the Provider interface using the Google
// Gemini REST API (POST /v1beta/models/{model}:generateContent).
type geminiProvider struct {
	config ProviderConfig
	client *http.Client
}

// newGemini creates a new Google Gemini provider.
func newGemini(cfg ProviderConfig) *geminiProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://generativelanguage.googleapis.com"
	}
	return &geminiProvider{
		config: cfg,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func (p *geminiProvider) Name() string { return "gemini" }

// Generate sends a generateContent request to the Gemini API using the default model.
func (p *geminiProvider) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	return p.GenerateWithModel(ctx, "", systemPrompt, userPrompt)
}

// GenerateWithModel sends a generateContent request using a specific model.
// If model is empty, the provider's default model is used.
func (p *geminiProvider) GenerateWithModel(ctx context.Context, model, systemPrompt, userPrompt string) (string, error) {
	if model == "" {
		model = p.config.Model
	}

	body := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: userPrompt}}},
		},
		GenerationConfig: &geminiGenerationConfig{
			MaxOutputTokens: 16384,
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("gemini marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent",
		p.config.BaseURL, model)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.config.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gemini read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result geminiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("gemini unmarshal: %w", err)
	}

	if len(result.Candidates) == 0 {
		return "", fmt.Errorf("gemini: no candidates returned")
	}

	// Detect output truncation: "MAX_TOKENS" means the model hit the token limit.
	if result.Candidates[0].FinishReason == "MAX_TOKENS" {
		return "", fmt.Errorf("%w: gemini response was truncated (output exceeded token limit)", ErrOutputTruncated)
	}

	// Extract text from the first candidate's parts.
	for _, part := range result.Candidates[0].Content.Parts {
		if part.Text != "" {
			return part.Text, nil
		}
	}

	return "", fmt.Errorf("gemini: no text in response")
}

// GenerateImage creates an image using Gemini's native generateContent API
// with responseModalities set to IMAGE. Uses ModelImage from config
// (e.g., "gemini-2.5-flash-image"). Returns image bytes and the content type.
func (p *geminiProvider) GenerateImage(ctx context.Context, prompt string) ([]byte, string, error) {
	model := p.config.ModelImage
	if model == "" {
		return nil, "", fmt.Errorf("gemini: image generation requires GEMINI_MODEL_IMAGE to be set")
	}

	body := geminiImageRequest{
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: "Generate an image of: " + prompt}}},
		},
		GenerationConfig: geminiImageConfig{
			ResponseModalities: []string{"IMAGE", "TEXT"},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("gemini image marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		p.config.BaseURL, model, p.config.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, "", fmt.Errorf("gemini image request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	imgClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := imgClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("gemini image http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("gemini image read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("gemini image API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result geminiImageResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, "", fmt.Errorf("gemini image unmarshal: %w", err)
	}

	// Extract the image data from the response parts.
	for _, c := range result.Candidates {
		for _, part := range c.Content.ImageParts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				imgBytes, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
				if err != nil {
					return nil, "", fmt.Errorf("gemini image decode base64: %w", err)
				}
				contentType := part.InlineData.MimeType
				if contentType == "" {
					contentType = "image/png"
				}
				return imgBytes, contentType, nil
			}
		}
	}

	return nil, "", fmt.Errorf("gemini image: no image data in response")
}

// --- Gemini API types ---

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"system_instruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

// --- Gemini native image generation types ---

type geminiImageRequest struct {
	Contents         []geminiContent  `json:"contents"`
	GenerationConfig geminiImageConfig `json:"generationConfig"`
}

type geminiImageConfig struct {
	ResponseModalities []string `json:"responseModalities"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiImagePart struct {
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
	Text       string            `json:"text,omitempty"`
}

type geminiImageContent struct {
	ImageParts []geminiImagePart `json:"parts"`
}

type geminiImageCandidate struct {
	Content geminiImageContent `json:"content"`
}

type geminiImageResponse struct {
	Candidates []geminiImageCandidate `json:"candidates"`
}
