package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newCORSTestHandler(allowedOrigins []string) http.Handler {
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return withCORS(backend, allowedOrigins)
}

func doCORSRequest(t *testing.T, handler http.Handler, method string, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "/api/query/login", nil)
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCORSAllowedOriginGetsExactHeaders(t *testing.T) {
	handler := newCORSTestHandler([]string{"http://localhost:5173"})
	response := doCORSRequest(t, handler, http.MethodPost, "http://localhost:5173")

	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("ACAO = %q, want the exact validated origin", got)
	}
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentialed requests need Access-Control-Allow-Credentials: true")
	}
	if response.Header().Get("Vary") != "Origin" {
		t.Fatalf("Vary = %q, want Origin", response.Header().Get("Vary"))
	}
	if response.Code != http.StatusOK {
		t.Fatalf("business handler did not run: %d", response.Code)
	}
}

func TestCORSDisallowedOriginGetsNoAllowHeaders(t *testing.T) {
	handler := newCORSTestHandler([]string{"http://localhost:5173"})
	for _, origin := range []string{
		"http://evil.example",
		"http://localhost:5173.evil.example",
		"https://localhost:5173",
		"null",
	} {
		response := doCORSRequest(t, handler, http.MethodPost, origin)
		if response.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("origin %q received an ACAO header", origin)
		}
		if response.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("origin %q received a credentials header", origin)
		}
		if response.Header().Get("Vary") != "Origin" {
			t.Fatalf("origin %q: Vary must still be set for cache correctness", origin)
		}
		if response.Code != http.StatusOK {
			t.Fatalf("origin %q: plain request must still reach the handler (browser enforces blocking), got %d", origin, response.Code)
		}
	}
}

func TestCORSNoOriginHeaderIsUntouched(t *testing.T) {
	handler := newCORSTestHandler([]string{"http://localhost:5173"})
	response := doCORSRequest(t, handler, http.MethodGet, "")

	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Vary"} {
		if response.Header().Get(header) != "" {
			t.Fatalf("same-origin request received CORS header %s", header)
		}
	}
	if response.Code != http.StatusOK {
		t.Fatalf("same-origin request blocked: %d", response.Code)
	}
}

func TestCORSPreflightAllowedOrigin(t *testing.T) {
	handler := newCORSTestHandler([]string{"http://localhost:5173"})
	response := doCORSRequest(t, handler, http.MethodOptions, "http://localhost:5173")

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got != corsAllowedMethods {
		t.Fatalf("Allow-Methods = %q, want %q", got, corsAllowedMethods)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != corsAllowedHeaders {
		t.Fatalf("Allow-Headers = %q, want %q", got, corsAllowedHeaders)
	}
	for _, forbidden := range []string{"TRACE", "CONNECT"} {
		if contains := response.Header().Get("Access-Control-Allow-Methods"); len(contains) > 0 && (contains == forbidden) {
			t.Fatalf("preflight advertises %s", forbidden)
		}
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got == "*" {
		t.Fatal("preflight must not allow arbitrary headers")
	}
}

func TestCORSPreflightDisallowedOriginHasNoAllowances(t *testing.T) {
	handler := newCORSTestHandler([]string{"http://localhost:5173"})
	response := doCORSRequest(t, handler, http.MethodOptions, "http://evil.example")

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", response.Code)
	}
	for _, header := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
	} {
		if response.Header().Get(header) != "" {
			t.Fatalf("disallowed preflight received %s", header)
		}
	}
}

func TestCORSVaryAppendsWithoutClobbering(t *testing.T) {
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backend.ServeHTTP(w, r)
	}), []string{"http://localhost:5173"})

	request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	recorder := httptest.NewRecorder()
	recorder.Header().Set("Vary", "Accept-Encoding")
	handler.ServeHTTP(recorder, request)

	values := recorder.Header().Values("Vary")
	joined := ""
	originCount := 0
	for _, value := range values {
		joined += value + ","
	}
	for _, value := range values {
		if value == "Origin" {
			originCount++
		}
	}
	if originCount != 1 {
		t.Fatalf("Vary Origin count = %d in %q, want exactly 1", originCount, joined)
	}
	foundExisting := false
	for _, value := range values {
		if value == "Accept-Encoding" {
			foundExisting = true
		}
	}
	if !foundExisting {
		t.Fatalf("existing Vary value was clobbered: %q", joined)
	}

	// A second pass through appendVaryOrigin must not duplicate Origin.
	appendVaryOrigin(recorder.Header())
	if got := len(recorder.Header().Values("Vary")); got != 2 {
		t.Fatalf("Vary values = %d after repeat append, want 2", got)
	}
}
