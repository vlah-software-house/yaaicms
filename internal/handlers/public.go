// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"yaaicms/internal/cache"
	"yaaicms/internal/engine"
	"yaaicms/internal/middleware"
	"yaaicms/internal/models"
	"yaaicms/internal/storage"
	"yaaicms/internal/store"
)

// Public groups handlers for the public-facing site rendered by the
// dynamic template engine. It checks the L2 Valkey page cache before
// invoking the template engine, and stores rendered results on miss.
type Public struct {
	engine           *engine.Engine
	contentStore     *store.ContentStore
	siteSettingStore *store.SiteSettingStore
	menuStore        *store.MenuStore
	mediaStore       *store.MediaStore
	variantStore     *store.VariantStore
	userProfileStore *store.UserProfileStore
	storageClient    *storage.Client
	pageCache        *cache.PageCache
	domainResolver   middleware.DomainResolver
	baseDomain       string
}

// NewPublic creates a new Public handler group. mediaStore, variantStore,
// and storageClient may be nil if S3 is not configured.
// domainResolver and baseDomain are used to compute canonical URLs for SEO meta tags.
func NewPublic(eng *engine.Engine, contentStore *store.ContentStore, siteSettingStore *store.SiteSettingStore, menuStore *store.MenuStore, mediaStore *store.MediaStore, variantStore *store.VariantStore, userProfileStore *store.UserProfileStore, storageClient *storage.Client, pageCache *cache.PageCache, domainResolver middleware.DomainResolver, baseDomain string) *Public {
	return &Public{
		engine:           eng,
		contentStore:     contentStore,
		siteSettingStore: siteSettingStore,
		menuStore:        menuStore,
		mediaStore:       mediaStore,
		variantStore:     variantStore,
		userProfileStore: userProfileStore,
		storageClient:    storageClient,
		pageCache:        pageCache,
		domainResolver:   domainResolver,
		baseDomain:       baseDomain,
	}
}

// Homepage renders the site homepage. If an article_loop template is active,
// it renders a blog-style post listing. Otherwise, it looks for a page with
// slug "home" or falls back to a simple default.
func (p *Public) Homepage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get tenant from context (set by tenant resolution middleware).
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		http.NotFound(w, r)
		return
	}

	// Check L2 cache first.
	if cached, ok := p.pageCache.Get(ctx, cache.HomepageKey(tenant.ID.String())); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(cached)
		return
	}

	// Try to render a blog-style homepage with the article_loop template.
	posts, err := p.contentStore.ListPublishedByType(tenant.ID, models.ContentTypePost)
	if err != nil {
		slog.Error("list published posts failed", "error", err)
	}

	if len(posts) > 0 {
		siteTitle, slogan := p.loadSiteTitleAndSlogan(tenant.ID, tenant.Name)
		menus := p.loadMenus(tenant.ID, "")
		authors := p.loadAuthors(posts)
		rendered, err := p.engine.RenderPostList(tenant.ID, siteTitle, slogan, posts, p.resolveFeaturedImages(tenant.ID, posts), authors, menus)
		if err == nil {
			p.pageCache.Set(ctx, cache.HomepageKey(tenant.ID.String()), rendered)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(rendered)
			return
		}
		slog.Warn("article_loop render failed, trying homepage", "error", err)
	}

	// Fall back to a "home" page if it exists.
	home, err := p.contentStore.FindBySlug(tenant.ID, "home")
	if err == nil && home != nil {
		siteTitle, slogan := p.loadSiteTitleAndSlogan(tenant.ID, tenant.Name)
		menus := p.loadMenus(tenant.ID, "home")
		author := p.loadAuthor(home.AuthorID)
		rendered, err := p.engine.RenderPage(tenant.ID, siteTitle, slogan, home, p.resolveFeaturedImage(tenant.ID, home), p.buildSocialMeta(tenant, home.Type, "/"), menus, author)
		if err == nil {
			p.pageCache.Set(ctx, cache.HomepageKey(tenant.ID.String()), rendered)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(rendered)
			return
		}
		slog.Warn("homepage render failed", "error", err)
	}

	// Default fallback when no templates or content exist yet (not cached).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>YaaiCMS</title>
