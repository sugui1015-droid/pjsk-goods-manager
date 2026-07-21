package query

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/clientip"
)

func doLoginFrom(t *testing.T, handler *Handler, remoteAddr string, forwardedFor string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/query/login", strings.NewReader(`{"cn":"victim","query_code":"wrong"}`))
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	recorder := httptest.NewRecorder()
	handler.Login(recorder, request)
	return recorder.Code
}

// The injected resolver's key must be what the in-memory limiter tracks:
// with a constant key, failures from different peers accumulate into one
// IP+CN block.
func TestInjectedResolverKeyDrivesLoginLimiter(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)
	handler.ConfigureClientIPResolver(func(*http.Request) string { return "injected-fixed-key" })

	for i := 0; i < newLoginLimiter().maxFailures; i++ {
		if code := doLoginFrom(t, handler, "10.0.0."+strconv.Itoa(1+i)+":50000", ""); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, code)
		}
	}
	if code := doLoginFrom(t, handler, "192.0.2.99:50000", ""); code != http.StatusTooManyRequests {
		t.Fatalf("after maxFailures failures on the injected key, status = %d, want 429", code)
	}
}

// Without trusted proxies (the NewHandler default), a spoofed
// X-Forwarded-For must not let a client escape its per-IP failure block.
func TestSpoofedForwardedForCannotEscapeLoginBlock(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)

	for i := 0; i < newLoginLimiter().maxFailures; i++ {
		if code := doLoginFrom(t, handler, "203.0.113.7:50000", ""); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, code)
		}
	}
	if code := doLoginFrom(t, handler, "203.0.113.7:50000", "198.51.100.9"); code != http.StatusTooManyRequests {
		t.Fatal("spoofed X-Forwarded-For escaped the per-IP+CN block")
	}
}

// The recovery request handler must pass the resolver's canonical key to the
// service, so the existing IdentifierHash("ip", ...) input stays stable.
func TestRecoveryRequestUsesResolverKey(t *testing.T) {
	service := &queryCodeRecoveryStub{}
	handler := NewHandler(stubStore{}, time.Hour, false)
	handler.ConfigureQueryCodeRecovery(service)

	request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/request", strings.NewReader(`{"cn":"TEST"}`))
	request.RemoteAddr = "::ffff:203.0.113.10"
	request.Header.Set("X-Forwarded-For", "1.2.3.4")
	response := httptest.NewRecorder()
	handler.RequestQueryCodeRecovery(response, request)

	if service.requestIP != "203.0.113.10" {
		t.Fatalf("service received %q, want unmapped peer 203.0.113.10", service.requestIP)
	}
}

// A malformed RemoteAddr must land in the shared unknown-client bucket
// instead of an empty string, so recovery is rate limited rather than
// silently failing on an empty identifier.
func TestInvalidRemoteAddrUsesUnknownClientBucket(t *testing.T) {
	service := &queryCodeRecoveryStub{}
	handler := NewHandler(stubStore{}, time.Hour, false)
	handler.ConfigureQueryCodeRecovery(service)

	request := httptest.NewRequest(http.MethodPost, "/api/query/recovery/request", strings.NewReader(`{"cn":"TEST"}`))
	request.RemoteAddr = "not-a-real-address"
	response := httptest.NewRecorder()
	handler.RequestQueryCodeRecovery(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("public recovery response changed: %d", response.Code)
	}
	if service.requestIP != clientip.UnknownClientKey {
		t.Fatalf("service received %q, want %q", service.requestIP, clientip.UnknownClientKey)
	}
}

// Router-style injection with a trusted loopback proxy: the limiter must key
// on the forwarded client, not on the shared proxy address, so one abusive
// client behind the proxy cannot block another.
func TestTrustedProxyResolverSeparatesClients(t *testing.T) {
	resolver := clientip.NewResolver([]netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})
	handler := NewHandler(stubStore{}, time.Hour, false)
	handler.ConfigureClientIPResolver(func(r *http.Request) string { return resolver.Resolve(r).Key() })

	for i := 0; i < newLoginLimiter().maxFailures; i++ {
		if code := doLoginFrom(t, handler, "127.0.0.1:50000", "203.0.113.10"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i, code)
		}
	}
	if code := doLoginFrom(t, handler, "127.0.0.1:50000", "203.0.113.10"); code != http.StatusTooManyRequests {
		t.Fatal("abusive forwarded client was not blocked")
	}
	if code := doLoginFrom(t, handler, "127.0.0.1:50000", "198.51.100.9"); code != http.StatusUnauthorized {
		t.Fatal("a different forwarded client was blocked by the abusive one")
	}
}
