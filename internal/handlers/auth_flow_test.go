// Copyright (c) 2026 Madalin Gabriel Ignisca <hi@madalin.me>
// Copyright (c) 2026 Vlah Software House SRL <contact@vlah.sh>
// All rights reserved. See LICENSE for details.

// auth_flow_test.go contains handler integration tests for the Auth handler
// methods: LoginPage, LoginSubmit, TwoFASetupPage, TwoFAVerifyPage,
// TwoFAVerifySubmit, and Logout. Tests exercise real database and Valkey
// connections; they are skipped when those services are unavailable.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"yaaicms/internal/session"
)

// --------------------------------------------------------------------------
// LoginPage
// --------------------------------------------------------------------------

// TestLoginPage_ReturnsHTML verifies that a GET to the login page returns
// HTTP 200 with HTML content when no session is present in the context.
func TestLoginPage_ReturnsHTML(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	rec := httptest.NewRecorder()

	env.Auth.LoginPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type: got %q, want text/html", ct)
	}
}

// TestLoginPage_AuthenticatedRedirectsToDashboard verifies that a fully
// authenticated user (session with TwoFADone=true) is redirected to the
// admin dashboard with a 303 See Other status.
func TestLoginPage_AuthenticatedRedirectsToDashboard(t *testing.T) {
	env := newTestEnv(t)

	sess := testSession(uuid.New(), "admin@yaaicms.local", "admin", true)
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.LoginPage(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/dashboard" {
		t.Errorf("Location: got %q, want /admin/dashboard", loc)
	}
}

// TestLoginPage_PartialSessionDoesNotRedirect verifies that a session with
// TwoFADone=false (login started but 2FA not completed) does NOT redirect;
// the login page is rendered normally.
func TestLoginPage_PartialSessionDoesNotRedirect(t *testing.T) {
	env := newTestEnv(t)

	sess := testSession(uuid.New(), "admin@yaaicms.local", "admin", false)
	req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.LoginPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (partial session should show login)", rec.Code, http.StatusOK)
	}
}

// --------------------------------------------------------------------------
// LoginSubmit
// --------------------------------------------------------------------------

// TestLoginSubmit_ValidCredentials verifies that valid email/password
// combination results in a 303 redirect to either the 2FA setup or verify
// page. The default seeded admin user (admin@yaaicms.local / admin) has
// no TOTP configured initially, so the redirect target is /admin/2fa/setup.
func TestLoginSubmit_ValidCredentials(t *testing.T) {
	env := newTestEnv(t)

	// Ensure the default admin user exists and has TOTP reset so we get a
	// predictable redirect to the 2FA setup page.
	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Reset TOTP so the user needs 2FA setup (predictable redirect target).
	if err := env.UserStore.ResetTOTP(user.ID); err != nil {
		t.Fatalf("reset totp: %v", err)
	}

	form := url.Values{}
	form.Set("email", "admin@yaaicms.local")
	form.Set("password", "admin")

	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.Auth.LoginSubmit(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}

	loc := rec.Header().Get("Location")
	if loc != "/admin/2fa/setup" && loc != "/admin/2fa/verify" {
		t.Errorf("Location: got %q, want /admin/2fa/setup or /admin/2fa/verify", loc)
	}

	// A session cookie should have been set.
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "sp_session" && c.Value != "" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected sp_session cookie to be set after successful login")
	}
}

// TestLoginSubmit_ValidCredentials_TOTPEnabled verifies that a user with
// TOTP already configured is redirected to /admin/2fa/verify instead of
// /admin/2fa/setup.
func TestLoginSubmit_ValidCredentials_TOTPEnabled(t *testing.T) {
	env := newTestEnv(t)

	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Set a TOTP secret and enable it so the user goes to verify page.
	if err := env.UserStore.SetTOTPSecret(user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("set totp secret: %v", err)
	}
	if err := env.UserStore.EnableTOTP(user.ID); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	t.Cleanup(func() {
		// Reset TOTP after test to avoid polluting other tests.
		env.UserStore.ResetTOTP(user.ID)
	})

	form := url.Values{}
	form.Set("email", "admin@yaaicms.local")
	form.Set("password", "admin")

	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.Auth.LoginSubmit(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}

	loc := rec.Header().Get("Location")
	if loc != "/admin/2fa/verify" {
		t.Errorf("Location: got %q, want /admin/2fa/verify", loc)
	}
}

