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

	"pjsk/backend/internal/querycoderecovery"
)

type queryCodeRecoveryStub struct {
	requestErr error
	verifyErr  error
	resetErr   error
	resetCN    string
	verified   querycoderecovery.VerifiedCode
	requestCN  string
	requestIP  string
	resetCalls int
}

func (s *queryCodeRecoveryStub) Request(_ context.Context, cn string, ip string) error {
	s.requestCN, s.requestIP = cn, ip
	return s.requestErr
}

func (s *queryCodeRecoveryStub) Verify(context.Context, string, string) (querycoderecovery.VerifiedCode, error) {
	return s.verified, s.verifyErr
}

func (s *queryCodeRecoveryStub) Reset(context.Context, string, string, string) (string, error) {
	s.resetCalls++
	if s.resetErr != nil {
		return "", s.resetErr
	}
	return s.resetCN, nil
}

func TestQueryCodeRecoveryRequestAlwaysUsesUnifiedPublicResponse(t *testing.T) {
	for _, service := range []*queryCodeRecoveryStub{
		{},
		{requestErr: querycoderecovery.ErrNotEligible},
		{requestErr: querycoderecovery.ErrRateLimited},
		{requestErr: querycoderecovery.ErrDeliveryFailed},
	} {
		handler := NewHandler(stubStore{}, time.Hour, false)
		handler.ConfigureQueryCodeRecovery(service)
		request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/request", strings.NewReader(`{"cn":"  Test CN  "}`))
		request.RemoteAddr = "10.1.2.3:40000"
		response := httptest.NewRecorder()
		handler.RequestQueryCodeRecovery(response, request)
		if response.Code != http.StatusOK || response.Body.String() != `{"success":true,"message":"如果该账号符合找回条件，验证码将发送至已登记邮箱。"}`+"\n" {
			t.Fatal("public recovery response varied by internal account state")
		}
		for _, forbidden := range []string{"email", "user_id", "eligible", "verified", "disabled", "merged"} {
			if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
				t.Fatal("public recovery response exposed internal account state")
			}
		}
		if service.requestCN == "" || service.requestIP != "10.1.2.3" {
			t.Fatal("request did not use normalized transport identity boundary")
		}
	}
}

func TestQueryCodeRecoveryVerifyUsesUnifiedRejection(t *testing.T) {
	for _, rejection := range []error{querycoderecovery.ErrRejected, querycoderecovery.ErrCodeMismatch, querycoderecovery.ErrAttemptsExhausted} {
		handler := NewHandler(stubStore{}, time.Hour, false)
		handler.ConfigureQueryCodeRecovery(&queryCodeRecoveryStub{verifyErr: rejection})
		request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/verify", strings.NewReader(`{"cn":"TEST","code":"123456"}`))
		response := httptest.NewRecorder()
		handler.VerifyQueryCodeRecovery(response, request)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), queryCodeRecoveryRejectMessage) {
			t.Fatal("verification rejection exposed an internal state distinction")
		}
	}
}

func TestQueryCodeRecoveryVerifyReturnsTokenOnlyOnSuccess(t *testing.T) {
	manager, err := querycoderecovery.NewManager(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(stubStore{}, time.Hour, false)
	handler.ConfigureQueryCodeRecovery(&queryCodeRecoveryStub{verified: querycoderecovery.VerifiedCode{ResetToken: token, ExpiresAt: time.Now().Add(time.Minute)}})
	request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/verify", strings.NewReader(`{"cn":"TEST","code":"123456"}`))
	response := httptest.NewRecorder()
	handler.VerifyQueryCodeRecovery(response, request)
	if response.Code != http.StatusOK {
		t.Fatal("verification success did not return 200")
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reset_token"] == "" || payload["expires_at"] == "" {
		t.Fatal("verification success omitted one-time reset capability")
	}
	for _, forbidden := range []string{"user_id", "recovery_email_id", "email", "code_hash", "token_hash"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatal("verification success exposed an internal field")
		}
	}
}

func TestResetRecoveredQueryCodeValidationAndMapping(t *testing.T) {
	manager, err := querycoderecovery.NewManager(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		body       string
		serviceErr error
		wantStatus int
	}{
		{name: "invalid token", body: `{"reset_token":"bad","new_query_code":"NewCode-123","confirm_query_code":"NewCode-123"}`, wantStatus: http.StatusBadRequest},
		{name: "mismatch", body: `{"reset_token":"` + token + `","new_query_code":"NewCode-123","confirm_query_code":"OtherCode-123"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid code", body: `{"reset_token":"` + token + `","new_query_code":"bad space","confirm_query_code":"bad space"}`, wantStatus: http.StatusBadRequest},
		{name: "rejected", body: `{"reset_token":"` + token + `","new_query_code":"NewCode-123","confirm_query_code":"NewCode-123"}`, serviceErr: querycoderecovery.ErrRejected, wantStatus: http.StatusBadRequest},
		{name: "same", body: `{"reset_token":"` + token + `","new_query_code":"NewCode-123","confirm_query_code":"NewCode-123"}`, serviceErr: querycoderecovery.ErrSameQueryCode, wantStatus: http.StatusBadRequest},
		{name: "success", body: `{"reset_token":"` + token + `","new_query_code":"NewCode-123","confirm_query_code":"NewCode-123"}`, wantStatus: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &queryCodeRecoveryStub{resetErr: test.serviceErr}
			handler := NewHandler(stubStore{}, time.Hour, false)
			handler.ConfigureQueryCodeRecovery(service)
			request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/reset", strings.NewReader(test.body))
			response := httptest.NewRecorder()
			handler.ResetRecoveredQueryCode(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.Contains(response.Body.String(), "$2") || strings.Contains(response.Body.String(), "user_id") {
				t.Fatal("reset response exposed sensitive storage details")
			}
		})
	}
}

func TestQueryCodeRecoveryUnexpectedErrorIsGeneric(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)
	handler.ConfigureQueryCodeRecovery(&queryCodeRecoveryStub{verifyErr: errors.New("storage failed")})
	request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/verify", strings.NewReader(`{"cn":"TEST","code":"123456"}`))
	response := httptest.NewRecorder()
	handler.VerifyQueryCodeRecovery(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "服务器内部错误") {
		t.Fatal("unexpected verification error was not safely collapsed")
	}
}
