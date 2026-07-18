package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

// TestPaymentSubmissionRoutesAreRegisteredAndGuarded confirms every payment-proof
// endpoint exists and sits behind the right authentication. A nil pool is safe
// because auth runs before the store is touched.
//
// The admin facets route shares a prefix with the "/{id}" detail route, so it is
// registered as an exact pattern; without that, "facets" would be read as a
// submission id. The user routes require a query session, the admin routes an
// admin session — an unauthenticated request to either must be 401, never 404
// (404 would mean the route is missing) and never 200 (200 would mean the guard
// is missing).
func TestPaymentSubmissionRoutesAreRegisteredAndGuarded(t *testing.T) {
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, nil)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/query/payment-submissions"},
		{http.MethodPost, "/api/query/payment-submissions"},
		{http.MethodGet, "/api/query/payment-submissions/11111111-1111-1111-1111-111111111111/image"},
		{http.MethodGet, "/api/admin/payment-submissions"},
		{http.MethodGet, "/api/admin/payment-submissions/facets?column=cn"},
		{http.MethodGet, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111"},
		{http.MethodGet, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/image"},
		{http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/reject"},
		{http.MethodPost, "/api/admin/payment-submissions/11111111-1111-1111-1111-111111111111/approve"},
	}

	for _, c := range cases {
		t.Run(c.method+" "+c.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(c.method, c.path, nil))
			if response.Code == http.StatusNotFound {
				t.Fatalf("route %s %s returned 404; route is not registered", c.method, c.path)
			}
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("route %s %s unauth status = %d, want 401: %s", c.method, c.path, response.Code, response.Body.String())
			}
		})
	}
}
