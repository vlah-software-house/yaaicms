// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------- Helpers ----------

// newTestServer creates an httptest.Server that responds with the given status
// code and body bytes. The caller must call Close on the returned server.
func newTestServer(t *testing.T, statusCode int, body []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(body)
	}))
}

// openAISuccessBody builds a JSON body matching the OpenAI chat completions
// response format with a single choice containing the given text.
func openAISuccessBody(text string) []byte {
	resp := openAIResponse{
		Choices: []openAIChoice{
			{Message: openAIMessage{Role: "assistant", Content: text}},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// claudeSuccessBody builds a JSON body matching the Anthropic Messages
// response format with a single text content block.
func claudeSuccessBody(text string) []byte {
	resp := claudeResponse{
		Content: []claudeContentBlock{
			{Type: "text", Text: text},
		},
		StopReason: "end_turn",
	}
	b, _ := json.Marshal(resp)
	return b
}

// geminiSuccessBody builds a JSON body matching the Gemini generateContent
// response format with a single candidate containing the given text.
func geminiSuccessBody(text string) []byte {
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{Content: geminiContent{Parts: []geminiPart{{Text: text}}}},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// =====================================================================
// OpenAI Provider Tests
// =====================================================================

func TestOpenAIGenerate_Success(t *testing.T) {
	want := "Hello from OpenAI"
	srv := newTestServer(t, http.StatusOK, openAISuccessBody(want))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	got, err := p.Generate(context.Background(), "You are helpful.", "Say hello")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Generate: got %q, want %q", got, want)
	}
}

func TestOpenAIGenerate_VerifiesRequestHeaders(t *testing.T) {
	// Capture request headers and body sent by the provider.
	var capturedHeaders http.Header
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openAISuccessBody("ok"))
	}))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "sk-test-12345",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	// Verify Authorization header.
	authHeader := capturedHeaders.Get("Authorization")
	if authHeader != "Bearer sk-test-12345" {
		t.Errorf("Authorization header: got %q, want %q", authHeader, "Bearer sk-test-12345")
	}

	// Verify Content-Type.
	ct := capturedHeaders.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	// Verify the request body contains the correct model and messages.
	var reqBody openAIRequest
	if err := json.Unmarshal(capturedBody, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody.Model != "gpt-4o" {
		t.Errorf("request model: got %q, want %q", reqBody.Model, "gpt-4o")
	}
	if len(reqBody.Messages) != 2 {
		t.Fatalf("request messages count: got %d, want 2", len(reqBody.Messages))
	}
	if reqBody.Messages[0].Role != "system" || reqBody.Messages[0].Content != "system prompt" {
		t.Errorf("system message: got %+v", reqBody.Messages[0])
	}
	if reqBody.Messages[1].Role != "user" || reqBody.Messages[1].Content != "user prompt" {
		t.Errorf("user message: got %+v", reqBody.Messages[1])
	}
}

func TestOpenAIGenerate_HTTPError(t *testing.T) {
	srv := newTestServer(t, http.StatusInternalServerError, []byte(`{"error":"internal"}`))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error should mention status 500: got %q", err.Error())
	}
}

func TestOpenAIGenerate_MalformedJSON(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, []byte(`{not json`))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal: got %q", err.Error())
	}
}

func TestOpenAIGenerate_EmptyChoices(t *testing.T) {
	emptyResp := openAIResponse{Choices: []openAIChoice{}}
	body, _ := json.Marshal(emptyResp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error should mention no choices: got %q", err.Error())
	}
}

func TestOpenAIGenerate_CancelledContext(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, openAISuccessBody("ok"))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := p.Generate(ctx, "sys", "usr")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestOpenAIGenerate_DefaultBaseURL(t *testing.T) {
	// Verify that an empty BaseURL defaults to the OpenAI API endpoint.
	p := newOpenAI(ProviderConfig{
		APIKey: "test-key",
		Model:  "gpt-4o",
		// BaseURL intentionally left empty.
	})
	if p.config.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("default BaseURL: got %q, want %q", p.config.BaseURL, "https://api.openai.com/v1")
	}
}

