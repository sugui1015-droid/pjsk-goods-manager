package query

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/querycoderecovery"
)

// codeStore is a minimal in-memory Store for the query-code normalization and
// lockout-release regressions. Users are keyed by limiterCNKey so lookups are
// case-insensitive, matching the production SQL.
type codeStore struct {
	users   map[string]*User
	bindErr error
}

func newCodeStore() *codeStore { return &codeStore{users: map[string]*User{}} }

func (s *codeStore) addUser(cn string, plainCode string) {
	user := &User{ID: "id-" + cn, CNCode: cn, Status: "active"}
	if plainCode != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(plainCode), bcrypt.MinCost)
		if err != nil {
			panic(err)
		}
		stored := string(hash)
		user.QueryCodeHash = &stored
	}
	s.users[limiterCNKey(cn)] = user
}

func (s *codeStore) FindUserByCN(_ context.Context, cn string) (User, error) {
	user, ok := s.users[limiterCNKey(cn)]
	if !ok {
		return User{}, ErrNotFound
	}
	return *user, nil
}

func (s *codeStore) BindQueryCode(_ context.Context, cn string, _ string, newQueryCodeHash string) error {
	if s.bindErr != nil {
		return s.bindErr
	}
	user, ok := s.users[limiterCNKey(cn)]
	if !ok {
		return ErrBindRejected
	}
	user.QueryCodeHash = &newQueryCodeHash
	return nil
}

func (s *codeStore) CreateSession(context.Context, string, string, time.Time) error { return nil }
func (s *codeStore) FindUserBySession(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}
func (s *codeStore) DeleteSession(context.Context, string) error           { return nil }
func (s *codeStore) ChangeQueryCode(context.Context, string, string) error { return nil }
func (s *codeStore) ListOrdersForUser(context.Context, string) (OrdersResponse, error) {
	return OrdersResponse{}, nil
}

// newCodeHandler wires a handler whose rate-limit key is taken verbatim from
// the X-Test-IP header, so tests can drive distinct client IPs directly.
func newCodeHandler(store Store) *Handler {
	handler := NewHandler(store, time.Hour, false)
	handler.ConfigureClientIPResolver(func(r *http.Request) string { return r.Header.Get("X-Test-IP") })
	return handler
}

