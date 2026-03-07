// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// Package engine provides the dynamic template rendering engine for public
// pages. It loads AI-generated templates from the database, compiles them
// as Go html/templates, and renders public pages by injecting content data.
package engine

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"yaaicms/internal/markdown"
	"yaaicms/internal/models"
	"yaaicms/internal/storage"
	"yaaicms/internal/store"
)

// contentCSS is the embedded typographic stylesheet for Markdown-rendered
// content bodies. Scoped to .yaaicms-content and injected into every
// public page render so content HTML always has proper typography.
//
//go:embed content.css
var contentCSS string

// contentStyleTag is the pre-built <style> block, computed once at init.
var contentStyleTag string //nolint:gochecknoglobals // computed once at startup, read-only thereafter

func init() { //nolint:gochecknoinits // one-time concatenation of constant CSS
	contentStyleTag = "<style>" + contentCSS + "</style>"
}

// FeaturedImage holds image data for templates including responsive variants.
// Templates can use {{.FeaturedImageURL}} for the main URL (backward-compat)
// and {{.FeaturedImageSrcset}} for responsive <img srcset="...">.
type FeaturedImage struct {
	URL    string // Public URL of the original image (or largest variant)
	Srcset string // Pre-built srcset string: "url_sm.webp 640w, url_md.webp 1024w, ..."
	Alt    string // Alt text for accessibility
}

// SocialMeta carries all context needed to generate Open Graph, Twitter Card,
// and standard SEO meta tags. Passed into RenderPage by the public handler.
type SocialMeta struct {
	CanonicalURL string            // Full canonical URL (https://host/slug)
	ContentType  models.ContentType // "post" or "page" — determines og:type
	Settings     models.SiteSettings // site_title, site_tagline, og_default_image, twitter_site
}

// TemplateMenuItem is the template-safe menu item available in templates.
type TemplateMenuItem struct {
	Label    string
	URL      string
	Target   string
	Active   bool               // true when URL matches current page slug
	Children []TemplateMenuItem
}

// Menus holds menu items keyed by location for template access.
// Templates use {{range .Menus.main}}, {{range .Menus.footer}}, etc.
type Menus map[string][]TemplateMenuItem

// FragmentData is passed to header/footer fragments so they can render
// dynamic navigation menus, site title, slogan, and current year.
type FragmentData struct {
	SiteTitle string
	Slogan    string
	Year      int
	Menus     Menus
}

// PageData holds all variables available to a page template when rendering
// a public page. Template authors (or AI) can use these as {{.Title}}, etc.
type PageData struct {
	SiteTitle           string
	Slogan              string
	Title               string
	Body                template.HTML // Content body — raw HTML from editor
	Excerpt             string
	MetaDescription     string
	MetaKeywords        string
	FeaturedImageURL    string        // Public URL of the featured image (empty if none)
	FeaturedImageSrcset string        // Responsive srcset for the featured image
	FeaturedImageAlt    string        // Alt text for the featured image
	Slug                string
	PublishedAt         string
	Header              template.HTML // Pre-rendered header fragment
	Footer              template.HTML // Pre-rendered footer fragment
	Year                int
	Menus               Menus
}

// PostItem represents a single post in a listing (used by article_loop template).
type PostItem struct {
	Title               string
	Slug                string
	Excerpt             string
	FeaturedImageURL    string // Public URL of the featured image (empty if none)
	FeaturedImageSrcset string // Responsive srcset for the featured image
	FeaturedImageAlt    string // Alt text for the featured image
	PublishedAt         string
}

// ListData holds variables available to the article_loop template.
type ListData struct {
	SiteTitle string
	Slogan    string
	Title     string
	Posts    []PostItem
	Header   template.HTML
	Footer   template.HTML
	Year     int
	Menus    Menus
}

// Engine compiles and renders templates from the database. It maintains
// an in-memory cache (L1) of compiled Go templates keyed by ID+version,
// so repeated renders skip the expensive template.Parse step.
//
// When media dependencies are configured (via SetMediaDeps), the engine
// also rewrites <img> tags in content bodies to include responsive
// srcset attributes using WebP variants from the media_variants table.
type Engine struct {
	templateStore *store.TemplateStore
	cache         *templateCache

	// Optional media dependencies for body image srcset rewriting.
	// Nil when S3 storage is not configured — rewriting is silently skipped.
	mediaStore    *store.MediaStore
	variantStore  *store.VariantStore
	storageClient *storage.Client
}

// New creates a new template rendering engine with an empty L1 cache.
func New(templateStore *store.TemplateStore) *Engine {
	return &Engine{
		templateStore: templateStore,
		cache:         newTemplateCache(),
	}
}