func TestOpenAIName(t *testing.T) {
	p := newOpenAI(ProviderConfig{APIKey: "k"})
	if p.Name() != "openai" {
		t.Errorf("Name: got %q, want %q", p.Name(), "openai")
	}
}

// =====================================================================
// Claude Provider Tests
// =====================================================================

func TestClaudeGenerate_Success(t *testing.T) {
	want := "Hello from Claude"
	srv := newTestServer(t, http.StatusOK, claudeSuccessBody(want))
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	got, err := p.Generate(context.Background(), "You are helpful.", "Say hello")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Generate: got %q, want %q", got, want)
	}
}

func TestClaudeGenerate_VerifiesRequestHeaders(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(claudeSuccessBody("ok"))
	}))
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "sk-ant-test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	// Verify x-api-key header (Claude uses this instead of Bearer token).
	apiKey := capturedHeaders.Get("x-api-key")
	if apiKey != "sk-ant-test-key" {
		t.Errorf("x-api-key header: got %q, want %q", apiKey, "sk-ant-test-key")
	}

	// Verify anthropic-version header.
	version := capturedHeaders.Get("anthropic-version")
	if version != "2023-06-01" {
		t.Errorf("anthropic-version: got %q, want %q", version, "2023-06-01")
	}

	// Verify Content-Type.
	ct := capturedHeaders.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	// Verify request body structure.
	var reqBody claudeRequest
	if err := json.Unmarshal(capturedBody, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody.Model != "claude-sonnet-4-6" {
		t.Errorf("request model: got %q, want %q", reqBody.Model, "claude-sonnet-4-6")
	}
	if reqBody.MaxTokens != 16384 {
		t.Errorf("max_tokens: got %d, want %d", reqBody.MaxTokens, 16384)
	}
	if reqBody.System != "system prompt" {
		t.Errorf("system: got %q, want %q", reqBody.System, "system prompt")
	}
	if len(reqBody.Messages) != 1 {
		t.Fatalf("messages count: got %d, want 1", len(reqBody.Messages))
	}
	if reqBody.Messages[0].Role != "user" || reqBody.Messages[0].Content != "user prompt" {
		t.Errorf("user message: got %+v", reqBody.Messages[0])
	}
}

func TestClaudeGenerate_HTTPError(t *testing.T) {
	srv := newTestServer(t, http.StatusTooManyRequests, []byte(`{"error":{"message":"rate limited"}}`))
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for HTTP 429, got nil")
	}
	if !strings.Contains(err.Error(), "status 429") {
		t.Errorf("error should mention status 429: got %q", err.Error())
	}
}

func TestClaudeGenerate_MalformedJSON(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, []byte(`<<<invalid>>>`))
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal: got %q", err.Error())
	}
}

func TestClaudeGenerate_NoTextContent(t *testing.T) {
	// Response with no "text" type content blocks.
	resp := claudeResponse{
		Content: []claudeContentBlock{
			{Type: "image", Text: ""},
		},
	}
	body, _ := json.Marshal(resp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for no text content, got nil")
	}
	if !strings.Contains(err.Error(), "no text content") {
		t.Errorf("error should mention no text content: got %q", err.Error())
	}
}

func TestClaudeGenerate_Truncated(t *testing.T) {
	// Simulate a response that was cut off due to max_tokens.
	resp := claudeResponse{
		Content:    []claudeContentBlock{{Type: "text", Text: "<div>incomplete"}},
		StopReason: "max_tokens",
	}
	body, _ := json.Marshal(resp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for truncated response, got nil")
	}
	if !errors.Is(err, ErrOutputTruncated) {
		t.Errorf("error should wrap ErrOutputTruncated: got %q", err.Error())
	}
}

