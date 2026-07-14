package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/clientip"
)

func newLoginTestHandler(t *testing.T, store Store) *Handler {
	t.Helper()
	handler := NewHandler(store, 12*time.Hour, false)
	handler.now = func() time.Time { return limiterEpoch }
	return handler
}

func activeAdminStore(t *testing.T) *fakeStore {
	t.Helper()
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeStore{account: Admin{
		ID:           "7ae45d7e-a7b7-4ab4-b4ca-b61a41371327",
		Username:     "admin",
		PasswordHash: string(passwordHash),
		Role:         "admin",
		Status:       "active",
	}}
}

func doAdminLogin(handler *Handler, remoteAddr string, username string, password string, forwardedFor string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(loginRequest{Username: username, Password: password})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(body))
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	recorder := httptest.NewRecorder()
	handler.Login(recorder, request)
	return recorder
}

func captureAdminLog(t *testing.T, action func()) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	action()
	return buffer.String()
}

func TestAdminLoginFailureBlockThenRecovery(t *testing.T) {
	handler := newLoginTestHandler(t, activeAdminStore(t))

	for i := 0; i < 4; i++ {
		if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "wrong-password", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("failure %d: status = %d, want 401", i+1, code)
		}
	}
	if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "wrong-password", "").Code; code != http.StatusUnauthorized {
		t.Fatalf("fifth failure itself must still return 401, got %d", code)
	}
	// The pair is now blocked: even the correct password is rejected.
	response := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked pair status = %d, want 429", response.Code)
	}

	// Other usernames and other IPs are unaffected.
	if code := doAdminLogin(handler, "203.0.113.7:50000", "other-admin", "wrong-password", "").Code; code != http.StatusUnauthorized {
		t.Fatalf("other username on the same IP: %d, want 401", code)
	}
	if code := doAdminLogin(handler, "203.0.113.8:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
		t.Fatalf("same username from another IP: %d, want 200", code)
	}

	// The block lifts after blockDuration.
	handler.now = func() time.Time { return limiterEpoch.Add(11 * time.Minute) }
	if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
		t.Fatalf("after the block window: %d, want 200", code)
	}
}

func TestAdminLoginUnknownUsernameCountsLikeWrongPassword(t *testing.T) {
	store := &fakeStore{findAccountError: ErrNotFound}
	handler := newLoginTestHandler(t, store)

	var lastBody string
	for i := 0; i < 5; i++ {
		response := doAdminLogin(handler, "203.0.113.7:50000", "ghost", "any-password", "")
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("unknown username attempt %d: %d, want 401", i+1, response.Code)
		}
		lastBody = response.Body.String()
	}
	if !strings.Contains(lastBody, "invalid username or password") {
		t.Fatalf("unknown-username response diverged from wrong-password response: %s", lastBody)
	}
	if code := doAdminLogin(handler, "203.0.113.7:50000", "ghost", "any-password", "").Code; code != http.StatusTooManyRequests {
		t.Fatalf("unknown username must hit the same block rule, got %d", code)
	}
}

func TestAdminLoginUsernameNormalizationSharesLimiterKey(t *testing.T) {
	handler := newLoginTestHandler(t, activeAdminStore(t))

	variants := []string{"admin", " ADMIN ", "Admin", "aDmIn", "  admin"}
	for i, variant := range variants {
		if code := doAdminLogin(handler, "203.0.113.7:50000", variant, "wrong-password", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("variant %d %q: %d, want 401", i, variant, code)
		}
	}
	if code := doAdminLogin(handler, "203.0.113.7:50000", "ADMIN", "correct-password", "").Code; code != http.StatusTooManyRequests {
		t.Fatal("case/whitespace variants must share one limiter key")
	}
}

func TestAdminLoginSuccessClearsOwnFailuresOnly(t *testing.T) {
	handler := newLoginTestHandler(t, activeAdminStore(t))

	for i := 0; i < 4; i++ {
		doAdminLogin(handler, "203.0.113.7:50000", "admin", "wrong-password", "")
		doAdminLogin(handler, "203.0.113.8:50000", "admin", "wrong-password", "")
	}
	if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
		t.Fatal("correct password before the threshold must succeed")
	}
	// Cleared pair: four more failures still return 401, not 429.
	for i := 0; i < 4; i++ {
		if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "wrong-password", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("post-clear failure %d: %d, want 401", i+1, code)
		}
	}
	// The other IP kept its four failures: one more blocks it.
	doAdminLogin(handler, "203.0.113.8:50000", "admin", "wrong-password", "")
	if code := doAdminLogin(handler, "203.0.113.8:50000", "admin", "correct-password", "").Code; code != http.StatusTooManyRequests {
		t.Fatal("success on one IP must not clear another IP's failures")
	}
}

func TestAdminLoginPerIPRequestCap(t *testing.T) {
	handler := newLoginTestHandler(t, activeAdminStore(t))

	for i := 0; i < 20; i++ {
		if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
			t.Fatalf("attempt %d within the per-IP window: %d, want 200", i+1, code)
		}
	}
	response := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "")
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("21st attempt: %d, want 429", response.Code)
	}
	if code := doAdminLogin(handler, "203.0.113.9:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
		t.Fatal("a different IP must not share the per-IP window")
	}
	handler.now = func() time.Time { return limiterEpoch.Add(61 * time.Second) }
	if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
		t.Fatal("the per-IP window must reset after a minute")
	}
}

