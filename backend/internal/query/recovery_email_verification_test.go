package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/recoveryemailverification"
)

type verificationServiceStub struct {
	sendResult   recoveryemailverification.SendResult
	verifyResult recoveryemailverification.VerifyResult
	sendErr      error
	verifyErr    error
	userID       string
	code         string
}

func (s *verificationServiceStub) Send(_ context.Context, userID string) (recoveryemailverification.SendResult, error) {
	s.userID = userID
	return s.sendResult, s.sendErr
}

func (s *verificationServiceStub) Verify(_ context.Context, userID string, code string) (recoveryemailverification.VerifyResult, error) {
	s.userID, s.code = userID, code
	return s.verifyResult, s.verifyErr
}

func newVerificationHandler(service RecoveryEmailVerificationService) *Handler {
	handler := NewHandler(&changeCodeStore{user: User{ID: "current-user", Status: "active"}}, time.Hour, false)
	handler.ConfigureRecoveryEmailVerification(service)
	return handler
}

func verificationRequest(method string, path string, body string) (*http.Request, *httptest.ResponseRecorder) {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "query-session-placeholder"})
	return request, httptest.NewRecorder()
}

func TestSendRecoveryEmailVerificationSuccessUsesSessionUser(t *testing.T) {
	service := &verificationServiceStub{sendResult: recoveryemailverification.SendResult{
		MaskedEmail: "v***@example.com", ExpiresAt: time.Date(2026, 7, 14, 12, 10, 0, 0, time.UTC), RetryAfterSeconds: 60,
	}}
	handler := newVerificationHandler(service)
	request, response := verificationRequest(http.MethodPost, "/api/query/recovery-email/send-verification?user_id=other", "")
	handler.SendRecoveryEmailVerification(response, request)

	if response.Code != http.StatusOK || service.userID != "current-user" {
		t.Fatalf("status = %d, user = %q", response.Code, service.userID)
	}
	body := response.Body.String()
	for _, forbidden := range []string{"full@example.com", "code_hash", "encrypted_email", "query-session-placeholder"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response contains forbidden field %q", forbidden)
		}
	}
}

func TestRecoveryEmailVerificationRequiresSession(t *testing.T) {
	handler := newVerificationHandler(&verificationServiceStub{})
	for _, call := range []func(http.ResponseWriter, *http.Request){handler.SendRecoveryEmailVerification, handler.VerifyRecoveryEmail} {
		request := httptest.NewRequest(http.MethodPost, "/api/query/recovery-email/verify", strings.NewReader(`{"code":"123456"}`))
		response := httptest.NewRecorder()
		call(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	}
}

func TestVerifyRecoveryEmailValidationAndSuccess(t *testing.T) {
	service := &verificationServiceStub{verifyResult: recoveryemailverification.VerifyResult{
		MaskedEmail: "v***@example.com", VerifiedAt: time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
	}}
	handler := newVerificationHandler(service)

	request, response := verificationRequest(http.MethodPost, "/api/query/recovery-email/verify", `{"code":"bad"}`)
	handler.VerifyRecoveryEmail(response, request)
	if response.Code != http.StatusBadRequest || service.code != "" {
		t.Fatalf("invalid status = %d, service code = %q", response.Code, service.code)
	}

	request, response = verificationRequest(http.MethodPost, "/api/query/recovery-email/verify", `{"code":"123456"}`)
	handler.VerifyRecoveryEmail(response, request)
	if response.Code != http.StatusOK || service.userID != "current-user" || service.code != "123456" {
		t.Fatalf("status = %d, user = %q", response.Code, service.userID)
	}
	var payload recoveryEmailVerificationResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success || payload.Status != "verified" || payload.MaskedEmail == "" || payload.VerifiedAt == "" {
		t.Fatalf("response = %+v", payload)
	}
}

func TestRecoveryEmailVerificationErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "mismatch", err: recoveryemailverification.ErrCodeMismatch, want: http.StatusBadRequest},
		{name: "locked", err: recoveryemailverification.ErrAttemptsExhausted, want: http.StatusTooManyRequests},
		{name: "state", err: recoveryemailverification.ErrNoRecoveryEmail, want: http.StatusConflict},
		{name: "inactive", err: recoveryemailverification.ErrUserInactive, want: http.StatusUnauthorized},
		{name: "delivery", err: recoveryemailverification.ErrDeliveryFailed, want: http.StatusServiceUnavailable},
		{name: "internal", err: errors.New("database failed"), want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newVerificationHandler(&verificationServiceStub{verifyErr: test.err})
			request, response := verificationRequest(http.MethodPost, "/api/query/recovery-email/verify", `{"code":"123456"}`)
			handler.VerifyRecoveryEmail(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}

	handler := newVerificationHandler(&verificationServiceStub{sendErr: &recoveryemailverification.RateLimitError{RetryAfter: 1500 * time.Millisecond}})
	request, response := verificationRequest(http.MethodPost, "/api/query/recovery-email/send-verification", "")
	handler.SendRecoveryEmailVerification(response, request)
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"retry_after_seconds":2`) {
		t.Fatalf("rate response = %d %s", response.Code, response.Body.String())
	}
}
