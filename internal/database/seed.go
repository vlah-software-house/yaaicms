// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package database

import (
	"database/sql"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"
)

// defaultTenantID is the well-known UUID for the default tenant created by
// both the migration (for existing data) and the seed (for fresh installs).
const defaultTenantID = "00000000-0000-0000-0000-000000000001"

// Seed populates the database with initial development data.
// It creates a default tenant, admin user, templates, and sample content.
// All seed functions are idempotent — they check before inserting.
func Seed(db *sql.DB) error {
	if err := seedDefaultTenant(db); err != nil {
		return err
	}
	if err := seedAdminUser(db); err != nil {
		return err
	}
	if err := seedSiteSettings(db); err != nil {
		return fmt.Errorf("seed site settings: %w", err)
	}
	if err := seedTemplates(db); err != nil {
		return fmt.Errorf("seed templates: %w", err)
	}
	if err := seedContent(db); err != nil {
		return fmt.Errorf("seed content: %w", err)
	}
	if err := seedMenus(db); err != nil {
		return fmt.Errorf("seed menus: %w", err)
	}
	err := seedAdminProfile(db)
	if err != nil {
		return fmt.Errorf("seed admin profile: %w", err)
	}
	return nil
}

// seedDefaultTenant creates the default tenant if it doesn't exist.
// The migration (00018) also creates this tenant for existing data,
// but on a fresh install the tables are empty.
func seedDefaultTenant(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tenants WHERE id = $1", defaultTenantID).Scan(&count); err != nil {
		return fmt.Errorf("seed check tenant: %w", err)
	}
	if count > 0 {
		return nil
	}

	_, err := db.Exec(`
		INSERT INTO tenants (id, name, subdomain)
		VALUES ($1, $2, $3)
	`, defaultTenantID, "Default", "default")
	if err != nil {
		return fmt.Errorf("seed insert tenant: %w", err)
	}

	slog.Info("seeded default tenant", "subdomain", "default")
	return nil
}

// seedAdminUser creates a default admin if no users exist.
// The admin is marked as super_admin and assigned to the default tenant.
func seedAdminUser(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return fmt.Errorf("seed check users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("seed bcrypt: %w", err)
	}

	// Create the user (no role column — role is per-tenant in user_tenants).
	var userID string
	err = db.QueryRow(`
		INSERT INTO users (email, password_hash, display_name, is_super_admin, totp_enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, "admin@yaaicms.local", string(hash), "Admin", true, false).Scan(&userID)
	if err != nil {
		return fmt.Errorf("seed insert admin: %w", err)
	}

	// Assign the admin to the default tenant with admin role.
	_, err = db.Exec(`
		INSERT INTO user_tenants (user_id, tenant_id, role)
		VALUES ($1, $2, $3)
	`, userID, defaultTenantID, "admin")
	if err != nil {
		return fmt.Errorf("seed assign admin to tenant: %w", err)
	}

	slog.Info("database seeded with default admin user",
		"email", "admin@yaaicms.local",
		"password", "admin",
		"is_super_admin", true,
	)
	return nil
}

// seedSiteSettings creates default site settings for the default tenant
// if none exist. The migration (00013) created global settings, but after
// multi-tenancy, settings are per-tenant.
func seedSiteSettings(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM site_settings WHERE tenant_id = $1", defaultTenantID).Scan(&count); err != nil {
		return fmt.Errorf("check site settings: %w", err)
	}
	if count > 0 {
		return nil
	}

	defaults := map[string]string{
		"site_title":     "My Website",
		"site_tagline":   "Just another SmartPress site",
		"timezone":       "UTC",
		"language":       "en",
		"date_format":    "2006-01-02",
		"posts_per_page": "10",
	}

	for key, value := range defaults {
		_, err := db.Exec(`
			INSERT INTO site_settings (tenant_id, key, value)
			VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, key) DO NOTHING
		`, defaultTenantID, key, value)
		if err != nil {
			return fmt.Errorf("seed setting %q: %w", key, err)
		}
	}

	slog.Info("seeded default site settings", "tenant", "default")
	return nil
}

// seedTemplates creates a minimal set of active templates for the default
// tenant so the public site works immediately after setup.
func seedTemplates(db *sql.DB) error { //nolint:funlen // template HTML literals make this inherently long
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM templates WHERE tenant_id = $1", defaultTenantID).Scan(&count); err != nil {
		return fmt.Errorf("check templates: %w", err)
	}
	if count > 0 {
		return nil
	}

	templates := []struct {
		name, tmplType, html string
	}{
		{
			name:     "Default Header",
			tmplType: "header",
			html: `<header class="bg-white border-b border-gray-200">
  <div class="max-w-5xl mx-auto px-4 py-4 flex items-center justify-between">
    <a href="/" class="text-xl font-bold text-indigo-600">{{ .SiteTitle }}</a>
    <nav class="space-x-4 text-sm text-gray-600">
      {{ range .Menus.main }}<a href="{{ .URL }}"{{ if .Target }} target="{{ .Target }}"{{ end }} class="{{ if .Active }}text-indigo-600 font-semibold{{ else }}hover:text-gray-900{{ end }}">{{ .Label }}</a>
      {{ end }}
    </nav>
  </div>
</header>`,
		},
		{
			name:     "Default Footer",
			tmplType: "footer",
			html: `<footer class="bg-gray-50 border-t border-gray-200 mt-12">
  <div class="max-w-5xl mx-auto px-4 py-6 text-center text-sm text-gray-500">
    {{ if .Menus.footer }}<nav class="mb-3 space-x-4">{{ range .Menus.footer }}<a href="{{ .URL }}"{{ if .Target }} target="{{ .Target }}"{{ end }} class="hover:text-gray-700">{{ .Label }}</a>{{ end }}</nav>{{ end }}
    <p>&copy; {{ .Year }} {{ .SiteTitle }}. All rights reserved.</p>
    {{ if .Menus.footer_legal }}<nav class="mt-2 space-x-3 text-xs">{{ range .Menus.footer_legal }}<a href="{{ .URL }}"{{ if .Target }} target="{{ .Target }}"{{ end }} class="hover:text-gray-700">{{ .Label }}</a>{{ end }}</nav>{{ end }}
  </div>
</footer>`,
		},
		{
			name:     "Default Page",
			tmplType: "page",
			html: `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{ .Title }} — {{ .SiteTitle }}</title>
  {{ if .MetaDescription }}<meta name="description" content="{{ .MetaDescription }}">{{ end }}
  {{ if .MetaKeywords }}<meta name="keywords" content="{{ .MetaKeywords }}">{{ end }}
  <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-white text-gray-900 min-h-screen flex flex-col">
  {{ .Header }}
  <main class="flex-1 max-w-3xl mx-auto px-4 py-8 w-full">
    <h1 class="text-3xl font-bold mb-6">{{ .Title }}</h1>
    <article class="prose max-w-none">{{ .Body }}</article>
  </main>
  {{ .Footer }}
