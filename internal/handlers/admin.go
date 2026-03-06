// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package handlers contains the HTTP handlers for the YaaiCMS CMS.
// Handlers are grouped by concern (admin, public, auth) and receive
// their dependencies through the handler struct.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/google/uuid"

	"yaaicms/internal/ai"
	"yaaicms/internal/cache"
	"yaaicms/internal/engine"
	"yaaicms/internal/middleware"
	"yaaicms/internal/models"
	"yaaicms/internal/render"
	"yaaicms/internal/session"
	"yaaicms/internal/slug"
	"yaaicms/internal/storage"
	"yaaicms/internal/store"
)

// AIProviderInfo holds display information about a configured AI provider.
// Used by the Settings page to show which providers are available.
type AIProviderInfo struct {
	Name      string // "openai", "gemini", "claude", "mistral"
	Label     string // Human-friendly label
	HasKey    bool   // Whether an API key is configured
	Active    bool   // Whether this is the currently active provider
	Model     string // Configured model name
	KeyEnvVar string // Environment variable name for the key
}

// AIConfig holds the AI provider configuration visible to admin handlers.
// Intentionally excludes actual API keys — only exposes what the UI needs.
type AIConfig struct {
	ActiveProvider string
	Providers      []AIProviderInfo
}

// Admin groups all admin panel HTTP handlers and their dependencies.
type Admin struct {
	renderer              *render.Renderer
	sessions              *session.Store
	contentStore          *store.ContentStore
	userStore             *store.UserStore
	templateStore         *store.TemplateStore
	mediaStore            *store.MediaStore
	variantStore          *store.VariantStore
	revisionStore         *store.RevisionStore
	templateRevisionStore *store.TemplateRevisionStore
	themeStore            *store.DesignThemeStore
	siteSettingStore      *store.SiteSettingStore
	categoryStore         *store.CategoryStore
	menuStore             *store.MenuStore
	storageClient         *storage.Client
	engine                *engine.Engine
	pageCache             *cache.PageCache
	cacheLog              *store.CacheLogStore
	aiRegistry            *ai.Registry
	aiConfig              *AIConfig
}

// NewAdmin creates a new Admin handler group with the given dependencies.
// storageClient, mediaStore, and variantStore may be nil if S3 is not configured.
func NewAdmin(renderer *render.Renderer, sessions *session.Store, contentStore *store.ContentStore, userStore *store.UserStore, templateStore *store.TemplateStore, mediaStore *store.MediaStore, variantStore *store.VariantStore, revisionStore *store.RevisionStore, templateRevisionStore *store.TemplateRevisionStore, themeStore *store.DesignThemeStore, siteSettingStore *store.SiteSettingStore, categoryStore *store.CategoryStore, menuStore *store.MenuStore, storageClient *storage.Client, eng *engine.Engine, pageCache *cache.PageCache, cacheLog *store.CacheLogStore, aiRegistry *ai.Registry, aiCfg *AIConfig) *Admin {
	return &Admin{
		renderer:              renderer,
		sessions:              sessions,
		contentStore:          contentStore,
		userStore:             userStore,
		templateStore:         templateStore,
		mediaStore:            mediaStore,
		variantStore:          variantStore,
		revisionStore:         revisionStore,
		templateRevisionStore: templateRevisionStore,
		themeStore:            themeStore,
		siteSettingStore:      siteSettingStore,
		categoryStore:         categoryStore,
		menuStore:             menuStore,
		storageClient:         storageClient,
		engine:                eng,
		pageCache:             pageCache,
		cacheLog:              cacheLog,
		aiRegistry:            aiRegistry,
		aiConfig:              aiCfg,
	}
}

// Dashboard renders the admin dashboard page with real stats.
func (a *Admin) Dashboard(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	postCount, _ := a.contentStore.CountByType(sess.TenantID, models.ContentTypePost)
	pageCount, _ := a.contentStore.CountByType(sess.TenantID, models.ContentTypePage)
	users, _ := a.userStore.ListByTenant(sess.TenantID)
	var mediaCount int
	if a.mediaStore != nil {
		mediaCount, _ = a.mediaStore.Count(sess.TenantID)
	}

	a.renderer.Page(w, r, "dashboard", &render.PageData{
		Title:   "Dashboard",
		Section: "dashboard",
		Data: map[string]any{
			"PostCount":  postCount,
			"PageCount":  pageCount,
			"UserCount":  len(users),
			"MediaCount": mediaCount,
		},
	})
}

// --- Posts CRUD ---

// PostsList renders the posts management page.
func (a *Admin) PostsList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	posts, err := a.contentStore.ListByType(sess.TenantID, models.ContentTypePost)
	if err != nil {
		slog.Error("list posts failed", "error", err)
	}

	a.renderer.Page(w, r, "posts_list", &render.PageData{
		Title:   "Posts",
		Section: "posts",
		Data:    map[string]any{"Items": posts},
	})
}

// PostNew renders the new post form.
func (a *Admin) PostNew(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	categories, _ := a.categoryStore.FlatTree(sess.TenantID)
	a.renderer.Page(w, r, "content_form", &render.PageData{
		Title:   "New Post",
		Section: "posts",
		Data: map[string]any{
			"ContentType": "post",
			"IsNew":       true,
			"Categories":  categories,
		},
	})
}

// PostCreate handles the new post form submission.
func (a *Admin) PostCreate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	a.createContent(w, r, models.ContentTypePost, sess)
}

// PostEdit renders the edit post form.
func (a *Admin) PostEdit(w http.ResponseWriter, r *http.Request) {
	a.editContent(w, r, "posts")
}

// PostUpdate handles the edit post form submission.
func (a *Admin) PostUpdate(w http.ResponseWriter, r *http.Request) {
	a.updateContent(w, r, "posts")
}

// PostDelete handles post deletion.
func (a *Admin) PostDelete(w http.ResponseWriter, r *http.Request) {
	a.deleteContent(w, r, "posts")
}

// --- Pages CRUD ---

// PagesList renders the pages management page.
func (a *Admin) PagesList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	pages, err := a.contentStore.ListByType(sess.TenantID, models.ContentTypePage)
	if err != nil {
		slog.Error("list pages failed", "error", err)
	}

	a.renderer.Page(w, r, "pages_list", &render.PageData{
		Title:   "Pages",
		Section: "pages",
		Data:    map[string]any{"Items": pages},
	})
}

