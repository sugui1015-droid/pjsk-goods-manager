package admin

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type fakeManagementStore struct {
	admins        map[string]ManagedAdmin
	appointInput  AppointAdminInput
	appointEvent  AdminAuthAuditEvent
	appointErr    error
	statusTarget  string
	statusValue   string
	statusEvent   AdminAuthAuditEvent
	statusErr     error
	revokeTarget  string
	revokeActor   string
	revokeEvent   AdminAuthAuditEvent
	revokeErr     error
	resetTarget   string
	resetHash     string
	resetEvent    AdminAuthAuditEvent
	resetErr      error
	auditRequests []string
}

func (s *fakeManagementStore) ListManagedAdmins(context.Context) ([]ManagedAdmin, error) {
	entries := make([]ManagedAdmin, 0, len(s.admins))
	for _, entry := range s.admins {
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *fakeManagementStore) GetManagedAdmin(_ context.Context, adminID string) (ManagedAdmin, error) {
	entry, ok := s.admins[adminID]
	if !ok {
		return ManagedAdmin{}, ErrNotFound
	}
	return entry, nil
}

func (s *fakeManagementStore) AppointAdmin(_ context.Context, input AppointAdminInput, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	s.appointInput = input
	s.appointEvent = event
	if s.appointErr != nil {
		return ManagedAdmin{}, s.appointErr
	}
	return ManagedAdmin{ID: "new-admin-id", Username: input.Username, Role: "admin", Status: "active", MustChangePassword: true}, nil
}

func (s *fakeManagementStore) SetManagedAdminStatus(_ context.Context, adminID string, status string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	s.statusTarget, s.statusValue, s.statusEvent = adminID, status, event
	if s.statusErr != nil {
		return ManagedAdmin{}, s.statusErr
	}
	entry := s.admins[adminID]
	entry.Status = status
	return entry, nil
}

func (s *fakeManagementStore) RevokeManagedAdmin(_ context.Context, adminID string, actorID string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	s.revokeTarget, s.revokeActor, s.revokeEvent = adminID, actorID, event
	if s.revokeErr != nil {
		return ManagedAdmin{}, s.revokeErr
	}
	entry := s.admins[adminID]
	entry.Status = "revoked"
	return entry, nil
}

func (s *fakeManagementStore) ResetManagedAdminPassword(_ context.Context, adminID string, newHash string, event AdminAuthAuditEvent) (ManagedAdmin, error) {
	s.resetTarget, s.resetHash, s.resetEvent = adminID, newHash, event
	if s.resetErr != nil {
		return ManagedAdmin{}, s.resetErr
	}
	entry := s.admins[adminID]
	entry.MustChangePassword = true
	return entry, nil
}

func (s *fakeManagementStore) ListManagedAdminAudit(_ context.Context, adminID string, _ int) ([]AdminAuthAuditSummaryEntry, error) {
	s.auditRequests = append(s.auditRequests, adminID)
	return []AdminAuthAuditSummaryEntry{}, nil
}

// managementTestEnv wires the full production middleware chain the router
// uses, backed by fakes, so tests exercise authentication, owner gating, and
// the reauth wall exactly as deployed.
func managementTestEnv(sessionAccount Admin, reauthFresh bool) (*fakeStore, *fakeSecurityStore, *fakeManagementStore, http.Handler, http.Handler) {
	store := &fakeStore{sessionAccount: sessionAccount}
	if sessionAccount.ID == "" {
		store.findSessionError = ErrNotFound
	}
	security := &fakeSecurityStore{}
	if reauthFresh {
		security.reauthSet = true
		security.reauthAt = time.Now()
	}
	management := &fakeManagementStore{admins: map[string]ManagedAdmin{
		"target-1": {ID: "target-1", Username: "helper", Role: "admin", Status: "active"},
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	handler.ConfigureSecurity(security, testRecoveryHMACKey, nil, nil)
	handler.ConfigureManagement(management)
	handler.random = rand.Reader

	collection := handler.RequireAuthentication(handler.RequireOwner(handler.RequireRecentReauthWhen(
		MutatingMatch, http.HandlerFunc(handler.ManagementCollection))))
	item := handler.RequireAuthentication(handler.RequireOwner(handler.RequireRecentReauthWhen(
		MutatingMatch, http.HandlerFunc(handler.ManagementItem))))
	return store, security, management, collection, item
}

func sessionRequest(method string, path string, body string) *http.Request {
	var reader = strings.NewReader(body)
	request := httptest.NewRequest(method, path, reader)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	return request
}

func ownerAccount() Admin {
	return Admin{ID: "08aca962-9c62-4ec6-a5b1-8684ba612343", Username: "owneruser", Role: "owner", Status: "active"}
}

func plainAdminAccount() Admin {
	return Admin{ID: "5f0c9dd2-58a5-4f3b-9d3e-0d31c1a6e001", Username: "helper", Role: "admin", Status: "active"}
}

func TestManagementAnonymousIsUnauthorized(t *testing.T) {
	_, _, _, collection, item := managementTestEnv(Admin{}, false)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/admin/owner/admins", nil),
		httptest.NewRequest(http.MethodPost, "/api/admin/owner/admins", strings.NewReader("{}")),
		httptest.NewRequest(http.MethodPost, "/api/admin/owner/admins/target-1/disable", strings.NewReader("{}")),
	} {
		recorder := httptest.NewRecorder()
		target := collection
		if strings.Contains(request.URL.Path, "target-1") {
			target = item
		}
		target.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: expected 401, got %d", request.Method, request.URL.Path, recorder.Code)
		}
	}
}

func TestManagementPlainAdminIsForbidden(t *testing.T) {
	_, _, management, collection, item := managementTestEnv(plainAdminAccount(), true)

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/admin/owner/admins"},
		{http.MethodPost, "/api/admin/owner/admins"},
		{http.MethodPost, "/api/admin/owner/admins/target-1/enable"},
		{http.MethodPost, "/api/admin/owner/admins/target-1/revoke"},
		{http.MethodGet, "/api/admin/owner/admins/target-1/audit"},
	} {
		recorder := httptest.NewRecorder()
		request := sessionRequest(tc.method, tc.path, "{}")
		target := collection
		if strings.Contains(tc.path, "target-1") {
			target = item
		}
		target.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s %s: expected 403 for plain admin, got %d", tc.method, tc.path, recorder.Code)
		}
	}
	if management.statusTarget != "" || management.revokeTarget != "" {
		t.Fatal("store must never be reached by a non-owner")
	}
}