func TestAdminLoginRateLimitResponseIsOpaque(t *testing.T) {
	store := &fakeStore{findAccountError: ErrNotFound}
	handler := newLoginTestHandler(t, store)

	var response *httptest.ResponseRecorder
	logged := captureAdminLog(t, func() {
		for i := 0; i < 5; i++ {
			doAdminLogin(handler, "203.0.113.7:50000", "SecretUser", "SecretPass-123", "")
		}
		response = doAdminLogin(handler, "203.0.113.7:50000", "SecretUser", "SecretPass-123", "")
	})

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.Code)
	}
	body := response.Body.String()
	for _, forbidden := range []string{"SecretUser", "SecretPass-123", "203.0.113.7", "ghost", "exists", "not found", "blocked until"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("429 body leaked %q: %s", forbidden, body)
		}
	}
	if retryAfter := response.Header().Get("Retry-After"); retryAfter != "" {
		t.Fatalf("Retry-After is deliberately not set (matches query login), got %q", retryAfter)
	}
	for _, forbidden := range []string{"SecretUser", "SecretPass-123", "203.0.113.7"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("log leaked %q: %q", forbidden, logged)
		}
	}
}

func TestAdminLoginDatabaseErrorIsNotAPasswordFailure(t *testing.T) {
	store := activeAdminStore(t)
	handler := newLoginTestHandler(t, store)

	store.findAccountError = errors.New("dial tcp 10.99.88.77:5432: SENSITIVE-DB-DETAIL")
	var logged string
	for i := 0; i < 10; i++ {
		var response *httptest.ResponseRecorder
		logged = captureAdminLog(t, func() {
			response = doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "")
		})
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("db error attempt %d: %d, want 500", i+1, response.Code)
		}
	}
	if strings.Contains(logged, "SENSITIVE-DB-DETAIL") || strings.Contains(logged, "10.99.88.77") {
		t.Fatalf("db error log leaked details: %q", logged)
	}
	if !strings.Contains(logged, "find admin for login:") {
		t.Fatalf("expected categorized db error log, got %q", logged)
	}

	// After the outage the pair must not be blocked by those 500s.
	store.findAccountError = nil
	if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "").Code; code != http.StatusOK {
		t.Fatal("database errors were wrongly counted as login failures")
	}
}

func TestAdminLoginInvalidRemoteAddrSharesUnknownBucket(t *testing.T) {
	store := &fakeStore{findAccountError: ErrNotFound}
	handler := newLoginTestHandler(t, store)

	addresses := []string{"", "not-an-address", "bad host", "%%%", "still-bad"}
	for i, remoteAddr := range addresses {
		if code := doAdminLogin(handler, remoteAddr, "admin", "wrong", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("invalid RemoteAddr %d: %d, want 401", i, code)
		}
	}
	// Five failures across distinct malformed peers share the unknown bucket.
	if code := doAdminLogin(handler, "also-invalid", "admin", "wrong", "").Code; code != http.StatusTooManyRequests {
		t.Fatal("malformed peers must share one unknown-client failure bucket")
	}
}

func TestAdminLoginIgnoresSpoofedForwardedForByDefault(t *testing.T) {
	store := &fakeStore{findAccountError: ErrNotFound}
	handler := newLoginTestHandler(t, store)

	for i := 0; i < 5; i++ {
		spoof := "198.51.100." + string(rune('1'+i))
		if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "wrong", spoof).Code; code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i+1, code)
		}
	}
	if code := doAdminLogin(handler, "203.0.113.7:50000", "admin", "wrong", "198.51.100.9").Code; code != http.StatusTooManyRequests {
		t.Fatal("spoofed X-Forwarded-For escaped the per-IP+username block")
	}
}

func TestAdminLoginTrustedProxySeparatesForwardedClients(t *testing.T) {
	store := &fakeStore{findAccountError: ErrNotFound}
	handler := newLoginTestHandler(t, store)
	resolver := clientip.NewResolver([]netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")})
	handler.ConfigureClientIPResolver(func(r *http.Request) string { return resolver.Resolve(r).Key() })

	for i := 0; i < 5; i++ {
		if code := doAdminLogin(handler, "127.0.0.1:50000", "admin", "wrong", "203.0.113.10").Code; code != http.StatusUnauthorized {
			t.Fatalf("client A attempt %d: %d, want 401", i+1, code)
		}
	}
	if code := doAdminLogin(handler, "127.0.0.1:50000", "admin", "wrong", "203.0.113.10").Code; code != http.StatusTooManyRequests {
		t.Fatal("client A was not blocked behind the trusted proxy")
	}
	if code := doAdminLogin(handler, "127.0.0.1:50000", "admin", "wrong", "198.51.100.9").Code; code != http.StatusUnauthorized {
		t.Fatal("client B was blocked by client A's failures")
	}
}

func TestAdminLoginSuccessKeepsSessionAndCookieBehavior(t *testing.T) {
	store := activeAdminStore(t)
	handler := newLoginTestHandler(t, store)
	handler.random = bytes.NewReader(bytes.Repeat([]byte{7}, 64))

	response := doAdminLogin(handler, "203.0.113.7:50000", "admin", "correct-password", "")
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", response.Code, response.Body.String())
	}
	if store.createdAdminID != store.account.ID || len(store.createdTokenHash) != 64 {
		t.Fatal("session creation changed")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName || !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie attributes changed: %+v", cookie)
	}
}
