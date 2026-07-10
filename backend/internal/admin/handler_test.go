package admin

import (
	"bytes"
	"context"
	"encoding/json"
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
	sessionAccount   Admin
	findSessionError error
	lastSessionHash  string
	deletedTokenHash string
}

func (s *fakeStore) FindByUsername(_ context.Context, username string) (Admin, error) {
	s.lastUsername = username
	return s.account, s.findAccountError
}

func (s *fakeStore) CreateSession(
	_ context.Context,
	adminID string,
	tokenHash string,
	expiresAt time.Time,
) error {
	s.createdAdminID = adminID
	s.createdTokenHash = tokenHash
	s.createdExpiresAt = expiresAt
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
