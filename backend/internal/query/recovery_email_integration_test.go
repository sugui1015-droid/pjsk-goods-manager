package query

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/recoveryemail"
	"pjsk/backend/internal/users"
)

func TestPostgresQueryRecoveryEmailIsolationAndSessionStates(t *testing.T) {
	pool := newQueryTestPool(t)
	prefix := "TEST_RECOVERY_EMAIL_QUERY_" + time.Now().Format("20060102150405.000000000")
	cleanupQueryRecoveryEmailFixture(t, pool, prefix)
	t.Cleanup(func() { cleanupQueryRecoveryEmailFixture(t, pool, prefix) })

	ctx := context.Background()
	adminID := insertQueryRecoveryAdmin(t, pool, prefix)
	store := NewPostgresStore(pool)
	userStore := users.NewPostgresStore(pool)
	protector := queryRecoveryProtector(t)
	handler := NewHandler(store, time.Hour, false)
	handler.ConfigureRecoveryEmail(userStore, protector)

	firstID := insertQueryRecoveryUser(t, pool, prefix+"_FIRST")
	secondID := insertQueryRecoveryUser(t, pool, prefix+"_SECOND")
	emptyID := insertQueryRecoveryUser(t, pool, prefix+"_EMPTY")
	disabledID := insertQueryRecoveryUser(t, pool, prefix+"_DISABLED")
	mergedID := insertQueryRecoveryUser(t, pool, prefix+"_MERGED")
	expiredID := insertQueryRecoveryUser(t, pool, prefix+"_EXPIRED")

	firstProtected, err := protector.Protect("First.User@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userStore.PutRecoveryEmail(ctx, firstID, adminID, "test registration", firstProtected); err != nil {
		t.Fatal(err)
	}
	secondProtected, err := protector.Protect("Second.User@example.org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := userStore.PutRecoveryEmail(ctx, secondID, adminID, "test registration", secondProtected); err != nil {
		t.Fatal(err)
	}

	firstCookie := loginQueryRecoveryUser(t, handler, prefix+"_FIRST")
	firstResponse := queryRecoveryEmailRequest(t, handler, firstCookie, "/api/query/recovery-email?user_id="+secondID+"&cn="+prefix+"_SECOND")
	if firstResponse.Code != http.StatusOK || !strings.Contains(firstResponse.Body.String(), `"masked_email":"F***@example.com"`) {
		t.Fatalf("first user response = %d/%s", firstResponse.Code, firstResponse.Body.String())
	}
	if strings.Contains(firstResponse.Body.String(), "Second.User") || strings.Contains(firstResponse.Body.String(), secondID) {
		t.Fatal("first user response exposed another user's recovery data")
	}
	assertQueryRecoveryResponseSafe(t, firstResponse.Body.String())

	emptyCookie := loginQueryRecoveryUser(t, handler, prefix+"_EMPTY")
	emptyResponse := queryRecoveryEmailRequest(t, handler, emptyCookie, "/api/query/recovery-email")
	if emptyResponse.Code != http.StatusOK || !strings.Contains(emptyResponse.Body.String(), `"has_recovery_email":false`) {
		t.Fatalf("empty state = %d/%s", emptyResponse.Code, emptyResponse.Body.String())
	}

	disabledCookie := loginQueryRecoveryUser(t, handler, prefix+"_DISABLED")
	if _, err := pool.Exec(ctx, `update users set status = 'disabled' where id = $1::uuid`, disabledID); err != nil {
		t.Fatal(err)
	}
	if response := queryRecoveryEmailRequest(t, handler, disabledCookie, "/api/query/recovery-email"); response.Code != http.StatusUnauthorized {
		t.Fatalf("disabled status = %d, want 401", response.Code)
	}

	mergedCookie := loginQueryRecoveryUser(t, handler, prefix+"_MERGED")
	if _, err := pool.Exec(ctx, `update users set status = 'merged' where id = $1::uuid`, mergedID); err != nil {
		t.Fatal(err)
	}
	if response := queryRecoveryEmailRequest(t, handler, mergedCookie, "/api/query/recovery-email"); response.Code != http.StatusUnauthorized {
		t.Fatalf("merged status = %d, want 401", response.Code)
	}

	expiredCookie := loginQueryRecoveryUser(t, handler, prefix+"_EXPIRED")
	if _, err := pool.Exec(ctx, `update query_sessions set expires_at = now() - interval '1 minute' where user_id = $1::uuid`, expiredID); err != nil {
		t.Fatal(err)
	}
	if response := queryRecoveryEmailRequest(t, handler, expiredCookie, "/api/query/recovery-email"); response.Code != http.StatusUnauthorized {
		t.Fatalf("expired status = %d, want 401", response.Code)
	}

	if response := queryRecoveryEmailRequest(t, handler, nil, "/api/query/recovery-email"); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.Code)
	}
	_ = emptyID
}

