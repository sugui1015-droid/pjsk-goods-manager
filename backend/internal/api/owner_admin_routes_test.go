package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

// TestOwnerAdminManagementRoutesAreRegisteredAndGuarded confirms every owner
// admin-management endpoint exists and rejects unauthenticated requests with
// 401. A nil pool is safe because authentication runs before the store is
// touched. Owner-only and reauth gating are covered by the admin package's
// middleware-chain tests; this test pins the router wiring itself.
func TestOwnerAdminManagementRoutesAreRegisteredAndGuarded(t *testing.T) {
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, nil)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/owner/admins"},
		{http.MethodPost, "/api/admin/owner/admins"},
		{http.MethodGet, "/api/admin/owner/admins/11111111-1111-1111-1111-111111111111"},
		{http.MethodPost, "/api/admin/owner/admins/11111111-1111-1111-1111-111111111111/enable"},
		{http.MethodPost, "/api/admin/owner/admins/11111111-1111-1111-1111-111111111111/disable"},
		{http.MethodPost, "/api/admin/owner/admins/11111111-1111-1111-1111-111111111111/revoke"},
		{http.MethodPost, "/api/admin/owner/admins/11111111-1111-1111-1111-111111111111/reset-password"},
		{http.MethodGet, "/api/admin/owner/admins/11111111-1111-1111-1111-111111111111/audit"},
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
