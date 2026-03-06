// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"yaaicms/internal/ai"
	"yaaicms/internal/engine"
	"yaaicms/internal/markdown"
	"yaaicms/internal/middleware"
	"yaaicms/internal/models"
	"yaaicms/internal/render"
	"yaaicms/internal/slug"
	"yaaicms/internal/storage"
)

// tenantAIProvider resolves the AI provider for the current tenant.
// It reads the "ai_provider" site setting; if not set or the stored provider
// is no longer available, falls back to the env-var default (Registry.ActiveName).
func (a *Admin) tenantAIProvider(r *http.Request) string {
	sess := middleware.SessionFromCtx(r.Context())
	provider, err := a.siteSettingStore.Get(sess.TenantID, "ai_provider", "")
	if err != nil || provider == "" {
		return a.aiRegistry.ActiveName()
	}
	if !a.aiRegistry.HasProvider(provider) {
		return a.aiRegistry.ActiveName()
	}
	return provider
}

// --- AI Assistant Endpoints ---
//
// These handlers power the content editor's AI assistant panel.
// Each endpoint accepts form values (title, body, tone) from HTMX requests,
// calls the active AI provider, and returns HTML fragments that get swapped
// into the assistant panel's result areas.

// AIGenerateContent generates a full article from a user-provided topic prompt.
// Returns an HTML fragment with the generated content and an "Apply" button
// that fills the body textarea.
func (a *Admin) AIGenerateContent(w http.ResponseWriter, r *http.Request) {
	prompt := strings.TrimSpace(r.FormValue("ai_content_prompt"))
	contentType := r.FormValue("content_type")

	if prompt == "" {
		writeAIError(w, "Please describe what you'd like to write about.")
		return
	}

	if contentType == "" {
		contentType = "article"
	}

	if !a.checkPromptSafety(w, r, prompt) {
		return
	}

	systemPrompt := fmt.Sprintf(`You are an expert content writer for a CMS. Write a complete %s based on the user's description.

Rules:
- Output ONLY the article body as clean Markdown.
- Use ## and ### for subheadings (not # — the CMS adds the title separately).
- Use standard Markdown syntax: **bold**, *italic*, > blockquotes, - lists, 1. numbered lists, [links](url), etc.
- Do NOT wrap the output in code fences.
- Write 3-6 well-structured paragraphs with subheadings where appropriate.
- Make the content informative, engaging, and ready to publish.`, contentType)

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskContent, systemPrompt, prompt)
	if err != nil {
		slog.Error("ai generate content failed", "error", err)
		writeAIError(w, "AI request failed. Check your provider configuration.")
		return
	}

	result = extractHTMLFromResponse(result)

	// Render Markdown to HTML for a human-readable preview while keeping
	// the raw Markdown for the "Apply" button (editor expects Markdown).
	previewHTML, err := markdown.ToHTML(result)
	if err != nil {
		previewHTML = html.EscapeString(result)
	}

	fragment := fmt.Sprintf(
		`<div class="space-y-3">
			<div class="ai-preview text-gray-700 bg-gray-50 rounded p-3 max-h-64 overflow-y-auto prose prose-sm">%s</div>
			<button type="button"
				onclick="if(window._markdownEditor){window._markdownEditor.value(%s)}else{document.getElementById('body').value=%s}; document.getElementById('body').dispatchEvent(new Event('input'))"
				class="w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-500 transition-colors">
				Apply to Content
			</button>
		</div>`,
		previewHTML,
		quoteJSString(result),
		quoteJSString(result),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fragment))
}

// AIGenerateImage generates an image using the selected AI provider's image
// generation capability. Accepts an optional "image_provider" form value to
// choose a specific provider (e.g., "openai" for DALL-E, "gemini" for Imagen);
// if empty, uses the active provider or falls back to any image-capable one.
// The generated image is uploaded to S3 and stored as a media record.
func (a *Admin) AIGenerateImage(w http.ResponseWriter, r *http.Request) {
	prompt := strings.TrimSpace(r.FormValue("ai_image_prompt"))
	if prompt == "" {
		writeAIError(w, "Please describe the image you'd like to generate.")
		return
	}

	imageProvider := strings.TrimSpace(r.FormValue("image_provider"))

	if a.storageClient == nil || a.mediaStore == nil {
		writeAIError(w, "Object storage is not configured. Cannot save generated images.")
		return
	}

	if !a.aiRegistry.SupportsImageGeneration() {
		writeAIError(w, "Image generation requires OpenAI (DALL-E) or Gemini with GEMINI_MODEL_IMAGE set.")
		return
	}

	if !a.checkPromptSafety(w, r, prompt) {
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	// Generate the image using the selected (or default) provider.
	imgBytes, contentType, err := a.aiRegistry.GenerateImage(r.Context(), imageProvider, prompt)
	if err != nil {
		slog.Error("ai generate image failed", "error", err)
		writeAIError(w, "Image generation failed. Check your provider configuration and API limits.")
		return
	}

	// Upload to S3 as a media item (same pipeline as manual uploads).
	now := time.Now()
	fileID := uuid.New().String()
	ext := ".png"
	if contentType == "image/jpeg" {
		ext = ".jpg"
	} else if contentType == "image/webp" {
		ext = ".webp"
	}
	s3Key := fmt.Sprintf("media/%d/%02d/%s%s", now.Year(), now.Month(), fileID, ext)
	bucket := a.storageClient.PublicBucket()

	ctx := r.Context()
	if err := a.storageClient.Upload(ctx, bucket, s3Key, contentType, bytes.NewReader(imgBytes), int64(len(imgBytes))); err != nil {
		slog.Error("ai image s3 upload failed", "error", err, "key", s3Key)
		writeAIError(w, "Failed to upload generated image.")
		return
	}

	// Generate responsive WebP variants (thumb, sm, md, lg).
	var thumbKey *string
	var pendingVariants []models.MediaVariant
	if variantTypes[contentType] {
		pendingVariants, thumbKey = a.generateAndUploadVariants(ctx, imgBytes, bucket, fileID, now)
	}

	// Create media record. Derive a descriptive filename from the prompt.
	altText := truncate(prompt, 500)
	safeName := slug.Generate(prompt)
	if len(safeName) > 80 {
		safeName = safeName[:80]
		// Trim at the last hyphen to avoid cutting a word in half.
		if i := strings.LastIndex(safeName, "-"); i > 20 {
			safeName = safeName[:i]
		}
	}
	if safeName == "" {
		safeName = "ai-generated"
	}
	media := &models.Media{
		Filename:     fileID + ext,
		OriginalName: safeName + ext,
		ContentType:  contentType,
		SizeBytes:    int64(len(imgBytes)),
		Bucket:       bucket,
		S3Key:        s3Key,
		ThumbS3Key:   thumbKey,
		AltText:      &altText,
		UploaderID:   sess.UserID,
	}

	created, err := a.mediaStore.Create(sess.TenantID, media)
	if err != nil {
		slog.Error("ai image media insert failed", "error", err)
		writeAIError(w, "Failed to save image metadata.")
		return
	}

	// Store variant records now that we have the media ID.
	a.saveVariants(created.ID, pendingVariants)

	// Build image URLs for the response.
	imgURL := a.storageClient.FileURL(created.S3Key)
	var thumbURL string
	if created.ThumbS3Key != nil {
		thumbURL = a.storageClient.FileURL(*created.ThumbS3Key)
	} else {
		thumbURL = imgURL
	}

	// Return JSON when the client requests it (used by the media picker modal),
	// otherwise return an HTML fragment (used by the featured image HTMX flow).
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":        created.ID.String(),
			"url":       imgURL,
			"thumb_url": thumbURL,
			"filename":  created.OriginalName,
			"alt_text":  altText,
		})
		return
	}

	fragment := fmt.Sprintf(
		`<div class="space-y-3">
			<img src="%s" alt="%s" class="w-full rounded-lg shadow-sm border border-gray-200">
			<button type="button"
				onclick="document.getElementById('featured_image_id').value = '%s';
				         document.getElementById('featured-image-preview').src = '%s';
				         document.getElementById('featured-image-container').classList.remove('hidden');
				         document.getElementById('featured-image-empty').classList.add('hidden')"
				class="w-full rounded-md bg-indigo-600 px-3 py-2 text-sm font-medium text-white hover:bg-indigo-500 transition-colors">
				Use as Featured Image
			</button>
		</div>`,
		html.EscapeString(thumbURL),
		html.EscapeString(altText),
		created.ID.String(),
		html.EscapeString(imgURL),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fragment))
}

