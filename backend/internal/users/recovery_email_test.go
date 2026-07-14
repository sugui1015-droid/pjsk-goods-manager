package users

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/recoveryemail"
)

type recoveryEmailStub struct {
	record        RecoveryEmailRecord
	getErr        error
	mutation      RecoveryEmailMutation
	putErr        error
	unbindChanged bool
	unbindErr     error
	putUserID     string
	putAdminID    string
	putReason     string
	unbindReason  string
}

func (s *recoveryEmailStub) GetRecoveryEmail(context.Context, string) (RecoveryEmailRecord, error) {
	return s.record, s.getErr
}

func (s *recoveryEmailStub) PutRecoveryEmail(_ context.Context, userID string, adminID string, reason string, protected recoveryemail.Protected) (RecoveryEmailMutation, error) {
	s.putUserID = userID
	s.putAdminID = adminID
	s.putReason = reason
	if s.mutation.Record.EncryptedEmail == nil {
		s.mutation.Record = RecoveryEmailRecord{HasRecoveryEmail: true, EncryptedEmail: protected.Encrypted, Status: "pending", UpdatedAt: "2026-07-14T00:00:00Z"}
	}
	if s.mutation.Action == "" {
		s.mutation.Action = "recovery_email_created"
	}
	return s.mutation, s.putErr
}

func (s *recoveryEmailStub) UnbindRecoveryEmail(_ context.Context, _ string, _ string, reason string) (bool, error) {
	s.unbindReason = reason
	return s.unbindChanged, s.unbindErr
}

func TestRecoveryEmailRequiresAdmin(t *testing.T) {
	handler := NewHandler(&stubStore{})
	handler.ConfigureRecoveryEmail(&recoveryEmailStub{}, testRecoveryProtector(t))
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		request := httptest.NewRequest(method, "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a/recovery-email", bytes.NewBufferString(`{}`))
		request.AddCookie(&http.Cookie{Name: "pjsk_query_session", Value: "ordinary-user-session"})
		recorder := httptest.NewRecorder()
		handler.Detail(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401", method, recorder.Code)
		}
	}
}

func TestRecoveryEmailGetReturnsOnlySafeEmptyState(t *testing.T) {
	handler := NewHandler(&stubStore{})
	handler.ConfigureRecoveryEmail(&recoveryEmailStub{}, testRecoveryProtector(t))
	request := authenticatedRequest(http.MethodGet, "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a/recovery-email", bytes.NewBuffer(nil))
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"has_recovery_email":false`) {
		t.Fatalf("status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
	for _, forbidden := range []string{"encrypted_email", "email_lookup_hash", "nonce", "admin_id"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("response contains forbidden field %q", forbidden)
		}
	}
}

func TestRecoveryEmailPutValidatesAndReturnsMaskedState(t *testing.T) {
	store := &recoveryEmailStub{}
	handler := NewHandler(&stubStore{})
	handler.ConfigureRecoveryEmail(store, testRecoveryProtector(t))
	path := "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a/recovery-email"

	missingReason := authenticatedRequest(http.MethodPut, path, bytes.NewBufferString(`{"email":"admin-test@example.com","reason":" "}`))
	missingReasonResponse := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(missingReasonResponse, missingReason)
	if missingReasonResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status = %d", missingReasonResponse.Code)
	}

	invalid := authenticatedRequest(http.MethodPut, path, bytes.NewBufferString(`{"email":"invalid value","reason":"登记"}`))
	invalidResponse := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid email status = %d", invalidResponse.Code)
	}

	request := authenticatedRequest(http.MethodPut, path, bytes.NewBufferString(`{"email":"Admin-Test@EXAMPLE.COM","reason":"登记"}`))
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if store.putUserID == "" || store.putAdminID != "admin-1" || store.putReason != "登记" {
		t.Fatalf("store call was not scoped to user/admin/reason")
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"masked_email":"A***@example.com"`) || !strings.Contains(body, `"status":"pending"`) {
		t.Fatalf("safe response missing expected state: %s", body)
	}
	for _, forbidden := range []string{"Admin-Test@", "encrypted_email", "email_lookup_hash"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaks forbidden value %q", forbidden)
		}
	}
}

func TestRecoveryEmailDeleteRequiresReasonAndMapsErrors(t *testing.T) {
	path := "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a/recovery-email"
	store := &recoveryEmailStub{unbindChanged: true}
	handler := NewHandler(&stubStore{})
	handler.ConfigureRecoveryEmail(store, testRecoveryProtector(t))

	missing := authenticatedRequest(http.MethodDelete, path, bytes.NewBufferString(`{"reason":""}`))
	missingResponse := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status = %d", missingResponse.Code)
	}

	request := authenticatedRequest(http.MethodDelete, path, bytes.NewBufferString(`{"reason":"用户要求解绑"}`))
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || store.unbindReason != "用户要求解绑" || !strings.Contains(recorder.Body.String(), `"has_recovery_email":false`) {
		t.Fatalf("delete result = %d/%s", recorder.Code, recorder.Body.String())
	}

	store.unbindErr = ErrRecoveryEmailUserMerged
	merged := authenticatedRequest(http.MethodDelete, path, bytes.NewBufferString(`{"reason":"测试"}`))
	mergedResponse := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(mergedResponse, merged)
	if mergedResponse.Code != http.StatusConflict {
		t.Fatalf("merged status = %d", mergedResponse.Code)
	}
}

func TestRecoveryEmailMissingProtectorFailsSafely(t *testing.T) {
	handler := NewHandler(&stubStore{})
	handler.ConfigureRecoveryEmail(&recoveryEmailStub{}, nil)
	request := authenticatedRequest(http.MethodPut, "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a/recovery-email", bytes.NewBufferString(`{"email":"safe@example.org","reason":"登记"}`))
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Detail).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func testRecoveryProtector(t *testing.T) *recoveryemail.Protector {
	t.Helper()
	encryptionKey := make([]byte, 32)
	hmacKey := make([]byte, 48)
	if _, err := rand.Read(encryptionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(hmacKey); err != nil {
		t.Fatal(err)
	}
	protector, err := recoveryemail.NewProtector(encryptionKey, hmacKey)
	if err != nil {
		t.Fatal(err)
	}
	return protector
}

func TestRecoveryEmailMutationErrorMapping(t *testing.T) {
	for storeErr, want := range map[error]int{
		ErrUserNotFound:                http.StatusNotFound,
		ErrRecoveryEmailReason:         http.StatusBadRequest,
		ErrRecoveryEmailUserMerged:     http.StatusConflict,
		errors.New("database failure"): http.StatusInternalServerError,
	} {
		store := &recoveryEmailStub{putErr: storeErr}
		handler := NewHandler(&stubStore{})
		handler.ConfigureRecoveryEmail(store, testRecoveryProtector(t))
		request := authenticatedRequest(http.MethodPut, "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a/recovery-email", bytes.NewBufferString(`{"email":"safe@example.org","reason":"登记"}`))
		recorder := httptest.NewRecorder()
		authenticatedHandler(handler.Detail).ServeHTTP(recorder, request)
		if recorder.Code != want {
			t.Fatalf("error %v status = %d, want %d", storeErr, recorder.Code, want)
		}
	}
}