func TestManagementMutationRequiresFreshReauth(t *testing.T) {
	_, _, management, collection, item := managementTestEnv(ownerAccount(), false)

	recorder := httptest.NewRecorder()
	collection.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins",
		`{"user_id":"u-1","username":"newadmin"}`))
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "reauth_required") {
		t.Fatalf("expected reauth_required 403, got %d: %s", recorder.Code, recorder.Body.String())
	}

	recorder = httptest.NewRecorder()
	item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/disable", "{}"))
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "reauth_required") {
		t.Fatalf("expected reauth_required 403, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if management.appointInput.Username != "" || management.statusTarget != "" {
		t.Fatal("store must not be reached without fresh reauth")
	}

	// Reads stay available without reauth.
	recorder = httptest.NewRecorder()
	collection.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/owner/admins", ""))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected list to work without reauth, got %d", recorder.Code)
	}
}

func TestAppointAdminReturnsTempPasswordOnce(t *testing.T) {
	_, _, management, collection, _ := managementTestEnv(ownerAccount(), true)

	recorder := httptest.NewRecorder()
	collection.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins",
		`{"user_id":"5c4dbb10-0000-0000-0000-000000000001","username":"NewAdmin","display_name":"帮手"}`))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Admin        ManagedAdmin `json:"admin"`
		TempPassword string       `json:"temp_password"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.TempPassword) < minAdminPasswordLength {
		t.Fatalf("temporary password too short: %d", len(response.TempPassword))
	}
	if management.appointInput.Username != "newadmin" {
		t.Fatalf("username must be normalized to lowercase, got %q", management.appointInput.Username)
	}
	if management.appointInput.PasswordHash == response.TempPassword {
		t.Fatal("plaintext must never reach the store")
	}
	if bcrypt.CompareHashAndPassword([]byte(management.appointInput.PasswordHash), []byte(response.TempPassword)) != nil {
		t.Fatal("stored hash must match the returned temporary password")
	}
	if management.appointEvent.ActorAdminID == nil || *management.appointEvent.ActorAdminID != ownerAccount().ID {
		t.Fatalf("appoint audit must carry the acting owner, got %#v", management.appointEvent.ActorAdminID)
	}
	if management.appointEvent.EventType != AdminAuthEventAdminAppointed {
		t.Fatalf("unexpected event type %q", management.appointEvent.EventType)
	}
}

func TestAppointAdminValidation(t *testing.T) {
	_, _, _, collection, _ := managementTestEnv(ownerAccount(), true)

	for _, body := range []string{
		`{"username":"newadmin"}`,                 // missing user_id
		`{"user_id":"u-1","username":"ab"}`,       // too short
		`{"user_id":"u-1","username":"Bad Name"}`, // space
		`{"user_id":"u-1","username":""}`,
	} {
		recorder := httptest.NewRecorder()
		collection.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins", body))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("body %s: expected 400, got %d", body, recorder.Code)
		}
	}
}

func TestManagementOwnerTargetIsRefused(t *testing.T) {
	store, _, management, _, item := managementTestEnv(ownerAccount(), true)
	management.statusErr = ErrTargetIsOwner
	management.revokeErr = ErrTargetIsOwner
	management.resetErr = ErrTargetIsOwner

	for _, action := range []string{"disable", "enable", "revoke", "reset-password"} {
		recorder := httptest.NewRecorder()
		item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/"+action, "{}"))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("action %s on owner: expected 403, got %d", action, recorder.Code)
		}
	}
	// Refusals are audited as failures with the dedicated reason.
	foundFailure := false
	for _, event := range store.auditEvents {
		if event.Result == AdminAuthResultFailure && event.ReasonCode == AdminAuthReasonTargetIsOwner {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Fatal("owner-target refusal must be audited with target_is_owner")
	}
}

func TestManagementActions(t *testing.T) {
	_, _, management, _, item := managementTestEnv(ownerAccount(), true)

	recorder := httptest.NewRecorder()
	item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/disable",
		`{"reason":"轮岗结束"}`))
	if recorder.Code != http.StatusOK {
		t.Fatalf("disable: expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if management.statusTarget != "target-1" || management.statusValue != "disabled" {
		t.Fatalf("unexpected status call: %q %q", management.statusTarget, management.statusValue)
	}
	if management.statusEvent.ManagementReason == nil || *management.statusEvent.ManagementReason != "轮岗结束" {
		t.Fatalf("reason must reach the audit event, got %#v", management.statusEvent.ManagementReason)
	}
	if management.statusEvent.UsernameNormalized != "helper" {
		t.Fatalf("audit must carry target username, got %q", management.statusEvent.UsernameNormalized)
	}

	recorder = httptest.NewRecorder()
	item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/enable", "{}"))
	if recorder.Code != http.StatusOK || management.statusValue != "active" {
		t.Fatalf("enable failed: %d %q", recorder.Code, management.statusValue)
	}

	recorder = httptest.NewRecorder()
	item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/revoke", "{}"))
	if recorder.Code != http.StatusOK || management.revokeTarget != "target-1" || management.revokeActor != ownerAccount().ID {
		t.Fatalf("revoke failed: %d %q %q", recorder.Code, management.revokeTarget, management.revokeActor)
	}

	recorder = httptest.NewRecorder()
	item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/reset-password", "{}"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("reset-password: expected 200, got %d", recorder.Code)
	}
	var response struct {
		TempPassword string `json:"temp_password"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.TempPassword) < minAdminPasswordLength {
		t.Fatal("reset must return a fresh temporary password once")
	}
	if bcrypt.CompareHashAndPassword([]byte(management.resetHash), []byte(response.TempPassword)) != nil {
		t.Fatal("stored hash must match the returned temporary password")
	}

	recorder = httptest.NewRecorder()
	item.ServeHTTP(recorder, sessionRequest(http.MethodPost, "/api/admin/owner/admins/target-1/promote", "{}"))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("unknown action must 404, got %d", recorder.Code)
	}
}