// AISuggestTitle generates title suggestions based on the content body.
// Returns an HTML fragment with clickable title options.
func (a *Admin) AISuggestTitle(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	title := r.FormValue("title")

	if body == "" && title == "" {
		writeAIError(w, "Please write some content or a working title first.")
		return
	}

	promptText := title + " " + body
	if !a.checkPromptSafety(w, r, truncate(promptText, 2000)) {
		return
	}

	prompt := fmt.Sprintf("Title: %s\n\nContent:\n%s", title, truncate(body, 2000))

	systemPrompt := `You are a headline writing expert for a CMS. Generate exactly 5 compelling,
SEO-friendly title suggestions for the given content. Each title should be on its own line,
numbered 1-5. Keep titles under 70 characters. Do not include any other text or explanation.`

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskLight, systemPrompt, prompt)
	if err != nil {
		slog.Error("ai suggest title failed", "error", err)
		writeAIError(w, "AI request failed. Check your provider configuration.")
		return
	}

	// Parse the numbered titles and render as clickable items.
	titles := parseNumberedList(result)
	if len(titles) == 0 {
		writeAIResult(w, result)
		return
	}

	var sb strings.Builder
	sb.WriteString(`<div class="space-y-1.5">`)
	for _, t := range titles {
		escaped := html.EscapeString(t)
		sb.WriteString(fmt.Sprintf(
			`<button type="button" onclick="document.getElementById('title').value = this.textContent.trim()"
				class="block w-full text-left text-xs px-2 py-1.5 rounded bg-indigo-50 text-indigo-800 hover:bg-indigo-100 transition-colors truncate"
				title="%s">%s</button>`,
			escaped, escaped,
		))
	}
	sb.WriteString(`</div>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(sb.String()))
}

// AIGenerateExcerpt creates a concise excerpt from the content body.
// Returns an HTML fragment with the excerpt and an "Apply" button.
func (a *Admin) AIGenerateExcerpt(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	title := r.FormValue("title")

	if body == "" {
		writeAIError(w, "Please write some content first so AI can generate an excerpt.")
		return
	}

	if !a.checkPromptSafety(w, r, truncate(body, 2000)) {
		return
	}

	prompt := fmt.Sprintf("Title: %s\n\nContent:\n%s", title, truncate(body, 2000))

	systemPrompt := `You are a content summarization expert. Generate a compelling excerpt/summary
of the given content in 1-2 sentences (max 160 characters). The excerpt should capture the essence
of the content and entice readers to click. Output ONLY the excerpt text, nothing else.`

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskLight, systemPrompt, prompt)
	if err != nil {
		slog.Error("ai generate excerpt failed", "error", err)
		writeAIError(w, "AI request failed. Check your provider configuration.")
		return
	}

	result = strings.TrimSpace(result)
	escaped := html.EscapeString(result)

	fragment := fmt.Sprintf(
		`<div class="space-y-2">
			<p class="text-xs text-gray-700 bg-gray-50 rounded p-2">%s</p>
			<button type="button" onclick="document.getElementById('excerpt').value = %s"
				class="w-full rounded-md bg-indigo-50 border border-indigo-200 px-2 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100 transition-colors">
				Apply to Excerpt
			</button>
		</div>`,
		escaped,
		quoteJSString(result),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fragment))
}

// AISEOMetadata generates SEO meta description and keywords from the content.
// Returns an HTML fragment with both fields and "Apply" buttons.
func (a *Admin) AISEOMetadata(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	title := r.FormValue("title")

	if body == "" && title == "" {
		writeAIError(w, "Please write some content or a title first.")
		return
	}

	promptText := title + " " + body
	if !a.checkPromptSafety(w, r, truncate(promptText, 2000)) {
		return
	}

	prompt := fmt.Sprintf("Title: %s\n\nContent:\n%s", title, truncate(body, 2000))

	systemPrompt := `You are an SEO expert. For the given content, generate:
1. A meta description (max 160 characters, compelling for search results)
2. A comma-separated list of 5-8 relevant keywords

Output your response in EXACTLY this format (two lines only):
DESCRIPTION: <your meta description>
KEYWORDS: <keyword1, keyword2, keyword3, ...>

Do not include any other text.`

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskLight, systemPrompt, prompt)
	if err != nil {
		slog.Error("ai seo metadata failed", "error", err)
		writeAIError(w, "AI request failed. Check your provider configuration.")
		return
	}

	desc, keywords := parseSEOResult(result)

	var sb strings.Builder
	sb.WriteString(`<div class="space-y-3">`)

	if desc != "" {
		escapedDesc := html.EscapeString(desc)
		sb.WriteString(fmt.Sprintf(
			`<div>
				<p class="text-xs font-medium text-gray-600 mb-1">Meta Description:</p>
				<p class="text-xs text-gray-700 bg-gray-50 rounded p-2">%s</p>
				<button type="button" onclick="document.getElementById('meta_description').value = %s"
					class="mt-1 w-full rounded-md bg-indigo-50 border border-indigo-200 px-2 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100 transition-colors">
					Apply Description
				</button>
			</div>`,
			escapedDesc,
			quoteJSString(desc),
		))
	}

	if keywords != "" {
		escapedKw := html.EscapeString(keywords)
		sb.WriteString(fmt.Sprintf(
			`<div>
				<p class="text-xs font-medium text-gray-600 mb-1">Keywords:</p>
				<p class="text-xs text-gray-700 bg-gray-50 rounded p-2">%s</p>
				<button type="button" onclick="document.getElementById('meta_keywords').value = %s"
					class="mt-1 w-full rounded-md bg-indigo-50 border border-indigo-200 px-2 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100 transition-colors">
					Apply Keywords
				</button>
			</div>`,
			escapedKw,
			quoteJSString(keywords),
		))
	}

	// Fallback if parsing failed: show raw result.
	if desc == "" && keywords == "" {
		writeAIResult(w, result)
		return
	}

	sb.WriteString(`</div>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(sb.String()))
}

// AIRewrite rewrites the content body in a specified tone.
// Returns an HTML fragment with the rewritten content and an "Apply" button.
func (a *Admin) AIRewrite(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	title := r.FormValue("title")
	tone := r.FormValue("tone")
	suggestion := strings.TrimSpace(r.FormValue("rewrite_suggestion"))

	if body == "" {
		writeAIError(w, "Please write some content first so AI can rewrite it.")
		return
	}

	if !a.checkPromptSafety(w, r, truncate(body, 3000)) {
		return
	}

	if suggestion != "" && !a.checkPromptSafety(w, r, suggestion) {
		return
	}

	if tone == "" {
		tone = "professional"
	}

	toneDescriptions := map[string]string{
		"professional": "professional, clear, and authoritative",
		"casual":       "casual, friendly, and conversational",
		"formal":       "formal, academic, and precise",
		"persuasive":   "persuasive, compelling, and action-oriented",
		"concise":      "concise, direct, and to-the-point",
	}

	toneDesc, ok := toneDescriptions[tone]
	if !ok {
		toneDesc = "professional, clear, and authoritative"
	}

	prompt := fmt.Sprintf("Title: %s\n\nContent to rewrite:\n%s", title, truncate(body, 3000))
	if suggestion != "" {
		prompt += fmt.Sprintf("\n\nEditor's guidance: %s", suggestion)
	}

	systemPrompt := fmt.Sprintf(`You are a professional content editor. Rewrite the given content
in a %s tone. Preserve the key information and structure but adjust the language and style.
The content uses Markdown formatting — preserve all Markdown syntax.
If the editor provided guidance, follow those instructions carefully while applying the requested tone.
Output ONLY the rewritten content, nothing else.`, toneDesc)

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskContent, systemPrompt, prompt)
	if err != nil {
		slog.Error("ai rewrite failed", "error", err)
		writeAIError(w, "AI request failed. Check your provider configuration.")
		return
	}

	result = strings.TrimSpace(result)

	rewriteHTML, err := markdown.ToHTML(result)
	if err != nil {
		rewriteHTML = html.EscapeString(result)
	}

	fragment := fmt.Sprintf(
		`<div class="space-y-2">
			<div class="ai-preview text-gray-700 bg-gray-50 rounded p-2 max-h-64 overflow-y-auto prose prose-sm">%s</div>
			<button type="button" onclick="if(window._markdownEditor){window._markdownEditor.value(%s)}else{document.getElementById('body').value=%s}"
				class="w-full rounded-md bg-indigo-50 border border-indigo-200 px-2 py-1 text-xs font-medium text-indigo-700 hover:bg-indigo-100 transition-colors">
				Apply to Content
			</button>
		</div>`,
		rewriteHTML,
		quoteJSString(result),
		quoteJSString(result),
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(fragment))
}

