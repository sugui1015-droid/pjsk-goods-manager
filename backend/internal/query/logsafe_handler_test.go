package query

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// failingStore returns a store error whose text carries markers standing in
// for SQL, parameter values, and user identity — none of which may reach a
// log line.
type failingStore struct {
	stubStore
	err error
}

func (s failingStore) FindUserByCN(context.Context, string) (User, error) {
	return User{}, s.err
}

func captureQueryLog(t *testing.T, action func()) string {
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

func TestLoginStoreErrorLogIsSanitized(t *testing.T) {
	storeErr := &pgconn.PgError{
		Code:    "22P02",
		Message: `invalid input syntax: "SENSITIVE-CN-INPUT"`,
		Detail:  "Key (cn)=(SENSITIVE-CN-INPUT) near SELECT query_code_hash FROM users",
		Where:   "parameter $1 = 'SENSITIVE-QUERY-CODE'",
	}
	handler := NewHandler(failingStore{err: storeErr}, time.Hour, false)

	request := httptest.NewRequest(http.MethodPost, "/api/query/login",
		strings.NewReader(`{"cn":"SENSITIVE-CN-INPUT","query_code":"SENSITIVE-QUERY-CODE"}`))
	request.RemoteAddr = "10.0.0.9:50000"
	response := httptest.NewRecorder()

	logged := captureQueryLog(t, func() {
		handler.Login(response, request)
	})

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("store failure must still map to 500, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "服务器内部错误") {
		t.Fatalf("public response changed: %s", response.Body.String())
	}
	if !strings.Contains(logged, "find query user: database error (SQLSTATE 22P02)") {
		t.Fatalf("expected categorized log line, got %q", logged)
	}
	for _, marker := range []string{
		"SENSITIVE-CN-INPUT",
		"SENSITIVE-QUERY-CODE",
		"SELECT query_code_hash",
		"invalid input syntax",
	} {
		if strings.Contains(logged, marker) {
			t.Fatalf("log leaked %q: %q", marker, logged)
		}
	}
}

func TestLoginTimeoutErrorIsCategorized(t *testing.T) {
	handler := NewHandler(failingStore{err: context.DeadlineExceeded}, time.Hour, false)

	request := httptest.NewRequest(http.MethodPost, "/api/query/login",
		strings.NewReader(`{"cn":"someone","query_code":"whatever"}`))
	request.RemoteAddr = "10.0.0.9:50000"
	response := httptest.NewRecorder()

	logged := captureQueryLog(t, func() {
		handler.Login(response, request)
	})
	if !strings.Contains(logged, "find query user: timeout") {
		t.Fatalf("expected timeout category, got %q", logged)
	}
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("timeout must keep the existing public mapping, got %d", response.Code)
	}
}