// PageNew renders the new page form.
func (a *Admin) PageNew(w http.ResponseWriter, r *http.Request) {
	a.renderer.Page(w, r, "content_form", &render.PageData{
		Title:   "New Page",
		Section: "pages",
		Data: map[string]any{
			"ContentType": "page",
			"IsNew":       true,
		},
	})
}

// PageCreate handles the new page form submission.
func (a *Admin) PageCreate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	a.createContent(w, r, models.ContentTypePage, sess)
}

// PageEdit renders the edit page form.
func (a *Admin) PageEdit(w http.ResponseWriter, r *http.Request) {
	a.editContent(w, r, "pages")
}

// PageUpdate handles the edit page form submission.
func (a *Admin) PageUpdate(w http.ResponseWriter, r *http.Request) {
	a.updateContent(w, r, "pages")
}

// PageDelete handles page deletion.
func (a *Admin) PageDelete(w http.ResponseWriter, r *http.Request) {
	a.deleteContent(w, r, "pages")
}

// --- Shared content helpers ---

// createContent handles creating a new post or page from the form.
func (a *Admin) createContent(w http.ResponseWriter, r *http.Request, contentType models.ContentType, sess *session.Data) {
	title := r.FormValue("title")
	body := r.FormValue("body")
	status := models.ContentStatus(r.FormValue("status"))
	contentSlug := r.FormValue("slug")
	excerpt := r.FormValue("excerpt")
	metaDesc := r.FormValue("meta_description")
	metaKw := r.FormValue("meta_keywords")
	featuredImageIDStr := r.FormValue("featured_image_id")

	// Validate inputs.
	if errMsg := validateContent(title, contentSlug, body); errMsg != "" {
		section := "posts"
		if contentType == models.ContentTypePage {
			section = "pages"
		}
		a.renderer.Page(w, r, "content_form", &render.PageData{
			Title:   "New " + string(contentType),
			Section: section,
			Data: map[string]any{
				"ContentType": string(contentType),
				"IsNew":       true,
				"Error":       errMsg,
			},
		})
		return
	}
	if errMsg := validateMetadata(excerpt, metaDesc, metaKw); errMsg != "" {
		section := "posts"
		if contentType == models.ContentTypePage {
			section = "pages"
		}
		a.renderer.Page(w, r, "content_form", &render.PageData{
			Title:   "New " + string(contentType),
			Section: section,
			Data: map[string]any{
				"ContentType": string(contentType),
				"IsNew":       true,
				"Error":       errMsg,
			},
		})
		return
	}

	if contentSlug == "" {
		contentSlug = slug.Generate(title)
	}

	if status == "" {
		status = models.ContentStatusDraft
	}

	// Determine body format from the form (Markdown editor sets this).
	bodyFormat := models.BodyFormat(r.FormValue("body_format"))
	if bodyFormat != models.BodyFormatHTML {
		bodyFormat = models.BodyFormatMarkdown // default for new content
	}

	c := &models.Content{
		Type:       contentType,
		Title:      title,
		Slug:       contentSlug,
		Body:       body,
		BodyFormat: bodyFormat,
		Status:     status,
		AuthorID:   sess.UserID,
	}
	if excerpt != "" {
		c.Excerpt = &excerpt
	}
	if metaDesc != "" {
		c.MetaDescription = &metaDesc
	}
	if metaKw != "" {
		c.MetaKeywords = &metaKw
	}
	if fid, err := uuid.Parse(featuredImageIDStr); err == nil {
		c.FeaturedImageID = &fid
	}
	if catID, err := uuid.Parse(r.FormValue("category_id")); err == nil {
		c.CategoryID = &catID
	}

	created, err := a.contentStore.Create(sess.TenantID, c)
	if err != nil {
		slog.Error("create content failed", "error", err, "type", contentType)
		section := "posts"
		if contentType == models.ContentTypePage {
			section = "pages"
		}
		a.renderer.Page(w, r, "content_form", &render.PageData{
			Title:   "New " + string(contentType),
			Section: section,
			Data: map[string]any{
				"ContentType": string(contentType),
				"IsNew":       true,
				"Error":       "Failed to create. The slug may already exist.",
				"Item":        c,
			},
		})
		return
	}

	// Invalidate cache for the new content (homepage may show it in listings).
	a.invalidateContentCache(r.Context(), sess.TenantID, created.ID, created.Slug, "create")

	if contentType == models.ContentTypePage {
		http.Redirect(w, r, "/admin/pages", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/posts", http.StatusSeeOther)
	}
}

// editContent renders the edit form for a content item.
func (a *Admin) editContent(w http.ResponseWriter, r *http.Request, section string) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	item, err := a.contentStore.FindByID(sess.TenantID, id)
	if err != nil || item == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	contentType := "post"
	title := "Edit Post"
	if section == "pages" {
		contentType = "page"
		title = "Edit Page"
	}

	data := map[string]any{
		"ContentType": contentType,
		"IsNew":       false,
		"Item":        item,
	}

	// Resolve featured image URL for display in the form.
	if item.FeaturedImageID != nil && a.mediaStore != nil && a.storageClient != nil {
		if media, err := a.mediaStore.FindByID(sess.TenantID, *item.FeaturedImageID); err == nil && media != nil {
			if media.Bucket == a.storageClient.PublicBucket() {
				data["FeaturedImageURL"] = a.storageClient.FileURL(media.S3Key)
			}
			if media.ThumbS3Key != nil {
				data["FeaturedImageThumbURL"] = a.storageClient.FileURL(*media.ThumbS3Key)
			}
			data["FeaturedImageName"] = media.OriginalName
		}
	}

	// Load revisions for the history panel.
	revisions, err := a.revisionStore.ListByContentID(item.ID)
	if err != nil {
		slog.Error("failed to load revisions", "error", err)
	}
	data["Revisions"] = revisions

	// Load categories for the category selector (posts only).
	if contentType == "post" {
		categories, _ := a.categoryStore.FlatTree(sess.TenantID)
		data["Categories"] = categories
	}

	a.renderer.Page(w, r, "content_form", &render.PageData{
		Title:   title,
		Section: section,
		Data:    data,
	})
}