<script src="https://cdn.tailwindcss.com"></script></head>
<body class="bg-gray-100 flex items-center justify-center min-h-screen">
<div class="text-center">
<h1 class="text-4xl font-bold text-gray-900"><span class="text-indigo-600">Smart</span>Press</h1>
<p class="mt-2 text-gray-500">Your site is running. Set up templates in the admin panel.</p>
<a href="/admin/login" class="mt-4 inline-block text-indigo-600 hover:text-indigo-800 text-sm">Go to Admin Panel</a>
</div></body></html>`))
}

// Page renders a public page or post by its slug using the template engine.
func (p *Public) Page(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slugParam := chi.URLParam(r, "slug")

	// Get tenant from context (set by tenant resolution middleware).
	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		http.NotFound(w, r)
		return
	}

	// Check L2 cache first.
	if cached, ok := p.pageCache.Get(ctx, cache.SlugKey(tenant.ID.String(), slugParam)); ok {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(cached)
		return
	}

	content, err := p.contentStore.FindBySlug(tenant.ID, slugParam)
	if err != nil {
		slog.Error("find content by slug failed", "error", err, "slug", slugParam)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if content == nil {
		http.NotFound(w, r)
		return
	}

	siteTitle, slogan := p.loadSiteTitleAndSlogan(tenant.ID, tenant.Name)
	menus := p.loadMenus(tenant.ID, slugParam)
	author := p.loadAuthor(content.AuthorID)
	rendered, err := p.engine.RenderPage(tenant.ID, siteTitle, slogan, content, p.resolveFeaturedImage(tenant.ID, content), p.buildSocialMeta(tenant, content.Type, "/"+content.Slug), menus, author)
	if err != nil {
		slog.Error("render page failed", "error", err, "slug", slugParam)
		// Fall back to a safe error page when the template engine fails.
		// Never render raw user content — it bypasses html/template escaping.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		safeTitle := html.EscapeString(content.Title)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>` + safeTitle + `</title>
<script src="https://cdn.tailwindcss.com"></script></head>
<body class="bg-gray-100 flex items-center justify-center min-h-screen">
<div class="text-center">
<h1 class="text-3xl font-bold text-gray-900">` + safeTitle + `</h1>
<p class="mt-2 text-gray-500">This page could not be rendered. Please check your templates.</p>
<a href="/" class="mt-4 inline-block text-indigo-600 hover:text-indigo-800 text-sm">Go to Homepage</a>
</div></body></html>`))
		return
	}

	// Store in L2 cache.
	p.pageCache.Set(ctx, cache.SlugKey(tenant.ID.String(), slugParam), rendered)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(rendered)
}