</body>
</html>`,
		},
		{
			name:     "Default Article Loop",
			tmplType: "article_loop",
			html: `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{ .Title }} — {{ .SiteTitle }}</title>
  <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-white text-gray-900 min-h-screen flex flex-col">
  {{ .Header }}
  <main class="flex-1 max-w-3xl mx-auto px-4 py-8 w-full">
    <h1 class="text-3xl font-bold mb-8">{{ .Title }}</h1>
    {{ range .Posts }}
    <article class="mb-8 pb-8 border-b border-gray-200 last:border-0">
      <h2 class="text-xl font-semibold">
        <a href="/{{ .Slug }}" class="text-indigo-600 hover:text-indigo-800">{{ .Title }}</a>
      </h2>
      {{ if .PublishedAt }}<time class="text-sm text-gray-500">{{ .PublishedAt }}</time>{{ end }}
      {{ if .Excerpt }}<p class="mt-2 text-gray-600">{{ .Excerpt }}</p>{{ end }}
    </article>
    {{ end }}
  </main>
  {{ .Footer }}
</body>
</html>`,
		},
		{
			name:     "Default Author Page",
			tmplType: "author_page",
			html: `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{ .Title }} — {{ .SiteTitle }}</title>
  <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-white text-gray-900 min-h-screen flex flex-col">
  {{ .Header }}
  <main class="flex-1 max-w-3xl mx-auto px-4 py-8 w-full">
    <div class="flex items-center gap-6 mb-8">
      {{ if .Author.AvatarURL }}<img src="{{ .Author.AvatarURL }}" alt="{{ .Author.Name }}" class="w-20 h-20 rounded-full object-cover">{{ end }}
      <div>
        <h1 class="text-3xl font-bold">{{ .Author.Name }}</h1>
        {{ if .Author.JobTitle }}<p class="text-gray-500">{{ .Author.JobTitle }}</p>{{ end }}
      </div>
    </div>
    {{ if .Author.Bio }}<p class="text-gray-700 mb-6">{{ .Author.Bio }}</p>{{ end }}
    <h2 class="text-xl font-semibold mb-4">Posts</h2>
    {{ range .Posts }}
    <article class="mb-6 pb-6 border-b border-gray-200 last:border-0">
      <h3 class="text-lg font-semibold">
        <a href="/{{ .Slug }}" class="text-indigo-600 hover:text-indigo-800">{{ .Title }}</a>
      </h3>
      {{ if .PublishedAt }}<time class="text-sm text-gray-500">{{ .PublishedAt }}</time>{{ end }}
      {{ if .Excerpt }}<p class="mt-1 text-gray-600">{{ .Excerpt }}</p>{{ end }}
    </article>
    {{ end }}
  </main>
  {{ .Footer }}