// updateContent handles the edit form submission for a content item.
// Before applying changes, it snapshots the current state as a revision.
func (a *Admin) updateContent(w http.ResponseWriter, r *http.Request, section string) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	item, err := a.contentStore.FindByID(sess.TenantID, id)
	if err != nil || item == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Capture the old state BEFORE applying form values (for revision snapshot).
	oldTitle := item.Title
	oldBody := item.Body
	oldSlug := item.Slug
	oldExcerpt := item.Excerpt
	oldStatus := string(item.Status)
	oldMetaDesc := item.MetaDescription
	oldMetaKw := item.MetaKeywords
	oldFeaturedImageID := item.FeaturedImageID
	oldCategoryID := item.CategoryID

	title := r.FormValue("title")
	body := r.FormValue("body")
	newSlug := r.FormValue("slug")
	excerpt := r.FormValue("excerpt")
	metaDesc := r.FormValue("meta_description")
	metaKw := r.FormValue("meta_keywords")
	featuredImageIDStr := r.FormValue("featured_image_id")
	revisionMessage := strings.TrimSpace(r.FormValue("revision_message"))

	// Validate inputs.
	if errMsg := validateContent(title, newSlug, body); errMsg != "" {
		a.renderer.Page(w, r, "content_form", &render.PageData{
			Title:   "Edit",
			Section: section,
			Data: map[string]any{
				"ContentType": string(item.Type),
				"IsNew":       false,
				"Item":        item,
				"Error":       errMsg,
			},
		})
		return
	}
	if errMsg := validateMetadata(excerpt, metaDesc, metaKw); errMsg != "" {
		a.renderer.Page(w, r, "content_form", &render.PageData{
			Title:   "Edit",
			Section: section,
			Data: map[string]any{
				"ContentType": string(item.Type),
				"IsNew":       false,
				"Item":        item,
				"Error":       errMsg,
			},
		})
		return
	}

	// Apply new values.
	item.Title = title
	item.Body = body
	item.Status = models.ContentStatus(r.FormValue("status"))
	item.Slug = newSlug

	// Update body format from the form.
	bodyFormat := models.BodyFormat(r.FormValue("body_format"))
	if bodyFormat != models.BodyFormatHTML {
		bodyFormat = models.BodyFormatMarkdown
	}
	oldBodyFormat := item.BodyFormat
	item.BodyFormat = bodyFormat

	if item.Slug == "" {
		item.Slug = slug.Generate(item.Title)
	}

	if excerpt != "" {
		item.Excerpt = &excerpt
	} else {
		item.Excerpt = nil
	}
	if metaDesc != "" {
		item.MetaDescription = &metaDesc
	} else {
		item.MetaDescription = nil
	}
	if metaKw != "" {
		item.MetaKeywords = &metaKw
	} else {
		item.MetaKeywords = nil
	}

	// Update featured image (posts only).
	if fid, err := uuid.Parse(featuredImageIDStr); err == nil {
		item.FeaturedImageID = &fid
	} else {
		item.FeaturedImageID = nil
	}

	// Update category (posts only).
	if catID, err := uuid.Parse(r.FormValue("category_id")); err == nil {
		item.CategoryID = &catID
	} else {
		item.CategoryID = nil
	}

	// Create revision snapshot of the OLD state before persisting changes.
	rev := &models.ContentRevision{
		ContentID:       item.ID,
		Title:           oldTitle,
		Slug:            oldSlug,
		Body:            oldBody,
		BodyFormat:      oldBodyFormat,
		Excerpt:         oldExcerpt,
		Status:          oldStatus,
		MetaDescription: oldMetaDesc,
		MetaKeywords:    oldMetaKw,
		FeaturedImageID: oldFeaturedImageID,
		CategoryID:      oldCategoryID,
		RevisionTitle:   revisionMessage,
		CreatedBy:       sess.UserID,
	}

	created, revErr := a.revisionStore.Create(rev)
	if revErr != nil {
		slog.Error("failed to create revision", "content_id", item.ID, "error", revErr)
		// Non-fatal: proceed with the update even if revision creation fails.
	}

	if err := a.contentStore.Update(item); err != nil {
		slog.Error("update content failed", "error", err)
		a.renderer.Page(w, r, "content_form", &render.PageData{
			Title:   "Edit",
			Section: section,
			Data: map[string]any{
				"ContentType": string(item.Type),
				"IsNew":       false,
				"Item":        item,
				"Error":       "Failed to update. The slug may already exist.",
			},
		})
		return
	}

	// Generate AI revision title + changelog in the background.
	if created != nil {
		//nolint:gosec,contextcheck // G118: intentionally uses context.Background — goroutine outlives the HTTP request.
		go a.generateRevisionMeta(created.ID, rev, item, revisionMessage, a.tenantAIProvider(r))
	}

	a.invalidateContentCache(r.Context(), sess.TenantID, item.ID, item.Slug, "update")
	http.Redirect(w, r, "/admin/"+section, http.StatusSeeOther)
}

