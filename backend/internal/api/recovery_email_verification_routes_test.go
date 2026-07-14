package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/config"
)

func TestRecoveryEmailVerificationRoutesRequireQuerySession(t *testing.T) {
	router := NewRouter(config.Config{}, nil)
	tests := []struct {
		path string
		body string
	}{
		{path: "/api/query/recovery-email/send-verification", body: `{}`},
		{path: "/api/query/recovery-email/verify", body: `{"code":"123456"}`},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
		})
	}
}