// SetMediaDeps configures optional media dependencies for body image
// srcset rewriting. Call after New() when S3 storage is available.
func (e *Engine) SetMediaDeps(mediaStore *store.MediaStore, variantStore *store.VariantStore, storageClient *storage.Client) {
	e.mediaStore = mediaStore
	e.variantStore = variantStore
	e.storageClient = storageClient
}

// InvalidateTemplate removes a specific template from the L1 cache.
// Called by admin handlers after template update or delete.
func (e *Engine) InvalidateTemplate(id string) {
	e.cache.invalidate(id)
}

// InvalidateAllTemplates clears the entire L1 cache. Called after
// template activation since it changes which template serves each type.
func (e *Engine) InvalidateAllTemplates() {
	e.cache.invalidateAll()
}

// RenderPage renders a content item using the active page template,
// header, and footer. tenantID scopes the template lookup to the tenant.
// img holds the featured image data including responsive variants (pass nil if none).
// siteTitle is the public-facing title from Settings; slogan is the tagline.
// social carries SEO/social meta context; pass nil to skip meta tag injection.
func (e *Engine) RenderPage(tenantID uuid.UUID, siteTitle, slogan string, content *models.Content, img *FeaturedImage, social *SocialMeta, menus Menus) ([]byte, error) {
	// Build fragment data for header/footer (menus, site title, slogan, year).
	fragData := &FragmentData{
		SiteTitle: siteTitle,
		Slogan:    slogan,
		Year:      time.Now().Year(),
		Menus:     menus,
	}

	// Load active templates for each component.
	header, err := e.renderFragment(tenantID, models.TemplateTypeHeader, fragData)
	if err != nil {
		slog.Warn("header template not found or failed", "error", err)
		header = ""
	}

	footer, err := e.renderFragment(tenantID, models.TemplateTypeFooter, fragData)
	if err != nil {
		slog.Warn("footer template not found or failed", "error", err)
		footer = ""
	}

	// Load the active page template.
	pageTmpl, err := e.templateStore.FindActiveByType(tenantID, models.TemplateTypePage)
	if err != nil || pageTmpl == nil {
		return nil, fmt.Errorf("no active page template found")
	}

	// Build the data for the page template.
	publishedAt := ""
	if content.PublishedAt != nil {
		publishedAt = content.PublishedAt.Format("January 2, 2006")
	}

	// Convert Markdown body to HTML if needed; raw HTML is passed through unchanged.
	bodyHTML := content.Body
	if content.BodyFormat == models.BodyFormatMarkdown {
		rendered, err := markdown.ToHTML(content.Body)
		if err != nil {
			slog.Warn("markdown conversion failed, using raw body", "error", err)
		} else {
			bodyHTML = rendered
		}
	}

	// Rewrite inline <img> tags to include responsive srcset when variants exist.
	bodyHTML = e.rewriteBodyImages(bodyHTML)

	// Wrap the body in a scoped container so content.css styles apply.
	bodyHTML = `<div class="yaaicms-content">` + bodyHTML + `</div>`

	data := PageData{
		SiteTitle:   siteTitle,
		Slogan:      slogan,
		Title:       content.Title,
		Body:        template.HTML(bodyHTML),
		Slug:        content.Slug,
		PublishedAt: publishedAt,
		Header:      template.HTML(header),
		Footer:      template.HTML(footer),
		Year:        time.Now().Year(),
		Menus:       menus,
	}

	if img != nil {
		data.FeaturedImageURL = img.URL
		data.FeaturedImageSrcset = img.Srcset
		data.FeaturedImageAlt = img.Alt
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

	// Compile and execute the page template (L1 cached by ID+version).
	rendered, err := e.compileAndRender(pageTmpl.ID.String(), pageTmpl.Version, pageTmpl.HTMLContent, data)
	if err != nil {
		return nil, err
	}

	result := injectContentCSS(rendered)
	result = injectAlpineJS(result)

	// Inject social/SEO meta tags before </head> when social context is provided.
	if social != nil {
		result = injectSocialMeta(result, data, social)
	}

	return result, nil
}

// RenderPostList renders the article_loop template with a list of posts.
// tenantID scopes the template lookup. siteTitle and slogan come from Settings.
// featuredImages maps content ID strings to their featured image data
// including responsive variants.
func (e *Engine) RenderPostList(tenantID uuid.UUID, siteTitle, slogan string, posts []models.Content, featuredImages map[string]*FeaturedImage, menus Menus) ([]byte, error) {
	// Build fragment data for header/footer (menus, site title, slogan, year).
	fragData := &FragmentData{
		SiteTitle: siteTitle,
		Slogan:    slogan,
		Year:      time.Now().Year(),
		Menus:     menus,
	}

	header, err := e.renderFragment(tenantID, models.TemplateTypeHeader, fragData)
	if err != nil {
		slog.Warn("header template not found or failed", "error", err)
		header = ""
	}

	footer, err := e.renderFragment(tenantID, models.TemplateTypeFooter, fragData)
	if err != nil {
		slog.Warn("footer template not found or failed", "error", err)
		footer = ""
	}

	loopTmpl, err := e.templateStore.FindActiveByType(tenantID, models.TemplateTypeArticleLoop)
	if err != nil || loopTmpl == nil {
		return nil, fmt.Errorf("no active article_loop template found")
	}

	var postItems []PostItem
	for _, p := range posts {
		item := PostItem{
			Title: p.Title,
			Slug:  p.Slug,
		}
		if p.Excerpt != nil {
			item.Excerpt = *p.Excerpt
		}
		if p.PublishedAt != nil {
			item.PublishedAt = p.PublishedAt.Format("January 2, 2006")
		}
		if img := featuredImages[p.ID.String()]; img != nil {
			item.FeaturedImageURL = img.URL
			item.FeaturedImageSrcset = img.Srcset
			item.FeaturedImageAlt = img.Alt
		}
		postItems = append(postItems, item)
	}

	data := ListData{
		SiteTitle: siteTitle,
		Slogan:    slogan,
		Title:     "Blog",
		Posts:    postItems,
		Header:   template.HTML(header),
		Footer:   template.HTML(footer),
		Year:     time.Now().Year(),
		Menus:    menus,
	}

	rendered, err := e.compileAndRender(loopTmpl.ID.String(), loopTmpl.Version, loopTmpl.HTMLContent, data)
	if err != nil {
		return nil, err
	}

	result := injectContentCSS(rendered)
	result = injectAlpineJS(result)
	return result, nil
}

// ValidateTemplate attempts to compile a template string and returns an
// error if the Go template syntax is invalid. Used before saving to DB.
func (e *Engine) ValidateTemplate(htmlContent string) error {
	_, err := template.New("validate").Parse(htmlContent)
	if err != nil {
		return fmt.Errorf("invalid template syntax: %w", err)
	}
	return nil
}

// ValidateAndRender compiles a template string and renders it with the
// given data. Used for live preview in the admin panel. Not cached since
// preview content is ephemeral.
func (e *Engine) ValidateAndRender(htmlContent string, data any) ([]byte, error) {
	return e.compileAndRender("", 0, htmlContent, data)
}

// renderFragment loads and renders a template fragment (header or footer).
func (e *Engine) renderFragment(tenantID uuid.UUID, tmplType models.TemplateType, data any) (string, error) {
	tmpl, err := e.templateStore.FindActiveByType(tenantID, tmplType)
	if err != nil || tmpl == nil {
		return "", fmt.Errorf("no active %s template", tmplType)
	}

	result, err := e.compileAndRender(tmpl.ID.String(), tmpl.Version, tmpl.HTMLContent, data)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

// compileAndRender compiles a template string and executes it with the
// given data. If id and version are provided (non-empty id), the compiled
// template is cached in L1 to avoid re-parsing on subsequent requests.
func (e *Engine) compileAndRender(id string, version int, tmplContent string, data any) ([]byte, error) {
	var compiled *template.Template

	// Try L1 cache first (skip for ad-hoc renders like preview).
	if id != "" {
		compiled = e.cache.get(id, version)
	}

	if compiled == nil {
		var err error
		compiled, err = template.New("page").Parse(tmplContent)
		if err != nil {
			return nil, fmt.Errorf("compile template: %w", err)
		}
		// Store in L1 cache for next time.
		if id != "" {
			e.cache.put(id, version, compiled)
		}
	}

	var buf bytes.Buffer
	if err := compiled.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	return buf.Bytes(), nil
}

// injectContentCSS inserts the content typography <style> block into the
// rendered HTML. It injects before </head> when present (standard HTML
// documents), otherwise prepends to the output (template fragments).
func injectContentCSS(rendered []byte) []byte {
	htmlContent := string(rendered)
	if idx := strings.Index(strings.ToLower(htmlContent), "</head>"); idx != -1 {
		return []byte(htmlContent[:idx] + contentStyleTag + htmlContent[idx:])
	}
	// No </head> — prepend the style block.
	return []byte(contentStyleTag + htmlContent)
}

// alpineJSTag is the CDN script tag for AlpineJS, used by header templates
// for interactive components like mobile menu toggles and dropdowns.
const alpineJSTag = `<script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3/dist/cdn.min.js"></script>`

// injectAlpineJS ensures the rendered HTML includes the AlpineJS CDN script.
// Skips injection if Alpine is already present (e.g., template included it).
func injectAlpineJS(rendered []byte) []byte {
	s := string(rendered)
	if strings.Contains(strings.ToLower(s), "alpinejs") {
		return rendered
	}
	if idx := strings.Index(strings.ToLower(s), "</head>"); idx != -1 {
		return []byte(s[:idx] + alpineJSTag + "\n" + s[idx:])
	}
	return rendered
}

// injectSocialMeta inserts Open Graph, Twitter Card, and standard SEO meta
// tags before </head> in the rendered HTML.
func injectSocialMeta(rendered []byte, data PageData, social *SocialMeta) []byte {
	tags := buildSocialMetaTags(data, social)
	if tags == "" {
		return rendered
	}

	h := string(rendered)
	if idx := strings.Index(strings.ToLower(h), "</head>"); idx != -1 {
		return []byte(h[:idx] + tags + h[idx:])
	}
	// No </head> — prepend.
	return []byte(tags + h)
}

// buildSocialMetaTags generates the full block of Open Graph, Twitter Card,
// canonical link, and standard SEO meta tags as raw HTML.
func buildSocialMetaTags(data PageData, social *SocialMeta) string {
	var b strings.Builder

	esc := html.EscapeString // shorthand for attribute escaping

	// --- Standard SEO ---
	if social.CanonicalURL != "" {
		b.WriteString(`<link rel="canonical" href="` + esc(social.CanonicalURL) + `">` + "\n")
	}
	if data.MetaDescription != "" {
		b.WriteString(`<meta name="description" content="` + esc(data.MetaDescription) + `">` + "\n")
	}
	if data.MetaKeywords != "" {
		b.WriteString(`<meta name="keywords" content="` + esc(data.MetaKeywords) + `">` + "\n")
	}

	// --- Resolve values with fallbacks ---
	title := data.Title
	if title == "" {
		title = social.Settings["site_title"]
	}

	description := data.MetaDescription
	if description == "" {
		description = data.Excerpt
	}
	if description == "" {
		description = social.Settings["site_tagline"]
	}

	imageURL := data.FeaturedImageURL
	if imageURL == "" {
		imageURL = social.Settings["og_default_image"]
	}

	siteName := social.Settings["site_title"]

	// og:type: "article" for posts, "website" for pages/homepage.
	ogType := "website"
	if social.ContentType == models.ContentTypePost {
		ogType = "article"
	}

	// --- Open Graph ---
	b.WriteString(`<meta property="og:title" content="` + esc(title) + `">` + "\n")
	if description != "" {
		b.WriteString(`<meta property="og:description" content="` + esc(description) + `">` + "\n")
	}
	if imageURL != "" {
		b.WriteString(`<meta property="og:image" content="` + esc(imageURL) + `">` + "\n")
	}
	if social.CanonicalURL != "" {
		b.WriteString(`<meta property="og:url" content="` + esc(social.CanonicalURL) + `">` + "\n")
	}
	b.WriteString(`<meta property="og:type" content="` + ogType + `">` + "\n")
	if siteName != "" {
		b.WriteString(`<meta property="og:site_name" content="` + esc(siteName) + `">` + "\n")
	}
	if data.PublishedAt != "" && ogType == "article" {
		b.WriteString(`<meta property="article:published_time" content="` + esc(data.PublishedAt) + `">` + "\n")
	}

	// --- Twitter Cards ---
	twitterCard := "summary"
	if imageURL != "" {
		twitterCard = "summary_large_image"
	}
	b.WriteString(`<meta name="twitter:card" content="` + twitterCard + `">` + "\n")

	twitterSite := social.Settings["twitter_site"]
	if twitterSite != "" {
		b.WriteString(`<meta name="twitter:site" content="` + esc(twitterSite) + `">` + "\n")
	}
	b.WriteString(`<meta name="twitter:title" content="` + esc(title) + `">` + "\n")
	if description != "" {
		b.WriteString(`<meta name="twitter:description" content="` + esc(description) + `">` + "\n")
	}
	if imageURL != "" {
		b.WriteString(`<meta name="twitter:image" content="` + esc(imageURL) + `">` + "\n")
	}

	return b.String()
}

// RewriteBodyImages is the exported wrapper for rewriteBodyImages, allowing
// other packages (e.g., admin handlers for preview) to apply the same
// responsive srcset rewriting to content bodies.
func (e *Engine) RewriteBodyImages(html string) string {
	return e.rewriteBodyImages(html)
}

// imgSrcRe matches <img ... src="..." ...> tags and captures the full tag
// and the src URL. It handles single and double quotes.
var imgSrcRe = regexp.MustCompile(`<img\s([^>]*?)src=["']([^"']+)["']([^>]*)>`)

// rewriteBodyImages scans HTML for <img> tags whose src URLs reference
// this site's S3 storage, looks up their responsive variants, and injects
// srcset attributes. Tags that already have srcset are left untouched.
// Returns the original HTML unchanged if media deps are not configured.
func (e *Engine) rewriteBodyImages(html string) string {
	if e.storageClient == nil || e.mediaStore == nil || e.variantStore == nil {
		return html
	}

	// Find all <img> tags with their src URLs.
	matches := imgSrcRe.FindAllStringSubmatchIndex(html, -1)
	if len(matches) == 0 {
		return html
	}

	// Extract S3 keys from matched img src URLs, deduplicating.
	type imgMatch struct {
		fullStart, fullEnd int    // indices of the full <img ...> tag
		preAttrs           string // attributes before src
		srcURL             string // the src URL value
		postAttrs          string // attributes after src
		s3Key              string // extracted S3 key (empty if not our storage)
	}

	var imgs []imgMatch
	keySet := make(map[string]bool)
	var s3Keys []string

	for _, loc := range matches {
		preAttrs := html[loc[2]:loc[3]]
		srcURL := html[loc[4]:loc[5]]
		postAttrs := html[loc[6]:loc[7]]

		im := imgMatch{
			fullStart: loc[0],
			fullEnd:   loc[1],
			preAttrs:  preAttrs,
			srcURL:    srcURL,
			postAttrs: postAttrs,
		}

		// Skip if this tag already has srcset.
		fullTag := html[loc[0]:loc[1]]
		if strings.Contains(fullTag, "srcset") {
			imgs = append(imgs, im)
			continue
		}

		if key, ok := e.storageClient.ExtractS3Key(srcURL); ok {
			im.s3Key = key
			if !keySet[key] {
				keySet[key] = true
				s3Keys = append(s3Keys, key)
			}
		}
		imgs = append(imgs, im)
	}

	if len(s3Keys) == 0 {
		return html
	}

	// Batch-fetch media records by S3 key.
	mediaByKey, err := e.mediaStore.FindByS3Keys(s3Keys)
	if err != nil {
		slog.Warn("body image rewrite: media lookup failed", "error", err)
		return html
	}

	// Collect media IDs for variant lookup.
	var mediaIDs []uuid.UUID
	for _, m := range mediaByKey {
		mediaIDs = append(mediaIDs, m.ID)
	}

	// Batch-fetch all variants.
	variantMap, err := e.variantStore.FindByMediaIDs(mediaIDs)
	if err != nil {
		slog.Warn("body image rewrite: variant lookup failed", "error", err)
		return html
	}

	// Build replacement HTML from back to front (so indices stay valid).
	result := []byte(html)
	for i := len(imgs) - 1; i >= 0; i-- {
		im := imgs[i]
		if im.s3Key == "" {
			continue
		}
		media := mediaByKey[im.s3Key]
		if media == nil {
			continue
		}
		variants := variantMap[media.ID]
		if len(variants) == 0 {
			continue
		}

		srcset := e.buildSrcsetFromVariants(variants)
		if srcset == "" {
			continue
		}

		// Rebuild the <img> tag with srcset and sizes injected after src.
		var tag strings.Builder
		tag.WriteString(`<img `)
		tag.WriteString(im.preAttrs)
		tag.WriteString(`src="`)
		tag.WriteString(im.srcURL)
		tag.WriteString(`" srcset="`)
		tag.WriteString(srcset)
		tag.WriteString(`" sizes="(max-width: 640px) 640px, (max-width: 1024px) 1024px, 1920px"`)
		tag.WriteString(im.postAttrs)
		tag.WriteString(`>`)

		// Replace the original tag.
		result = append(result[:im.fullStart], append([]byte(tag.String()), result[im.fullEnd:]...)...)
	}

	return string(result)
}

// buildSrcsetFromVariants constructs an HTML srcset string from variants,
// excluding thumb (too small for content images).
func (e *Engine) buildSrcsetFromVariants(variants []models.MediaVariant) string {
	var parts []string
	for _, v := range variants {
		if v.Name == "thumb" {
			continue
		}
		url := e.storageClient.FileURL(v.S3Key)
		parts = append(parts, fmt.Sprintf("%s %dw", url, v.Width))
	}
	return strings.Join(parts, ", ")
}