func TestClaudeGenerate_EmptyContentBlocks(t *testing.T) {
	// Response with empty content array.
	resp := claudeResponse{Content: []claudeContentBlock{}}
	body, _ := json.Marshal(resp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for empty content blocks, got nil")
	}
	if !strings.Contains(err.Error(), "no text content") {
		t.Errorf("error should mention no text content: got %q", err.Error())
	}
}

func TestClaudeGenerate_CancelledContext(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, claudeSuccessBody("ok"))
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Generate(ctx, "sys", "usr")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestClaudeGenerate_DefaultBaseURL(t *testing.T) {
	p := newClaude(ProviderConfig{
		APIKey: "test-key",
		Model:  "claude-sonnet-4-6",
	})
	if p.config.BaseURL != "https://api.anthropic.com" {
		t.Errorf("default BaseURL: got %q, want %q", p.config.BaseURL, "https://api.anthropic.com")
	}
}

func TestClaudeName(t *testing.T) {
	p := newClaude(ProviderConfig{APIKey: "k"})
	if p.Name() != "claude" {
		t.Errorf("Name: got %q, want %q", p.Name(), "claude")
	}
}

// =====================================================================
// Gemini Provider Tests
// =====================================================================

func TestGeminiGenerate_Success(t *testing.T) {
	want := "Hello from Gemini"
	srv := newTestServer(t, http.StatusOK, geminiSuccessBody(want))
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	got, err := p.Generate(context.Background(), "You are helpful.", "Say hello")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Generate: got %q, want %q", got, want)
	}
}

func TestGeminiGenerate_VerifiesRequestHeaders(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody []byte
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(geminiSuccessBody("ok"))
	}))
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "gemini-api-key-123",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	// Verify x-goog-api-key header.
	apiKey := capturedHeaders.Get("x-goog-api-key")
	if apiKey != "gemini-api-key-123" {
		t.Errorf("x-goog-api-key: got %q, want %q", apiKey, "gemini-api-key-123")
	}

	// Verify Content-Type.
	ct := capturedHeaders.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	// Verify URL path includes model name.
	expectedPath := "/v1beta/models/gemini-3.1-pro-preview:generateContent"
	if capturedPath != expectedPath {
		t.Errorf("request path: got %q, want %q", capturedPath, expectedPath)
	}

	// Verify request body structure.
	var reqBody geminiRequest
	if err := json.Unmarshal(capturedBody, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody.SystemInstruction == nil {
		t.Fatal("SystemInstruction should not be nil")
	}
	if len(reqBody.SystemInstruction.Parts) != 1 || reqBody.SystemInstruction.Parts[0].Text != "system prompt" {
		t.Errorf("system instruction: got %+v", reqBody.SystemInstruction)
	}
	if len(reqBody.Contents) != 1 {
		t.Fatalf("contents count: got %d, want 1", len(reqBody.Contents))
	}
	if len(reqBody.Contents[0].Parts) != 1 || reqBody.Contents[0].Parts[0].Text != "user prompt" {
		t.Errorf("content parts: got %+v", reqBody.Contents[0])
	}
}

func TestGeminiGenerate_HTTPError(t *testing.T) {
	srv := newTestServer(t, http.StatusForbidden, []byte(`{"error":{"message":"forbidden"}}`))
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Errorf("error should mention status 403: got %q", err.Error())
	}
}

func TestGeminiGenerate_MalformedJSON(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, []byte(`[broken json`))
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal: got %q", err.Error())
	}
}

func TestGeminiGenerate_NoCandidates(t *testing.T) {
	resp := geminiResponse{Candidates: []geminiCandidate{}}
	body, _ := json.Marshal(resp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for no candidates, got nil")
	}
	if !strings.Contains(err.Error(), "no candidates") {
		t.Errorf("error should mention no candidates: got %q", err.Error())
	}
}

