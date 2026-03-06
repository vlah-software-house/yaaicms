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
	storageClient    *storage.Client
	pageCache        *cache.PageCache
	domainResolver   middleware.DomainResolver
	baseDomain       string
}

// NewPublic creates a new Public handler group. mediaStore, variantStore,
// and storageClient may be nil if S3 is not configured.
// domainResolver and baseDomain are used to compute canonical URLs for SEO meta tags.
func NewPublic(eng *engine.Engine, contentStore *store.ContentStore, siteSettingStore *store.SiteSettingStore, menuStore *store.MenuStore, mediaStore *store.MediaStore, variantStore *store.VariantStore, storageClient *storage.Client, pageCache *cache.PageCache, domainResolver middleware.DomainResolver, baseDomain string) *Public {
	return &Public{
		engine:           eng,
		contentStore:     contentStore,
		siteSettingStore: siteSettingStore,
		menuStore:        menuStore,
		mediaStore:       mediaStore,
		variantStore:     variantStore,
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
		w.Write(cached)
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
		rendered, err := p.engine.RenderPostList(tenant.ID, siteTitle, slogan, posts, p.resolveFeaturedImages(posts), menus)
		if err == nil {
			p.pageCache.Set(ctx, cache.HomepageKey(tenant.ID.String()), rendered)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(rendered)
			return
		}
		slog.Warn("article_loop render failed, trying homepage", "error", err)
	}

	// Fall back to a "home" page if it exists.
	home, err := p.contentStore.FindBySlug(tenant.ID, "home")
	if err == nil && home != nil {
		siteTitle, slogan := p.loadSiteTitleAndSlogan(tenant.ID, tenant.Name)
		menus := p.loadMenus(tenant.ID, "home")
		rendered, err := p.engine.RenderPage(tenant.ID, siteTitle, slogan, home, p.resolveFeaturedImage(home), p.buildSocialMeta(tenant, home.Type, "/"), menus)
		if err == nil {
			p.pageCache.Set(ctx, cache.HomepageKey(tenant.ID.String()), rendered)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(rendered)
			return
		}
		slog.Warn("homepage render failed", "error", err)
	}

	// Default fallback when no templates or content exist yet (not cached).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
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
		w.Write(cached)
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
	rendered, err := p.engine.RenderPage(tenant.ID, siteTitle, slogan, content, p.resolveFeaturedImage(content), p.buildSocialMeta(tenant, content.Type, "/"+content.Slug), menus)
	if err != nil {
		slog.Error("render page failed", "error", err, "slug", slugParam)
		// Fall back to a safe error page when the template engine fails.
		// Never render raw user content — it bypasses html/template escaping.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		safeTitle := html.EscapeString(content.Title)
		w.Write([]byte(`<!DOCTYPE html><html><head><title>` + safeTitle + `</title>
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
	w.Write(rendered)
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
func (p *Public) resolveFeaturedImage(content *models.Content) *engine.FeaturedImage {
	if content.FeaturedImageID == nil || p.mediaStore == nil || p.storageClient == nil {
		return nil
	}
	media, err := p.mediaStore.FindByID(*content.FeaturedImageID)
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
func (p *Public) resolveFeaturedImages(posts []models.Content) map[string]*engine.FeaturedImage {
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
		media, err := p.mediaStore.FindByID(ref.mediaID)
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
		menus[loc] = p.convertMenuItems(menu.Items, currentSlug)
	}
	return menus
}

// convertMenuItems converts model menu items to template-safe items,
// resolving content slugs and setting Active state.
func (p *Public) convertMenuItems(items []models.MenuItem, currentSlug string) []engine.TemplateMenuItem {
	var result []engine.TemplateMenuItem
	for _, item := range items {
		ti := engine.TemplateMenuItem{
			Label:  item.Label,
			URL:    item.URL,
			Target: item.Target,
		}

		// Resolve URL from content slug when linked to content.
		if item.ContentID != nil {
			content, err := p.contentStore.FindByID(*item.ContentID)
			if err == nil && content != nil {
				ti.URL = "/" + content.Slug
			}
		}

		// Mark as active if URL matches current page.
		if currentSlug != "" && ti.URL == "/"+currentSlug {
			ti.Active = true
		}

		if len(item.Children) > 0 {
			ti.Children = p.convertMenuItems(item.Children, currentSlug)
		}

		result = append(result, ti)
	}
	return result
}
