package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// fakeSecurityStore implements SecurityStore in memory for handler tests.
type fakeSecurityStore struct {
	reauthAt       time.Time
	reauthSet      bool
	setReauthHash  string
	setReauthAt    time.Time
	setReauthEvent AdminAuthAuditEvent
	sessionMissing bool

	updatedHash   string
	keptTokenHash string
	passwordEvent AdminAuthAuditEvent

	replacedBatch  string
	replacedHashes [][]byte
	codeStatus     RecoveryCodeBatchStatus

	resetAdminID  string
	resetCodeHash []byte
	resetNewHash  string
	resetEvent    AdminAuthAuditEvent
	resetErr      error

	emailRecord  AdminRecoveryEmailRecord
	listedEvents []AdminAuthAuditSummaryEntry
}

func (s *fakeSecurityStore) SessionReauthAt(context.Context, string) (time.Time, bool, error) {
	if s.sessionMissing {
		return time.Time{}, false, ErrNotFound
	}
	return s.reauthAt, s.reauthSet, nil
}

func (s *fakeSecurityStore) SetSessionReauth(_ context.Context, tokenHash string, at time.Time, event AdminAuthAuditEvent) error {
	s.setReauthHash = tokenHash
	s.setReauthAt = at
	s.setReauthEvent = event
	return nil
}

func (s *fakeSecurityStore) UpdatePasswordKeepSession(_ context.Context, _ string, newHash string, keepTokenHash string, _ time.Time, event AdminAuthAuditEvent) error {
	s.updatedHash = newHash
	s.keptTokenHash = keepTokenHash
	s.passwordEvent = event
	return nil
}

func (s *fakeSecurityStore) ReplaceRecoveryCodes(_ context.Context, _ string, batchID string, hashes [][]byte, _ AdminAuthAuditEvent) error {
	s.replacedBatch = batchID
	s.replacedHashes = hashes
	return nil
}

func (s *fakeSecurityStore) RecoveryCodeStatus(context.Context, string) (RecoveryCodeBatchStatus, error) {
	return s.codeStatus, nil
}

func (s *fakeSecurityStore) ResetPasswordWithRecoveryCode(_ context.Context, adminID string, codeHash []byte, newHash string, event AdminAuthAuditEvent) error {
	if s.resetErr != nil {
		return s.resetErr
	}
	s.resetAdminID = adminID
	s.resetCodeHash = codeHash
	s.resetNewHash = newHash
	s.resetEvent = event
	return nil
}

func (s *fakeSecurityStore) RecoveryEmail(context.Context, string) (AdminRecoveryEmailRecord, error) {
	return s.emailRecord, nil
}

func (s *fakeSecurityStore) UpsertPendingRecoveryEmail(context.Context, string, []byte, string) error {
	return nil
}

func (s *fakeSecurityStore) CreateEmailCode(context.Context, string, string, string, time.Time) error {
	return nil
}

func (s *fakeSecurityStore) ConsumeEmailCode(context.Context, string, string, string) error {
	return ErrNoUsableCode
}

func (s *fakeSecurityStore) MarkRecoveryEmailVerified(context.Context, string, AdminAuthAuditEvent) error {
	return nil
}

func (s *fakeSecurityStore) ResetPasswordWithEmailCode(context.Context, string, string, string, AdminAuthAuditEvent) error {
	return ErrNoUsableCode
}

func (s *fakeSecurityStore) ListAuthEvents(context.Context, string, int) ([]AdminAuthAuditSummaryEntry, error) {
	return s.listedEvents, nil
}

var testRecoveryHMACKey = bytes.Repeat([]byte{0x42}, 32)

func securedTestHandler(store *fakeStore, security *fakeSecurityStore) *Handler {
	handler := NewHandler(store, 12*time.Hour, false)
	handler.ConfigureSecurity(security, testRecoveryHMACKey, nil, nil)
	handler.random = rand.Reader
	return handler
}