func TestGeminiGenerate_EmptyPartsInCandidate(t *testing.T) {
	// Candidate exists but has no parts with text.
	resp := geminiResponse{
		Candidates: []geminiCandidate{
			{Content: geminiContent{Parts: []geminiPart{{Text: ""}}}},
		},
	}
	body, _ := json.Marshal(resp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for empty parts text, got nil")
	}
	if !strings.Contains(err.Error(), "no text") {
		t.Errorf("error should mention no text: got %q", err.Error())
	}
}

func TestGeminiGenerate_CancelledContext(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, geminiSuccessBody("ok"))
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-3.1-pro-preview",
		BaseURL: srv.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Generate(ctx, "sys", "usr")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestGeminiGenerate_DefaultBaseURL(t *testing.T) {
	p := newGemini(ProviderConfig{
		APIKey: "test-key",
		Model:  "gemini-3.1-pro-preview",
	})
	if p.config.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Errorf("default BaseURL: got %q, want %q", p.config.BaseURL, "https://generativelanguage.googleapis.com")
	}
}

func TestGeminiName(t *testing.T) {
	p := newGemini(ProviderConfig{APIKey: "k"})
	if p.Name() != "gemini" {
		t.Errorf("Name: got %q, want %q", p.Name(), "gemini")
	}
}

// =====================================================================
// Mistral Provider Tests
// =====================================================================

func TestMistralGenerate_Success(t *testing.T) {
	want := "Hello from Mistral"
	srv := newTestServer(t, http.StatusOK, openAISuccessBody(want))
	defer srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "test-key",
		Model:   "mistral-large-latest",
		BaseURL: srv.URL,
	})

	got, err := p.Generate(context.Background(), "You are helpful.", "Say hello")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Generate: got %q, want %q", got, want)
	}
}

func TestMistralGenerate_VerifiesRequestHeaders(t *testing.T) {
	var capturedHeaders http.Header
	var capturedBody []byte
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		capturedBody, _ = io.ReadAll(r.Body)
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openAISuccessBody("ok"))
	}))
	defer srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "mistral-api-key-456",
		Model:   "mistral-large-latest",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "system prompt", "user prompt")
	if err != nil {
		t.Fatalf("Generate: unexpected error: %v", err)
	}

	// Mistral uses Bearer token auth (OpenAI-compatible).
	authHeader := capturedHeaders.Get("Authorization")
	if authHeader != "Bearer mistral-api-key-456" {
		t.Errorf("Authorization header: got %q, want %q", authHeader, "Bearer mistral-api-key-456")
	}

	// Verify Content-Type.
	ct := capturedHeaders.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: got %q, want %q", ct, "application/json")
	}

	// Verify path matches OpenAI-compatible endpoint.
	if capturedPath != "/chat/completions" {
		t.Errorf("request path: got %q, want %q", capturedPath, "/chat/completions")
	}

	// Verify request body structure.
	var reqBody openAIRequest
	if err := json.Unmarshal(capturedBody, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody.Model != "mistral-large-latest" {
		t.Errorf("request model: got %q, want %q", reqBody.Model, "mistral-large-latest")
	}
	if len(reqBody.Messages) != 2 {
		t.Fatalf("messages count: got %d, want 2", len(reqBody.Messages))
	}
	if reqBody.Messages[0].Role != "system" || reqBody.Messages[0].Content != "system prompt" {
		t.Errorf("system message: got %+v", reqBody.Messages[0])
	}
	if reqBody.Messages[1].Role != "user" || reqBody.Messages[1].Content != "user prompt" {
		t.Errorf("user message: got %+v", reqBody.Messages[1])
	}
}

func TestMistralGenerate_HTTPError(t *testing.T) {
	srv := newTestServer(t, http.StatusBadGateway, []byte(`{"error":"bad gateway"}`))
	defer srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "test-key",
		Model:   "mistral-large-latest",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for HTTP 502, got nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Errorf("error should mention status 502: got %q", err.Error())
	}
}

func TestMistralGenerate_MalformedJSON(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, []byte(`{{{malformed`))
	defer srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "test-key",
		Model:   "mistral-large-latest",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal: got %q", err.Error())
	}
}