// generateRevisionMeta uses AI to create a short title and changelog for a
// revision, comparing the old state (rev) with the new state (updated item).
// Runs in a background goroutine — errors are logged but don't affect the user.
func (a *Admin) generateRevisionMeta(revID uuid.UUID, old *models.ContentRevision, updated *models.Content, userMessage, providerName string) {
	// Build a concise diff summary for the AI.
	var changes []string
	if old.Title != updated.Title {
		changes = append(changes, fmt.Sprintf("Title: %q -> %q", truncateStr(old.Title, 80), truncateStr(updated.Title, 80)))
	}
	if old.Body != updated.Body {
		oldLen := len(old.Body)
		newLen := len(updated.Body)
		changes = append(changes, fmt.Sprintf("Body: changed (%d -> %d chars)", oldLen, newLen))
	}
	if old.Slug != updated.Slug {
		changes = append(changes, fmt.Sprintf("Slug: %q -> %q", old.Slug, updated.Slug))
	}
	if old.Status != string(updated.Status) {
		changes = append(changes, fmt.Sprintf("Status: %s -> %s", old.Status, string(updated.Status)))
	}
	if ptrStr(old.Excerpt) != ptrStr(updated.Excerpt) {
		changes = append(changes, "Excerpt: updated")
	}
	if ptrStr(old.MetaDescription) != ptrStr(updated.MetaDescription) {
		changes = append(changes, "Meta description: updated")
	}
	if ptrStr(old.MetaKeywords) != ptrStr(updated.MetaKeywords) {
		changes = append(changes, "Meta keywords: updated")
	}
	if ptrUUID(old.FeaturedImageID) != ptrUUID(updated.FeaturedImageID) {
		changes = append(changes, "Featured image: changed")
	}

	if len(changes) == 0 {
		changes = append(changes, "No visible changes")
	}

	diffSummary := strings.Join(changes, "\n")

	// Generate revision title if the user didn't provide one.
	ctx := context.Background()
	revTitle := userMessage
	if revTitle == "" {
		prompt := fmt.Sprintf("Changes made:\n%s\n\nOld title: %q\nNew title: %q",
			diffSummary, truncateStr(old.Title, 100), truncateStr(updated.Title, 100))

		systemPrompt := `You are a version control assistant. Generate a very short revision title
(max 60 characters) that summarizes the changes made, like a git commit message.
Output ONLY the title text, nothing else. Use imperative mood (e.g. "Update title and body content").`

		result, err := a.aiRegistry.GenerateForTaskAs(ctx, providerName, ai.TaskLight, systemPrompt, prompt)
		if err != nil {
			slog.Warn("ai revision title failed", "error", err)
			revTitle = "Content updated"
		} else {
			revTitle = strings.TrimSpace(result)
			revTitle = strings.Trim(revTitle, `"'`)
			if len(revTitle) > 80 {
				revTitle = revTitle[:77] + "..."
			}
		}
	}

	// Generate a changelog describing what changed.
	changelogPrompt := fmt.Sprintf("Changes:\n%s", diffSummary)

	changelogSystem := `You are a version control assistant. Generate a brief changelog (2-4 bullet points)
describing what changed in this content revision. Each bullet should start with "- ".
Be concise and factual. Output ONLY the bullet points, nothing else.`

	changelog, err := a.aiRegistry.GenerateForTaskAs(ctx, providerName, ai.TaskLight, changelogSystem, changelogPrompt)
	if err != nil {
		slog.Warn("ai revision changelog failed", "error", err)
		changelog = diffSummary
	} else {
		changelog = strings.TrimSpace(changelog)
	}

	if err := a.revisionStore.UpdateMeta(revID, revTitle, changelog); err != nil {
		slog.Error("failed to update revision meta", "id", revID, "error", err)
	}
}

