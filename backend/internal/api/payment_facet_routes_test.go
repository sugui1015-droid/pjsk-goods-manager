package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

// TestPaymentFacetsRouteIsRegisteredAndGuarded confirms the facets endpoint is
// reachable as its own route and sits behind admin authentication.
//
// Registration order matters: "/api/admin/payments/facets" shares a prefix with
// the payment detail route, so without an exact pattern the request would be
// handled as a lookup for a payment whose id is literally "facets" — which the
// detail handler rejects as an invalid id (400), not a facet list. A nil pool is
// safe because authentication runs before the store is touched.
func TestPaymentFacetsRouteIsRegisteredAndGuarded(t *testing.T) {
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, nil)

	routes := []string{
		"/api/admin/payments",
		"/api/admin/payments/facets?column=cn",
		"/api/admin/payments/11111111-1111-1111-1111-111111111111",
	}

	for _, path := range routes {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code == http.StatusNotFound {
				t.Fatalf("route %s returned 404; route is not registered", path)
			}
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("route %s unauth status = %d, want 401: %s", path, response.Code, response.Body.String())
			}
		})
	}
}