func postAs(handler http.HandlerFunc, ip string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	request.Header.Set("X-Test-IP", ip)
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func jsonBody(fields map[string]string) string {
	encoded, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func login(h *Handler, ip string, cn string, code string) *httptest.ResponseRecorder {
	return postAs(h.Login, ip, jsonBody(map[string]string{"cn": cn, "query_code": code}))
}

func bind(h *Handler, ip string, cn string, token string, code string) *httptest.ResponseRecorder {
	return postAs(h.BindCode, ip, jsonBody(map[string]string{
		"cn": cn, "bind_token": token, "new_query_code": code, "confirm_query_code": code,
	}))
}

// TestLoginTrimsSurroundingWhitespaceOnlyOnQueryCode is the core regression:
// every write path stored a trimmed code while login compared the raw request
// value, so a single trailing space from a mobile keyboard rejected a code the
// user had just successfully set. Interior spaces are part of the secret and
// must survive.
func TestLoginTrimsSurroundingWhitespaceOnlyOnQueryCode(t *testing.T) {
	tests := []struct {
		name      string
		stored    string
		submitted string
		wantCode  int
	}{
		{"trailing space accepted", "abc123", "abc123 ", http.StatusOK},
		{"leading space accepted", "abc123", " abc123", http.StatusOK},
		{"surrounding whitespace accepted", "abc123", "\t abc123 \n", http.StatusOK},
		{"exact match still accepted", "abc123", "abc123", http.StatusOK},
		{"interior space preserved", "ab c123", "ab c123", http.StatusOK},
		{"interior space not stripped", "ab c123", "abc123", http.StatusUnauthorized},
		{"case is not folded", "abc123", "ABC123", http.StatusUnauthorized},
		{"wrong code still rejected", "abc123", "abc124", http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newCodeStore()
			store.addUser("CN001", test.stored)
			handler := newCodeHandler(store)
			if got := login(handler, "203.0.113.1", "CN001", test.submitted).Code; got != test.wantCode {
				t.Fatalf("status = %d, want %d", got, test.wantCode)
			}
		})
	}
}

// TestBindCodeReleasesLoginBlockForSameClient reproduces the reported
// lockout: the user fails login enough times to get blocked, then sets a new
// query code with a valid bind token. The new code must work immediately —
// the block came from the code that no longer exists.
func TestBindCodeReleasesLoginBlockForSameClient(t *testing.T) {
	store := newCodeStore()
	store.addUser("CN001", "") // no query code yet: first-time bind flow
	handler := newCodeHandler(store)
	const ip = "203.0.113.1"

	for i := 0; i < handler.limiter.maxFailures; i++ {
		if got := login(handler, ip, "CN001", "guess-000").Code; got != http.StatusUnauthorized {
			t.Fatalf("failure %d: status = %d, want 401", i, got)
		}
	}
	if got := login(handler, ip, "CN001", "guess-000").Code; got != http.StatusTooManyRequests {
		t.Fatalf("client was not blocked after maxFailures: status = %d", got)
	}

	if got := bind(handler, ip, "CN001", "BINDTOKEN1", "NewCode-123").Code; got != http.StatusOK {
		t.Fatalf("bind status = %d, want 200", got)
	}

	response := login(handler, ip, "CN001", "NewCode-123")
	if response.Code == http.StatusTooManyRequests {
		t.Fatal("new query code still hit 尝试次数过多 immediately after a successful bind")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("login with the just-bound code: status = %d, want 200", response.Code)
	}
	if got := login(handler, ip, "CN001", "guess-000").Code; got != http.StatusUnauthorized {
		t.Fatalf("old code should be rejected on its merits: status = %d, want 401", got)
	}
}

// A failed write must leave the block exactly where it was: only a committed
// query-code write is proof of identity.
func TestFailedBindDoesNotReleaseLoginBlock(t *testing.T) {
	store := newCodeStore()
	store.addUser("CN001", "")
	store.bindErr = ErrBindRejected
	handler := newCodeHandler(store)
	const ip = "203.0.113.1"

	for i := 0; i < handler.limiter.maxFailures; i++ {
		login(handler, ip, "CN001", "guess-000")
	}
	if got := bind(handler, ip, "CN001", "WRONGTOKEN", "NewCode-123").Code; got != http.StatusUnauthorized {
		t.Fatalf("bind status = %d, want 401", got)
	}
	if got := login(handler, ip, "CN001", "NewCode-123").Code; got != http.StatusTooManyRequests {
		t.Fatalf("a rejected bind released the login block: status = %d, want 429", got)
	}
}

// Releasing must be scoped to exactly one IP+CN pair.
func TestReleaseIsScopedToOneClientAndCN(t *testing.T) {
	store := newCodeStore()
	store.addUser("CN001", "")
	store.addUser("CN002", "other-code-1")
	handler := newCodeHandler(store)
	const bindingIP = "203.0.113.1"
	const otherIP = "198.51.100.9"

	// Block CN002 from the same IP, and CN001 from a different IP.
	for i := 0; i < handler.limiter.maxFailures; i++ {
		login(handler, bindingIP, "CN002", "guess-000")
		login(handler, otherIP, "CN001", "guess-000")
	}

	if got := bind(handler, bindingIP, "CN001", "BINDTOKEN1", "NewCode-123").Code; got != http.StatusOK {
		t.Fatalf("bind status = %d, want 200", got)
	}

	if got := login(handler, bindingIP, "CN002", "other-code-1").Code; got != http.StatusTooManyRequests {
		t.Fatalf("another user's block was cleared from the same IP: status = %d, want 429", got)
	}
	if got := login(handler, otherIP, "CN001", "NewCode-123").Code; got != http.StatusTooManyRequests {
		t.Fatalf("the same CN's block on a different IP was cleared: status = %d, want 429", got)
	}
}

// The admin reset hook clears a CN across IPs — the admin's own address says
// nothing about where the user will log in from — but must not touch any
// other account.
func TestReleaseLoginLockForCNIsScopedToThatCN(t *testing.T) {
	store := newCodeStore()
	store.addUser("CN001", "code-abc-1")
	store.addUser("CN002", "code-abc-2")
	handler := newCodeHandler(store)
	// Distinct IPs per user, so this asserts on the CN-scoped block itself
	// rather than on the shared per-IP attempt gate.
	const firstIP = "203.0.113.1"
	const secondIP = "198.51.100.9"

	for i := 0; i < handler.limiter.maxFailures; i++ {
		login(handler, firstIP, "CN001", "guess-000")
		login(handler, secondIP, "CN002", "guess-000")
	}

	handler.ReleaseLoginLockForCN("cn001") // case-insensitive, as the DB lookup is

	if got := login(handler, firstIP, "CN001", "code-abc-1").Code; got != http.StatusOK {
		t.Fatalf("admin reset did not lift the login block: status = %d, want 200", got)
	}
	if got := login(handler, secondIP, "CN002", "code-abc-2").Code; got != http.StatusTooManyRequests {
		t.Fatalf("admin reset for one CN cleared another CN: status = %d, want 429", got)
	}
}

// Varying the case of a CN must neither dodge an active block nor split it,
// since the account lookup itself is case-insensitive.
func TestLoginBlockIsCaseInsensitiveOnCN(t *testing.T) {
	store := newCodeStore()
	store.addUser("CN001", "code-abc-1")
	handler := newCodeHandler(store)
	const ip = "203.0.113.1"

	for i := 0; i < handler.limiter.maxFailures; i++ {
		login(handler, ip, "CN001", "guess-000")
	}
	if got := login(handler, ip, "cn001", "guess-000").Code; got != http.StatusTooManyRequests {
		t.Fatalf("case-varied CN escaped the block: status = %d, want 429", got)
	}
}

// Whatever else changes, the plaintext query code must never reach a log line
// or a response body.
func TestQueryCodePlaintextNeverReachesLogsOrResponses(t *testing.T) {
	const secret = "Sup3rSecret-Code"
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })

	store := newCodeStore()
	store.addUser("CN001", "")
	handler := newCodeHandler(store)
	const ip = "203.0.113.1"

	bodies := []string{
		bind(handler, ip, "CN001", "BINDTOKEN1", secret).Body.String(),
		login(handler, ip, "CN001", secret).Body.String(),
		login(handler, ip, "CN001", secret+"-wrong").Body.String(),
	}
	for _, body := range bodies {
		if strings.Contains(body, secret) {
			t.Fatalf("response body leaked the query code: %s", body)
		}
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("logs leaked the query code: %s", logs.String())
	}
}