func TestMustChangePasswordLocksOtherEndpoints(t *testing.T) {
	account := plainAdminAccount()
	account.MustChangePassword = true
	store := &fakeStore{sessionAccount: account}
	handler := NewHandler(store, 12*time.Hour, false)

	protected := handler.RequireAuthentication(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	protected.ServeHTTP(recorder, sessionRequest(http.MethodGet, "/api/admin/payments", ""))
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), passwordChangeRequiredMessage) {
		t.Fatalf("expected password_change_required 403, got %d: %s", recorder.Code, recorder.Body.String())
	}

	for _, path := range []string{"/api/admin/me", "/api/admin/logout", "/api/admin/reauth", "/api/admin/security/password"} {
		recorder := httptest.NewRecorder()
		protected.ServeHTTP(recorder, sessionRequest(http.MethodGet, path, ""))
		if recorder.Code != http.StatusOK {
			t.Fatalf("path %s must stay reachable during forced change, got %d", path, recorder.Code)
		}
	}
}

func TestLoginReportsMustChangePassword(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("temp-password-1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{account: Admin{
		ID:                 "5f0c9dd2-58a5-4f3b-9d3e-0d31c1a6e001",
		Username:           "helper",
		PasswordHash:       string(passwordHash),
		Role:               "admin",
		Status:             "active",
		MustChangePassword: true,
	}}
	handler := NewHandler(store, 12*time.Hour, false)
	handler.random = rand.Reader

	recorder := httptest.NewRecorder()
	handler.Login(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/login",
		strings.NewReader(`{"username":"helper","password":"temp-password-1"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"must_change_password":true`) {
		t.Fatalf("login response must flag the forced change: %s", recorder.Body.String())
	}
}