func TestMistralGenerate_EmptyChoices(t *testing.T) {
	emptyResp := openAIResponse{Choices: []openAIChoice{}}
	body, _ := json.Marshal(emptyResp)
	srv := newTestServer(t, http.StatusOK, body)
	defer srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "test-key",
		Model:   "mistral-large-latest",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error should mention no choices: got %q", err.Error())
	}
}

func TestMistralGenerate_CancelledContext(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, openAISuccessBody("ok"))
	defer srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "test-key",
		Model:   "mistral-large-latest",
		BaseURL: srv.URL,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Generate(ctx, "sys", "usr")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestMistralGenerate_DefaultBaseURL(t *testing.T) {
	p := newMistral(ProviderConfig{
		APIKey: "test-key",
		Model:  "mistral-large-latest",
	})
	if p.inner.config.BaseURL != "https://api.mistral.ai/v1" {
		t.Errorf("default BaseURL: got %q, want %q", p.inner.config.BaseURL, "https://api.mistral.ai/v1")
	}
}

func TestMistralName(t *testing.T) {
	p := newMistral(ProviderConfig{APIKey: "k"})
	if p.Name() != "mistral" {
		t.Errorf("Name: got %q, want %q", p.Name(), "mistral")
	}
}

// =====================================================================
// Registry integration with real HTTP (end-to-end via httptest)
// =====================================================================

func TestRegistryGenerate_WithRealHTTPProviders(t *testing.T) {
	// Set up mock servers for each provider.
	openaiSrv := newTestServer(t, http.StatusOK, openAISuccessBody("openai response"))
	defer openaiSrv.Close()

	claudeSrv := newTestServer(t, http.StatusOK, claudeSuccessBody("claude response"))
	defer claudeSrv.Close()

	geminiSrv := newTestServer(t, http.StatusOK, geminiSuccessBody("gemini response"))
	defer geminiSrv.Close()

	mistralSrv := newTestServer(t, http.StatusOK, openAISuccessBody("mistral response"))
	defer mistralSrv.Close()

	configs := map[string]ProviderConfig{
		"openai":  {APIKey: "ok1", Model: "gpt-4o", BaseURL: openaiSrv.URL},
		"claude":  {APIKey: "ok2", Model: "claude-sonnet-4-6", BaseURL: claudeSrv.URL},
		"gemini":  {APIKey: "ok3", Model: "gemini-pro", BaseURL: geminiSrv.URL},
		"mistral": {APIKey: "ok4", Model: "mistral-large", BaseURL: mistralSrv.URL},
	}

	reg := NewRegistry("openai", configs)

	// Test each provider through the registry.
	tests := []struct {
		providerName string
		wantResult   string
	}{
		{"openai", "openai response"},
		{"claude", "claude response"},
		{"gemini", "gemini response"},
		{"mistral", "mistral response"},
	}

	for _, tt := range tests {
		t.Run(tt.providerName, func(t *testing.T) {
			if err := reg.SetActive(tt.providerName); err != nil {
				t.Fatalf("SetActive(%q): %v", tt.providerName, err)
			}

			got, err := reg.Generate(context.Background(), "system", "user")
			if err != nil {
				t.Fatalf("Generate with %s: %v", tt.providerName, err)
			}
			if got != tt.wantResult {
				t.Errorf("Generate with %s: got %q, want %q", tt.providerName, got, tt.wantResult)
			}
		})
	}
}

// =====================================================================
// Edge cases: server connection refused
// =====================================================================

func TestOpenAIGenerate_ConnectionRefused(t *testing.T) {
	// Point at a server that was immediately closed — connection will be refused.
	srv := newTestServer(t, http.StatusOK, openAISuccessBody("ok"))
	srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
	if !strings.Contains(err.Error(), "openai http") {
		t.Errorf("error should be wrapped with 'openai http': got %q", err.Error())
	}
}

