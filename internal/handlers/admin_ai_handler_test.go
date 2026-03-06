// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"yaaicms/internal/models"
)

// setMockAIResponse reconfigures the test env's AI mock to return a given response.
func setMockAIResponse(env *testEnv, response string, err error) {
	env.AIRegistry.Register("test", &mockAIProvider{
		name:     "test",
		response: response,
		err:      err,
	})
}

// --- AISuggestTitle ---

func TestAISuggestTitle_EmptyInput_ReturnsError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/suggest-title", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISuggestTitle(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Please write some content") {
		t.Errorf("expected error message, got: %s", body[:min(len(body), 200)])
	}
}

func TestAISuggestTitle_Success(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "1. Great Title One\n2. Another Title\n3. Third Option", nil)

	form := url.Values{}
	form.Set("body", "This is content about testing.")
	form.Set("title", "Working Title")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/suggest-title", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISuggestTitle(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Great Title One") {
		t.Error("expected parsed title in response")
	}
	if !strings.Contains(body, "button") {
		t.Error("expected clickable buttons in response")
	}
}

func TestAISuggestTitle_AIError(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "", fmt.Errorf("provider unreachable"))

	form := url.Values{}
	form.Set("body", "content")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/suggest-title", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISuggestTitle(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "AI request failed") {
		t.Errorf("expected error message, got: %s", body[:min(len(body), 200)])
	}
}

func TestAISuggestTitle_UnparsedResponse(t *testing.T) {
	env := newTestEnv(t)
	// A response with no numbered list — should fall through to writeAIResult.
	setMockAIResponse(env, "", nil)

	form := url.Values{}
	form.Set("body", "content")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/suggest-title", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISuggestTitle(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

// --- AIGenerateExcerpt ---

func TestAIGenerateExcerpt_EmptyBody_ReturnsError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("title", "Title but no body")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/generate-excerpt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIGenerateExcerpt(rec, req)

	if !strings.Contains(rec.Body.String(), "Please write some content") {
		t.Error("expected error message for empty body")
	}
}

func TestAIGenerateExcerpt_Success(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "This is a compelling excerpt about the article.", nil)

	form := url.Values{}
	form.Set("body", "Long article body content here.")
	form.Set("title", "My Article")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/generate-excerpt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIGenerateExcerpt(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "compelling excerpt") {
		t.Error("expected excerpt in response")
	}
	if !strings.Contains(body, "Apply to Excerpt") {
		t.Error("expected Apply button in response")
	}
}

func TestAIGenerateExcerpt_AIError(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "", fmt.Errorf("timeout"))

	form := url.Values{}
	form.Set("body", "content")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/generate-excerpt", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIGenerateExcerpt(rec, req)

	if !strings.Contains(rec.Body.String(), "AI request failed") {
		t.Error("expected AI error message")
	}
}

// --- AISEOMetadata ---

func TestAISEOMetadata_EmptyInput_ReturnsError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/seo-metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISEOMetadata(rec, req)

	if !strings.Contains(rec.Body.String(), "Please write some content") {
		t.Error("expected error message for empty input")
	}
}

func TestAISEOMetadata_Success(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "DESCRIPTION: A great Go tutorial for beginners.\nKEYWORDS: go, programming, tutorial, beginner", nil)

	form := url.Values{}
	form.Set("body", "Article about learning Go.")
	form.Set("title", "Learn Go")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/seo-metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISEOMetadata(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Apply Description") {
		t.Error("expected Apply Description button")
	}
	if !strings.Contains(body, "Apply Keywords") {
		t.Error("expected Apply Keywords button")
	}
}

func TestAISEOMetadata_UnparsedFallback(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "Some random response without structure", nil)

	form := url.Values{}
	form.Set("body", "content")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/seo-metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISEOMetadata(rec, req)

	// When parsing fails, writeAIResult is called as fallback.
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

func TestAISEOMetadata_AIError(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "", fmt.Errorf("connection refused"))

	form := url.Values{}
	form.Set("title", "Some Title")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/seo-metadata", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AISEOMetadata(rec, req)

	if !strings.Contains(rec.Body.String(), "AI request failed") {
		t.Error("expected AI error message")
	}
}

// --- AIRewrite ---

func TestAIRewrite_EmptyBody_ReturnsError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("title", "Title only")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/rewrite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIRewrite(rec, req)

	if !strings.Contains(rec.Body.String(), "Please write some content") {
		t.Error("expected error message for empty body")
	}
}

func TestAIRewrite_Success(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "<p>Rewritten content in a professional tone.</p>", nil)

	form := url.Values{}
	form.Set("body", "Original content here.")
	form.Set("title", "My Post")
	form.Set("tone", "professional")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/rewrite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIRewrite(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Rewritten content") {
		t.Error("expected rewritten content in response")
	}
	if !strings.Contains(body, "Apply to Content") {
		t.Error("expected Apply button")
	}
}

