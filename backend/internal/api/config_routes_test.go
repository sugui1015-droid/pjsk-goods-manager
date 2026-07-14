package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pjsk/backend/internal/config"
)

func TestConfigReportsEmailDeliveryAvailability(t *testing.T) {
	for _, test := range []struct {
		mode    string
		enabled bool
	}{{mode: "disabled", enabled: false}, {mode: "smtp", enabled: true}, {mode: "fake", enabled: true}} {
		t.Run(test.mode, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			response := httptest.NewRecorder()
			(&server{config: config.Config{RecoveryEmailSenderMode: test.mode}}).configHandler(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d", response.Code)
			}
			var body appConfigResponse
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.EmailDeliveryEnabled != test.enabled {
				t.Fatalf("email delivery enabled = %t", body.EmailDeliveryEnabled)
			}
		})
	}
}