// RevisionRestore restores a content item to the state captured in a revision.
// Returns an HTML fragment that triggers a page redirect via HTMX.
func (a *Admin) RevisionRestore(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	revIDStr := chi.URLParam(r, "revisionID")
	revID, err := uuid.Parse(revIDStr)
	if err != nil {
		http.Error(w, "Invalid revision ID", http.StatusBadRequest)
		return
	}

	rev, err := a.revisionStore.FindByID(revID)
	if err != nil || rev == nil {
		http.Error(w, "Revision not found", http.StatusNotFound)
		return
	}

	// Load the content item to check it exists and to create a "pre-restore" revision.
	item, err := a.contentStore.FindByID(sess.TenantID, rev.ContentID)
	if err != nil || item == nil {
		http.Error(w, "Content not found", http.StatusNotFound)
		return
	}

	// Create a revision of the current state before restoring.
	preRestore := &models.ContentRevision{
		ContentID:       item.ID,
		Title:           item.Title,
		Slug:            item.Slug,
		Body:            item.Body,
		BodyFormat:      item.BodyFormat,
		Excerpt:         item.Excerpt,
		Status:          string(item.Status),
		MetaDescription: item.MetaDescription,
		MetaKeywords:    item.MetaKeywords,
		FeaturedImageID: item.FeaturedImageID,
		CategoryID:      item.CategoryID,
		RevisionTitle:   "Before restore",
		RevisionLog:     fmt.Sprintf("- State before restoring to revision from %s", rev.CreatedAt.Format("Jan 2, 2006 15:04")),
		CreatedBy:       sess.UserID,
	}
	if _, err := a.revisionStore.Create(preRestore); err != nil {
		slog.Error("failed to create pre-restore revision", "error", err)
	}

	// Apply the revision data to the content item.
	item.Title = rev.Title
	item.Slug = rev.Slug
	item.Body = rev.Body
	item.BodyFormat = rev.BodyFormat
	item.Excerpt = rev.Excerpt
	item.Status = models.ContentStatus(rev.Status)
	item.MetaDescription = rev.MetaDescription
	item.MetaKeywords = rev.MetaKeywords
	item.FeaturedImageID = rev.FeaturedImageID
	item.CategoryID = rev.CategoryID

	if err := a.contentStore.Update(item); err != nil {
		slog.Error("restore revision failed", "error", err)
		http.Error(w, "Failed to restore revision", http.StatusInternalServerError)
		return
	}

	a.invalidateContentCache(r.Context(), sess.TenantID, item.ID, item.Slug, "restore")

	// Determine section for redirect.
	section := "posts"
	if item.Type == models.ContentTypePage {
		section = "pages"
	}
	redirectURL := fmt.Sprintf("/admin/%s/%s", section, item.ID)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// RevisionUpdateTitle updates a revision's user-provided title.
func (a *Admin) RevisionUpdateTitle(w http.ResponseWriter, r *http.Request) {
	revIDStr := chi.URLParam(r, "revisionID")
	revID, err := uuid.Parse(revIDStr)
	if err != nil {
		http.Error(w, "Invalid revision ID", http.StatusBadRequest)
		return
	}

	rev, err := a.revisionStore.FindByID(revID)
	if err != nil || rev == nil {
		http.Error(w, "Revision not found", http.StatusNotFound)
		return
	}

	newTitle := strings.TrimSpace(r.FormValue("revision_title"))
	if newTitle == "" {
		writeAIError(w, "Title cannot be empty.")
		return
	}

	if err := a.revisionStore.UpdateMeta(revID, newTitle, rev.RevisionLog); err != nil {
		slog.Error("failed to update revision title", "error", err)
		writeAIError(w, "Failed to update revision title.")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<span class="text-xs font-medium text-gray-900">%s</span>`, html.EscapeString(newTitle))
}

// ptrStr safely dereferences a *string pointer, returning "" if nil.
func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ptrUUID safely dereferences a *uuid.UUID pointer, returning "" if nil.
func ptrUUID(u *uuid.UUID) string {
	if u == nil {
		return ""
	}
	return u.String()
}

// truncateStr cuts a string to maxLen, appending "..." if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// deleteContent handles content deletion.
func (a *Admin) deleteContent(w http.ResponseWriter, r *http.Request, section string) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Look up the slug before deleting so we can invalidate its cache entry.
	item, _ := a.contentStore.FindByID(sess.TenantID, id)

	if err := a.contentStore.Delete(sess.TenantID, id); err != nil {
		slog.Error("delete content failed", "error", err)
	} else if item != nil {
		a.invalidateContentCache(r.Context(), sess.TenantID, id, item.Slug, "delete")
	}

	http.Redirect(w, r, "/admin/"+section, http.StatusSeeOther)
}

// --- Template management ---

// templateGroup holds templates sharing the same name prefix (theme).
// Templates named "Theme — Header" share prefix "Theme".
// Templates without " — " go into a group with their full name as prefix.
type templateGroup struct {
	Prefix    string
	Templates []models.Template
	AllActive bool // true when every template in the group is active
}

// TemplatesList renders the templates management page with real data.
// Templates are grouped by name prefix (text before " — ") so that
// theme sets from Restyle All appear together with bulk activation.
func (a *Admin) TemplatesList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	templates, err := a.templateStore.List(sess.TenantID)
	if err != nil {
		slog.Error("list templates failed", "error", err)
	}

	// Group templates by prefix.
	var groups []templateGroup
	groupIndex := make(map[string]int)
	for _, t := range templates {
		prefix := t.Name
		if idx := strings.Index(t.Name, " \u2014 "); idx > 0 {
			prefix = t.Name[:idx]
		}
		if i, ok := groupIndex[prefix]; ok {
			groups[i].Templates = append(groups[i].Templates, t)
		} else {
			groupIndex[prefix] = len(groups)
			groups = append(groups, templateGroup{Prefix: prefix, Templates: []models.Template{t}})
		}
	}

	// Mark groups where every template is already active.
	for i := range groups {
		allActive := true
		for _, t := range groups[i].Templates {
			if !t.IsActive {
				allActive = false
				break
			}
		}
		groups[i].AllActive = allActive
	}

	a.renderer.Page(w, r, "templates_list", &render.PageData{
		Title:   "AI Design",
		Section: "templates",
		Data:    map[string]any{"Groups": groups},
	})
}

// TemplateNew renders the new template form.
func (a *Admin) TemplateNew(w http.ResponseWriter, r *http.Request) {
	a.renderer.Page(w, r, "template_form", &render.PageData{
		Title:   "New Template",
		Section: "templates",
		Data:    map[string]any{"IsNew": true},
	})
}

// TemplateCreate handles the new template form submission.
func (a *Admin) TemplateCreate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	tmplType := models.TemplateType(r.FormValue("type"))
	htmlContent := r.FormValue("html_content")

	// Validate input lengths.
	if errMsg := validateTemplate(name, htmlContent); errMsg != "" {
		a.renderer.Page(w, r, "template_form", &render.PageData{
			Title:   "New Template",
			Section: "templates",
			Data: map[string]any{
				"IsNew": true,
				"Error": errMsg,
				"Item":  &models.Template{Name: name, Type: tmplType, HTMLContent: htmlContent},
			},
		})
		return
	}

	// Validate the template syntax before saving.
	if err := a.engine.ValidateTemplate(htmlContent); err != nil {
		a.renderer.Page(w, r, "template_form", &render.PageData{
			Title:   "New Template",
			Section: "templates",
			Data: map[string]any{
				"IsNew": true,
				"Error": "Template syntax error: " + err.Error(),
				"Item":  &models.Template{Name: name, Type: tmplType, HTMLContent: htmlContent},
			},
		})
		return
	}

	t := &models.Template{
		Name:        name,
		Type:        tmplType,
		HTMLContent: htmlContent,
	}

	sess := middleware.SessionFromCtx(r.Context())
	created, err := a.templateStore.Create(sess.TenantID, t)
	if err != nil {
		slog.Error("create template failed", "error", err)
		a.renderer.Page(w, r, "template_form", &render.PageData{
			Title:   "New Template",
			Section: "templates",
			Data: map[string]any{
				"IsNew": true,
				"Error": "Failed to create template.",
				"Item":  t,
			},
		})
		return
	}

	// New templates aren't active yet, but log the event for auditing.
	a.cacheLog.Log(sess.TenantID, "template", created.ID, "create")
	http.Redirect(w, r, "/admin/templates", http.StatusSeeOther)
}

// TemplateEdit renders the edit template form.
func (a *Admin) TemplateEdit(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	item, err := a.templateStore.FindByID(sess.TenantID, id)
	if err != nil || item == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Load revision history for the template.
	var revisions []*models.TemplateRevision
	if a.templateRevisionStore != nil {
		revisions, err = a.templateRevisionStore.ListByTemplateID(item.ID)
		if err != nil {
			slog.Error("failed to load template revisions", "error", err)
		}
	}

	a.renderer.Page(w, r, "template_form", &render.PageData{
		Title:   "Edit Template",
		Section: "templates",
		Data: map[string]any{
			"IsNew":     false,
			"Item":      item,
			"Revisions": revisions,
		},
	})
}

// TemplateUpdate handles the edit template form submission.
func (a *Admin) TemplateUpdate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	item, err := a.templateStore.FindByID(sess.TenantID, id)
	if err != nil || item == nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	// Capture old state before applying updates for the revision snapshot.
	oldName := item.Name
	oldHTML := item.HTMLContent

	newName := r.FormValue("name")
	htmlContent := r.FormValue("html_content")
	revisionMessage := strings.TrimSpace(r.FormValue("revision_message"))

	// Validate syntax.
	if err := a.engine.ValidateTemplate(htmlContent); err != nil {
		a.renderer.Page(w, r, "template_form", &render.PageData{
			Title:   "Edit Template",
			Section: "templates",
			Data: map[string]any{
				"IsNew": false,
				"Error": "Template syntax error: " + err.Error(),
				"Item":  item,
			},
		})
		return
	}

	// Create a revision snapshot of the old state before persisting the update.
	if a.templateRevisionStore != nil {
		rev := &models.TemplateRevision{
			TemplateID:    item.ID,
			Name:          oldName,
			HTMLContent:   oldHTML,
			RevisionTitle: revisionMessage,
			CreatedBy:     sess.UserID,
		}
		created, revErr := a.templateRevisionStore.Create(rev)
		if revErr != nil {
			slog.Error("failed to create template revision", "error", revErr)
		} else {
			// Generate AI revision metadata in background.
			//nolint:gosec,contextcheck // G118: intentionally uses context.Background — goroutine outlives the HTTP request.
			go a.generateTemplateRevisionMeta(created.ID, oldName, oldHTML, newName, htmlContent, revisionMessage, a.tenantAIProvider(r))
		}
	}

	item.Name = newName
	item.HTMLContent = htmlContent
	if err := a.templateStore.Update(item); err != nil {
		slog.Error("update template failed", "error", err)
	} else {
		// Template content changed — invalidate L1 (compiled) and L2 (rendered pages).
		a.invalidateTemplateCache(r.Context(), sess.TenantID, item.ID, "update")
	}

	http.Redirect(w, r, "/admin/templates/"+item.ID.String(), http.StatusSeeOther)
}

// TemplateActivate sets a template as active for its type.
func (a *Admin) TemplateActivate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := a.templateStore.Activate(sess.TenantID, id); err != nil {
		slog.Error("activate template failed", "error", err)
	} else {
		// Activation changes which template renders for a type — clear everything.
		a.invalidateAllTemplateCache(r.Context(), sess.TenantID, id, "update")
	}

	http.Redirect(w, r, "/admin/templates", http.StatusSeeOther)
}

// TemplateDelete handles template deletion.
func (a *Admin) TemplateDelete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := a.templateStore.Delete(sess.TenantID, id); err != nil {
		slog.Error("delete template failed", "error", err)
	} else {
		a.invalidateTemplateCache(r.Context(), sess.TenantID, id, "delete")
	}

	http.Redirect(w, r, "/admin/templates", http.StatusSeeOther)
}

// TemplatePreview renders a preview of a template with data. Accepts optional
// "template_type" (page, article_loop, header, footer) and "content_id" params
// to render with type-appropriate structure and real content data.
func (a *Admin) TemplatePreview(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	htmlContent := r.FormValue("html_content")
	if htmlContent == "" {
		http.Error(w, "No template content", http.StatusBadRequest)
		return
	}

	tmplType := r.FormValue("template_type")
	contentID := r.FormValue("content_id")

	// Build preview data: use real content if a content_id was provided,
	// then type-specific dummy data, then generic page data as fallback.
	var data any
	if contentID != "" {
		if tmplType == "" {
			tmplType = "page"
		}
		data = a.buildRealPreviewData(sess.TenantID, tmplType, contentID)
	}
	if data == nil {
		if tmplType != "" {
			data = buildPreviewData(tmplType)
		} else {
			data = buildPreviewData("page")
		}
	}

	result, err := a.engine.ValidateAndRender(htmlContent, data)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		safeErr := html.EscapeString(err.Error())
		_, _ = w.Write([]byte(`<div class="p-4 bg-red-50 border border-red-200 rounded text-red-800 text-sm">Template error: ` + safeErr + `</div>`))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(result)
}

// TemplateRevisionRestore restores a template to the state captured in a revision.
func (a *Admin) TemplateRevisionRestore(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	revIDStr := chi.URLParam(r, "revisionID")
	revID, err := uuid.Parse(revIDStr)
	if err != nil {
		http.Error(w, "Invalid revision ID", http.StatusBadRequest)
		return
	}

	rev, err := a.templateRevisionStore.FindByID(sess.TenantID, revID)
	if err != nil || rev == nil {
		http.Error(w, "Revision not found", http.StatusNotFound)
		return
	}

	// Load the template to check it exists and to create a "pre-restore" snapshot.
	item, err := a.templateStore.FindByID(sess.TenantID, rev.TemplateID)
	if err != nil || item == nil {
		http.Error(w, "Template not found", http.StatusNotFound)
		return
	}

	// Create a revision of the current state before restoring.
	preRestore := &models.TemplateRevision{
		TemplateID:    item.ID,
		Name:          item.Name,
		HTMLContent:   item.HTMLContent,
		RevisionTitle: "Before restore",
		RevisionLog:   fmt.Sprintf("- State before restoring to revision from %s", rev.CreatedAt.Format("Jan 2, 2006 15:04")),
		CreatedBy:     sess.UserID,
	}
	if _, err := a.templateRevisionStore.Create(preRestore); err != nil {
		slog.Error("failed to create pre-restore template revision", "error", err)
	}

	// Apply the revision data to the template.
	item.Name = rev.Name
	item.HTMLContent = rev.HTMLContent
	if err := a.templateStore.Update(item); err != nil {
		slog.Error("restore template revision failed", "error", err)
		http.Error(w, "Failed to restore revision", http.StatusInternalServerError)
		return
	}

	a.invalidateTemplateCache(r.Context(), sess.TenantID, item.ID, "restore")

	redirectURL := fmt.Sprintf("/admin/templates/%s", item.ID)
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// TemplateRevisionUpdateTitle updates a template revision's user-provided title.
func (a *Admin) TemplateRevisionUpdateTitle(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	revIDStr := chi.URLParam(r, "revisionID")
	revID, err := uuid.Parse(revIDStr)
	if err != nil {
		http.Error(w, "Invalid revision ID", http.StatusBadRequest)
		return
	}

	rev, err := a.templateRevisionStore.FindByID(sess.TenantID, revID)
	if err != nil || rev == nil {
		http.Error(w, "Revision not found", http.StatusNotFound)
		return
	}

	newTitle := strings.TrimSpace(r.FormValue("revision_title"))
	if newTitle == "" {
		writeAIError(w, "Title cannot be empty.")
		return
	}

	if err := a.templateRevisionStore.UpdateMeta(revID, newTitle, rev.RevisionLog); err != nil {
		slog.Error("failed to update template revision title", "error", err)
		writeAIError(w, "Failed to update revision title.")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<span class="text-xs font-medium text-gray-900">%s</span>`, html.EscapeString(newTitle))
}

// generateTemplateRevisionMeta generates AI-powered revision title and changelog
// for a template revision, running in the background.
func (a *Admin) generateTemplateRevisionMeta(revID uuid.UUID, oldName, oldHTML, newName, newHTML, userMessage, providerName string) {
	// Build a concise diff summary.
	var changes []string
	if oldName != newName {
		changes = append(changes, fmt.Sprintf("Name: %q -> %q", truncateStr(oldName, 80), truncateStr(newName, 80)))
	}
	if oldHTML != newHTML {
		oldLen := len(oldHTML)
		newLen := len(newHTML)
		changes = append(changes, fmt.Sprintf("HTML content: changed (%d -> %d chars)", oldLen, newLen))
	}

	if len(changes) == 0 {
		changes = append(changes, "No visible changes")
	}

	diffSummary := strings.Join(changes, "\n")

	ctx := context.Background()
	revTitle := userMessage
	if revTitle == "" {
		prompt := fmt.Sprintf("Changes made to a template:\n%s\n\nOld name: %q\nNew name: %q",
			diffSummary, truncateStr(oldName, 100), truncateStr(newName, 100))

		systemPrompt := `You are a version control assistant. Generate a very short revision title
(max 60 characters) that summarizes the template changes, like a git commit message.
Output ONLY the title text, nothing else. Use imperative mood (e.g. "Restyle header with dark nav bar").`

		result, err := a.aiRegistry.GenerateForTaskAs(ctx, providerName, ai.TaskLight, systemPrompt, prompt)
		if err != nil {
			slog.Warn("ai template revision title failed", "error", err)
			revTitle = "Template updated"
		} else {
			revTitle = strings.TrimSpace(result)
			revTitle = strings.Trim(revTitle, `"'`)
			if len(revTitle) > 80 {
				revTitle = revTitle[:77] + "..."
			}
		}
	}

	// Generate a changelog.
	changelogPrompt := fmt.Sprintf("Changes to a CMS template:\n%s", diffSummary)
	changelogSystem := `You are a version control assistant. Generate a brief changelog (2-4 bullet points)
describing what changed in this template revision. Each bullet should start with "- ".
Be concise and factual. Output ONLY the bullet points, nothing else.`

	changelog, err := a.aiRegistry.GenerateForTaskAs(ctx, providerName, ai.TaskLight, changelogSystem, changelogPrompt)
	if err != nil {
		slog.Warn("ai template revision changelog failed", "error", err)
		changelog = diffSummary
	} else {
		changelog = strings.TrimSpace(changelog)
	}

	if err := a.templateRevisionStore.UpdateMeta(revID, revTitle, changelog); err != nil {
		slog.Error("failed to update template revision meta", "id", revID, "error", err)
	}
}

// UsersList renders the user management page with real data.
func (a *Admin) UsersList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	users, err := a.userStore.ListByTenant(sess.TenantID)
	if err != nil {
		slog.Error("list users failed", "error", err)
	}

	a.renderer.Page(w, r, "users_list", &render.PageData{
		Title:   "Users",
		Section: "users",
		Data:    map[string]any{"Users": users},
	})
}