// The email-recovery reset is a third write path into the same query code, so
// it must lift the login block exactly like bind and change do — and only
// when the reset actually succeeded.
func TestRecoveryResetReleasesLoginBlockOnlyOnSuccess(t *testing.T) {
	for _, test := range []struct {
		name       string
		serviceErr error
		wantStatus int
	}{
		{"successful reset releases the block", nil, http.StatusOK},
		{"rejected reset keeps the block", querycoderecovery.ErrRejected, http.StatusTooManyRequests},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newCodeStore()
			store.addUser("CN001", "code-abc-1")
			handler := newCodeHandler(store)
			handler.ConfigureQueryCodeRecovery(&queryCodeRecoveryStub{resetCN: "CN001", resetErr: test.serviceErr})
			const ip = "203.0.113.1"

			for i := 0; i < handler.limiter.maxFailures; i++ {
				login(handler, ip, "CN001", "guess-000")
			}
			postAs(handler.ResetRecoveredQueryCode, ip, jsonBody(map[string]string{
				"reset_token":        strings.Repeat("a", 43),
				"new_query_code":     "code-abc-1",
				"confirm_query_code": "code-abc-1",
			}))

			if got := login(handler, ip, "CN001", "code-abc-1").Code; got != test.wantStatus {
				t.Fatalf("login after reset: status = %d, want %d", got, test.wantStatus)
			}
		})
	}
}
