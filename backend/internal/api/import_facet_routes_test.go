package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

// TestImportFacetsRouteIsRegisteredAndGuarded confirms the import-history facets
// endpoint is reachable as its own route and sits behind admin authentication.
//
// The exact "/api/admin/imports/facets" pattern must win over the
// "/api/admin/imports/" detail prefix, otherwise "facets" would be read as an
// import id. A nil pool is safe because authentication runs before the store.
func TestImportFacetsRouteIsRegisteredAndGuarded(t *testing.T) {
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, nil)

	routes := []string{
		"/api/admin/imports",
		"/api/admin/imports/facets?column=filename",
		"/api/admin/imports/11111111-1111-1111-1111-111111111111",
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