func TestAIRewrite_DefaultTone(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "Default tone result.", nil)

	form := url.Values{}
	form.Set("body", "Some content.")
	// No tone set — should default to "professional".
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/rewrite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIRewrite(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

func TestAIRewrite_UnknownTone(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "Result with unknown tone.", nil)

	form := url.Values{}
	form.Set("body", "Some content.")
	form.Set("tone", "mysterious")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/rewrite", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIRewrite(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

// --- AIExtractTags ---

func TestAIExtractTags_EmptyInput_ReturnsError(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/extract-tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIExtractTags(rec, req)

	if !strings.Contains(rec.Body.String(), "Please write some content") {
		t.Error("expected error message for empty input")
	}
}

func TestAIExtractTags_Success(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "go, testing, web development, api design", nil)

	form := url.Values{}
	form.Set("body", "Article about Go web APIs.")
	form.Set("title", "Go APIs")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/extract-tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIExtractTags(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "go") {
		t.Error("expected parsed tags in response")
	}
	if !strings.Contains(body, "Click tags") {
		t.Error("expected instruction text")
	}
}

func TestAIExtractTags_AIError(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "", fmt.Errorf("rate limited"))

	form := url.Values{}
	form.Set("body", "content")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/extract-tags", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AIExtractTags(rec, req)

	if !strings.Contains(rec.Body.String(), "AI request failed") {
		t.Error("expected AI error message")
	}
}

// --- AITemplatePage ---

func TestAITemplatePage_Returns200(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/templates/ai", nil)
	rec := httptest.NewRecorder()
	env.Admin.AITemplatePage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("AITemplatePage: got %d, want %d", rec.Code, http.StatusOK)
	}
}

// --- AITemplateGenerate ---

func TestAITemplateGenerate_EmptyPrompt_Returns400(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateGenerate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestAITemplateGenerate_ValidPrompt_ReturnsJSON(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, `<header class="bg-white p-4"><nav>{{.SiteTitle}}</nav></header>`, nil)

	form := url.Values{}
	form.Set("prompt", "Create a simple header with nav")
	form.Set("template_type", "header")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateGenerate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	var resp templateGenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if resp.HTML == "" {
		t.Error("expected non-empty HTML")
	}
	if !resp.Valid {
		t.Errorf("expected valid template, got validation error: %s", resp.ValidationError)
	}
	if resp.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestAITemplateGenerate_InvalidSyntax(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, `<header>{{.Broken</header>`, nil)

	form := url.Values{}
	form.Set("prompt", "Make something")
	form.Set("template_type", "header")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateGenerate(rec, req)

	var resp templateGenResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Valid {
		t.Error("expected invalid template")
	}
	if resp.ValidationError == "" {
		t.Error("expected validation error string")
	}
}

func TestAITemplateGenerate_WithCurrentHTML(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, `<header class="updated">{{.SiteTitle}}</header>`, nil)

	form := url.Values{}
	form.Set("prompt", "Make it blue")
	form.Set("template_type", "header")
	form.Set("current_html", `<header>{{.SiteTitle}}</header>`)
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateGenerate(rec, req)

	var resp templateGenResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.HTML == "" {
		t.Error("expected generated HTML")
	}
}

func TestAITemplateGenerate_WithChatHistory(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, `<header>{{.SiteTitle}}</header>`, nil)

	form := url.Values{}
	form.Set("prompt", "Now add a logo")
	form.Set("template_type", "header")
	form.Set("chat_history", "User: Create a header\nAI: Done")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateGenerate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
}

func TestAITemplateGenerate_AIError(t *testing.T) {
	env := newTestEnv(t)
	setMockAIResponse(env, "", fmt.Errorf("provider down"))

	form := url.Values{}
	form.Set("prompt", "Create a header")
	form.Set("template_type", "header")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-generate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateGenerate(rec, req)

	var resp templateGenResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error == "" {
		t.Error("expected error in response")
	}
}

// --- AITemplateSave ---

func TestAITemplateSave_EmptyFields_Returns400(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateSave(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestAITemplateSave_InvalidSyntax(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("name", "Bad Template")
	form.Set("type", "header")
	form.Set("html_content", "<header>{{.Broken</header>")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateSave(rec, req)

	var resp templateSaveResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error == "" {
		t.Error("expected error for invalid syntax")
	}
}

func TestAITemplateSave_ValidTemplate(t *testing.T) {
	env := newTestEnv(t)

	name := "AI Saved Template Test"
	t.Cleanup(func() { cleanTemplates(t, env.DB, name) })

	form := url.Values{}
	form.Set("name", name)
	form.Set("type", string(models.TemplateTypeHeader))
	form.Set("html_content", "<header>{{.SiteTitle}}</header>")
	req := httptest.NewRequest(http.MethodPost, "/admin/ai/template-save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	env.Admin.AITemplateSave(rec, req)

	var resp templateSaveResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error != "" {
		t.Fatalf("unexpected error: %s", resp.Error)
	}
	if resp.ID == "" {
		t.Error("expected template ID in response")
	}
}