// TestLoginSubmit_InvalidPassword verifies that a valid email with a wrong
// password re-renders the login page (200) rather than redirecting.
func TestLoginSubmit_InvalidPassword(t *testing.T) {
	env := newTestEnv(t)

	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	form := url.Values{}
	form.Set("email", "admin@yaaicms.local")
	form.Set("password", "wrong-password-definitely-not-correct")

	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.Auth.LoginSubmit(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (should re-render login)", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid email or password") {
		t.Error("expected error message in response body")
	}
}

// TestLoginSubmit_NonexistentEmail verifies that a completely unknown email
// address re-renders the login page (200) with an error message.
func TestLoginSubmit_NonexistentEmail(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("email", "nonexistent-user-xyz@example.com")
	form.Set("password", "irrelevant")

	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.Auth.LoginSubmit(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (should re-render login)", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid email or password") {
		t.Error("expected error message in response body")
	}
}

// --------------------------------------------------------------------------
// TwoFASetupPage
// --------------------------------------------------------------------------

// TestTwoFASetupPage_NoSession verifies that accessing the 2FA setup page
// without a session in the context results in a redirect to /admin/login.
func TestTwoFASetupPage_NoSession(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/2fa/setup", nil)
	rec := httptest.NewRecorder()

	env.Auth.TwoFASetupPage(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", loc)
	}
}

// TestTwoFASetupPage_WithSession verifies that an authenticated user (with
// session but no TOTP set up yet) sees the 2FA setup page (200) containing
// a QR code.
func TestTwoFASetupPage_WithSession(t *testing.T) {
	env := newTestEnv(t)

	// Find the admin user to get a real user ID.
	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Reset TOTP so the user needs setup (no secret, not enabled).
	if err := env.UserStore.ResetTOTP(user.ID); err != nil {
		t.Fatalf("reset totp: %v", err)
	}

	sess := testSession(user.ID, user.Email, "admin", false)
	req := httptest.NewRequest(http.MethodGet, "/admin/2fa/setup", nil)
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.TwoFASetupPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	// The page should contain a base64-encoded QR code image.
	if !strings.Contains(body, "data:image/png;base64,") && !strings.Contains(body, "QRCode") {
		t.Error("expected QR code data in the 2FA setup page response")
	}
}

// TestTwoFASetupPage_AlreadyEnabled verifies that a user who already has
// TOTP fully enabled is redirected to /admin/dashboard (they should not
// be able to re-setup).
func TestTwoFASetupPage_AlreadyEnabled(t *testing.T) {
	env := newTestEnv(t)

	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Set a secret and enable TOTP.
	if err := env.UserStore.SetTOTPSecret(user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("set totp secret: %v", err)
	}
	if err := env.UserStore.EnableTOTP(user.ID); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	t.Cleanup(func() {
		env.UserStore.ResetTOTP(user.ID)
	})

	sess := testSession(user.ID, user.Email, "admin", false)
	req := httptest.NewRequest(http.MethodGet, "/admin/2fa/setup", nil)
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.TwoFASetupPage(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/dashboard" {
		t.Errorf("Location: got %q, want /admin/dashboard", loc)
	}
}

// --------------------------------------------------------------------------
// TwoFAVerifyPage
// --------------------------------------------------------------------------

// TestTwoFAVerifyPage_NoSession verifies that accessing the 2FA verify page
// without a session redirects to /admin/login.
func TestTwoFAVerifyPage_NoSession(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/2fa/verify", nil)
	rec := httptest.NewRecorder()

	env.Auth.TwoFAVerifyPage(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", loc)
	}
}

// TestTwoFAVerifyPage_WithSession verifies that the 2FA verify page renders
// successfully (200) when a valid session exists.
func TestTwoFAVerifyPage_WithSession(t *testing.T) {
	env := newTestEnv(t)

	sess := testSession(uuid.New(), "admin@yaaicms.local", "admin", false)
	req := httptest.NewRequest(http.MethodGet, "/admin/2fa/verify", nil)
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.TwoFAVerifyPage(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type: got %q, want text/html", ct)
	}
}

// --------------------------------------------------------------------------
// TwoFAVerifySubmit
// --------------------------------------------------------------------------

