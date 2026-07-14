package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/config"
	"pjsk/backend/internal/querycoderecovery"
)

func TestQueryCodeRecoveryAnonymousRoutesAreRegistered(t *testing.T) {
	router := NewRouter(config.Config{}, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/request", strings.NewReader(`{"cn":"TEST"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "如果该账号符合找回条件") {
		t.Fatal("anonymous recovery request route was not safely registered")
	}

	verify := httptest.NewRequest(http.MethodPost, "/api/query/recovery/verify", strings.NewReader(`{"cn":"TEST","code":"123456"}`))
	verifyResponse := httptest.NewRecorder()
	router.ServeHTTP(verifyResponse, verify)
	if verifyResponse.Code != http.StatusServiceUnavailable {
		t.Fatal("unconfigured recovery verification route did not fail safely")
	}

	manager, err := querycoderecovery.NewManager(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	token, err := manager.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	reset := httptest.NewRequest(http.MethodPost, "/api/query/recovery/reset", strings.NewReader(`{"reset_token":"`+token+`","new_query_code":"NewCode-123","confirm_query_code":"NewCode-123"}`))
	resetResponse := httptest.NewRecorder()
	router.ServeHTTP(resetResponse, reset)
	if resetResponse.Code != http.StatusServiceUnavailable {
		t.Fatal("unconfigured recovery reset route did not fail safely")
	}
}