// UserResetTwoFA resets another user's 2FA, forcing re-setup on next login.
func (a *Admin) UserResetTwoFA(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	idStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// Cannot reset your own 2FA.
	if targetID == sess.UserID {
		http.Error(w, "Cannot reset your own 2FA", http.StatusForbidden)
		return
	}

	if err := a.userStore.ResetTOTP(targetID); err != nil {
		slog.Error("reset 2fa failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	slog.Info("2fa reset by admin", "admin", sess.Email, "target_user", targetID)
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// UserNew renders the new user creation form.
func (a *Admin) UserNew(w http.ResponseWriter, r *http.Request) {
	a.renderer.Page(w, r, "user_form", &render.PageData{
		Title:   "New User",
		Section: "users",
		Data:    map[string]any{},
	})
}

// UserCreate handles the new user form submission.
func (a *Admin) UserCreate(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	password := r.FormValue("password")
	role := models.Role(r.FormValue("role"))

	// Validate inputs.
	var errMsg string
	switch {
	case email == "":
		errMsg = "Email is required."
	case displayName == "":
		errMsg = "Display name is required."
	case len(password) < 8:
		errMsg = "Password must be at least 8 characters."
	case role != models.RoleAdmin && role != models.RoleEditor && role != models.RoleAuthor:
		errMsg = "Invalid role."
	}

	if errMsg != "" {
		a.renderer.Page(w, r, "user_form", &render.PageData{
			Title:   "New User",
			Section: "users",
			Data: map[string]any{
				"Error":       errMsg,
				"Email":       email,
				"DisplayName": displayName,
				"Role":        string(role),
			},
		})
		return
	}

	// Check for duplicate email.
	existing, _ := a.userStore.FindByEmail(email)
	if existing != nil {
		a.renderer.Page(w, r, "user_form", &render.PageData{
			Title:   "New User",
			Section: "users",
			Data: map[string]any{
				"Error":       "A user with this email already exists.",
				"Email":       email,
				"DisplayName": displayName,
				"Role":        string(role),
			},
		})
		return
	}

	sess := middleware.SessionFromCtx(r.Context())

	user, err := a.userStore.Create(email, password, displayName)
	if err != nil {
		slog.Error("create user failed", "error", err)
		a.renderer.Page(w, r, "user_form", &render.PageData{
			Title:   "New User",
			Section: "users",
			Data: map[string]any{
				"Error":       "Failed to create user.",
				"Email":       email,
				"DisplayName": displayName,
				"Role":        string(role),
			},
		})
		return
	}

	if err := a.userStore.AddToTenant(user.ID, sess.TenantID, role); err != nil {
		slog.Error("add user to tenant failed", "error", err, "user_id", user.ID, "tenant_id", sess.TenantID)
	}

	slog.Info("user created", "admin", sess.Email, "new_user", email, "role", role)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/admin/users")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

// --- Cache invalidation helpers ---

// invalidateContentCache purges the L2 page cache for a content item and
// logs the event. Always invalidates the homepage too since post listings
// or the "home" page might have changed.
func (a *Admin) invalidateContentCache(ctx context.Context, tenantID uuid.UUID, contentID uuid.UUID, contentSlug, action string) {
	a.pageCache.InvalidatePage(ctx, cache.SlugKey(tenantID.String(), contentSlug))
	a.pageCache.InvalidateHomepage(ctx)
	a.cacheLog.Log(tenantID, "content", contentID, action)
}

// invalidateTemplateCache purges both L1 (compiled template) and L2 (all
// rendered pages) caches, since any template change can affect any page.
func (a *Admin) invalidateTemplateCache(ctx context.Context, tenantID uuid.UUID, templateID uuid.UUID, action string) {
	a.engine.InvalidateTemplate(templateID.String())
	a.pageCache.InvalidateAll(ctx)
	a.cacheLog.Log(tenantID, "template", templateID, action)
}

// invalidateAllTemplateCache clears the entire L1 cache and all L2 pages.
// Used for template activation which changes the active template for a type.
func (a *Admin) invalidateAllTemplateCache(ctx context.Context, tenantID uuid.UUID, templateID uuid.UUID, action string) {
	a.engine.InvalidateAllTemplates()
	a.pageCache.InvalidateAll(ctx)
	a.cacheLog.Log(tenantID, "template", templateID, action)
}

// SettingsPage renders the settings page with site configuration and AI provider info.
func (a *Admin) SettingsPage(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	settings, err := a.siteSettingStore.All(sess.TenantID)
	if err != nil {
		slog.Error("failed to load site settings", "error", err)
		settings = make(models.SiteSettings)
	}

	a.renderer.Page(w, r, "settings", &render.PageData{
		Title:   "Settings",
		Section: "settings",
		Data: map[string]any{
			"Providers": a.providerInfoForTenant(r),
			"Settings":  settings,
		},
	})
}

// SettingsSave handles POST /admin/settings to update site configuration.
func (a *Admin) SettingsSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	updates := map[string]string{
		"site_title":        r.FormValue("site_title"),
		"site_tagline":      r.FormValue("site_tagline"),
		"timezone":          r.FormValue("timezone"),
		"language":          r.FormValue("language"),
		"date_format":       r.FormValue("date_format"),
		"posts_per_page":    r.FormValue("posts_per_page"),
		"og_default_image":  r.FormValue("og_default_image"),
		"twitter_site":      r.FormValue("twitter_site"),
	}

	sess := middleware.SessionFromCtx(r.Context())
	if err := a.siteSettingStore.SetMany(sess.TenantID, updates); err != nil {
		slog.Error("failed to save site settings", "error", err)
		http.Error(w, "failed to save settings", http.StatusInternalServerError)
		return
	}

	slog.Info("site settings updated")

	// Reload the page to show saved values.
	if r.Header.Get("HX-Request") == "true" {
		a.SettingsPage(w, r)
		return
	}
	http.Redirect(w, r, "/admin/settings", http.StatusSeeOther)
}

// --- Help ---

// HelpPage renders the built-in help documentation page.
func (a *Admin) HelpPage(w http.ResponseWriter, r *http.Request) {
	a.renderer.Page(w, r, "help", &render.PageData{
		Title:   "Help",
		Section: "help",
	})
}

// --- Categories ---

// CategoriesList renders the category manager page.
func (a *Admin) CategoriesList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	tree, err := a.categoryStore.Tree(sess.TenantID)
	if err != nil {
		slog.Error("list categories failed", "error", err)
	}

	a.renderer.Page(w, r, "categories", &render.PageData{
		Title:   "Categories",
		Section: "categories",
		Data:    map[string]any{"Tree": tree},
	})
}

// CategoryCreate handles creating a new category.
func (a *Admin) CategoryCreate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))
	catSlug := strings.TrimSpace(r.FormValue("slug"))
	description := strings.TrimSpace(r.FormValue("description"))
	parentIDStr := r.FormValue("parent_id")

	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if catSlug == "" {
		catSlug = slug.Generate(name)
	}

	cat := &models.Category{
		Name:        name,
		Slug:        catSlug,
		Description: description,
	}

	if pid, err := uuid.Parse(parentIDStr); err == nil {
		cat.ParentID = &pid
	}

	nextOrder, _ := a.categoryStore.NextSortOrder(sess.TenantID, cat.ParentID)
	cat.SortOrder = nextOrder

	if _, err := a.categoryStore.Create(sess.TenantID, cat); err != nil {
		slog.Error("create category failed", "error", err)
		http.Error(w, "Failed to create category. Slug may already exist.", http.StatusConflict)
		return
	}

	// Return the full category list for HTMX swap.
	a.CategoriesList(w, r)
}