// TestTwoFAVerifySubmit_NoSession verifies that submitting a 2FA code
// without a session redirects to /admin/login.
func TestTwoFAVerifySubmit_NoSession(t *testing.T) {
	env := newTestEnv(t)

	form := url.Values{}
	form.Set("code", "123456")

	req := httptest.NewRequest(http.MethodPost, "/admin/2fa/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	env.Auth.TwoFAVerifySubmit(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", loc)
	}
}

// TestTwoFAVerifySubmit_InvalidCode verifies that submitting an incorrect
// TOTP code re-renders the verification form (200) with an error message.
func TestTwoFAVerifySubmit_InvalidCode(t *testing.T) {
	env := newTestEnv(t)

	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Set a known TOTP secret and enable it.
	if err := env.UserStore.SetTOTPSecret(user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("set totp secret: %v", err)
	}
	if err := env.UserStore.EnableTOTP(user.ID); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	t.Cleanup(func() {
		env.UserStore.ResetTOTP(user.ID)
	})

	sess := testSession(user.ID, user.Email, "admin", false)

	form := url.Values{}
	form.Set("code", "000000") // Almost certainly wrong.

	req := httptest.NewRequest(http.MethodPost, "/admin/2fa/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.TwoFAVerifySubmit(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (should re-render form)", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Invalid code") {
		t.Error("expected 'Invalid code' error message in response body")
	}
}

// TestTwoFAVerifySubmit_NoTOTPSecret verifies that if the user's TOTP
// secret is nil (not set up at all), the handler redirects to /admin/2fa/setup.
func TestTwoFAVerifySubmit_NoTOTPSecret(t *testing.T) {
	env := newTestEnv(t)

	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Ensure no TOTP secret is set.
	if err := env.UserStore.ResetTOTP(user.ID); err != nil {
		t.Fatalf("reset totp: %v", err)
	}

	sess := testSession(user.ID, user.Email, "admin", false)

	form := url.Values{}
	form.Set("code", "123456")

	req := httptest.NewRequest(http.MethodPost, "/admin/2fa/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(ctxWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()

	env.Auth.TwoFAVerifySubmit(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/2fa/setup" {
		t.Errorf("Location: got %q, want /admin/2fa/setup", loc)
	}
}

// --------------------------------------------------------------------------
// Logout
// --------------------------------------------------------------------------

// TestLogout_RedirectsToLogin verifies that the Logout handler destroys
// the session and redirects to /admin/login with a 303 status.
func TestLogout_RedirectsToLogin(t *testing.T) {
	env := newTestEnv(t)

	// First create a real session so there is something to destroy.
	user, err := env.UserStore.FindByEmail("admin@yaaicms.local")
	if err != nil || user == nil {
		t.Skip("skipping: default admin user not found in database — run seed first")
	}

	// Create a session in Valkey and get the cookie.
	createRec := httptest.NewRecorder()
	ctx := context.Background()
	sessID, err := env.Sessions.Create(ctx, createRec, &session.Data{
		UserID:       user.ID,
		Email:        user.Email,
		IsSuperAdmin: user.IsSuperAdmin,
		TenantRole:   "admin",
		TwoFADone:    true,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sessID == "" {
		t.Fatal("session ID should not be empty")
	}

	// Build the logout request with the session cookie from the create response.
	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	for _, c := range createRec.Result().Cookies() {
		req.AddCookie(c)
	}
	// Add session data to context as the middleware would.
	sess := testSession(user.ID, user.Email, "admin", true)
	req = req.WithContext(ctxWithSession(req.Context(), sess))

	rec := httptest.NewRecorder()
	env.Auth.Logout(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", loc)
	}

	// Verify the session cookie was cleared (MaxAge < 0).
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sp_session" {
			if c.MaxAge >= 0 {
				t.Errorf("expected sp_session MaxAge < 0 (cleared), got %d", c.MaxAge)
			}
			break
		}
	}
}

// TestLogout_NoCookie verifies that Logout handles the case where no
// session cookie is present gracefully (still redirects to login).
func TestLogout_NoCookie(t *testing.T) {
	env := newTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	rec := httptest.NewRecorder()

	env.Auth.Logout(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusSeeOther)
	}
	loc := rec.Header().Get("Location")
	if loc != "/admin/login" {
		t.Errorf("Location: got %q, want /admin/login", loc)
	}
}

