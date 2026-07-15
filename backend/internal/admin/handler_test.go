package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type fakeStore struct {
	account          Admin
	findAccountError error
	lastUsername     string
	createdAdminID   string
	createdTokenHash string
	createdExpiresAt time.Time
	auditEvents      []AdminAuthAuditEvent
	createdAudit     AdminAuthAuditEvent
	auditErr         error
	sessionErr       error
	sessionAccount   Admin
	findSessionError error
	lastSessionHash  string
	deletedTokenHash string
}

func (s *fakeStore) FindByUsername(_ context.Context, username string) (Admin, error) {
	s.lastUsername = username
	return s.account, s.findAccountError
}

func (s *fakeStore) CreateSessionWithAudit(
	_ context.Context,
	adminID string,
	tokenHash string,
	expiresAt time.Time,
	event AdminAuthAuditEvent,
) error {
	if s.sessionErr != nil {
		return s.sessionErr
	}
	s.createdAdminID = adminID
	s.createdTokenHash = tokenHash
	s.createdExpiresAt = expiresAt
	s.createdAudit = event
	return nil
}

func (s *fakeStore) RecordAdminAuthEvent(_ context.Context, event AdminAuthAuditEvent) error {
	if s.auditErr != nil {
		return s.auditErr
	}
	s.auditEvents = append(s.auditEvents, event)
	return nil
}

func (s *fakeStore) FindBySession(_ context.Context, tokenHash string) (Admin, error) {
	s.lastSessionHash = tokenHash
	return s.sessionAccount, s.findSessionError
}

func (s *fakeStore) DeleteSession(_ context.Context, tokenHash string) error {
	s.deletedTokenHash = tokenHash
	return nil
}

func TestLoginCreatesHttpOnlySession(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{account: Admin{
		ID:           "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327",
		Username:     "Admin",
		PasswordHash: string(passwordHash),
		Role:         "admin",
		Status:       "active",
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	handler.now = func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) }
	handler.random = bytes.NewReader(bytes.Repeat([]byte{7}, 32))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/login",
		strings.NewReader(`{"username":"  aDmIn  ","password":"correct-password"}`),
	)
	handler.Login(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if store.lastUsername != "aDmIn" {
		t.Fatalf("expected trimmed username, got %q", store.lastUsername)
	}
	if store.createdAdminID != store.account.ID || len(store.createdTokenHash) != 64 {
		t.Fatal("session was not created with the expected admin and hashed token")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Value == "" {
		t.Fatal("expected a non-empty HttpOnly session cookie")
	}
	if cookies[0].Value == store.createdTokenHash {
		t.Fatal("raw cookie token must differ from the stored token hash")
	}
	if store.createdAudit.EventType != AdminAuthEventLoginSucceeded || store.createdAudit.Result != AdminAuthResultSuccess || store.createdAudit.ReasonCode != AdminAuthReasonNone {
		t.Fatalf("unexpected success audit event: %+v", store.createdAudit)
	}
	if store.createdAudit.AdminID == nil || *store.createdAudit.AdminID != store.account.ID {
		t.Fatalf("audit admin id = %#v, want %q", store.createdAudit.AdminID, store.account.ID)
	}
	if store.createdAudit.UsernameNormalized != "admin" {
		t.Fatalf("audit username = %q, want admin", store.createdAudit.UsernameNormalized)
	}
	if strings.Contains(store.createdAudit.UsernameNormalized, "correct-password") || strings.Contains(store.createdAudit.ClientIP, "correct-password") {
		t.Fatal("audit event leaked password material")
	}
}

func TestLoginRejectsInvalidCredentialsAndDisabledAccount(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		password string
		status   string
	}{
		{name: "wrong password", password: "wrong-password", status: "active"},
		{name: "disabled account", password: "correct-password", status: "disabled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{account: Admin{
				ID:           "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327",
				Username:     "admin",
				PasswordHash: string(passwordHash),
				Role:         "admin",
				Status:       test.status,
			}}
			handler := NewHandler(store, 12*time.Hour, false)
			recorder := httptest.NewRecorder()
			body, _ := json.Marshal(loginRequest{Username: "admin", Password: test.password})
			request := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(body))

			handler.Login(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", recorder.Code)
			}
			if store.createdTokenHash != "" {
				t.Fatal("rejected login must not create a session")
			}
		})
	}
}

func TestAuthenticationMiddlewareAndMe(t *testing.T) {
	store := &fakeStore{sessionAccount: Admin{
		ID:       "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327",
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	protected := handler.RequireAuthentication(http.HandlerFunc(handler.Me))

	withoutCookie := httptest.NewRecorder()
	protected.ServeHTTP(withoutCookie, httptest.NewRequest(http.MethodGet, "/api/admin/me", nil))
	if withoutCookie.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", withoutCookie.Code)
	}

	withCookie := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-token"})
	protected.ServeHTTP(withCookie, request)
	if withCookie.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid session, got %d", withCookie.Code)
	}
	if store.lastSessionHash != hashToken("raw-session-token") {
		t.Fatal("middleware did not hash the cookie token before lookup")
	}
}

