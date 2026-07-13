package users

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"

	queryapi "pjsk/backend/internal/query"
)

func TestPostgresAdminQueryAccountLifecycle(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "QUERY_ACCOUNT_TEST_" + time.Now().Format("20060102150405")
	cleanupQueryAccountFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupQueryAccountFixture(t, pool, prefix) })

	ctx := context.Background()
	userID := insertQueryAccountUser(t, pool, prefix+"CN", "")
	adminHandler := NewHandler(NewPostgresStore(pool))
	queryHandler := queryapi.NewHandler(queryapi.NewPostgresStore(pool), time.Hour, false)

	invalid := httptest.NewRecorder()
	adminHandler.Detail(invalid, httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/query-code", bytes.NewBufferString(`{"query_code":"短"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid query code status = %d, want 400", invalid.Code)
	}

	setBody := bytes.NewBufferString(`{"query_code":"TestCode-123"}`)
	setResponse := httptest.NewRecorder()
	adminHandler.Detail(setResponse, httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/query-code", setBody))
	if setResponse.Code != http.StatusOK {
		t.Fatalf("set query code status = %d: %s", setResponse.Code, setResponse.Body.String())
	}
	assertNoQuerySecret(t, setResponse.Body.String(), "TestCode-123")
	assertHasQueryCode(t, pool, userID, true)

	firstCookie := queryLogin(t, queryHandler, prefix+"CN", "TestCode-123", http.StatusOK)
	assertSessionCount(t, pool, userID, 1)

	resetBody := bytes.NewBufferString(`{"query_code":"NextCode-456"}`)
	resetResponse := httptest.NewRecorder()
	adminHandler.Detail(resetResponse, httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/query-code", resetBody))
	if resetResponse.Code != http.StatusOK {
		t.Fatalf("reset query code status = %d: %s", resetResponse.Code, resetResponse.Body.String())
	}
	assertNoQuerySecret(t, resetResponse.Body.String(), "NextCode-456")
	assertSessionCount(t, pool, userID, 0)
	queryLogin(t, queryHandler, prefix+"CN", "TestCode-123", http.StatusUnauthorized)
	secondCookie := queryLogin(t, queryHandler, prefix+"CN", "NextCode-456", http.StatusOK)
	assertSessionCount(t, pool, userID, 1)

	disableResponse := httptest.NewRecorder()
	adminHandler.Detail(disableResponse, httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+userID+"/status", bytes.NewBufferString(`{"status":"disabled"}`)))
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("disable status = %d: %s", disableResponse.Code, disableResponse.Body.String())
	}
	assertSessionCount(t, pool, userID, 0)
	queryLogin(t, queryHandler, prefix+"CN", "NextCode-456", http.StatusUnauthorized)

	oldSessionResponse := httptest.NewRecorder()
	oldSessionRequest := httptest.NewRequest(http.MethodGet, "/api/query/orders", nil)
	oldSessionRequest.AddCookie(firstCookie)
	queryHandler.Orders(oldSessionResponse, oldSessionRequest)
	if oldSessionResponse.Code != http.StatusUnauthorized {
		t.Fatalf("reset old session status = %d, want 401", oldSessionResponse.Code)
	}
	disabledSessionResponse := httptest.NewRecorder()
	disabledSessionRequest := httptest.NewRequest(http.MethodGet, "/api/query/orders", nil)
	disabledSessionRequest.AddCookie(secondCookie)
	queryHandler.Orders(disabledSessionResponse, disabledSessionRequest)
	if disabledSessionResponse.Code != http.StatusUnauthorized {
		t.Fatalf("disabled old session status = %d, want 401", disabledSessionResponse.Code)
	}

	enableResponse := httptest.NewRecorder()
	adminHandler.Detail(enableResponse, httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+userID+"/status", bytes.NewBufferString(`{"status":"active"}`)))
	if enableResponse.Code != http.StatusOK {
		t.Fatalf("enable status = %d: %s", enableResponse.Code, enableResponse.Body.String())
	}
	queryLogin(t, queryHandler, prefix+"CN", "NextCode-456", http.StatusOK)

	missingResponse := httptest.NewRecorder()
	adminHandler.Detail(missingResponse, httptest.NewRequest(http.MethodPatch, "/api/admin/users/11111111-1111-1111-1111-111111111111/status", bytes.NewBufferString(`{"status":"disabled"}`)))
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing user status = %d, want 404", missingResponse.Code)
	}

	var lastLogin string
	if err := pool.QueryRow(ctx, `select coalesce(to_char(last_query_login_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '') from users where id = $1::uuid`, userID).Scan(&lastLogin); err != nil {
		t.Fatalf("read last login: %v", err)
	}
	if lastLogin == "" {
		t.Fatal("last_query_login_at was not recorded")
	}
}

