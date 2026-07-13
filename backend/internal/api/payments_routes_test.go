package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

// TestAdminPaymentAndUserRoutesRequireAuth confirms that every endpoint
// capable of creating, allocating, or voiding a payment — and every CN
// merge endpoint — is unreachable without an authenticated admin session.
// This is a backend-level guarantee independent of any frontend hiding of
// buttons or inputs.
func TestAdminPaymentAndUserRoutesRequireAuth(t *testing.T) {
	pool := newAPITestPool(t)
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, pool)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/payments"},
		{http.MethodPost, "/api/admin/payments"},
		{http.MethodGet, "/api/admin/payments/cn?cn=CN001"},
		{http.MethodGet, "/api/admin/payments/unpaid?cn=CN001"},
		{http.MethodGet, "/api/admin/payments/11111111-1111-1111-1111-111111111111"},
		{http.MethodPost, "/api/admin/payments/11111111-1111-1111-1111-111111111111/void"},
		{http.MethodGet, "/api/admin/users"},
		{http.MethodGet, "/api/admin/users/11111111-1111-1111-1111-111111111111"},
		{http.MethodPost, "/api/admin/users/11111111-1111-1111-1111-111111111111/query-code"},
		{http.MethodPost, "/api/admin/users/11111111-1111-1111-1111-111111111111/query-code-bind-token"},
		{http.MethodPatch, "/api/admin/users/11111111-1111-1111-1111-111111111111/status"},
		{http.MethodGet, "/api/admin/users/merge-preview?source_id=11111111-1111-1111-1111-111111111111&target_cn=CN002"},
		{http.MethodPost, "/api/admin/users/merge"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
			if response.Code == http.StatusNotFound {
				t.Fatalf("route %s %s returned 404; route is not registered", route.method, route.path)
			}
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("route %s %s unauth status = %d, want 401: %s", route.method, route.path, response.Code, response.Body.String())
			}
		})
	}
}

// TestPublicQueryRoutesRejectAdminOnlyPayloads confirms the regular-user
// query endpoints never accept payment-allocation or admin-mutation style
// bodies — the public API surface has no create/void capability at all.
func TestPublicQueryRoutesRejectAdminOnlyPayloads(t *testing.T) {
	pool := newAPITestPool(t)
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, pool)

	adminOnlyPaths := []string{
		"/api/query/payments",
		"/api/query/payments/create",
		"/api/query/payments/void",
	}
	for _, path := range adminOnlyPaths {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("path %s status = %d, want 404 (no such route should exist for regular users)", path, response.Code)
			}
		})
	}
}
