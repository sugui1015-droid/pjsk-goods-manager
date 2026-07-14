package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/config"
)

func postAdminLogin(router http.Handler, remoteAddr string, forwardedFor string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/admin/login", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// The router must wire the shared clientip resolver into the admin handler:
// with no trusted proxies configured, spoofed X-Forwarded-For values cannot
// give an attacker fresh rate-limit identities.
func TestRouterAdminLoginRateLimitIgnoresSpoofedXFF(t *testing.T) {
	pool := newAPITestPool(t)
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, pool)

	body := `{"username":"router-rl-ghost-a","password":"wrong-password"}`
	for i := 0; i < 5; i++ {
		spoof := "198.51.100." + string(rune('1'+i))
		if code := postAdminLogin(router, "203.0.113.77:50000", spoof, body).Code; code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i+1, code)
		}
	}
	response := postAdminLogin(router, "203.0.113.77:50000", "198.51.100.9", body)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked pair via router: %d, want 429", response.Code)
	}
	// The query login limiter is separate state: the same peer is not
	// rate-limited there by admin failures.
	queryResponse := httptest.NewRecorder()
	queryRequest := httptest.NewRequest(http.MethodPost, "/api/query/login", strings.NewReader(`{"cn":"router-rl-ghost","query_code":"wrong-code"}`))
	queryRequest.RemoteAddr = "203.0.113.77:50000"
	router.ServeHTTP(queryResponse, queryRequest)
	if queryResponse.Code == http.StatusTooManyRequests {
		t.Fatal("admin login failures leaked into the query login limiter")
	}
}

// End-to-end against the isolated test database with a real (temporary)
// admin account: successful login works, failures below the threshold are
// cleared by a success, and five failures block the pair even for the
// correct password.
func TestRouterAdminLoginRateLimitEndToEnd(t *testing.T) {
	pool := newAPITestPool(t)
	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, pool)
	prefix := "rl-e2e-" + time.Now().UTC().Format("150405.000000") + "-"
	t.Cleanup(func() { cleanupAPITestAdmin(t, pool, prefix) })

	// Creates the admin and proves a correct login succeeds.
	cookie := loginAPITestAdmin(t, pool, router, prefix)
	if cookie.Value == "" {
		t.Fatal("expected a session cookie from the correct login")
	}

	username := prefix + "admin"
	wrongBody := `{"username":"` + username + `","password":"wrong-password"}`
	rightBody := `{"username":"` + username + `","password":"export-route-password"}`

	// Four failures, then a success: the pair's counter clears.
	for i := 0; i < 4; i++ {
		if code := postAdminLogin(router, "203.0.113.50:50000", "", wrongBody).Code; code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d, want 401", i+1, code)
		}
	}
	if code := postAdminLogin(router, "203.0.113.50:50000", "", rightBody).Code; code != http.StatusOK {
		t.Fatal("correct password below the threshold must succeed")
	}

	// Five fresh failures block the pair even for the correct password.
	for i := 0; i < 5; i++ {
		if code := postAdminLogin(router, "203.0.113.50:50000", "", wrongBody).Code; code != http.StatusUnauthorized {
			t.Fatalf("post-clear failure %d: %d, want 401", i+1, code)
		}
	}
	blocked := postAdminLogin(router, "203.0.113.50:50000", "", rightBody)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked pair: %d, want 429", blocked.Code)
	}
	for _, forbidden := range []string{username, "203.0.113.50", "blocked"} {
		if strings.Contains(blocked.Body.String(), forbidden) {
			t.Fatalf("429 body leaked %q: %s", forbidden, blocked.Body.String())
		}
	}
	// A different client IP still logs in during the block.
	if code := postAdminLogin(router, "203.0.113.51:50000", "", rightBody).Code; code != http.StatusOK {
		t.Fatal("another IP must not be affected by the blocked pair")
	}
}

// With a trusted loopback proxy configured, the router-injected resolver
// must separate forwarded clients for the admin login limiter.
func TestRouterAdminLoginRateLimitHonorsTrustedProxy(t *testing.T) {
	pool := newAPITestPool(t)
	router := NewRouter(config.Config{
		AdminSessionTTL:   12 * time.Hour,
		FrontendOrigins:   []string{"http://localhost:5173"},
		TrustedProxyCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
	}, pool)

	body := `{"username":"router-rl-ghost-b","password":"wrong-password"}`
	for i := 0; i < 5; i++ {
		if code := postAdminLogin(router, "127.0.0.1:50000", "203.0.113.10", body).Code; code != http.StatusUnauthorized {
			t.Fatalf("client A attempt %d: %d, want 401", i+1, code)
		}
	}
	if code := postAdminLogin(router, "127.0.0.1:50000", "203.0.113.10", body).Code; code != http.StatusTooManyRequests {
		t.Fatal("client A behind the trusted proxy was not blocked")
	}
	if code := postAdminLogin(router, "127.0.0.1:50000", "198.51.100.9", body).Code; code != http.StatusUnauthorized {
		t.Fatal("client B was blocked by client A's failures")
	}
}
