// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"yaaicms/internal/middleware"
	"yaaicms/internal/models"
	"yaaicms/internal/render"
	"yaaicms/internal/session"
	"yaaicms/internal/store"
)

// TenantAdmin groups all super-admin tenant management HTTP handlers.
type TenantAdmin struct {
	renderer    *render.Renderer
	sessions    *session.Store
	tenantStore *store.TenantStore
	userStore   *store.UserStore
}

// NewTenantAdmin creates a new TenantAdmin handler group.
func NewTenantAdmin(renderer *render.Renderer, sessions *session.Store, tenantStore *store.TenantStore, userStore *store.UserStore) *TenantAdmin {
	return &TenantAdmin{
		renderer:    renderer,
		sessions:    sessions,
		tenantStore: tenantStore,
		userStore:   userStore,
	}
}

// TenantList renders the list of all tenants (super-admin only).
func (ta *TenantAdmin) TenantList(w http.ResponseWriter, r *http.Request) {
	tenants, err := ta.tenantStore.List()
	if err != nil {
		slog.Error("failed to list tenants", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	ta.renderer.Page(w, r, "tenant_list", &render.PageData{
		Section: "tenants",
		Data:    map[string]any{"Tenants": tenants},
	})
}

// TenantNew renders the create tenant form.
func (ta *TenantAdmin) TenantNew(w http.ResponseWriter, r *http.Request) {
	ta.renderer.Page(w, r, "tenant_form", &render.PageData{
		Section: "tenants",
		Data:    map[string]any{"IsNew": true},
	})
}

// TenantCreate processes the create tenant form.
func (ta *TenantAdmin) TenantCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	subdomain := strings.TrimSpace(strings.ToLower(r.FormValue("subdomain")))

	if name == "" || subdomain == "" {
		ta.renderer.Page(w, r, "tenant_form", &render.PageData{
			Section: "tenants",
			Data: map[string]any{
				"IsNew":     true,
				"Error":     "Name and subdomain are required.",
				"Name":      name,
				"Subdomain": subdomain,
			},
		})
		return
	}

	// Check for duplicate subdomain.
	existing, err := ta.tenantStore.FindBySubdomain(subdomain)
	if err != nil {
		slog.Error("tenant subdomain check failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		ta.renderer.Page(w, r, "tenant_form", &render.PageData{
			Section: "tenants",
			Data: map[string]any{
				"IsNew":     true,
				"Error":     "A tenant with this subdomain already exists.",
				"Name":      name,
				"Subdomain": subdomain,
			},
		})
		return
	}

	tenant, err := ta.tenantStore.Create(name, subdomain)
	if err != nil {
		slog.Error("failed to create tenant", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	slog.Info("tenant created", "id", tenant.ID, "subdomain", subdomain)

	// Redirect to tenant list with HTMX-compatible redirect.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/admin/tenants")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/tenants", http.StatusSeeOther)
}

// TenantEdit renders the edit form for a specific tenant.
func (ta *TenantAdmin) TenantEdit(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tenant, err := ta.tenantStore.FindByID(id)
	if err != nil {
		slog.Error("failed to find tenant", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		http.NotFound(w, r)
		return
	}

	ta.renderer.Page(w, r, "tenant_form", &render.PageData{
		Section: "tenants",
		Data: map[string]any{
			"IsNew":    false,
			"Tenant":   tenant,
			"Name":     tenant.Name,
			"IsActive": tenant.IsActive,
		},
	})
}

// TenantUpdate processes the edit tenant form.
func (ta *TenantAdmin) TenantUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	isActive := r.FormValue("is_active") == "on"

	if name == "" {
		tenant, _ := ta.tenantStore.FindByID(id)
		ta.renderer.Page(w, r, "tenant_form", &render.PageData{
			Section: "tenants",
			Data: map[string]any{
				"IsNew":  false,
				"Tenant": tenant,
				"Error":  "Name is required.",
				"Name":   name,
			},
		})
		return
	}

	if err := ta.tenantStore.Update(id, name, isActive); err != nil {
		slog.Error("failed to update tenant", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	slog.Info("tenant updated", "id", id, "name", name, "is_active", isActive)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/admin/tenants")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/tenants", http.StatusSeeOther)
}

// TenantDelete removes a tenant.
func (ta *TenantAdmin) TenantDelete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := ta.tenantStore.Delete(id); err != nil {
		slog.Error("failed to delete tenant", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	slog.Info("tenant deleted", "id", id)

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/admin/tenants")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, "/admin/tenants", http.StatusSeeOther)
}

// TenantUsers renders the user management page for a specific tenant.
func (ta *TenantAdmin) TenantUsers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tenant, err := ta.tenantStore.FindByID(id)
	if err != nil || tenant == nil {
		slog.Error("failed to find tenant for users", "error", err)
		http.NotFound(w, r)
		return
	}

	users, err := ta.userStore.ListByTenant(id)
	if err != nil {
		slog.Error("failed to list tenant users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Get all users for the "add user" dropdown (exclude those already in the tenant).
	allUsers, err := ta.userStore.List()
	if err != nil {
		slog.Error("failed to list all users", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Filter out users already in this tenant.
	memberIDs := make(map[uuid.UUID]bool)
	for _, u := range users {
		memberIDs[u.User.ID] = true
	}
	var availableUsers []models.User
	for _, u := range allUsers {
		if !memberIDs[u.ID] {
			availableUsers = append(availableUsers, u)
		}
	}

	ta.renderer.Page(w, r, "tenant_users", &render.PageData{
		Section: "tenants",
		Data: map[string]any{
			"Tenant":         tenant,
			"Users":          users,
			"AvailableUsers": availableUsers,
		},
	})
}

// TenantAddUser assigns a user to a tenant with a given role.
func (ta *TenantAdmin) TenantAddUser(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	userID, err := uuid.Parse(r.FormValue("user_id"))
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	role := models.Role(r.FormValue("role"))
	if role != models.RoleAdmin && role != models.RoleEditor && role != models.RoleAuthor {
		role = models.RoleAuthor
	}

	if err := ta.userStore.AddToTenant(userID, tenantID, role); err != nil {
		slog.Error("failed to add user to tenant", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	slog.Info("user added to tenant", "user_id", userID, "tenant_id", tenantID, "role", role)

	redirectURL := "/admin/tenants/" + tenantID.String() + "/users"
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// TenantRemoveUser removes a user from a tenant.
func (ta *TenantAdmin) TenantRemoveUser(w http.ResponseWriter, r *http.Request) {
	tenantID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}

	userID, err := uuid.Parse(chi.URLParam(r, "uid"))
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	if err := ta.userStore.RemoveFromTenant(userID, tenantID); err != nil {
		slog.Error("failed to remove user from tenant", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	slog.Info("user removed from tenant", "user_id", userID, "tenant_id", tenantID)

	redirectURL := "/admin/tenants/" + tenantID.String() + "/users"
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", redirectURL)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// SelectTenantPage renders the tenant picker for users with multiple tenants.
// Shown after 2FA when the user belongs to more than one tenant.
func (ta *TenantAdmin) SelectTenantPage(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	tenants, err := ta.userStore.GetTenants(sess.UserID)
	if err != nil {
		slog.Error("failed to get user tenants", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	ta.renderer.Page(w, r, "select_tenant", &render.PageData{
		Title:   "Select Tenant",
		Section: "tenants",
		Data:    map[string]any{"Tenants": tenants},
	})
}

// SelectTenantSubmit processes the tenant selection and updates the session.
func (ta *TenantAdmin) SelectTenantSubmit(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	tenantID, err := uuid.Parse(r.FormValue("tenant_id"))
	if err != nil {
		http.Error(w, "Invalid tenant ID", http.StatusBadRequest)
		return
	}

	// Verify the user actually belongs to this tenant.
	role, err := ta.userStore.GetTenantRole(sess.UserID, tenantID)
	if err != nil {
		slog.Error("get tenant role failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if role == "" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Update session with the selected tenant.
	sess.TenantID = tenantID
	sess.TenantRole = string(role)

	if err := ta.sessions.Update(r.Context(), r, sess); err != nil {
		slog.Error("session update failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
}