// AuthorPage renders a public author profile page with their published posts.
func (p *Public) AuthorPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	authorSlug := chi.URLParam(r, "slug")

	tenant := middleware.TenantFromCtx(ctx)
	if tenant == nil {
		http.NotFound(w, r)
		return
	}

	// Look up the author by their profile slug.
	userID, displayName, profile, err := p.userProfileStore.FindAuthorBySlug(authorSlug)
	if err != nil {
		slog.Error("find author by slug failed", "error", err, "slug", authorSlug)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if profile == nil {
		http.NotFound(w, r)
		return
	}

	author := engine.TemplateAuthor{
		Name:      displayName,
		Bio:       profile.Bio,
		AvatarURL: profile.AvatarURL,
		Website:   profile.Website,
		Location:  profile.Location,
		JobTitle:  profile.JobTitle,
		Pronouns:  profile.Pronouns,
		Twitter:   profile.Twitter,
		GitHub:    profile.GitHub,
		LinkedIn:  profile.LinkedIn,
		Instagram: profile.Instagram,
		Slug:      profile.Slug,
	}

	// Load the author's published posts.
	posts, err := p.contentStore.ListPublishedByAuthor(tenant.ID, userID)
	if err != nil {
		slog.Error("list posts by author failed", "error", err)
	}

	siteTitle, slogan := p.loadSiteTitleAndSlogan(tenant.ID, tenant.Name)
	menus := p.loadMenus(tenant.ID, "")

	// Build PostItems with featured images.
	featuredImages := p.resolveFeaturedImages(tenant.ID, posts)
	var postItems []engine.PostItem
	for _, post := range posts {
		item := engine.PostItem{
			Title:      post.Title,
			Slug:       post.Slug,
			AuthorName: displayName,
			AuthorSlug: profile.Slug,
		}
		if post.Excerpt != nil {
			item.Excerpt = *post.Excerpt
		}
		if post.PublishedAt != nil {
			item.PublishedAt = post.PublishedAt.Format("January 2, 2006")
		}
		if img := featuredImages[post.ID.String()]; img != nil {
			item.FeaturedImageURL = img.URL
			item.FeaturedImageSrcset = img.Srcset
			item.FeaturedImageAlt = img.Alt
		}
		postItems = append(postItems, item)
	}

	rendered, err := p.engine.RenderAuthorPage(tenant.ID, siteTitle, slogan, author, postItems, menus)
	if err != nil {
		slog.Error("render author page failed", "error", err, "slug", authorSlug)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(rendered)
}

// loadAuthor loads an author's profile and converts it to a TemplateAuthor.
// Returns nil if the profile doesn't exist or can't be loaded.
func (p *Public) loadAuthor(authorID uuid.UUID) *engine.TemplateAuthor {
	displayName, profile, err := p.userProfileStore.FindAuthorByUserID(authorID)
	if err != nil {
		slog.Warn("load author profile failed", "error", err)
		return nil
	}
	author := &engine.TemplateAuthor{Name: displayName}
	if profile != nil {
		author.Bio = profile.Bio
		author.AvatarURL = profile.AvatarURL
		author.Website = profile.Website
		author.Location = profile.Location
		author.JobTitle = profile.JobTitle
		author.Pronouns = profile.Pronouns
		author.Twitter = profile.Twitter
		author.GitHub = profile.GitHub
		author.LinkedIn = profile.LinkedIn
		author.Instagram = profile.Instagram
		// Only expose the author slug (for /author/{slug} links) when the
		// profile is published. This prevents templates from linking to a 404.
		if profile.IsPublished {
			author.Slug = profile.Slug
		}
	}
	return author
}

// loadAuthors batch-loads author profiles for a list of posts and returns
// a map of user ID → TemplateAuthor suitable for passing to RenderPostList.
func (p *Public) loadAuthors(posts []models.Content) map[uuid.UUID]*engine.TemplateAuthor {
	// Collect unique author IDs.
	seen := make(map[uuid.UUID]bool)
	var authorIDs []uuid.UUID
	for _, post := range posts {
		if !seen[post.AuthorID] {
			seen[post.AuthorID] = true
			authorIDs = append(authorIDs, post.AuthorID)
		}
	}

	if len(authorIDs) == 0 {
		return nil
	}

	result := make(map[uuid.UUID]*engine.TemplateAuthor)
	for _, id := range authorIDs {
		if a := p.loadAuthor(id); a != nil {
			result[id] = a
		}
	}
	return result
}

// buildSocialMeta constructs the SocialMeta context for a page render.
// It loads site settings and resolves the canonical URL from the tenant's
// primary domain (or subdomain fallback).
func (p *Public) buildSocialMeta(tenant *models.Tenant, contentType models.ContentType, path string) *engine.SocialMeta {
	settings, err := p.siteSettingStore.All(tenant.ID)
	if err != nil {
		slog.Warn("failed to load site settings for social meta", "error", err)
		settings = make(models.SiteSettings)
	}

	// Resolve canonical host: primary domain > subdomain.baseDomain.
	canonicalHost := tenant.Subdomain + "." + p.baseDomain
	if p.domainResolver != nil {
		if primary, err := p.domainResolver.FindPrimaryDomain(tenant.ID); err == nil && primary != "" {
			canonicalHost = primary
		}
	}

	return &engine.SocialMeta{
		CanonicalURL: "https://" + canonicalHost + path,
		ContentType:  contentType,
		Settings:     settings,
	}
}

// resolveFeaturedImage returns the featured image data (URL, srcset, alt)
// for a content item, or nil if none is set or storage is not configured.
func (p *Public) resolveFeaturedImage(tenantID uuid.UUID, content *models.Content) *engine.FeaturedImage {
	if content.FeaturedImageID == nil || p.mediaStore == nil || p.storageClient == nil {
		return nil
	}
	media, err := p.mediaStore.FindByID(tenantID, *content.FeaturedImageID)
	if err != nil || media == nil {
		return nil
	}
	if media.Bucket != p.storageClient.PublicBucket() {
		return nil
	}

	img := &engine.FeaturedImage{
		URL: p.storageClient.FileURL(media.S3Key),
	}
	if media.AltText != nil {
		img.Alt = *media.AltText
	}

	// Build srcset from responsive variants.
	if p.variantStore != nil {
		img.Srcset = p.buildSrcset(media.ID)
	}

	return img
}

// resolveFeaturedImages returns a map of content ID → featured image data
// for a slice of content items. Uses batch variant lookup for efficiency.
func (p *Public) resolveFeaturedImages(tenantID uuid.UUID, posts []models.Content) map[string]*engine.FeaturedImage {
	if p.mediaStore == nil || p.storageClient == nil {
		return nil
	}

	// Collect media IDs to look up.
	type mediaRef struct {
		contentID string
		mediaID   uuid.UUID
	}
	var refs []mediaRef
	var mediaIDs []uuid.UUID
	for _, post := range posts {
		if post.FeaturedImageID == nil {
			continue
		}
		refs = append(refs, mediaRef{contentID: post.ID.String(), mediaID: *post.FeaturedImageID})
		mediaIDs = append(mediaIDs, *post.FeaturedImageID)
	}

	if len(refs) == 0 {
		return nil
	}

	// Batch-fetch variants for all media IDs at once.
	var variantMap map[uuid.UUID][]models.MediaVariant
	if p.variantStore != nil && len(mediaIDs) > 0 {
		variantMap, _ = p.variantStore.FindByMediaIDs(mediaIDs)
	}

	result := make(map[string]*engine.FeaturedImage)
	for _, ref := range refs {
		media, err := p.mediaStore.FindByID(tenantID, ref.mediaID)
		if err != nil || media == nil {
			continue
		}
		if media.Bucket != p.storageClient.PublicBucket() {
			continue
		}
		img := &engine.FeaturedImage{
			URL: p.storageClient.FileURL(media.S3Key),
		}
		if media.AltText != nil {
			img.Alt = *media.AltText
		}
		if variants, ok := variantMap[ref.mediaID]; ok {
			img.Srcset = p.buildSrcsetFromVariants(variants)
		}
		result[ref.contentID] = img
	}
	return result
}

// buildSrcset fetches variants for a single media ID and builds an HTML
// srcset string like "url_sm.webp 640w, url_md.webp 1024w".
func (p *Public) buildSrcset(mediaID uuid.UUID) string {
	variants, err := p.variantStore.FindByMediaID(mediaID)
	if err != nil || len(variants) == 0 {
		return ""
	}
	return p.buildSrcsetFromVariants(variants)
}

// buildSrcsetFromVariants constructs an HTML srcset string from a slice of
// media variants. Only includes non-thumb variants (sm, md, lg) since
// thumb is too small for content images.
func (p *Public) buildSrcsetFromVariants(variants []models.MediaVariant) string {
	var parts []string
	for _, v := range variants {
		if v.Name == "thumb" {
			continue // Thumb is for admin previews, not srcset.
		}
		url := p.storageClient.FileURL(v.S3Key)
		parts = append(parts, fmt.Sprintf("%s %dw", url, v.Width))
	}
	return strings.Join(parts, ", ")
}

// loadSiteTitleAndSlogan fetches the public-facing site title and tagline
// from site settings. Falls back to tenantName if no title is configured.
func (p *Public) loadSiteTitleAndSlogan(tenantID uuid.UUID, tenantName string) (string, string) {
	title, _ := p.siteSettingStore.Get(tenantID, "site_title", tenantName)
	slogan, _ := p.siteSettingStore.Get(tenantID, "site_tagline", "")
	return title, slogan
}

// loadMenus loads all menu locations for a tenant and converts them to
// engine.Menus for template rendering. Items with content_id have their
// URL resolved from the content's slug. currentSlug marks matching items
// as Active for navigation highlighting.
func (p *Public) loadMenus(tenantID uuid.UUID, currentSlug string) engine.Menus {
	menus := make(engine.Menus)
	for _, loc := range store.MenuLocations {
		menu, err := p.menuStore.FindByLocation(tenantID, loc)
		if err != nil || menu == nil {
			continue
		}
		menus[loc] = p.convertMenuItems(tenantID, menu.Items, currentSlug)
	}
	return menus
}

// convertMenuItems converts model menu items to template-safe items,
// resolving content slugs and setting Active state.
func (p *Public) convertMenuItems(tenantID uuid.UUID, items []models.MenuItem, currentSlug string) []engine.TemplateMenuItem {
	var result []engine.TemplateMenuItem
	for _, item := range items {
		ti := engine.TemplateMenuItem{
			Label:  item.Label,
			URL:    item.URL,
			Target: item.Target,
		}

		// Resolve URL from content slug when linked to content.
		if item.ContentID != nil {
			content, err := p.contentStore.FindByID(tenantID, *item.ContentID)
			if err == nil && content != nil {
				ti.URL = "/" + content.Slug
			}
		}

		// Mark as active if URL matches current page.
		if currentSlug != "" && ti.URL == "/"+currentSlug {
			ti.Active = true
		}

		if len(item.Children) > 0 {
			ti.Children = p.convertMenuItems(tenantID, item.Children, currentSlug)
		}

		result = append(result, ti)
	}
	return result
}