// CategoryUpdate handles updating an existing category.
func (a *Admin) CategoryUpdate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	cat, err := a.categoryStore.FindByID(sess.TenantID, id)
	if err != nil || cat == nil {
		http.Error(w, "Category not found", http.StatusNotFound)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	cat.Name = name
	cat.Description = strings.TrimSpace(r.FormValue("description"))

	newSlug := strings.TrimSpace(r.FormValue("slug"))
	if newSlug != "" {
		cat.Slug = newSlug
	}

	if err := a.categoryStore.Update(cat); err != nil {
		slog.Error("update category failed", "error", err)
		http.Error(w, "Failed to update category", http.StatusInternalServerError)
		return
	}

	a.CategoriesList(w, r)
}

// CategoryDelete handles deleting a category.
func (a *Admin) CategoryDelete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := a.categoryStore.Delete(sess.TenantID, id); err != nil {
		slog.Error("delete category failed", "error", err)
		http.Error(w, "Failed to delete category", http.StatusInternalServerError)
		return
	}

	a.CategoriesList(w, r)
}

// CategoryReorder handles the drag & drop reorder request (JSON body).
func (a *Admin) CategoryReorder(w http.ResponseWriter, r *http.Request) {
	var items []store.ReorderItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := a.categoryStore.Reorder(items); err != nil {
		slog.Error("reorder categories failed", "error", err)
		http.Error(w, "Failed to reorder", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}