</body>
</html>`,
		},
	}

	for _, t := range templates {
		_, err := db.Exec(`
			INSERT INTO templates (tenant_id, name, type, html_content, version, is_active)
			VALUES ($1, $2, $3, $4, 1, true)
		`, defaultTenantID, t.name, t.tmplType, t.html)
		if err != nil {
			return fmt.Errorf("insert template %q: %w", t.name, err)
		}
	}

	slog.Info("seeded default templates", "count", len(templates))
	return nil
}

// seedContent creates a sample homepage and blog post for the default tenant
// so the public site renders meaningful content right after setup.
func seedContent(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM content WHERE tenant_id = $1", defaultTenantID).Scan(&count); err != nil {
		return fmt.Errorf("check content: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Look up the admin user to assign as author.
	var authorID string
	if err := db.QueryRow("SELECT id FROM users LIMIT 1").Scan(&authorID); err != nil {
		return fmt.Errorf("find author: %w", err)
	}

	pages := []struct {
		contentType, title, slug, body, excerpt, status string
		published                                       bool
	}{
		{
			contentType: "page",
			title:       "Welcome to SmartPress",
			slug:        "home",
			body:        `<p>This is your new SmartPress site. Edit this page from the <a href="/admin">admin panel</a>, or create AI-powered templates to customize the look and feel.</p>`,
			status:      "published",
			published:   true,
		},
		{
			contentType: "post",
			title:       "Hello World",
			slug:        "hello-world",
			body:        `<p>This is a sample blog post created during setup. You can edit or delete it from the admin panel.</p>`,
			excerpt:     "A sample blog post to get you started with SmartPress.",
			status:      "published",
			published:   true,
		},
	}

	for _, p := range pages {
		publishedAt := "NULL"
		if p.published {
			publishedAt = "NOW()"
		}

		var excerpt *string
		if p.excerpt != "" {
			excerpt = &p.excerpt
		}

		_, err := db.Exec(fmt.Sprintf(`
			INSERT INTO content (tenant_id, type, title, slug, body, excerpt, status, author_id, published_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, %s)
		`, publishedAt), defaultTenantID, p.contentType, p.title, p.slug, p.body, excerpt, p.status, authorID)
		if err != nil {
			return fmt.Errorf("insert content %q: %w", p.slug, err)
		}
	}

	slog.Info("seeded sample content", "count", len(pages))
	return nil
}

// seedMenus creates the 3 predefined menu locations for the default tenant.
func seedMenus(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM menus WHERE tenant_id = $1", defaultTenantID).Scan(&count); err != nil {
		return fmt.Errorf("check menus: %w", err)
	}
	if count > 0 {
		return nil
	}

	for _, loc := range []string{"main", "footer", "footer_legal"} {
		_, err := db.Exec(`
			INSERT INTO menus (tenant_id, location)
			VALUES ($1, $2)
			ON CONFLICT (tenant_id, location) DO NOTHING
		`, defaultTenantID, loc)
		if err != nil {
			return fmt.Errorf("seed menu %q: %w", loc, err)
		}
	}

	slog.Info("seeded menu locations", "count", 3)
	return nil
}

// seedAdminProfile creates a profile for the admin user if none exists.
func seedAdminProfile(db *sql.DB) error {
	// Find the admin user.
	var userID string
	err := db.QueryRow("SELECT id FROM users WHERE email = 'admin@yaaicms.local'").Scan(&userID)
	if err != nil {
		// No admin user yet — nothing to seed.
		return nil //nolint:nilerr // Intentional: missing admin user is not an error for seeding.
	}

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM user_profiles WHERE user_id = $1", userID).Scan(&count)
	if err != nil {
		return fmt.Errorf("check admin profile: %w", err)
	}
	if count > 0 {
		return nil
	}

	_, err = db.Exec(`
		INSERT INTO user_profiles (user_id, slug, bio, job_title, is_published)
		VALUES ($1, 'admin', 'Site administrator.', 'Administrator', TRUE)
	`, userID)
	if err != nil {
		return fmt.Errorf("seed admin profile: %w", err)
	}

	slog.Info("seeded admin user profile")
	return nil
}