func TestAdminQueryAccountRouteRejectsRegularQuerySession(t *testing.T) {
	pool := newUsersTestPool(t)
	prefix := "QUERY_ACCOUNT_ROUTE_TEST_" + time.Now().Format("20060102150405")
	cleanupQueryAccountFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupQueryAccountFixture(t, pool, prefix) })

	userID := insertQueryAccountUser(t, pool, prefix+"CN", "RouteCode-123")
	queryHandler := queryapi.NewHandler(queryapi.NewPostgresStore(pool), time.Hour, false)
	cookie := queryLogin(t, queryHandler, prefix+"CN", "RouteCode-123", http.StatusOK)

	adminHandler := NewHandler(NewPostgresStore(pool))
	request := httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+userID+"/status", bytes.NewBufferString(`{"status":"disabled"}`))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	authenticatedHandler(adminHandler.Detail).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("regular query session admin status = %d, want 401", response.Code)
	}
}

func newUsersTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = godotenv.Load("../.env")
	_ = godotenv.Load("../../.env")
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `
		alter table users
			add column if not exists query_code_updated_at timestamptz,
			add column if not exists last_query_login_at timestamptz
	`); err != nil {
		t.Fatalf("ensure user query account columns: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func insertQueryAccountUser(t *testing.T, pool *pgxpool.Pool, cn string, queryCode string) string {
	t.Helper()
	var hash any
	if queryCode != "" {
		hashBytes, err := bcrypt.GenerateFromPassword([]byte(queryCode), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("hash fixture query code: %v", err)
		}
		hash = string(hashBytes)
	}
	var userID string
	if err := pool.QueryRow(context.Background(), `
		insert into users (cn_code, display_name, query_code_hash, status)
		values ($1, 'Query account test user', $2, 'active')
		returning id::text
	`, cn, hash).Scan(&userID); err != nil {
		t.Fatalf("insert query account user: %v", err)
	}
	return userID
}

func queryLogin(t *testing.T, handler *queryapi.Handler, cn string, code string, wantStatus int) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"cn": cn, "query_code": code})
	request := httptest.NewRequest(http.MethodPost, "/api/query/login", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.Login(response, request)
	if response.Code != wantStatus {
		t.Fatalf("query login %s status = %d, want %d: %s", cn, response.Code, wantStatus, response.Body.String())
	}
	if wantStatus != http.StatusOK {
		if strings.Contains(response.Body.String(), "尚未设置") || strings.Contains(response.Body.String(), "停用") {
			t.Fatalf("login error leaks account state: %s", response.Body.String())
		}
		return nil
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "pjsk_query_session" {
			return cookie
		}
	}
	t.Fatal("query session cookie not set")
	return nil
}

func assertNoQuerySecret(t *testing.T, body string, plain string) {
	t.Helper()
	if strings.Contains(body, plain) || strings.Contains(body, "query_code_hash") || strings.Contains(body, "$2a$") || strings.Contains(body, "$2b$") {
		t.Fatalf("response exposes query secret material: %s", body)
	}
}

func assertHasQueryCode(t *testing.T, pool *pgxpool.Pool, userID string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(context.Background(), `select (coalesce(query_code_hash, '') <> '') from users where id = $1::uuid`, userID).Scan(&got); err != nil {
		t.Fatalf("read has query code: %v", err)
	}
	if got != want {
		t.Fatalf("has query code = %v, want %v", got, want)
	}
}

func assertSessionCount(t *testing.T, pool *pgxpool.Pool, userID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(context.Background(), `select count(*)::int from query_sessions where user_id = $1::uuid`, userID).Scan(&got); err != nil {
		t.Fatalf("count query sessions: %v", err)
	}
	if got != want {
		t.Fatalf("query session count = %d, want %d", got, want)
	}
}

func cleanupQueryAccountFixture(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `delete from query_sessions where user_id in (select id from users where cn_code like $1)`, prefix+"%"); err != nil {
		t.Fatalf("cleanup query sessions: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from users where cn_code like $1`, prefix+"%"); err != nil {
		t.Fatalf("cleanup users: %v", err)
	}
}