// AIExtractTags extracts relevant tags/categories from the content.
// Returns an HTML fragment with clickable tag pills.
func (a *Admin) AIExtractTags(w http.ResponseWriter, r *http.Request) {
	body := r.FormValue("body")
	title := r.FormValue("title")

	if body == "" && title == "" {
		writeAIError(w, "Please write some content or a title first.")
		return
	}

	promptText := title + " " + body
	if !a.checkPromptSafety(w, r, truncate(promptText, 2000)) {
		return
	}

	prompt := fmt.Sprintf("Title: %s\n\nContent:\n%s", title, truncate(body, 2000))

	systemPrompt := `You are a content categorization expert. Extract 5-10 relevant tags from
the given content. Tags should be short (1-3 words), lowercase, and relevant for blog categorization.
Output ONLY the tags as a comma-separated list on a single line. No other text.`

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskLight, systemPrompt, prompt)
	if err != nil {
		slog.Error("ai extract tags failed", "error", err)
		writeAIError(w, "AI request failed. Check your provider configuration.")
		return
	}

	tags := parseTags(result)
	if len(tags) == 0 {
		writeAIResult(w, result)
		return
	}

	var sb strings.Builder
	sb.WriteString(`<div class="flex flex-wrap gap-1.5">`)
	for _, tag := range tags {
		escaped := html.EscapeString(tag)
		// Clicking a tag appends it to the meta_keywords field.
		sb.WriteString(fmt.Sprintf(
			`<button type="button"
				onclick="var f=document.getElementById('meta_keywords'); f.value = f.value ? f.value + ', %s' : '%s'"
				class="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-700 hover:bg-indigo-100 hover:text-indigo-700 transition-colors cursor-pointer">
				%s
			</button>`,
			escaped, escaped, escaped,
		))
	}
	sb.WriteString(`</div>`)
	sb.WriteString(`<p class="text-xs text-gray-400 mt-1.5">Click tags to add them to keywords.</p>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(sb.String()))
}

// --- Provider Switching ---

// AISetProvider switches the AI provider for the current tenant.
// Persists the choice in site_settings so each tenant can use a different
// provider without affecting other tenants.
func (a *Admin) AISetProvider(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("provider"))
	if name == "" {
		writeAIError(w, "No provider specified.")
		return
	}

	if !a.aiRegistry.HasProvider(name) {
		slog.Warn("failed to switch AI provider", "provider", name) //nolint:gosec // G706: slog structured logging safely escapes the value field.
		writeAIError(w, fmt.Sprintf("Cannot switch to %q: provider not available (no API key configured).", name))
		return
	}

	// Persist the choice per-tenant in site_settings.
	sess := middleware.SessionFromCtx(r.Context())
	err := a.siteSettingStore.Set(sess.TenantID, "ai_provider", name)
	if err != nil {
		slog.Error("failed to save AI provider setting", "error", err, "tenant_id", sess.TenantID)
		writeAIError(w, "Failed to save provider setting.")
		return
	}

	slog.Info("ai provider switched", "provider", name, "tenant_id", sess.TenantID) //nolint:gosec // G706: name is validated by HasProvider; only known provider names reach here.

	// If this is an HTMX request from the AI assistant panel, return the
	// updated provider selector dropdown.
	source := r.Header.Get("HX-Target")
	if r.Header.Get("HX-Request") == "true" && source == "ai-provider-select" {
		a.writeProviderSelector(w, name)
		return
	}

	// HTMX request from settings page or non-HTMX: redirect to settings.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/admin/settings")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// AIProviderStatus returns the current provider selector fragment.
// Used by the content form to load the initial state of the provider dropdown.
func (a *Admin) AIProviderStatus(w http.ResponseWriter, r *http.Request) {
	a.writeProviderSelector(w, a.tenantAIProvider(r))
}

// AIImageProviders returns a JSON list of providers that support image
// generation. Used by the frontend to populate image provider selectors.
func (a *Admin) AIImageProviders(w http.ResponseWriter, _ *http.Request) {
	names := a.aiRegistry.ImageProviders()

	type providerInfo struct {
		Name  string `json:"name"`
		Label string `json:"label"`
	}

	var providers []providerInfo
	for _, p := range a.aiConfig.Providers {
		for _, name := range names {
			if p.Name == name {
				providers = append(providers, providerInfo{
					Name:  p.Name,
					Label: p.Label,
				})
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(providers)
}

// providerInfoForTenant returns a copy of the provider list with the Active
// flag set based on the tenant's stored ai_provider setting.
func (a *Admin) providerInfoForTenant(r *http.Request) []AIProviderInfo {
	active := a.tenantAIProvider(r)
	providers := make([]AIProviderInfo, len(a.aiConfig.Providers))
	copy(providers, a.aiConfig.Providers)
	for i := range providers {
		providers[i].Active = providers[i].Name == active
	}
	return providers
}

// writeProviderSelector writes an HTML fragment containing the provider
// dropdown selector with the current active provider highlighted.
func (a *Admin) writeProviderSelector(w http.ResponseWriter, active string) {
	var sb strings.Builder
	sb.WriteString(`<select name="provider" `)
	sb.WriteString(`hx-post="/admin/ai/set-provider" `)
	sb.WriteString(`hx-target="#ai-provider-select" `)
	sb.WriteString(`hx-swap="innerHTML" `)
	sb.WriteString(`hx-include="[name='csrf_token']" `)
	sb.WriteString(`class="block w-full rounded-md border border-gray-300 px-2 py-1 text-xs shadow-sm `)
	sb.WriteString(`focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 focus:outline-none">`)

	for _, p := range a.aiConfig.Providers {
		if !p.HasKey {
			continue
		}
		selected := ""
		if p.Name == active {
			selected = " selected"
		}
		sb.WriteString(fmt.Sprintf(
			`<option value="%s"%s>%s (%s)</option>`,
			html.EscapeString(p.Name),
			selected,
			html.EscapeString(p.Label),
			html.EscapeString(p.Model),
		))
	}
	sb.WriteString(`</select>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(sb.String()))
}

// --- Helper functions ---

// checkPromptSafety runs the user prompt through the moderation API.
// Returns true if the prompt is safe (or if no moderator is available).
// If the prompt is flagged, writes an error response and returns false.
func (a *Admin) checkPromptSafety(w http.ResponseWriter, r *http.Request, prompt string) bool {
	result, err := a.aiRegistry.CheckPrompt(r.Context(), prompt)
	if err != nil {
		slog.Warn("moderation check failed, allowing prompt", "error", err)
		return true // fail open — providers have their own safety filters
	}

	if result.Safe {
		return true
	}

	categories := strings.Join(result.Categories, ", ")
	slog.Warn("prompt flagged by moderation", "categories", categories)

	msg := fmt.Sprintf(
		"Your prompt was flagged for: %s. Please reformulate your request and try again.",
		categories,
	)
	writeAIError(w, msg)
	return false
}

// writeAIError writes an error message HTML fragment.
func writeAIError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<p class="text-xs text-red-600 bg-red-50 rounded p-2">%s</p>`, html.EscapeString(msg))
}

// writeAIResult writes a plain text result as an HTML fragment.
func writeAIResult(w http.ResponseWriter, result string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<p class="text-xs text-gray-700 bg-gray-50 rounded p-2 whitespace-pre-wrap">%s</p>`,
		html.EscapeString(strings.TrimSpace(result)))
}

// truncate cuts a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// parseNumberedList extracts items from a numbered list (e.g., "1. Title Here").
func parseNumberedList(text string) []string {
	var items []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip common numbered list prefixes: "1. ", "1) ", "- "
		for _, prefix := range []string{"1. ", "2. ", "3. ", "4. ", "5. ", "6. ", "7. ", "8. ", "9. ", "10. ",
			"1) ", "2) ", "3) ", "4) ", "5) ", "6) ", "7) ", "8) ", "9) ", "10) ",
			"- ", "* "} {
			if strings.HasPrefix(line, prefix) {
				line = strings.TrimPrefix(line, prefix)
				break
			}
		}
		line = strings.TrimSpace(line)
		// Remove surrounding quotes if present.
		line = strings.Trim(line, `"'`)
		if line != "" {
			items = append(items, line)
		}
	}
	return items
}

