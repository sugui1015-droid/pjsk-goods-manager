package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

// TestPaymentQRRoutesRequireAuth confirms the payment QR endpoints reject
// unauthenticated requests before any database access. Admin management routes
// require an admin session; user read routes require a query session. A nil
// pool is safe here precisely because authentication is enforced first — the
// request never reaches the store.
func TestPaymentQRRoutesRequireAuth(t *testing.T) {
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, nil)

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/admin/payment-qr"},
		{http.MethodPost, "/api/admin/payment-qr/alipay"},
		{http.MethodPost, "/api/admin/payment-qr/wechat"},
		{http.MethodPost, "/api/admin/payment-qr/alipay/disable"},
		{http.MethodGet, "/api/admin/payment-qr/alipay/image"},
		{http.MethodGet, "/api/query/payment-qr"},
		{http.MethodGet, "/api/query/payment-qr/alipay/image"},
		{http.MethodGet, "/api/query/payment-qr/wechat/image"},
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