func insertQueryRecoveryAdmin(t *testing.T, pool *pgxpool.Pool, prefix string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-admin-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := pool.QueryRow(context.Background(), `
		insert into admins (username, password_hash, display_name, role, status)
		values ($1, $2, 'Recovery email test admin', 'admin', 'active')
		returning id::text
	`, prefix+"_ADMIN", string(hash)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertQueryRecoveryUser(t *testing.T, pool *pgxpool.Pool, cn string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("test-query-code"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := pool.QueryRow(context.Background(), `
		insert into users (cn_code, display_name, query_code_hash, status)
		values ($1, 'Recovery email test user', $2, 'active')
		returning id::text
	`, cn, string(hash)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func loginQueryRecoveryUser(t *testing.T, handler *Handler, cn string) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/query/login", bytes.NewBufferString(`{"cn":"`+cn+`","query_code":"test-query-code"}`))
	request.RemoteAddr = "127.0.0.1:45000"
	response := httptest.NewRecorder()
	handler.Login(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("query login status = %d", response.Code)
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			return cookie
		}
	}
	t.Fatal("query session cookie missing")
	return nil
}

func queryRecoveryEmailRequest(t *testing.T, handler *Handler, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.RecoveryEmail(response, request)
	return response
}

func assertQueryRecoveryResponseSafe(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{"encrypted_email", "email_lookup_hash", "user_id", "admin_id", "created_by", "updated_by"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("ordinary response contains forbidden field %q", forbidden)
		}
	}
}

func cleanupQueryRecoveryEmailFixture(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	pattern := prefix + "%"
	var orders, payments, merges int
	if err := pool.QueryRow(ctx, `select count(*)::int from orders where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*)::int from payments where user_id in (select id from users where cn_code like $1)`, pattern).Scan(&payments); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		select count(*)::int from cn_merge_logs
		where source_user_id in (select id from users where cn_code like $1)
		   or target_user_id in (select id from users where cn_code like $1)
	`, pattern).Scan(&merges); err != nil {
		t.Fatal(err)
	}
	if orders != 0 || payments != 0 || merges != 0 {
		t.Fatalf("refusing query recovery cleanup: orders=%d payments=%d merges=%d", orders, payments, merges)
	}
	statements := []string{
		`delete from account_security_audit_logs where target_user_id in (select id from users where cn_code like $1)`,
		`delete from user_recovery_emails where user_id in (select id from users where cn_code like $1)`,
		`delete from query_sessions where user_id in (select id from users where cn_code like $1)`,
		`delete from users where cn_code like $1`,
		`delete from admin_sessions where admin_id in (select id from admins where username like $1)`,
		`delete from admins where username like $1`,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement, pattern); err != nil {
			t.Fatalf("cleanup query recovery fixture: %v", err)
		}
	}
	for label, query := range map[string]string{
		"users":    `select count(*)::int from users where cn_code like $1`,
		"sessions": `select count(*)::int from query_sessions where user_id in (select id from users where cn_code like $1)`,
	} {
		var count int
		if err := pool.QueryRow(ctx, query, pattern).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s fixture rows remain", label)
		}
	}
}

func TestQueryRecoveryProtectorUsesProcessOnlyKeys(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if _, err := recoveryemail.NewProtector(key, append(key, key...)); err != nil {
		t.Fatal(err)
	}
}