// parseSEOResult extracts meta description and keywords from the structured
// AI response (expects "DESCRIPTION: ..." and "KEYWORDS: ..." lines).
func parseSEOResult(text string) (description, keywords string) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "DESCRIPTION:") {
			description = strings.TrimSpace(line[len("DESCRIPTION:"):])
		} else if strings.HasPrefix(upper, "KEYWORDS:") {
			keywords = strings.TrimSpace(line[len("KEYWORDS:"):])
		} else if strings.HasPrefix(upper, "META DESCRIPTION:") {
			description = strings.TrimSpace(line[len("META DESCRIPTION:"):])
		} else if strings.HasPrefix(upper, "META KEYWORDS:") {
			keywords = strings.TrimSpace(line[len("META KEYWORDS:"):])
		}
	}
	return description, keywords
}

// parseTags splits a comma-separated tag string into individual trimmed tags.
func parseTags(text string) []string {
	var tags []string
	for _, tag := range strings.Split(text, ",") {
		tag = strings.TrimSpace(tag)
		// Remove surrounding quotes, dashes, bullets.
		tag = strings.Trim(tag, `"'-*`)
		tag = strings.TrimSpace(tag)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// --- AI Template Builder Endpoints ---
//
// These handlers power the "AI Design" template builder — a chat-based UI
// where users describe a template and the AI generates HTML+TailwindCSS
// with Go template variables.

// AITemplatePage renders the AI template builder chat interface.
func (a *Admin) AITemplatePage(w http.ResponseWriter, r *http.Request) {
	a.renderer.Page(w, r, "template_ai", &render.PageData{
		Title:   "AI Template Builder",
		Section: "templates",
	})
}

// templateGenResponse is the JSON response from the template generation endpoint.
type templateGenResponse struct {
	HTML            string `json:"html"`
	Message         string `json:"message"`
	Valid           bool   `json:"valid"`
	ValidationError string `json:"validation_error,omitempty"`
	Preview         string `json:"preview,omitempty"`
	Error           string `json:"error,omitempty"`
}

// templateSaveResponse is the JSON response from the template save endpoint.
type templateSaveResponse struct {
	ID    string `json:"id,omitempty"`
	Error string `json:"error,omitempty"`
}

// AITemplateGenerate generates an HTML+TailwindCSS template from a user prompt.
// It accepts the template type, prompt, optional conversation history, and
// optional current HTML for iterative refinement. Returns JSON with the
// generated HTML, validation status, and a rendered preview.
func (a *Admin) AITemplateGenerate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	prompt := r.FormValue("prompt")
	tmplType := r.FormValue("template_type")
	chatHistory := r.FormValue("chat_history")
	currentHTML := r.FormValue("current_html")

	if prompt == "" {
		writeJSON(w, http.StatusBadRequest, templateGenResponse{Error: "Please enter a prompt."})
		return
	}

	// Check prompt safety before generating.
	modResult, err := a.aiRegistry.CheckPrompt(r.Context(), prompt)
	if err != nil {
		slog.Warn("moderation check failed for template prompt", "error", err)
	} else if !modResult.Safe {
		categories := strings.Join(modResult.Categories, ", ")
		slog.Warn("template prompt flagged by moderation", "categories", categories)
		writeJSON(w, http.StatusOK, templateGenResponse{
			Error: fmt.Sprintf("Your prompt was flagged for: %s. Please reformulate your request and try again.", categories),
		})
		return
	}

	// Build the system prompt with type-specific variable documentation and
	// the active design brief (if any) for visual consistency.
	designBrief := a.getActiveDesignBrief(sess.TenantID)
	systemPrompt := buildTemplateSystemPrompt(tmplType, designBrief)

	// Build the user prompt with context.
	var userPrompt strings.Builder
	userPrompt.WriteString(fmt.Sprintf("Template type: %s\n\n", tmplType))

	if currentHTML != "" {
		userPrompt.WriteString("Current template HTML (modify based on my new request):\n```html\n")
		userPrompt.WriteString(truncate(currentHTML, 4000))
		userPrompt.WriteString("\n```\n\n")
	}

	if chatHistory != "" && currentHTML == "" {
		userPrompt.WriteString("Conversation so far:\n")
		userPrompt.WriteString(truncate(chatHistory, 2000))
		userPrompt.WriteString("\n\n")
	}

	userPrompt.WriteString("Request: ")
	userPrompt.WriteString(prompt)

	result, err := a.aiRegistry.GenerateForTaskAs(r.Context(), a.tenantAIProvider(r), ai.TaskTemplate, systemPrompt, userPrompt.String())
	if err != nil {
		slog.Error("ai template generate failed", "error", err)
		writeJSON(w, http.StatusOK, templateGenResponse{
			Error: "AI request failed. Check your provider configuration.",
		})
		return
	}

	// Extract HTML from the response (the AI may wrap it in markdown code blocks).
	htmlContent := extractHTMLFromResponse(result)

	// Validate as a Go template.
	validationErr := a.engine.ValidateTemplate(htmlContent)
	valid := validationErr == nil
	validationErrStr := ""
	if validationErr != nil {
		validationErrStr = validationErr.Error()
	}

	// Generate a preview — use real content if a content_id was provided.
	var previewHTML string
	if valid {
		contentID := r.FormValue("content_id")
		var previewData any
		if contentID != "" {
			previewData = a.buildRealPreviewData(sess.TenantID, tmplType, contentID)
		}
		if previewData == nil {
			previewData = buildPreviewData(tmplType)
		}
		rendered, err := a.engine.ValidateAndRender(htmlContent, previewData)
		if err == nil {
			previewHTML = string(rendered)
		}
	}

	// Build a summary message for the chat.
	message := "Template generated successfully."
	if !valid {
		message = "Template generated but has a syntax error. I'll try to fix it — describe the issue or try again."
	}

	writeJSON(w, http.StatusOK, templateGenResponse{
		HTML:            htmlContent,
		Message:         message,
		Valid:           valid,
		ValidationError: validationErrStr,
		Preview:         previewHTML,
	})
}

// AITemplateSave saves a generated template to the database.
// Validates the template before saving and triggers cache invalidation.
func (a *Admin) AITemplateSave(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	name := r.FormValue("name")
	tmplType := models.TemplateType(r.FormValue("type"))
	htmlContent := r.FormValue("html_content")

	if name == "" || htmlContent == "" {
		writeJSON(w, http.StatusBadRequest, templateSaveResponse{Error: "Name and HTML content are required."})
		return
	}

	// Validate the template syntax before saving.
	if err := a.engine.ValidateTemplate(htmlContent); err != nil {
		writeJSON(w, http.StatusOK, templateSaveResponse{Error: "Template syntax error: " + err.Error()})
		return
	}

	t := &models.Template{
		Name:        name,
		Type:        tmplType,
		HTMLContent: htmlContent,
	}

	created, err := a.templateStore.Create(sess.TenantID, t)
	if err != nil {
		slog.Error("ai save template failed", "error", err)
		writeJSON(w, http.StatusOK, templateSaveResponse{Error: "Failed to save template."})
		return
	}

	a.cacheLog.Log(sess.TenantID, "template", created.ID, "create")
	writeJSON(w, http.StatusOK, templateSaveResponse{ID: created.ID.String()})
}

// --- Design Theme Endpoints ---

// themeResponse is the JSON representation of a design theme.
type themeResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	StylePrompt string `json:"style_prompt"`
	IsActive    bool   `json:"is_active"`
}

// AIThemeList returns all design themes as JSON.
func (a *Admin) AIThemeList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	themes, err := a.themeStore.List(sess.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list themes."})
		return
	}

	items := make([]themeResponse, 0, len(themes))
	for _, t := range themes {
		items = append(items, themeResponse{
			ID:          t.ID.String(),
			Name:        t.Name,
			StylePrompt: t.StylePrompt,
			IsActive:    t.IsActive,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

// AIThemeCreate creates a new design theme.
func (a *Admin) AIThemeCreate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	name := strings.TrimSpace(r.FormValue("name"))
	stylePrompt := strings.TrimSpace(r.FormValue("style_prompt"))

	if name == "" || stylePrompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name and style prompt are required."})
		return
	}

	theme := &models.DesignTheme{Name: name, StylePrompt: stylePrompt}
	created, err := a.themeStore.Create(sess.TenantID, theme)
	if err != nil {
		slog.Error("create design theme failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create theme."})
		return
	}

	writeJSON(w, http.StatusOK, themeResponse{
		ID:          created.ID.String(),
		Name:        created.Name,
		StylePrompt: created.StylePrompt,
		IsActive:    created.IsActive,
	})
}

