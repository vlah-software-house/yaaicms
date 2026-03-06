// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"yaaicms/internal/middleware"
	"yaaicms/internal/models"
	"yaaicms/internal/render"
	"yaaicms/internal/store"
)

// MenusPage renders the admin menus management page with all 3 menu locations.
func (a *Admin) MenusPage(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	// Ensure all predefined menu locations exist for this tenant.
	if err := a.menuStore.EnsureLocations(sess.TenantID); err != nil {
		slog.Error("ensure menu locations failed", "error", err)
	}

	menus, err := a.menuStore.AllWithItems(sess.TenantID)
	if err != nil {
		slog.Error("list menus failed", "error", err)
	}

	// Load published pages and posts for the content picker.
	pages, _ := a.contentStore.ListPublishedByType(sess.TenantID, models.ContentTypePage)
	posts, _ := a.contentStore.ListPublishedByType(sess.TenantID, models.ContentTypePost)

	a.renderer.Page(w, r, "menus", &render.PageData{
		Title:   "Menus",
		Section: "menus",
		Data: map[string]any{
			"Menus":   menus,
			"Pages":   pages,
			"Posts":   posts,
		},
	})
}

// MenuItemCreate handles creating a new menu item.
func (a *Admin) MenuItemCreate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	menuIDStr := strings.TrimSpace(r.FormValue("menu_id"))
	menuID, err := uuid.Parse(menuIDStr)
	if err != nil {
		http.Error(w, "Invalid menu ID", http.StatusBadRequest)
		return
	}

	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		http.Error(w, "Label is required", http.StatusBadRequest)
		return
	}

	item := &models.MenuItem{
		MenuID: menuID,
		Label:  label,
		URL:    strings.TrimSpace(r.FormValue("url")),
		Target: strings.TrimSpace(r.FormValue("target")),
	}

	// Link to content if specified.
	if contentIDStr := strings.TrimSpace(r.FormValue("content_id")); contentIDStr != "" {
		if cid, err := uuid.Parse(contentIDStr); err == nil {
			item.ContentID = &cid
			// Resolve URL from content slug as a default.
			if content, err := a.contentStore.FindByID(sess.TenantID, cid); err == nil && content != nil {
				item.URL = "/" + content.Slug
			}
		}
	}

	// Parent ID for nested items (main nav only).
	if parentIDStr := strings.TrimSpace(r.FormValue("parent_id")); parentIDStr != "" {
		if pid, err := uuid.Parse(parentIDStr); err == nil {
			item.ParentID = &pid
		}
	}

	nextOrder, _ := a.menuStore.NextItemSortOrder(menuID, item.ParentID)
	item.SortOrder = nextOrder

	if _, err := a.menuStore.CreateItem(item); err != nil {
		slog.Error("create menu item failed", "error", err)
		http.Error(w, "Failed to create menu item", http.StatusInternalServerError)
		return
	}

	// Invalidate page cache since menus affect all public pages.
	a.pageCache.InvalidateAll(r.Context())

	// Re-render the full menus page for HTMX swap.
	a.MenusPage(w, r)
}

// MenuItemUpdate handles updating an existing menu item.
func (a *Admin) MenuItemUpdate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	item, err := a.menuStore.FindItemByID(sess.TenantID, id)
	if err != nil || item == nil {
		http.Error(w, "Menu item not found", http.StatusNotFound)
		return
	}

	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		http.Error(w, "Label is required", http.StatusBadRequest)
		return
	}

	item.Label = label
	item.URL = strings.TrimSpace(r.FormValue("url"))
	item.Target = strings.TrimSpace(r.FormValue("target"))

	// Update content link.
	if contentIDStr := strings.TrimSpace(r.FormValue("content_id")); contentIDStr != "" {
		if cid, err := uuid.Parse(contentIDStr); err == nil {
			item.ContentID = &cid
			if content, err := a.contentStore.FindByID(sess.TenantID, cid); err == nil && content != nil {
				item.URL = "/" + content.Slug
			}
		}
	} else {
		item.ContentID = nil
	}

	if err := a.menuStore.UpdateItem(sess.TenantID, item); err != nil {
		slog.Error("update menu item failed", "error", err)
		http.Error(w, "Failed to update menu item", http.StatusInternalServerError)
		return
	}

	a.pageCache.InvalidateAll(r.Context())

	a.MenusPage(w, r)
}

// MenuItemDelete handles deleting a menu item.
func (a *Admin) MenuItemDelete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	if err := a.menuStore.DeleteItem(sess.TenantID, id); err != nil {
		slog.Error("delete menu item failed", "error", err)
		http.Error(w, "Failed to delete menu item", http.StatusInternalServerError)
		return
	}

	a.pageCache.InvalidateAll(r.Context())

	a.MenusPage(w, r)
}

// MenuItemReorder handles the drag & drop reorder request (JSON body).
func (a *Admin) MenuItemReorder(w http.ResponseWriter, r *http.Request) {
	var items []store.MenuReorderItem
	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Extract menu_id from request.
	menuIDStr := r.URL.Query().Get("menu_id")
	menuID, err := uuid.Parse(menuIDStr)
	if err != nil {
		http.Error(w, "Invalid menu ID", http.StatusBadRequest)
		return
	}

	if err := a.menuStore.ReorderItems(menuID, items); err != nil {
		slog.Error("reorder menu items failed", "error", err)
		http.Error(w, "Failed to reorder", http.StatusInternalServerError)
		return
	}

	a.pageCache.InvalidateAll(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// MenuContentList returns a JSON list of published pages and posts for
// the menu item content picker.
func (a *Admin) MenuContentList(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())

	type contentOption struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		Slug  string `json:"slug"`
		Type  string `json:"type"`
	}

	var options []contentOption

	pages, _ := a.contentStore.ListPublishedByType(sess.TenantID, models.ContentTypePage)
	for _, p := range pages {
		options = append(options, contentOption{ID: p.ID.String(), Title: p.Title, Slug: p.Slug, Type: "page"})
	}

	posts, _ := a.contentStore.ListPublishedByType(sess.TenantID, models.ContentTypePost)
	for _, p := range posts {
		options = append(options, contentOption{ID: p.ID.String(), Title: p.Title, Slug: p.Slug, Type: "post"})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(options)
}