func authedRequest(method string, path string, body string, account Admin, token string) *http.Request {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("{}")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	return request.WithContext(ContextWithAdmin(request.Context(), account))
}

func testOwnerAccount(t *testing.T, password string) Admin {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return Admin{
		ID:           "08aca962-9c62-4ec6-a5b1-8684ba612343",
		Username:     "owneruser",
		PasswordHash: string(hash),
		Role:         "owner",
		Status:       "active",
	}
}

// --- recovery code primitives ---

func TestGenerateRecoveryCodeFormatAndUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		code, err := generateRecoveryCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		groups := strings.Split(code, "-")
		if len(groups) != recoveryCodeGroups {
			t.Fatalf("expected %d groups, got %q", recoveryCodeGroups, code)
		}
		for _, group := range groups {
			if len(group) != recoveryCodeGroupLen {
				t.Fatalf("bad group length in %q", code)
			}
			for _, r := range group {
				if !strings.ContainsRune(recoveryCodeCharset, r) {
					t.Fatalf("character %q outside charset in %q", r, code)
				}
			}
		}
		if seen[code] {
			t.Fatalf("duplicate code generated: %q", code)
		}
		seen[code] = true
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	valid, err := generateRecoveryCode(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	normalized := normalizeRecoveryCode(valid)
	if len(normalized) != recoveryCodeGroups*recoveryCodeGroupLen {
		t.Fatalf("normalize of generated code failed: %q", normalized)
	}
	if got := normalizeRecoveryCode(strings.ToLower(strings.ReplaceAll(valid, "-", " "))); got != normalized {
		t.Fatalf("lowercase/space form should normalize identically: %q vs %q", got, normalized)
	}
	for _, bad := range []string{"", "SHORT", strings.Repeat("A", 19), strings.Repeat("A", 21), strings.Repeat("0", 20), strings.Repeat("I", 20)} {
		if normalizeRecoveryCode(bad) != "" {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}

func TestHashRecoveryCodeDomainAndKeySeparation(t *testing.T) {
	normalized := strings.Repeat("A", 20)
	first := hashRecoveryCode(testRecoveryHMACKey, normalized)
	second := hashRecoveryCode(testRecoveryHMACKey, normalized)
	if !bytes.Equal(first, second) {
		t.Fatal("hash must be deterministic")
	}
	if len(first) != 32 {
		t.Fatalf("expected raw 32-byte digest, got %d", len(first))
	}
	otherKey := bytes.Repeat([]byte{0x43}, 32)
	if bytes.Equal(first, hashRecoveryCode(otherKey, normalized)) {
		t.Fatal("different keys must produce different digests")
	}
	if hashEmailCode(testRecoveryHMACKey, "bind", "123456") == hashEmailCode(testRecoveryHMACKey, "reset", "123456") {
		t.Fatal("email code purposes must be domain separated")
	}
}

func TestGenerateEmailCodeShape(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := generateEmailCode(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
			t.Fatalf("expected 6 digits, got %q", code)
		}
	}
}

func TestValidateAdminPassword(t *testing.T) {
	if err := validateAdminPassword("short", "user"); err == nil {
		t.Fatal("short password must be rejected")
	}
	if err := validateAdminPassword(strings.Repeat("x", 129), "user"); err == nil {
		t.Fatal("overlong password must be rejected")
	}
	if err := validateAdminPassword("Owneruser1", "owneruser1"); err == nil {
		t.Fatal("password equal to username must be rejected")
	}
	if err := validateAdminPassword("long-enough-password", "user"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
}

// --- reauth ---

func TestReauthStampsSessionAndAudits(t *testing.T) {
	account := testOwnerAccount(t, "correct-password")
	security := &fakeSecurityStore{}
	handler := securedTestHandler(&fakeStore{account: account}, security)

	recorder := httptest.NewRecorder()
	request := authedRequest(http.MethodPost, "/api/admin/reauth", `{"password":"correct-password"}`, account, "session-token")
	handler.Reauth(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if security.setReauthHash != hashToken("session-token") {
		t.Fatal("reauth must stamp the current session by token hash")
	}
	if security.setReauthEvent.EventType != AdminAuthEventReauthSucceeded {
		t.Fatalf("expected success audit, got %s", security.setReauthEvent.EventType)
	}
}

func TestReauthWrongPasswordFailsAndAudits(t *testing.T) {
	account := testOwnerAccount(t, "correct-password")
	store := &fakeStore{account: account}
	security := &fakeSecurityStore{}
	handler := securedTestHandler(store, security)

	recorder := httptest.NewRecorder()
	request := authedRequest(http.MethodPost, "/api/admin/reauth", `{"password":"wrong-password"}`, account, "session-token")
	handler.Reauth(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if security.setReauthHash != "" {
		t.Fatal("failed reauth must not stamp the session")
	}
	if len(store.auditEvents) != 1 || store.auditEvents[0].EventType != AdminAuthEventReauthFailed {
		t.Fatalf("expected one reauth-failed audit event, got %+v", store.auditEvents)
	}
}

func TestReauthRateLimited(t *testing.T) {
	account := testOwnerAccount(t, "correct-password")
	store := &fakeStore{account: account}
	handler := securedTestHandler(store, &fakeSecurityStore{})

	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		handler.Reauth(recorder, authedRequest(http.MethodPost, "/api/admin/reauth", `{"password":"wrong-password"}`, account, "session-token"))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.Reauth(recorder, authedRequest(http.MethodPost, "/api/admin/reauth", `{"password":"correct-password"}`, account, "session-token"))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated failures, got %d", recorder.Code)
	}
}

func TestRequireRecentReauthFreshAndStale(t *testing.T) {
	account := testOwnerAccount(t, "pw-irrelevant")
	base := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name       string
		reauthAt   time.Time
		reauthSet  bool
		wantStatus int
	}{
		{"fresh", base.Add(-9 * time.Minute), true, http.StatusOK},
		{"stale", base.Add(-11 * time.Minute), true, http.StatusForbidden},
		{"never", time.Time{}, false, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			security := &fakeSecurityStore{reauthAt: test.reauthAt, reauthSet: test.reauthSet}
			handler := securedTestHandler(&fakeStore{account: account}, security)
			handler.now = func() time.Time { return base }

			called := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			recorder := httptest.NewRecorder()
			handler.RequireRecentReauth(next).ServeHTTP(recorder, authedRequest(http.MethodPost, "/x", "", account, "session-token"))

			if recorder.Code != test.wantStatus {
				t.Fatalf("expected %d, got %d", test.wantStatus, recorder.Code)
			}
			if (test.wantStatus == http.StatusOK) != called {
				t.Fatalf("next called=%v for status %d", called, test.wantStatus)
			}
			if test.wantStatus == http.StatusForbidden && !strings.Contains(recorder.Body.String(), reauthRequiredMessage) {
				t.Fatalf("stale reauth must answer %q, got %s", reauthRequiredMessage, recorder.Body.String())
			}
		})
	}
}

func TestRequireOwnerRejectsPlainAdmin(t *testing.T) {
	handler := securedTestHandler(&fakeStore{}, &fakeSecurityStore{})
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	adminAccount := testOwnerAccount(t, "x")
	adminAccount.Role = "admin"
	recorder := httptest.NewRecorder()
	handler.RequireOwner(next).ServeHTTP(recorder, authedRequest(http.MethodGet, "/x", "", adminAccount, "session-token"))
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("plain admin should get 403, got %d", recorder.Code)
	}

	ownerAccount := testOwnerAccount(t, "x")
	recorder = httptest.NewRecorder()
	handler.RequireOwner(next).ServeHTTP(recorder, authedRequest(http.MethodGet, "/x", "", ownerAccount, "session-token"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("owner should pass, got %d", recorder.Code)
	}
}

func TestMutatingMatchers(t *testing.T) {
	get := httptest.NewRequest(http.MethodGet, "/api/admin/imports/abc/revert", nil)
	post := httptest.NewRequest(http.MethodPost, "/api/admin/imports/abc/revert", nil)
	detail := httptest.NewRequest(http.MethodGet, "/api/admin/imports/abc", nil)
	if MutatingSuffixMatch("/revert")(get) {
		t.Fatal("GET must never match")
	}
	if !MutatingSuffixMatch("/revert")(post) {
		t.Fatal("POST …/revert must match")
	}
	if MutatingSuffixMatch("/revert")(detail) {
		t.Fatal("detail read must not match")
	}
	if MutatingMatch(get) || !MutatingMatch(post) {
		t.Fatal("MutatingMatch must gate only non-read methods")
	}
}

// --- change password ---

func TestChangePasswordSuccessRevokesOtherSessions(t *testing.T) {
	account := testOwnerAccount(t, "old-password-1")
	security := &fakeSecurityStore{}
	handler := securedTestHandler(&fakeStore{account: account}, security)

	recorder := httptest.NewRecorder()
	body := `{"current_password":"old-password-1","new_password":"brand-new-password-1"}`
	handler.ChangePassword(recorder, authedRequest(http.MethodPost, "/api/admin/security/password", body, account, "session-token"))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if security.keptTokenHash != hashToken("session-token") {
		t.Fatal("current session must be the surviving session")
	}
	if bcrypt.CompareHashAndPassword([]byte(security.updatedHash), []byte("brand-new-password-1")) != nil {
		t.Fatal("stored hash must verify against the new password")
	}
	if security.passwordEvent.EventType != AdminAuthEventPasswordChanged {
		t.Fatalf("expected password-changed audit, got %s", security.passwordEvent.EventType)
	}
}

func TestChangePasswordRejectsWrongCurrentAndWeakNew(t *testing.T) {
	account := testOwnerAccount(t, "old-password-1")
	security := &fakeSecurityStore{}
	handler := securedTestHandler(&fakeStore{account: account}, security)

	recorder := httptest.NewRecorder()
	handler.ChangePassword(recorder, authedRequest(http.MethodPost, "/x", `{"current_password":"wrong","new_password":"brand-new-password-1"}`, account, "session-token"))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password: expected 401, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.ChangePassword(recorder, authedRequest(http.MethodPost, "/x", `{"current_password":"old-password-1","new_password":"short"}`, account, "session-token"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("weak new password: expected 400, got %d", recorder.Code)
	}
	if security.updatedHash != "" {
		t.Fatal("no password may be stored on failure")
	}
}

// --- owner recovery codes ---

func TestOwnerRecoveryCodesGenerateReturnsBatchOnce(t *testing.T) {
	account := testOwnerAccount(t, "x")
	security := &fakeSecurityStore{}
	handler := securedTestHandler(&fakeStore{account: account}, security)

	recorder := httptest.NewRecorder()
	handler.OwnerRecoveryCodes(recorder, authedRequest(http.MethodPost, "/api/admin/owner/recovery-codes", "", account, "session-token"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Codes []string `json:"codes"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Codes) != RecoveryCodeBatchSize {
		t.Fatalf("expected %d codes, got %d", RecoveryCodeBatchSize, len(response.Codes))
	}
	if len(security.replacedHashes) != RecoveryCodeBatchSize {
		t.Fatalf("expected %d stored hashes, got %d", RecoveryCodeBatchSize, len(security.replacedHashes))
	}
	for i, code := range response.Codes {
		if !bytes.Equal(security.replacedHashes[i], hashRecoveryCode(testRecoveryHMACKey, normalizeRecoveryCode(code))) {
			t.Fatalf("stored hash %d does not match the returned code", i)
		}
		if len(security.replacedHashes[i]) != 32 {
			t.Fatal("stored value must be a raw 32-byte digest, never plaintext")
		}
	}
}

func TestOwnerRecoveryCodesUnavailableWithoutKey(t *testing.T) {
	account := testOwnerAccount(t, "x")
	handler := NewHandler(&fakeStore{account: account}, 12*time.Hour, false)
	handler.ConfigureSecurity(&fakeSecurityStore{}, nil, nil, nil)

	recorder := httptest.NewRecorder()
	handler.OwnerRecoveryCodes(recorder, authedRequest(http.MethodGet, "/x", "", account, "session-token"))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without HMAC key, got %d", recorder.Code)
	}
}

// --- unauthenticated recovery-code reset ---

func TestRecoveryCodeResetSuccessNeverIssuesSession(t *testing.T) {
	account := testOwnerAccount(t, "old-password-1")
	store := &fakeStore{account: account}
	security := &fakeSecurityStore{}
	handler := securedTestHandler(store, security)

	code, err := generateRecoveryCode(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"username":"owneruser","recovery_code":"` + code + `","new_password":"fresh-new-password-1"}`
	recorder := httptest.NewRecorder()
	handler.RecoveryCodeReset(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/recovery/code-reset", strings.NewReader(body)))

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(recorder.Result().Cookies()) != 0 {
		t.Fatal("recovery must never set a session cookie")
	}
	if !bytes.Equal(security.resetCodeHash, hashRecoveryCode(testRecoveryHMACKey, normalizeRecoveryCode(code))) {
		t.Fatal("reset must consume exactly the presented code")
	}
	if bcrypt.CompareHashAndPassword([]byte(security.resetNewHash), []byte("fresh-new-password-1")) != nil {
		t.Fatal("stored hash must verify against the new password")
	}
	if security.resetEvent.EventType != AdminAuthEventRecoveryCodeResetSucceeded {
		t.Fatalf("expected success audit, got %s", security.resetEvent.EventType)
	}
}

func TestRecoveryCodeResetInvalidCodeIsGenericAndAudited(t *testing.T) {
	account := testOwnerAccount(t, "old-password-1")
	store := &fakeStore{account: account}
	security := &fakeSecurityStore{resetErr: ErrNoUsableCode}
	handler := securedTestHandler(store, security)

	code, err := generateRecoveryCode(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"username":"owneruser","recovery_code":"` + code + `","new_password":"fresh-new-password-1"}`
	recorder := httptest.NewRecorder()
	handler.RecoveryCodeReset(recorder, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid username or recovery code") {
		t.Fatalf("must answer the generic message, got %s", recorder.Body.String())
	}
	if len(store.auditEvents) != 1 || store.auditEvents[0].EventType != AdminAuthEventRecoveryCodeResetFailed {
		t.Fatalf("expected one failure audit event, got %+v", store.auditEvents)
	}
}

func TestRecoveryCodeResetUnknownUserSameAnswer(t *testing.T) {
	store := &fakeStore{findAccountError: ErrNotFound}
	handler := securedTestHandler(store, &fakeSecurityStore{})

	body := `{"username":"ghost","recovery_code":"ABCDE-FGHJK-MNPQR-STVWX","new_password":"fresh-new-password-1"}`
	recorder := httptest.NewRecorder()
	handler.RecoveryCodeReset(recorder, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid username or recovery code") {
		t.Fatalf("unknown user must get the same generic answer, got %s", recorder.Body.String())
	}
}

func TestRecoveryCodeResetRateLimited(t *testing.T) {
	account := testOwnerAccount(t, "old-password-1")
	store := &fakeStore{account: account}
	handler := securedTestHandler(store, &fakeSecurityStore{resetErr: ErrNoUsableCode})

	body := `{"username":"owneruser","recovery_code":"ABCDE-FGHJK-MNPQR-STVWX","new_password":"fresh-new-password-1"}`
	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		handler.RecoveryCodeReset(recorder, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.RecoveryCodeReset(recorder, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after repeated failures, got %d", recorder.Code)
	}
}

// --- email recovery disabled state ---

func TestEmailRecoveryDisabledAnswersExplicit503(t *testing.T) {
	account := testOwnerAccount(t, "x")
	handler := securedTestHandler(&fakeStore{account: account}, &fakeSecurityStore{})

	recorder := httptest.NewRecorder()
	handler.RecoveryEmailBindRequest(recorder, authedRequest(http.MethodPost, "/x", `{"email":"a@b.example"}`, account, "session-token"))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("bind request without sender: expected 503, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.RecoveryEmailResetRequest(recorder, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"username":"owneruser"}`)))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("reset request without sender: expected 503, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.RecoveryEmailStatus(recorder, authedRequest(http.MethodGet, "/x", "", account, "session-token"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status must still answer 200, got %d", recorder.Code)
	}
	var status struct {
		DeliveryEnabled bool `json:"delivery_enabled"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.DeliveryEnabled {
		t.Fatal("delivery must report disabled")
	}
}

// --- audit vocabulary stays in lockstep with migration 0022 ---

func TestMigration0022ContainsEveryAuditEnumMember(t *testing.T) {
	sqlBytes, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0022_admin_owner_security.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(sqlBytes)

	events := []AdminAuthEventType{
		AdminAuthEventLoginSucceeded, AdminAuthEventLoginFailed, AdminAuthEventLoginRateLimited, AdminAuthEventLogoutSucceeded,
		AdminAuthEventReauthSucceeded, AdminAuthEventReauthFailed, AdminAuthEventPasswordChanged,
		AdminAuthEventRecoveryEmailBound, AdminAuthEventRecoveryEmailBindFailed,
		AdminAuthEventRecoveryCodesGenerated,
		AdminAuthEventRecoveryCodeResetSucceeded, AdminAuthEventRecoveryCodeResetFailed,
		AdminAuthEventRecoveryEmailResetSucceeded, AdminAuthEventRecoveryEmailResetFailed,
		AdminAuthEventOwnerPromoted, AdminAuthEventOwnerCLIPasswordReset,
	}
	for _, event := range events {
		if !validAdminAuthEventType(event) {
			t.Fatalf("event %s must be valid in Go", event)
		}
		if !strings.Contains(sql, "'"+string(event)+"'") {
			t.Fatalf("migration 0022 is missing event type %s", event)
		}
	}

	reasons := []AdminAuthReasonCode{
		AdminAuthReasonNone, AdminAuthReasonInvalidCredentials, AdminAuthReasonAccountDisabled, AdminAuthReasonRateLimited,
		AdminAuthReasonDatabaseError, AdminAuthReasonAuditWriteError,
		AdminAuthReasonInvalidRecoveryCode, AdminAuthReasonInvalidVerificationCode, AdminAuthReasonVerificationCodeExpired,
		AdminAuthReasonRecoveryEmailNotConfigured, AdminAuthReasonEmailDeliveryDisabled,
		AdminAuthReasonWeakPassword, AdminAuthReasonNotOwner, AdminAuthReasonValidationFailed,
	}
	for _, reason := range reasons {
		if !validAdminAuthReason(reason) {
			t.Fatalf("reason %s must be valid in Go", reason)
		}
		if !strings.Contains(sql, "'"+string(reason)+"'") {
			t.Fatalf("migration 0022 is missing reason code %s", reason)
		}
	}

	for _, objectName := range []string{
		"admins_role_check",
		"admins_single_owner_unique",
		"admins_protect_last_owner",
		"admin_recovery_emails",
		"admin_recovery_email_codes",
		"admin_recovery_codes",
		"reauth_at",
	} {
		if !strings.Contains(sql, objectName) {
			t.Fatalf("migration 0022 is missing object %s", objectName)
		}
	}
}
