package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

func TestFeedbackRoutesAreRegisteredAndGuarded(t *testing.T) {
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, nil)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/query/feedbacks"},
		{http.MethodGet, "/api/admin/feedbacks"},
		{http.MethodPatch, "/api/admin/feedbacks/11111111-1111-1111-1111-111111111111/status"},
	}
	for _, current := range cases {
		t.Run(current.method+" "+current.path, func(t *testing.T) {
			request := httptest.NewRequest(current.method, current.path, nil)
			// A regular-user cookie must never grant access to the admin surface.
			if current.path != "/api/query/feedbacks" {
				request.AddCookie(&http.Cookie{Name: "pjsk_query_session", Value: "regular-user-token"})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code == http.StatusNotFound {
				t.Fatalf("route returned 404; route is not registered")
			}
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status = %d, want 401: %s", response.Code, response.Body.String())
			}
		})
	}
}
