package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubStore struct{}

func (stubStore) FindUserByCN(context.Context, string) (User, error) { return User{}, ErrNotFound }
func (stubStore) CreateSession(context.Context, string, string, time.Time) error {
	return nil
}
func (stubStore) FindUserBySession(context.Context, string) (User, error) {
	return User{}, ErrNotFound
}
func (stubStore) DeleteSession(context.Context, string) error { return nil }
func (stubStore) ListOrdersForUser(context.Context, string) (OrdersResponse, error) {
	return OrdersResponse{}, nil
}

func TestLoginRateLimitedAfterRepeatedFailures(t *testing.T) {
	handler := NewHandler(stubStore{}, time.Hour, false)

	doLogin := func() int {
		request := httptest.NewRequest(http.MethodPost, "/api/query/login", strings.NewReader(`{"cn":"succ","query_code":"wrong"}`))
		request.RemoteAddr = "10.0.0.1:50000"
		recorder := httptest.NewRecorder()
		handler.Login(recorder, request)
		return recorder.Code
	}

	for i := 0; i < handler.limiter.maxFailures; i++ {
		if code := doLogin(); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, code)
		}
	}
	if code := doLogin(); code != http.StatusTooManyRequests {
		t.Fatalf("blocked attempt: status = %d, want 429", code)
	}
}

func TestNormalizeCN(t *testing.T) {
	tests := map[string]string{
		"  Succ  ":     "Succ",
		"a   b\tc":     "a b c",
		"墓靑(ねこ)  neko": "墓靑(ねこ) neko",
	}
	for input, want := range tests {
		if got := normalizeCN(input); got != want {
			t.Fatalf("normalizeCN(%q) = %q, want %q", input, got, want)
		}
	}
}
