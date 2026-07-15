package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pjsk/backend/internal/config"
	"pjsk/backend/internal/testdb"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func TestAdminExportExcelRoutesRequireAuthAndReturnXLSX(t *testing.T) {
	pool := newAPITestPool(t)
	prefix := "API_EXPORT_ROUTE_TEST_" + time.Now().Format("20060102150405")
	cleanupAPITestAdmin(t, pool, prefix)
	t.Cleanup(func() { cleanupAPITestAdmin(t, pool, prefix) })

	router := NewRouter(config.Config{
		AdminSessionTTL: 12 * time.Hour,
		CookieSecure:    false,
		FrontendOrigins: []string{"http://localhost:5173"},
	}, pool)

	routes := []string{
		"/api/admin/export/users.xlsx",
		"/api/admin/export/payments.xlsx",
		"/api/admin/export/order-items.xlsx",
		"/api/admin/export/order-items.xlsx?unpaid_only=1",
	}
	for _, route := range routes {
		t.Run("unauth "+route, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
			if response.Code == http.StatusNotFound {
				t.Fatalf("route %s returned 404; export route is not registered", route)
			}
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("route %s unauth status = %d, want 401", route, response.Code)
			}
		})
	}

	cookie := loginAPITestAdmin(t, pool, router, prefix)
	for _, route := range routes {
		t.Run("auth "+route, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, route, nil)
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("route %s status = %d, want 200: %s", route, response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
				t.Fatalf("route %s content type = %q", route, got)
			}
			assertXLSXResponse(t, response.Body.Bytes())
		})
	}
}

// newAPITestPool returns a pool for this test's own throwaway database, with
// the schema built by the real migration runner.
//
// It used to load the real backend/.env and connect to DATABASE_URL — which
// pointed at the production database — and then create the
// admin_auth_audit_events table from its own copy of 0019's CREATE TABLE. That
// copy had no CREATE INDEX statements and wrote no schema_migrations row, which
// is exactly the unregistered table found in production. The DDL below is gone
// on purpose: migrations own the schema.
func newAPITestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return testdb.New(t, "api")
}

func loginAPITestAdmin(t *testing.T, pool *pgxpool.Pool, router http.Handler, prefix string) *http.Cookie {
	t.Helper()
	username := prefix + "admin"
	password := "export-route-password"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	_, err = pool.Exec(context.Background(), `
		insert into admins (username, password_hash, display_name, role, status)
		values ($1, $2, 'API export route test admin', 'admin', 'active')
	`, username, string(hash))
	if err != nil {
		t.Fatalf("insert admin: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(body))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "pjsk_admin_session" && strings.TrimSpace(cookie.Value) != "" {
			return cookie
		}
	}
	t.Fatal("login did not return admin session cookie")
	return nil
}

func assertXLSXResponse(t *testing.T, body []byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("response is not a readable xlsx zip: %v; body prefix %q", err, string(body[:minInt(len(body), 80)]))
	}
	parts := map[string]bool{}
	for _, file := range reader.File {
		parts[file.Name] = true
	}
	for _, name := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml", "xl/styles.xml"} {
		if !parts[name] {
			t.Fatalf("xlsx response missing %s; parts=%v", name, parts)
		}
	}
}

func cleanupAPITestAdmin(t *testing.T, pool *pgxpool.Pool, prefix string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `delete from admin_sessions where admin_id in (select id from admins where username like $1)`, prefix+"%"); err != nil {
		t.Fatalf("cleanup admin sessions: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from admin_auth_audit_events where username_normalized like lower($1)`, prefix+"%"); err != nil {
		t.Fatalf("cleanup admin auth audit events: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from admins where username like $1`, prefix+"%"); err != nil {
		t.Fatalf("cleanup admins: %v", err)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