func TestLogoutDeletesSessionAndClearsCookie(t *testing.T) {
	store := &fakeStore{sessionAccount: Admin{
		ID:       "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327",
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	protected := handler.RequireAuthentication(http.HandlerFunc(handler.Logout))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-token"})

	protected.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if store.deletedTokenHash != hashToken("raw-session-token") {
		t.Fatal("logout did not delete the expected hashed session")
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("logout must expire the session cookie")
	}
}

func TestLoginAuditsInvalidCredentialsAndDisabledAccount(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		account     Admin
		findErr     error
		password    string
		wantReason  AdminAuthReasonCode
		wantAdminID bool
	}{
		{
			name:       "unknown username",
			findErr:    ErrNotFound,
			password:   "wrong-password",
			wantReason: AdminAuthReasonInvalidCredentials,
		},
		{
			name:        "wrong password",
			account:     Admin{ID: "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327", Username: "Admin", PasswordHash: string(passwordHash), Role: "admin", Status: "active"},
			password:    "wrong-password",
			wantReason:  AdminAuthReasonInvalidCredentials,
			wantAdminID: true,
		},
		{
			name:        "disabled account",
			account:     Admin{ID: "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327", Username: "Admin", PasswordHash: string(passwordHash), Role: "admin", Status: "disabled"},
			password:    "correct-password",
			wantReason:  AdminAuthReasonAccountDisabled,
			wantAdminID: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeStore{account: test.account, findAccountError: test.findErr}
			handler := NewHandler(store, 12*time.Hour, false)
			handler.now = func() time.Time { return time.Date(2026, 7, 15, 9, 30, 0, 0, time.UTC) }
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"  Admin  ","password":"`+test.password+`"}`))
			request.Header.Set("User-Agent", "fixture-agent\r\nshould-be-removed")

			handler.Login(recorder, request)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", recorder.Code)
			}
			if store.createdTokenHash != "" {
				t.Fatal("rejected login must not create a session")
			}
			if len(store.auditEvents) != 1 {
				t.Fatalf("audit events = %d, want 1", len(store.auditEvents))
			}
			event := store.auditEvents[0]
			if event.EventType != AdminAuthEventLoginFailed || event.Result != AdminAuthResultFailure || event.ReasonCode != test.wantReason {
				t.Fatalf("unexpected audit event: %+v", event)
			}
			if event.UsernameNormalized != "admin" {
				t.Fatalf("username = %q, want admin", event.UsernameNormalized)
			}
			if test.wantAdminID && (event.AdminID == nil || *event.AdminID != test.account.ID) {
				t.Fatalf("admin id = %#v, want %q", event.AdminID, test.account.ID)
			}
			if !test.wantAdminID && event.AdminID != nil {
				t.Fatalf("unknown username must not set admin id: %#v", event.AdminID)
			}
			if event.UserAgentSummary == nil || strings.Contains(*event.UserAgentSummary, "\r") || strings.Contains(*event.UserAgentSummary, "\n") {
				t.Fatalf("user agent was not sanitized: %#v", event.UserAgentSummary)
			}
			if strings.Contains(event.UsernameNormalized, test.password) || strings.Contains(event.ClientIP, test.password) {
				t.Fatal("audit event leaked password material")
			}
		})
	}
}

func TestLoginRateLimitedAuditIsDeduped(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{account: Admin{
		ID: "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327", Username: "admin", PasswordHash: string(passwordHash), Role: "admin", Status: "active",
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	handler.now = func() time.Time { return now }

	for i := 0; i < handler.limiter.maxAttempts; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong-password"}`))
		handler.Login(recorder, request)
	}
	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong-password"}`))
		handler.Login(recorder, request)
		if recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("rate-limited attempt %d status = %d", i+1, recorder.Code)
		}
	}

	rateLimited := 0
	for _, event := range store.auditEvents {
		if event.EventType == AdminAuthEventLoginRateLimited {
			rateLimited++
			if event.ReasonCode != AdminAuthReasonRateLimited {
				t.Fatalf("rate-limit reason = %q", event.ReasonCode)
			}
		}
	}
	if rateLimited != 1 {
		t.Fatalf("rate-limit audit events = %d, want 1", rateLimited)
	}
}

func TestLoginSuccessAuditFailureBlocksSession(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		account:    Admin{ID: "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327", Username: "admin", PasswordHash: string(passwordHash), Role: "admin", Status: "active"},
		sessionErr: errors.New("audit write failed"),
	}
	handler := NewHandler(store, 12*time.Hour, false)
	handler.random = bytes.NewReader(bytes.Repeat([]byte{7}, 32))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"correct-password"}`))
	handler.Login(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", recorder.Code)
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("session cookie must not be set when success audit/session transaction fails")
	}
	if store.createdTokenHash != "" {
		t.Fatal("fake store should not record a created session on transaction failure")
	}
}

func TestLoginFailureAuditFailureKeepsResponseUniform(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		account:  Admin{ID: "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327", Username: "admin", PasswordHash: string(passwordHash), Role: "admin", Status: "active"},
		auditErr: errors.New("audit write failed"),
	}
	handler := NewHandler(store, 12*time.Hour, false)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(`{"username":"admin","password":"wrong-password"}`))

	handler.Login(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 despite audit failure, got %d", recorder.Code)
	}
	if store.createdTokenHash != "" {
		t.Fatal("failed login must not create a session")
	}
}

func TestLogoutRecordsAuditEvent(t *testing.T) {
	store := &fakeStore{sessionAccount: Admin{
		ID:       "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327",
		Username: "admin",
		Role:     "admin",
		Status:   "active",
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	protected := handler.RequireAuthentication(http.HandlerFunc(handler.Logout))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "raw-session-token"})

	protected.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", recorder.Code)
	}
	if len(store.auditEvents) != 1 {
		t.Fatalf("audit events = %d, want 1", len(store.auditEvents))
	}
	event := store.auditEvents[0]
	if event.EventType != AdminAuthEventLogoutSucceeded || event.Result != AdminAuthResultSuccess || event.ReasonCode != AdminAuthReasonNone {
		t.Fatalf("unexpected logout audit event: %+v", event)
	}
	if event.AdminID == nil || *event.AdminID != store.sessionAccount.ID {
		t.Fatalf("logout audit admin id = %#v", event.AdminID)
	}
}