func TestClaudeGenerate_ConnectionRefused(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, claudeSuccessBody("ok"))
	srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "test-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
	if !strings.Contains(err.Error(), "claude http") {
		t.Errorf("error should be wrapped with 'claude http': got %q", err.Error())
	}
}

func TestGeminiGenerate_ConnectionRefused(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, geminiSuccessBody("ok"))
	srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "test-key",
		Model:   "gemini-pro",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
	if !strings.Contains(err.Error(), "gemini http") {
		t.Errorf("error should be wrapped with 'gemini http': got %q", err.Error())
	}
}

func TestMistralGenerate_ConnectionRefused(t *testing.T) {
	srv := newTestServer(t, http.StatusOK, openAISuccessBody("ok"))
	srv.Close()

	p := newMistral(ProviderConfig{
		APIKey:  "test-key",
		Model:   "mistral-large",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
	if !strings.Contains(err.Error(), "openai http") {
		t.Errorf("error should be wrapped with 'openai http' (mistral uses openai doChat): got %q", err.Error())
	}
}

// =====================================================================
// HTTP 4xx error bodies are included in error messages
// =====================================================================

func TestOpenAIGenerate_ErrorBodyIncluded(t *testing.T) {
	errBody := `{"error":{"message":"invalid API key","type":"invalid_request_error"}}`
	srv := newTestServer(t, http.StatusUnauthorized, []byte(errBody))
	defer srv.Close()

	p := newOpenAI(ProviderConfig{
		APIKey:  "bad-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error message should include the response body for debugging.
	if !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("error should contain API error body: got %q", err.Error())
	}
}

func TestClaudeGenerate_ErrorBodyIncluded(t *testing.T) {
	errBody := `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`
	srv := newTestServer(t, http.StatusUnauthorized, []byte(errBody))
	defer srv.Close()

	p := newClaude(ProviderConfig{
		APIKey:  "bad-key",
		Model:   "claude-sonnet-4-6",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Errorf("error should contain API error body: got %q", err.Error())
	}
}

func TestGeminiGenerate_ErrorBodyIncluded(t *testing.T) {
	errBody := `{"error":{"code":400,"message":"API key not valid"}}`
	srv := newTestServer(t, http.StatusBadRequest, []byte(errBody))
	defer srv.Close()

	p := newGemini(ProviderConfig{
		APIKey:  "bad-key",
		Model:   "gemini-pro",
		BaseURL: srv.URL,
	})

	_, err := p.Generate(context.Background(), "sys", "usr")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "API key not valid") {
		t.Errorf("error should contain API error body: got %q", err.Error())
	}
}

// =====================================================================
// Registry.Register (dynamic provider injection)
// =====================================================================

func TestRegistryRegister(t *testing.T) {
	t.Run("adds a new provider", func(t *testing.T) {
		reg := NewRegistry("openai", map[string]ProviderConfig{
			"openai": {APIKey: "key1", Model: "gpt-4o"},
		})

		if reg.HasProvider("custom") {
			t.Fatal("custom provider should not exist yet")
		}

		mock := &mockProvider{name: "custom", response: "custom reply"}
		reg.Register("custom", mock)

		if !reg.HasProvider("custom") {
			t.Fatal("custom provider should exist after Register")
		}

		// Switch to the new provider and call Generate.
		if err := reg.SetActive("custom"); err != nil {
			t.Fatalf("SetActive(custom): %v", err)
		}
		got, err := reg.Generate(context.Background(), "sys", "usr")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if got != "custom reply" {
			t.Errorf("got %q, want %q", got, "custom reply")
		}
	})

	t.Run("replaces an existing provider", func(t *testing.T) {
		reg := NewRegistry("openai", map[string]ProviderConfig{
			"openai": {APIKey: "key1", Model: "gpt-4o"},
		})

		replacement := &mockProvider{name: "openai", response: "replaced"}
		reg.Register("openai", replacement)

		got, err := reg.Generate(context.Background(), "sys", "usr")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if got != "replaced" {
			t.Errorf("got %q, want %q", got, "replaced")
		}
	})
}
