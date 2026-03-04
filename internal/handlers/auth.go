// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

package handlers

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/pquerna/otp/totp"
	qrcode "github.com/skip2/go-qrcode"

	"yaaicms/internal/middleware"
	"yaaicms/internal/render"
	"yaaicms/internal/session"
	"yaaicms/internal/store"
)

// Auth groups all authentication-related HTTP handlers.
type Auth struct {
	renderer  *render.Renderer
	sessions  *session.Store
	userStore *store.UserStore
}

// NewAuth creates a new Auth handler group.
func NewAuth(renderer *render.Renderer, sessions *session.Store, userStore *store.UserStore) *Auth {
	return &Auth{
		renderer:  renderer,
		sessions:  sessions,
		userStore: userStore,
	}
}

// LoginPage renders the login form.
func (a *Auth) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in with 2FA complete, redirect to dashboard.
	sess := middleware.SessionFromCtx(r.Context())
	if sess != nil && sess.TwoFADone {
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}

	a.renderer.Page(w, r, "login", &render.PageData{
		Title: "Sign In",
	})
}

// LoginSubmit processes the login form.
func (a *Auth) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	// Find the user by email.
	user, err := a.userStore.FindByEmail(email)
	if err != nil {
		slog.Error("login lookup failed", "error", err)
		a.renderer.Page(w, r, "login", &render.PageData{
			Title: "Sign In",
			Data:  map[string]any{"Error": "An unexpected error occurred."},
		})
		return
	}

	// Validate credentials.
	if user == nil || !a.userStore.CheckPassword(user, password) {
		a.renderer.Page(w, r, "login", &render.PageData{
			Title: "Sign In",
			Data:  map[string]any{"Error": "Invalid email or password."},
		})
		return
	}

	// Create a session. TwoFADone starts as false — user must complete 2FA.
	// TenantID and TenantRole are set after 2FA verification, during tenant selection.
	_, err = a.sessions.Create(r.Context(), w, &session.Data{
		UserID:       user.ID,
		Email:        user.Email,
		DisplayName:  user.DisplayName,
		IsSuperAdmin: user.IsSuperAdmin,
		TwoFADone:    false,
	})
	if err != nil {
		slog.Error("session create failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Route based on 2FA status:
	// - Not set up yet → go to setup page
	// - Already set up → go to verification page
	if user.Needs2FASetup() {
		http.Redirect(w, r, "/admin/2fa/setup", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/2fa/verify", http.StatusSeeOther)
	}
}

// TwoFASetupPage displays the QR code for TOTP setup. It only generates
// a new secret if the user doesn't already have one (first-time setup).
// Refreshing the page re-displays the same secret, preventing accidental
// secret rotation that would invalidate already-scanned QR codes.
func (a *Auth) TwoFASetupPage(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	// Check if the user already has a pending (unverified) secret.
	user, err := a.userStore.FindByID(sess.UserID)
	if err != nil || user == nil {
		slog.Error("user lookup for 2fa setup failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var secret string
	if user.TOTPSecret != nil && !user.TOTPEnabled {
		// Reuse the existing pending secret (user refreshed the page).
		secret = *user.TOTPSecret
	} else if user.TOTPSecret == nil {
		// Generate a new TOTP key for first-time setup.
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "YaaiCMS",
			AccountName: sess.Email,
		})
		if err != nil {
			slog.Error("totp generate failed", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		secret = key.Secret()

		// Save the secret to the database (still unverified).
		if err := a.userStore.SetTOTPSecret(sess.UserID, secret); err != nil {
			slog.Error("save totp secret failed", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	} else {
		// User already has 2FA enabled — shouldn't be here.
		http.Redirect(w, r, "/admin/dashboard", http.StatusSeeOther)
		return
	}

	// Generate QR code as base64-encoded PNG.
	otpURL := fmt.Sprintf("otpauth://totp/YaaiCMS:%s?secret=%s&issuer=YaaiCMS", sess.Email, secret)
	qrPNG, err := qrcode.Encode(otpURL, qrcode.Medium, 256)
	if err != nil {
		slog.Error("qr code generation failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	qrBase64 := base64.StdEncoding.EncodeToString(qrPNG)

	a.renderer.Page(w, r, "2fa_setup", &render.PageData{
		Title: "Set Up Two-Factor Authentication",
		Data: map[string]any{
			"QRCode": qrBase64,
			"Secret": secret,
		},
	})
}

// TwoFAVerifyPage renders the 2FA code entry form (for users who already have 2FA set up).
func (a *Auth) TwoFAVerifyPage(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	a.renderer.Page(w, r, "2fa_verify", &render.PageData{
		Title: "Two-Factor Authentication",
	})
}

// TwoFAVerifySubmit validates the TOTP code and completes authentication.
func (a *Auth) TwoFAVerifySubmit(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}

	code := r.FormValue("code")

	// Look up the user's TOTP secret.
	user, err := a.userStore.FindByID(sess.UserID)
	if err != nil || user == nil {
		slog.Error("user lookup for 2fa failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if user.TOTPSecret == nil {
		http.Redirect(w, r, "/admin/2fa/setup", http.StatusSeeOther)
		return
	}

	// Validate the TOTP code.
	valid := totp.Validate(code, *user.TOTPSecret)
	if !valid {
		// Determine which page to show the error on.
		templateName := "2fa_verify"
		if !user.TOTPEnabled {
			templateName = "2fa_setup"

			// Re-generate QR code for the setup page.
			qrPNG, _ := qrcode.Encode(
				fmt.Sprintf("otpauth://totp/YaaiCMS:%s?secret=%s&issuer=YaaiCMS", user.Email, *user.TOTPSecret),
				qrcode.Medium, 256,
			)
			qrBase64 := base64.StdEncoding.EncodeToString(qrPNG)

			a.renderer.Page(w, r, templateName, &render.PageData{
				Title: "Set Up Two-Factor Authentication",
				Data: map[string]any{
					"Error":  "Invalid code. Please try again.",
					"QRCode": qrBase64,
					"Secret": *user.TOTPSecret,
				},
			})
			return
		}

		a.renderer.Page(w, r, templateName, &render.PageData{
			Title: "Two-Factor Authentication",
			Data:  map[string]any{"Error": "Invalid code. Please try again."},
		})
		return
	}

	// If this is the first-time setup, enable TOTP in the database.
	if !user.TOTPEnabled {
		if err := a.userStore.EnableTOTP(user.ID); err != nil {
			slog.Error("enable totp failed", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	// Mark 2FA as complete in the session.
	sess.TwoFADone = true

	// Look up the user's tenant memberships to determine where to redirect.
	tenants, err := a.userStore.GetTenants(sess.UserID)
	if err != nil {
		slog.Error("get tenants failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var redirectURL string

	switch len(tenants) {
	case 0:
		// No tenant memberships.
		if sess.IsSuperAdmin {
			// Super admins can manage tenants without belonging to one.
			redirectURL = "/admin/tenants"
		} else {
			// Regular user with no tenants — cannot proceed.
			a.renderer.Page(w, r, "2fa_verify", &render.PageData{
				Title: "Two-Factor Authentication",
				Data:  map[string]any{"Error": "You are not a member of any tenant. Please contact an administrator."},
			})
			return
		}
	case 1:
		// Exactly one tenant — auto-select it.
		sess.TenantID = tenants[0].Tenant.ID
		sess.TenantRole = string(tenants[0].Role)
		redirectURL = "/admin/dashboard"
	default:
		// Multiple tenants — redirect to tenant picker.
		redirectURL = "/admin/select-tenant"
	}

	if err := a.sessions.Update(r.Context(), r, sess); err != nil {
		slog.Error("session update failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// Logout destroys the session and redirects to the login page.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	a.sessions.Destroy(r.Context(), w, r)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}