// AIThemeUpdate updates an existing design theme's name and style prompt.
func (a *Admin) AIThemeUpdate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid theme ID."})
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	stylePrompt := strings.TrimSpace(r.FormValue("style_prompt"))

	if name == "" || stylePrompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name and style prompt are required."})
		return
	}

	if err := a.themeStore.Update(id, name, stylePrompt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update theme."})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AIThemeActivate sets a theme as the active design brief.
func (a *Admin) AIThemeActivate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid theme ID."})
		return
	}

	if err := a.themeStore.Activate(sess.TenantID, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to activate theme."})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AIThemeDeactivate disables the active theme without activating another.
func (a *Admin) AIThemeDeactivate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid theme ID."})
		return
	}

	if err := a.themeStore.Deactivate(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to deactivate theme."})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AIThemeDelete removes a design theme (cannot delete active).
func (a *Admin) AIThemeDelete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid theme ID."})
		return
	}

	if err := a.themeStore.Delete(sess.TenantID, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AIActiveTheme returns the currently active design theme (or null).
func (a *Admin) AIActiveTheme(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	theme, err := a.themeStore.FindActive(sess.TenantID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch active theme."})
		return
	}
	if theme == nil {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	writeJSON(w, http.StatusOK, themeResponse{
		ID:          theme.ID.String(),
		Name:        theme.Name,
		StylePrompt: theme.StylePrompt,
		IsActive:    true,
	})
}

// getActiveDesignBrief fetches the active theme's style prompt, returning
// an empty string if no theme is active or the lookup fails.
func (a *Admin) getActiveDesignBrief(tenantID uuid.UUID) string {
	theme, err := a.themeStore.FindActive(tenantID)
	if err != nil || theme == nil {
		return ""
	}
	return theme.StylePrompt
}

// --- Restyle All ---

// restylePreviewRequest holds the 4 template HTMLs for a combined preview.
type restylePreviewRequest struct {
	HeaderHTML      string `json:"header_html"`
	FooterHTML      string `json:"footer_html"`
	PageHTML        string `json:"page_html"`
	ArticleLoopHTML string `json:"article_loop_html"`
	ContentID       string `json:"content_id"`
}

// restylePreviewResponse returns the rendered combined previews.
type restylePreviewResponse struct {
	PagePreview        string `json:"page_preview"`
	ArticleLoopPreview string `json:"article_loop_preview"`
	Error              string `json:"error,omitempty"`
}

// AIRestylePreview renders a combined preview of all 4 templates together.
// It compiles the header and footer templates, renders them, then injects
// their output into the page and article_loop templates for a unified view.
func (a *Admin) AIRestylePreview(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	var req restylePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, restylePreviewResponse{Error: "Invalid request."})
		return
	}

	// Render header and footer templates to get their HTML output.
	headerData := struct {
		SiteTitle string
		Slogan    string
		Year      int
	}{SiteTitle: "YaaiCMS", Slogan: "Your AI-powered CMS", Year: time.Now().Year()}

	renderedHeader := ""
	if req.HeaderHTML != "" {
		result, err := a.engine.ValidateAndRender(req.HeaderHTML, headerData)
		if err == nil {
			renderedHeader = string(result)
		}
	}

	renderedFooter := ""
	if req.FooterHTML != "" {
		result, err := a.engine.ValidateAndRender(req.FooterHTML, headerData)
		if err == nil {
			renderedFooter = string(result)
		}
	}

	resp := restylePreviewResponse{}

	// Render page preview with the generated header/footer.
	if req.PageHTML != "" {
		var pageData any
		if req.ContentID != "" {
			pageData = a.buildRealPreviewData(sess.TenantID, "page", req.ContentID)
		}
		if pageData == nil {
			pageData = buildPreviewData("page")
		}

		// Inject the rendered header/footer into the preview data.
		if pd, ok := pageData.(engine.PageData); ok {
			if renderedHeader != "" {
				pd.Header = template.HTML(renderedHeader)
			}
			if renderedFooter != "" {
				pd.Footer = template.HTML(renderedFooter)
			}
			pageData = pd
		}

		result, err := a.engine.ValidateAndRender(req.PageHTML, pageData)
		if err == nil {
			resp.PagePreview = string(result)
		}
	}

	// Render article loop preview with the generated header/footer.
	if req.ArticleLoopHTML != "" {
		var loopData any
		loopData = a.buildRealPreviewData(sess.TenantID, "article_loop", "")
		if loopData == nil {
			loopData = buildPreviewData("article_loop")
		}

		// Inject the rendered header/footer.
		if ld, ok := loopData.(engine.ListData); ok {
			if renderedHeader != "" {
				ld.Header = template.HTML(renderedHeader)
			}
			if renderedFooter != "" {
				ld.Footer = template.HTML(renderedFooter)
			}
			loopData = ld
		}

		result, err := a.engine.ValidateAndRender(req.ArticleLoopHTML, loopData)
		if err == nil {
			resp.ArticleLoopPreview = string(result)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- Preview Content Selection ---

// previewContentItem is a JSON-serializable summary of a content item
// for the preview content selector dropdown.
type previewContentItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
	Slug  string `json:"slug"`
}

// AIPreviewContentList returns a JSON list of available content items
// (posts and pages) that can be used for real-data template previews.
func (a *Admin) AIPreviewContentList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	var items []previewContentItem

	// Fetch published posts.
	posts, err := a.contentStore.ListPublishedByType(sess.TenantID, models.ContentTypePost)
	if err != nil {
		slog.Warn("preview content list: failed to list posts", "error", err)
	}
	for _, p := range posts {
		items = append(items, previewContentItem{
			ID:    p.ID.String(),
			Title: p.Title,
			Type:  "post",
			Slug:  p.Slug,
		})
	}

	// Fetch published pages.
	pages, err := a.contentStore.ListPublishedByType(sess.TenantID, models.ContentTypePage)
	if err != nil {
		slog.Warn("preview content list: failed to list pages", "error", err)
	}
	for _, p := range pages {
		items = append(items, previewContentItem{
			ID:    p.ID.String(),
			Title: p.Title,
			Type:  "page",
			Slug:  p.Slug,
		})
	}

	// Also include draft content — useful for previewing unpublished work.
	drafts, err := a.contentStore.ListByType(sess.TenantID, models.ContentTypePost)
	if err == nil {
		for _, d := range drafts {
			if d.Status == models.ContentStatusDraft {
				items = append(items, previewContentItem{
					ID:    d.ID.String(),
					Title: d.Title + " (draft)",
					Type:  "post",
					Slug:  d.Slug,
				})
			}
		}
	}
	draftPages, err := a.contentStore.ListByType(sess.TenantID, models.ContentTypePage)
	if err == nil {
		for _, d := range draftPages {
			if d.Status == models.ContentStatusDraft {
				items = append(items, previewContentItem{
					ID:    d.ID.String(),
					Title: d.Title + " (draft)",
					Type:  "page",
					Slug:  d.Slug,
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(items)
}

// buildRealPreviewData builds template preview data from real content.
// For "page" templates, it fetches a specific content item by ID.
// For "article_loop" templates, it fetches all published posts.
// Returns nil if the content cannot be found or an error occurs.
func (a *Admin) buildRealPreviewData(tenantID uuid.UUID, tmplType string, contentID string) any {
	switch tmplType {
	case "page":
		return a.buildRealPagePreview(tenantID, contentID)
	case "article_loop":
		return a.buildRealArticleLoopPreview(tenantID)
	default:
		// Header/footer don't use content data.
		return buildPreviewData(tmplType)
	}
}

// buildRealPagePreview fetches a content item and its featured image,
// then assembles PageData for template preview rendering.
func (a *Admin) buildRealPagePreview(tenantID uuid.UUID, contentID string) any {
	id, err := uuid.Parse(contentID)
	if err != nil {
		return nil
	}

	content, err := a.contentStore.FindByID(tenantID, id)
	if err != nil || content == nil {
		return nil
	}

	// Convert Markdown body to HTML if needed.
	bodyHTML := content.Body
	if content.BodyFormat == models.BodyFormatMarkdown {
		rendered, err := markdown.ToHTML(content.Body)
		if err == nil {
			bodyHTML = rendered
		}
	}

	// Rewrite inline images for responsive srcset.
	bodyHTML = a.engine.RewriteBodyImages(bodyHTML)

	publishedAt := ""
	if content.PublishedAt != nil {
		publishedAt = content.PublishedAt.Format("January 2, 2006")
	}

	data := engine.PageData{
		SiteTitle:   "YaaiCMS",
		Slogan:      "Your AI-powered CMS",
		Title:       content.Title,
		Body:        template.HTML(bodyHTML),
		Slug:        content.Slug,
		PublishedAt: publishedAt,
		Header:      "<header class='bg-gray-800 text-white p-4'><nav class='max-w-6xl mx-auto flex justify-between items-center'><span class='text-xl font-bold'>YaaiCMS</span><div class='space-x-4'><a href='/' class='hover:text-gray-300'>Home</a><a href='/blog' class='hover:text-gray-300'>Blog</a></div></nav></header>",
		Footer:      "<footer class='bg-gray-800 text-gray-400 p-6 text-center text-sm'>&copy; 2026 YaaiCMS. All rights reserved.</footer>",
		Year:        time.Now().Year(),
	}

	if content.Excerpt != nil {
		data.Excerpt = *content.Excerpt
	}
	if content.MetaDescription != nil {
		data.MetaDescription = *content.MetaDescription
	}
	if content.MetaKeywords != nil {
		data.MetaKeywords = *content.MetaKeywords
	}

	// Resolve featured image if available.
	if content.FeaturedImageID != nil && a.mediaStore != nil && a.storageClient != nil {
		media, err := a.mediaStore.FindByID(tenantID, *content.FeaturedImageID)
		if err == nil && media != nil && media.Bucket == a.storageClient.PublicBucket() {
			data.FeaturedImageURL = a.storageClient.FileURL(media.S3Key)
			if media.AltText != nil {
				data.FeaturedImageAlt = *media.AltText
			}
			// Build srcset from variants.
			if a.variantStore != nil {
				variants, err := a.variantStore.FindByMediaIDs([]uuid.UUID{media.ID})
				if err == nil {
					data.FeaturedImageSrcset = buildSrcsetForPreview(a.storageClient, variants[media.ID])
				}
			}
		}
	}

	return data
}

// buildRealArticleLoopPreview fetches published posts and their featured
// images, then assembles ListData for article_loop template preview.
func (a *Admin) buildRealArticleLoopPreview(tenantID uuid.UUID) any {
	posts, err := a.contentStore.ListPublishedByType(tenantID, models.ContentTypePost)
	if err != nil || len(posts) == 0 {
		return nil
	}

	// Collect featured image IDs for batch lookup.
	var mediaIDs []uuid.UUID
	mediaIDSet := make(map[uuid.UUID]bool)
	for _, p := range posts {
		if p.FeaturedImageID != nil && !mediaIDSet[*p.FeaturedImageID] {
			mediaIDs = append(mediaIDs, *p.FeaturedImageID)
			mediaIDSet[*p.FeaturedImageID] = true
		}
	}

	// Batch-fetch variants for all featured images.
	var variantMap map[uuid.UUID][]models.MediaVariant
	if len(mediaIDs) > 0 && a.variantStore != nil {
		variantMap, _ = a.variantStore.FindByMediaIDs(mediaIDs)
	}

	var postItems []engine.PostItem
	for _, p := range posts {
		item := engine.PostItem{
			Title: p.Title,
			Slug:  p.Slug,
		}
		if p.Excerpt != nil {
			item.Excerpt = *p.Excerpt
		}
		if p.PublishedAt != nil {
			item.PublishedAt = p.PublishedAt.Format("January 2, 2006")
		}

		// Resolve featured image.
		if p.FeaturedImageID != nil && a.mediaStore != nil && a.storageClient != nil {
			media, err := a.mediaStore.FindByID(tenantID, *p.FeaturedImageID)
			if err == nil && media != nil && media.Bucket == a.storageClient.PublicBucket() {
				item.FeaturedImageURL = a.storageClient.FileURL(media.S3Key)
				if media.AltText != nil {
					item.FeaturedImageAlt = *media.AltText
				}
				if variantMap != nil {
					item.FeaturedImageSrcset = buildSrcsetForPreview(a.storageClient, variantMap[media.ID])
				}
			}
		}
		postItems = append(postItems, item)
	}

	return engine.ListData{
		SiteTitle: "YaaiCMS",
		Slogan:    "Your AI-powered CMS",
		Title:     "Blog",
		Posts:    postItems,
		Header:   "<header class='bg-gray-800 text-white p-4'><nav class='max-w-6xl mx-auto flex justify-between items-center'><span class='text-xl font-bold'>YaaiCMS</span><div class='space-x-4'><a href='/' class='hover:text-gray-300'>Home</a><a href='/blog' class='hover:text-gray-300'>Blog</a></div></nav></header>",
		Footer:   "<footer class='bg-gray-800 text-gray-400 p-6 text-center text-sm'>&copy; 2026 YaaiCMS. All rights reserved.</footer>",
		Year:     time.Now().Year(),
	}
}

// buildSrcsetForPreview constructs a srcset string from media variants,
// excluding thumbnails (too small for content display).
func buildSrcsetForPreview(storageClient *storage.Client, variants []models.MediaVariant) string {
	var parts []string
	for _, v := range variants {
		if v.Name == "thumb" {
			continue
		}
		url := storageClient.FileURL(v.S3Key)
		parts = append(parts, fmt.Sprintf("%s %dw", url, v.Width))
	}
	return strings.Join(parts, ", ")
}

// buildTemplateSystemPrompt creates a system prompt that instructs the LLM
// to generate an HTML+TailwindCSS template with the correct Go template
// variables for the given template type. Each variable includes a detailed
// description of its content, purpose, and recommended usage patterns so
// the AI can make informed design decisions.
func buildTemplateSystemPrompt(tmplType string, designBrief ...string) string {
	base := `You are an expert web designer who creates beautiful, modern HTML templates using TailwindCSS.
You generate complete, production-ready HTML+TailwindCSS templates for a CMS called YaaiCMS.

CRITICAL RULES:
1. Output ONLY the HTML template code. No explanations, no markdown code fences, no comments outside the HTML.
2. Use TailwindCSS utility classes for all styling. Do not use custom CSS.
3. Use Go template syntax for dynamic content: {{.VariableName}}
4. For raw HTML content (like Body, Header, Footer), the CMS handles escaping — just use {{.Body}} etc.
5. Templates should be responsive and look professional on all screen sizes.
6. Use semantic HTML elements (header, nav, main, article, footer, section, etc.).
7. Include the TailwindCSS CDN script tag only in full page templates (page, article_loop).
8. Guard optional fields with {{if .Field}} to avoid rendering empty markup.`

	var vars string
	switch tmplType {
	case "header":
		vars = `

TEMPLATE TYPE: Header (navigation bar)
The header is a reusable fragment injected at the top of every page via {{.Header}}.
It should NOT include <html>, <head>, or <body> tags — just the header/nav markup.

AVAILABLE VARIABLES:
- {{.SiteTitle}} (string, always set)
  The site's public title, e.g., "My Blog" or "Acme Corp".
  Use as the logo text or brand name in the navigation bar.

- {{.Slogan}} (string, may be empty)
  The site's tagline or slogan, e.g., "Just another SmartPress site".
  Display below or beside the site title if set: {{if .Slogan}}<span>{{.Slogan}}</span>{{end}}

- {{.Year}} (int, always set)
  The current calendar year (e.g., 2026).
  Rarely needed in headers, but available for copyright if combined header/footer.

- {{range .Menus.main}} — Main navigation menu items
  Each item has: {{.Label}}, {{.URL}}, {{.Target}}, {{.Active}}, {{.Children}}
  Use {{range .Children}} for dropdown sub-items (one level only).
  {{.Active}} is true when the item matches the current page — use for active state styling.
  {{.Target}} is "_blank" for external links — use: {{if .Target}}target="{{.Target}}"{{end}}
  IMPORTANT: NEVER hardcode navigation links. Always use {{range .Menus.main}} to render
  the main navigation. Users manage these links through the admin Menus page.

DESIGN GUIDELINES:
- Include a prominent site title/logo linking to "/" (the homepage).
- Optionally show the slogan near the title if set.
- Render navigation from {{range .Menus.main}} — do NOT hardcode any nav links.
- Support dropdown menus for items with {{.Children}} (one level of nesting).
- Make the header responsive: hamburger menu or collapsible nav on mobile.
- Use a contrasting background (e.g., dark bg with light text, or white with border-bottom).
- Consider making it sticky with "sticky top-0 z-50" for better UX.`

	case "footer":
		vars = `

TEMPLATE TYPE: Footer
The footer is a reusable fragment injected at the bottom of every page via {{.Footer}}.
It should NOT include <html>, <head>, or <body> tags — just the footer markup.

AVAILABLE VARIABLES:
- {{.SiteTitle}} (string, always set)
  The site's public title. Use in the copyright line.

- {{.Slogan}} (string, may be empty)
  The site's tagline or slogan. Optionally display in the footer.

- {{.Year}} (int, always set)
  The current year. Use for "© 2026 SiteTitle" copyright notices.

- {{range .Menus.footer}} — Footer navigation links
  Each item has: {{.Label}}, {{.URL}}, {{.Target}}
  Use for secondary navigation links in the footer (e.g., About, Contact, Blog).
  IMPORTANT: NEVER hardcode footer navigation links. Always use {{range .Menus.footer}}.

- {{range .Menus.footer_legal}} — Footer legal links (Privacy, Terms, etc.)
  Each item has: {{.Label}}, {{.URL}}, {{.Target}}
  Use for legal/compliance links typically displayed smaller or in a separate row.
  IMPORTANT: NEVER hardcode footer legal links. Always use {{range .Menus.footer_legal}}.

DESIGN GUIDELINES:
- Include a copyright notice: "© {{.Year}} {{.SiteTitle}}. All rights reserved."
- Render footer navigation from {{range .Menus.footer}} — do NOT hardcode any nav links.
- Render legal links from {{range .Menus.footer_legal}} in a separate row or section.
- Keep it compact — footers should complement, not compete with content.
- Use a background that pairs with the header for visual consistency.`

	case "page":
		vars = `

TEMPLATE TYPE: Page (full single-page layout)
Page templates render individual posts and pages. They are FULL HTML documents
that include the <html>, <head>, and <body> tags. The header and footer are
pre-rendered HTML fragments injected via {{.Header}} and {{.Footer}}.

Include the TailwindCSS CDN in <head>: <script src="https://cdn.tailwindcss.com"></script>

AVAILABLE VARIABLES:

Layout fragments (pre-rendered HTML, always present):
- {{.Header}} (template.HTML)
  The site header/navigation bar, pre-rendered from the active header template.
  Place at the top of <body>. It outputs raw HTML — no escaping needed.

- {{.Footer}} (template.HTML)
  The site footer, pre-rendered from the active footer template.
  Place at the bottom of <body>. It outputs raw HTML — no escaping needed.

Content fields:
- {{.Title}} (string, always set)
  The page or post title, e.g., "Getting Started with Go".
  Display prominently as an <h1> in the hero section or article header.

- {{.Body}} (template.HTML, always set)
  The main content body as rendered HTML. This is converted from Markdown and
  may contain headings (h2, h3), paragraphs, lists, blockquotes, code blocks,
  inline images (with responsive srcset already injected), and links.
  Style with Tailwind's prose classes: <div class="prose prose-lg max-w-none">{{.Body}}</div>

- {{.Excerpt}} (string, may be empty)
  A short 1-2 sentence summary of the content, useful as a subtitle or lead paragraph.
  Guard with: {{if .Excerpt}}<p class="lead">{{.Excerpt}}</p>{{end}}

- {{.Slug}} (string, always set)
  The URL-friendly identifier, e.g., "getting-started-with-go".
  The page is accessible at /{{.Slug}}. Useful for canonical URLs.

- {{.PublishedAt}} (string, may be empty)
  A human-readable publication date like "February 25, 2026".
  Display near the title as metadata: {{if .PublishedAt}}<time>{{.PublishedAt}}</time>{{end}}

Featured image (all three are empty strings when no image is set):
- {{.FeaturedImageURL}} (string)
  Public URL of the original featured image (e.g., PNG/JPG hosted on S3).
  Use as the src attribute. Always guard: {{if .FeaturedImageURL}}...{{end}}

- {{.FeaturedImageSrcset}} (string)
  Pre-built responsive srcset string with WebP variants at multiple widths,
  e.g., "https://s3.../img_sm.webp 640w, .../img_md.webp 1024w, .../img_lg.webp 1920w".
  Browsers automatically select the best size. Use with sizes attribute.

- {{.FeaturedImageAlt}} (string)
  Descriptive alt text for accessibility and SEO.

  RECOMMENDED featured image pattern:
  {{if .FeaturedImageURL}}
  <img src="{{.FeaturedImageURL}}"
       {{if .FeaturedImageSrcset}}srcset="{{.FeaturedImageSrcset}}"
       sizes="(max-width: 640px) 100vw, (max-width: 1024px) 1024px, 1920px"{{end}}
       alt="{{.FeaturedImageAlt}}"
       class="w-full h-auto rounded-lg object-cover" loading="lazy">
  {{end}}

SEO metadata (for <head>):
- {{.MetaDescription}} (string, may be empty)
  SEO meta description for search engine results (max ~160 chars).
  Use in <head>: {{if .MetaDescription}}<meta name="description" content="{{.MetaDescription}}">{{end}}

- {{.MetaKeywords}} (string, may be empty)
  Comma-separated SEO keywords.
  Use: {{if .MetaKeywords}}<meta name="keywords" content="{{.MetaKeywords}}">{{end}}

Site-level:
- {{.SiteTitle}} (string, always set)
  The site's public title. Use in <title>: <title>{{.Title}} | {{.SiteTitle}}</title>

- {{.Slogan}} (string, may be empty)
  The site's tagline. Rarely needed in page templates but available.

- {{.Year}} (int, always set)
  Current year. Available but rarely needed in page templates (footer handles copyright).

DESIGN GUIDELINES:
- Structure: <html> → <head> (with TailwindCSS CDN, meta tags) → <body> → {{.Header}} → <main> → {{.Footer}}
- Use a hero section with the title, date, and optional featured image.
- Render {{.Body}} inside a prose container for proper typography.
- Make the layout responsive: full-width on mobile, max-w-4xl centered on desktop.`

	case "article_loop":
		vars = `

TEMPLATE TYPE: Article Loop (post listing / blog index)
Article loop templates show a list or grid of blog posts. They are FULL HTML
documents with <html>, <head>, <body> tags. Posts are iterated with {{range .Posts}}.

Include the TailwindCSS CDN in <head>: <script src="https://cdn.tailwindcss.com"></script>

AVAILABLE VARIABLES:

Layout:
- {{.Header}} (template.HTML) — Pre-rendered site header. Place at top of <body>.
- {{.Footer}} (template.HTML) — Pre-rendered site footer. Place at bottom of <body>.
- {{.SiteTitle}} (string) — Site title, for <title> tag.
- {{.Slogan}} (string, may be empty) — Site tagline.
- {{.Year}} (int) — Current year.
- {{.Title}} (string) — Page title, typically "Blog" or "Posts". Display as <h1>.

Post loop — iterate with {{range .Posts}} ... {{end}}:
Each post item has these fields:

- {{.Title}} (string, always set)
  The post title. Display as a clickable heading linking to the post.

- {{.Slug}} (string, always set)
  URL slug for the post. Link as: <a href="/{{.Slug}}">{{.Title}}</a>

- {{.Excerpt}} (string, may be empty)
  A brief summary of the post content (1-2 sentences).
  Display below the title as a preview teaser.

- {{.PublishedAt}} (string, may be empty)
  Human-readable date like "February 25, 2026".

- {{.FeaturedImageURL}} (string, empty if no image)
  Public URL of the post's featured image.

- {{.FeaturedImageSrcset}} (string, empty if no image)
  Responsive srcset with WebP variants at multiple widths.

- {{.FeaturedImageAlt}} (string, empty if no image)
  Alt text for the featured image.

  RECOMMENDED post card image pattern:
  {{if .FeaturedImageURL}}
  <img src="{{.FeaturedImageURL}}"
       {{if .FeaturedImageSrcset}}srcset="{{.FeaturedImageSrcset}}"
       sizes="(max-width: 640px) 100vw, (max-width: 768px) 50vw, 33vw"{{end}}
       alt="{{.FeaturedImageAlt}}"
       class="w-full h-48 object-cover" loading="lazy">
  {{end}}

DESIGN GUIDELINES:
- Display posts in a responsive grid: 1 column on mobile, 2-3 columns on desktop.
- Each post card should include: featured image (if any), title (linked), excerpt, date.
- Use consistent card styling with hover effects for interactivity.
- Include the page title as an <h1> above the post grid.
- Consider adding visual interest when no featured image exists (colored placeholder, icon, etc.).`

	default:
		vars = "\nGenerate a generic HTML template using TailwindCSS."
	}

	prompt := base + "\n" + vars

	// Inject the design brief if one is active. This ensures all templates
	// share the same visual language (colors, typography, spacing, mood).
	if len(designBrief) > 0 && designBrief[0] != "" {
		prompt += `

DESIGN BRIEF — Follow this style guide for visual consistency across all templates:
` + designBrief[0] + `

You MUST follow the design brief above. Match the colors, typography, spacing, and
visual mood described. All templates (header, footer, page, article_loop) should
feel like they belong to the same website.`
	}

	return prompt
}

// buildPreviewData creates dummy data appropriate for the template type,
// used to render a preview of the generated template.
func buildPreviewData(tmplType string) any {
	switch tmplType {
	case "page":
		return engine.PageData{
			SiteTitle:           "YaaiCMS",
			Slogan:              "Your AI-powered CMS",
			Title:               "Preview Page Title",
			Body:                "<p>This is preview content. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.</p><p>Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.</p>",
			Excerpt:             "A brief preview excerpt for the page.",
			MetaDescription:     "Preview meta description for search engines",
			FeaturedImageURL:    "https://placehold.co/1200x630/0f172a/e2e8f0?text=Featured+Image",
			FeaturedImageSrcset: "https://placehold.co/640x336/0f172a/e2e8f0?text=640w 640w, https://placehold.co/1024x538/0f172a/e2e8f0?text=1024w 1024w, https://placehold.co/1920x1008/0f172a/e2e8f0?text=1920w 1920w",
			FeaturedImageAlt:    "A preview featured image",
			Slug:                "preview-page",
			PublishedAt:         "February 25, 2026",
			Header:              "<header class='bg-gray-800 text-white p-4'><nav class='max-w-6xl mx-auto flex justify-between items-center'><span class='text-xl font-bold'>YaaiCMS</span><div class='space-x-4'><a href='/' class='hover:text-gray-300'>Home</a><a href='/blog' class='hover:text-gray-300'>Blog</a></div></nav></header>",
			Footer:              "<footer class='bg-gray-800 text-gray-400 p-6 text-center text-sm'>&copy; 2026 YaaiCMS. All rights reserved.</footer>",
			Year:                2026,
			Menus: engine.Menus{
				"main":         {{Label: "Home", URL: "/", Active: true}, {Label: "About", URL: "/about"}, {Label: "Blog", URL: "/blog"}},
				"footer":       {{Label: "About", URL: "/about"}, {Label: "Contact", URL: "/contact"}},
				"footer_legal": {{Label: "Privacy", URL: "/privacy"}, {Label: "Terms", URL: "/terms"}},
			},
		}
	case "article_loop":
		return engine.ListData{
			SiteTitle: "YaaiCMS",
			Slogan:    "Your AI-powered CMS",
			Title:     "Blog",
			Posts: []engine.PostItem{
				{Title: "Getting Started with YaaiCMS", Slug: "getting-started", Excerpt: "Learn how to set up your YaaiCMS CMS and create your first blog post.", FeaturedImageURL: "https://placehold.co/800x450/0f172a/e2e8f0?text=Post+1", FeaturedImageSrcset: "https://placehold.co/640x360/0f172a/e2e8f0?text=640w 640w, https://placehold.co/800x450/0f172a/e2e8f0?text=800w 800w", FeaturedImageAlt: "Getting started guide", PublishedAt: "February 25, 2026"},
				{Title: "Building Modern Websites", Slug: "modern-websites", Excerpt: "Discover the latest techniques for building fast, responsive websites.", FeaturedImageURL: "https://placehold.co/800x450/1e3a5f/e2e8f0?text=Post+2", FeaturedImageSrcset: "https://placehold.co/640x360/1e3a5f/e2e8f0?text=640w 640w, https://placehold.co/800x450/1e3a5f/e2e8f0?text=800w 800w", FeaturedImageAlt: "Modern website design", PublishedAt: "February 24, 2026"},
				{Title: "AI-Powered Content Creation", Slug: "ai-content", Excerpt: "How artificial intelligence is transforming the way we create web content.", FeaturedImageURL: "https://placehold.co/800x450/3b0764/e2e8f0?text=Post+3", FeaturedImageSrcset: "https://placehold.co/640x360/3b0764/e2e8f0?text=640w 640w, https://placehold.co/800x450/3b0764/e2e8f0?text=800w 800w", FeaturedImageAlt: "AI content creation", PublishedAt: "February 23, 2026"},
			},
			Header: "<header class='bg-gray-800 text-white p-4'><nav class='max-w-6xl mx-auto flex justify-between items-center'><span class='text-xl font-bold'>YaaiCMS</span><div class='space-x-4'><a href='/' class='hover:text-gray-300'>Home</a><a href='/blog' class='hover:text-gray-300'>Blog</a></div></nav></header>",
			Footer: "<footer class='bg-gray-800 text-gray-400 p-6 text-center text-sm'>&copy; 2026 YaaiCMS. All rights reserved.</footer>",
			Year:   2026,
			Menus: engine.Menus{
				"main":         {{Label: "Home", URL: "/", Active: true}, {Label: "About", URL: "/about"}, {Label: "Blog", URL: "/blog"}},
				"footer":       {{Label: "About", URL: "/about"}, {Label: "Contact", URL: "/contact"}},
				"footer_legal": {{Label: "Privacy", URL: "/privacy"}, {Label: "Terms", URL: "/terms"}},
			},
		}
	default:
		// Header and footer use FragmentData with sample menus for preview.
		return engine.FragmentData{
			SiteTitle: "YaaiCMS",
			Slogan:    "Your AI-powered CMS",
			Year:      2026,
			Menus: engine.Menus{
				"main": {
					{Label: "Home", URL: "/", Active: true},
					{Label: "About", URL: "/about"},
					{Label: "Blog", URL: "/blog"},
					{Label: "Services", URL: "#", Children: []engine.TemplateMenuItem{
						{Label: "Consulting", URL: "/consulting"},
						{Label: "Development", URL: "/development"},
					}},
					{Label: "Contact", URL: "/contact"},
				},
				"footer": {
					{Label: "About", URL: "/about"},
					{Label: "Blog", URL: "/blog"},
					{Label: "Contact", URL: "/contact"},
				},
				"footer_legal": {
					{Label: "Privacy Policy", URL: "/privacy"},
					{Label: "Terms of Service", URL: "/terms"},
				},
			},
		}
	}
}

// extractHTMLFromResponse strips markdown code fences and other non-HTML
// content from the AI's response, returning clean HTML.
func extractHTMLFromResponse(response string) string {
	response = strings.TrimSpace(response)

	// Remove markdown code fences: ```html ... ``` or ``` ... ```
	if strings.HasPrefix(response, "```") {
		// Find the end of the opening fence line.
		firstNewline := strings.Index(response, "\n")
		if firstNewline != -1 {
			response = response[firstNewline+1:]
		}
		// Remove the closing fence.
		if idx := strings.LastIndex(response, "```"); idx != -1 {
			response = response[:idx]
		}
	}

	return strings.TrimSpace(response)
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// quoteJSString produces a JavaScript string literal safe for embedding in
// HTML attributes (e.g., onclick). Escapes backslashes, quotes, newlines,
// and HTML-significant characters using JS hex escapes to prevent XSS.
func quoteJSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `"`, `\x22`)
	s = strings.ReplaceAll(s, "<", `\x3c`)
	s = strings.ReplaceAll(s, ">", `\x3e`)
	s = strings.ReplaceAll(s, "&", `\x26`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", ``)
	return `'` + s + `'`
}
