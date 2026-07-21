// Package compat holds cross-release compatibility tests. It contains no
// non-test code, so it never enters the production binary.
//
// TestOldReleaseRunsAgainst0023Schema proves the application-layer rollback
// guarantee for migration 0023: the previously shipped release
// (commit 95036a07911bfcdbfc62e6278982dd6e268d8447, whose embedded migrations
// stop at 0022) starts cleanly against a database that has already applied
// 0023, serves /health, authenticates the seeded owner, and neither rolls back,
// re-runs, nor mutates 0023 — so rolling the release back never requires
// rolling the database back.
//
// It is OFF by default. It runs only when BOTH:
//   - PJSK_RUN_DB_INTEGRATION_TESTS=1 (a throwaway local database is available), and
//   - PJSK_RUN_OLD_RELEASE_COMPAT_TEST=1 (opt in to building and running the old binary).
//
// It uses a fully isolated throwaway database (never the frozen local pjsk
// archive, never the cloud). The old binary is built from a temporary detached
// git worktree of the old commit and removed afterwards; no old binary,
// password, connection string, or absolute path is ever committed.
package compat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"pjsk/backend/internal/testdb"
)

const oldReleaseCommit = "95036a07911bfcdbfc62e6278982dd6e268d8447"

func TestOldReleaseRunsAgainst0023Schema(t *testing.T) {
	if os.Getenv("PJSK_RUN_OLD_RELEASE_COMPAT_TEST") != "1" {
		t.Skip("set PJSK_RUN_OLD_RELEASE_COMPAT_TEST=1 to run the old-release compatibility test")
	}

	// Isolated throwaway DB, migrated to 0023 by the CURRENT code.
	pool, dsn := testdb.NewWithDSN(t, "oldcompat")
	ctx := context.Background()

	assertMigration(t, pool, 23, "0023_admin_management.sql")
	assert0023ColumnsPresent(t, pool)

	// Minimal but valid data using the 0023 columns and vocabulary.
	ownerPassword := "compat-owner-pass-123"
	ownerHash, err := bcrypt.GenerateFromPassword([]byte(ownerPassword), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	var userID, ownerID, adminID string
	if err := pool.QueryRow(ctx, `insert into users (cn_code, status) values ('COMPAT_CN_001','active') returning id::text`).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		insert into admins (username, password_hash, display_name, role, status)
		values ('compat_owner', $1, '苏归', 'owner', 'active') returning id::text
	`, string(ownerHash)).Scan(&ownerID); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	// A managed admin linked to the user, with the forced-change flag set — the
	// exact shape 0023 introduced.
	if err := pool.QueryRow(ctx, `
		insert into admins (username, password_hash, display_name, role, status, user_id, must_change_password)
		values ('compat_admin', $1, '演示', 'admin', 'active', $2::uuid, true) returning id::text
	`, string(ownerHash), userID).Scan(&adminID); err != nil {
		t.Fatalf("seed managed admin: %v", err)
	}
	// One management audit event exercising the new actor / management_reason columns.
	if _, err := pool.Exec(ctx, `
		insert into admin_auth_audit_events
			(event_type, occurred_at, admin_id, username_normalized, client_ip, result, reason_code, actor_admin_id, management_reason)
		values ('admin_appointed', now(), $1::uuid, 'compat_admin', 'compat-test', 'success', 'none', $2::uuid, '兼容性测试任命')
	`, adminID, ownerID); err != nil {
		t.Fatalf("seed management audit: %v", err)
	}

	// Snapshot before running the old binary.
	before := snapshot(t, pool)

	// Build the old binary from a temporary detached worktree of the old commit.
	oldBin := buildOldBinary(t)

	// Run it against the 0023 database on a free port.
	port := freePort(t)
	runOldBinary(t, oldBin, dsn, port)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForHealth(t, baseURL, 20*time.Second)

	// The old release authenticates the seeded owner and serves /api/admin/me —
	// i.e. it reads the admins table (now carrying 0023's extra columns) without
	// error.
	cookie := oldLogin(t, baseURL, "compat_owner", ownerPassword)
	role := oldMe(t, baseURL, cookie)
	if role != "owner" {
		t.Fatalf("old release /api/admin/me role = %q, want owner", role)
	}

	// The old release must not have rolled back, re-run, or mutated 0023.
	assertMigration(t, pool, 23, "0023_admin_management.sql")
	assert0023ColumnsPresent(t, pool)
	after := snapshot(t, pool)
	if before != after {
		t.Fatalf("old release changed 0023 state:\n before=%s\n after =%s", before, after)
	}
	// Exactly one 0020..0023 row each — no duplicate re-application.
	assertSingleMigrationRows(t, pool)
}

// --- helpers ---

func assertMigration(t *testing.T, pool *pgxpool.Pool, wantCount int, wantMax string) {
	t.Helper()
	var count int
	var max string
	if err := pool.QueryRow(context.Background(),
		`select count(*), max(version) from schema_migrations`).Scan(&count, &max); err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	if count != wantCount || max != wantMax {
		t.Fatalf("migrations = %d/%s, want %d/%s", count, max, wantCount, wantMax)
	}
}

func assertSingleMigrationRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, v := range []string{
		"0020_payment_qr_codes.sql", "0021_payment_submissions.sql",
		"0022_admin_owner_security.sql", "0023_admin_management.sql",
	} {
		var n int
		if err := pool.QueryRow(context.Background(),
			`select count(*) from schema_migrations where version=$1`, v).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", v, err)
		}
		if n != 1 {
			t.Fatalf("migration %s applied %d times, want exactly 1", v, n)
		}
	}
}

func assert0023ColumnsPresent(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, c := range []struct{ table, column string }{
		{"admins", "user_id"}, {"admins", "must_change_password"},
		{"admins", "revoked_at"}, {"admins", "revoked_by"},
		{"admin_auth_audit_events", "actor_admin_id"},
		{"admin_auth_audit_events", "management_reason"},
	} {
		var exists bool
		if err := pool.QueryRow(context.Background(), `
			select exists(select 1 from information_schema.columns
				where table_name=$1 and column_name=$2)
		`, c.table, c.column).Scan(&exists); err != nil {
			t.Fatalf("check %s.%s: %v", c.table, c.column, err)
		}
		if !exists {
			t.Fatalf("0023 column %s.%s missing", c.table, c.column)
		}
	}
}

// snapshot captures the 0023-sensitive state that the old release must leave
// untouched: the managed admin's link/flags and the management audit event.
func snapshot(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(), `
		select
			(select string_agg(username||'|'||role||'|'||status||'|'||coalesce(user_id::text,'-')||'|'||must_change_password::text, ',' order by username) from admins)
			|| ' :: ' ||
			(select count(*)::text from admin_auth_audit_events where event_type='admin_appointed' and actor_admin_id is not null and management_reason is not null)
	`).Scan(&s); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return s
}

func buildOldBinary(t *testing.T) string {
	t.Helper()
	repoRoot := repoRoot(t)
	wt := filepath.Join(t.TempDir(), "oldrelease")

	run(t, repoRoot, "git", "worktree", "add", "--detach", wt, oldReleaseCommit)
	t.Cleanup(func() { _ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wt).Run() })

	binName := "pjsk-backend-old"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	outPath := filepath.Join(t.TempDir(), binName)
	// Build for the host OS: the old binary runs locally in this test.
	cmd := exec.Command("go", "build", "-o", outPath, ".")
	cmd.Dir = filepath.Join(wt, "backend")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build old binary: %v\n%s", err, out)
	}
	return outPath
}

func runOldBinary(t *testing.T, bin, dsn string, port int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// A throwaway dev-only HMAC key; never a production value, never persisted.
	devKey := base64.StdEncoding.EncodeToString([]byte("compat-test-query-code-recovery-hmac-key-32b"))
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = t.TempDir() // no .env here -> the binary uses the env below
	cmd.Env = append(os.Environ(),
		"APP_ENV=development",
		"SERVER_HOST=127.0.0.1",
		fmt.Sprintf("APP_PORT=%d", port),
		"DATABASE_URL="+dsn,
		"ADMIN_SESSION_TTL=12h",
		"ADMIN_COOKIE_SECURE=false",
		"QUERY_CODE_RECOVERY_HMAC_KEY="+devKey,
	)
	// Surface the child's logs on failure without leaking secrets (env is not logged).
	logBuf := &strings.Builder{}
	cmd.Stdout = logBuf
	cmd.Stderr = logBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start old binary: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("old binary output:\n%s", logBuf.String())
		}
	})
}

func waitForHealth(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("old binary /health did not return 200 within %s", timeout)
}

func oldLogin(t *testing.T, baseURL, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	resp, err := http.Post(baseURL+"/api/admin/login", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("old login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("old login status = %d, want 200", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "pjsk_admin_session" && c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("old login returned no session cookie")
	return ""
}

func oldMe(t *testing.T, baseURL, cookie string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, baseURL+"/api/admin/me", nil)
	req.Header.Set("Cookie", cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("old /me: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("old /me status = %d, want 200", resp.StatusCode)
	}
	var payload struct {
		Admin struct {
			Role string `json:"role"`
		} `json:"admin"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	return payload.Admin.Role
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func repoRoot(t *testing.T) string {
	t.Helper()
	// This file lives at backend/internal/compat; the repo root is three up.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func run(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}
