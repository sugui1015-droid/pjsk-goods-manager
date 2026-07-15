// Package testdb hands database integration tests their own throwaway
// PostgreSQL database, built from the repository's real migrations.
//
// It exists because the previous per-package test fixtures loaded the real
// backend/.env and connected to whatever DATABASE_URL pointed at — which was
// the production database. That is how production ended up with an
// admin_auth_audit_events table that had no indexes and no schema_migrations
// row (a test fixture had created it with its own copy of 0019's CREATE TABLE),
// and with 208 audit rows written by the rate-limit tests' hardcoded ghost
// accounts. See docs/development-logs/2026-07-15-production-database-readonly-verification.md
//
// The rules this package enforces, so that cannot happen again:
//
//   - Tests are OFF by default. Without PJSK_RUN_DB_INTEGRATION_TESTS=1 every
//     caller skips, so `go test ./...` never touches a database.
//   - The general DATABASE_URL is never read, and .env is never loaded. The
//     admin connection comes only from PJSK_TEST_DATABASE_ADMIN_DSN.
//   - The DSN string must carry no password (rely on PostgreSQL's pgpass file)
//     and must point at a local host.
//   - The maintenance connection must be the `postgres` database, never `pjsk`.
//   - Every test database name must match ^pjsk_integration_test_[a-z0-9_]+$,
//     and the production/system names are refused outright.
//   - Schema comes from the real migration runner, never from DDL copied into
//     a test — copied DDL is what silently created an unregistered table.
//   - Each database is dropped by exact name; never a wildcard.
//
// This package is only ever imported from _test.go files, so it is not reachable
// from main and never enters the production binary. TestNoProductionCodeImportsTestdb
// in the api package enforces that.
package testdb

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pjsk/backend/internal/database"
)

const (
	// RunEnvVar must be exactly "1" for any database integration test to run.
	RunEnvVar = "PJSK_RUN_DB_INTEGRATION_TESTS"

	// AdminDSNEnvVar overrides the maintenance DSN. It must never contain a
	// password and must never be the production DSN.
	AdminDSNEnvVar = "PJSK_TEST_DATABASE_ADMIN_DSN"

	// defaultAdminDSN carries no password on purpose: pgx resolves credentials
	// through PostgreSQL's own pgpass file, exactly like psql -w.
	defaultAdminDSN = "postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable"

	// DatabasePrefix is the only prefix a test database may use.
	DatabasePrefix = "pjsk_integration_test_"
)

// NamePattern is the only shape a test database name may take.
var NamePattern = regexp.MustCompile(`^pjsk_integration_test_[a-z0-9_]+$`)

// ForbiddenDatabaseNames must never be created, connected to, or dropped.
var ForbiddenDatabaseNames = []string{"pjsk", "postgres", "template0", "template1"}

var nameCounter atomic.Uint64

// SkipUnlessEnabled skips the test unless the explicit opt-in is set.
func SkipUnlessEnabled(t testing.TB) {
	t.Helper()
	if os.Getenv(RunEnvVar) != "1" {
		t.Skipf("%s is not set to 1; skipping database integration test", RunEnvVar)
	}
}

// New returns a pool for a brand-new isolated database whose schema was built
// by the repository's real migration runner. The database is dropped when the
// test finishes, pass or fail.
//
// It skips the test unless PJSK_RUN_DB_INTEGRATION_TESTS=1, so callers can use
// it unconditionally.
func New(t testing.TB, label string) *pgxpool.Pool {
	t.Helper()
	SkipUnlessEnabled(t)

	dbName := uniqueName(t, label)
	createDatabase(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDatabaseDSN(t, dbName))
	if err != nil {
		t.Fatalf("open pool for %s: %v", dbName, err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping %s: %v", dbName, err)
	}
	assertConnectedDatabase(t, ctx, pool, dbName)

	// Schema comes from the real migrations, never from DDL copied into a test.
	fsys, dir := migrationsFS(t)
	if err := database.RunMigrations(ctx, pool, fsys, dir); err != nil {
		t.Fatalf("run migrations on %s: %v", dbName, err)
	}

	return pool
}

// uniqueName builds a name that satisfies NamePattern. label is sanitized, not
// trusted.
func uniqueName(t testing.TB, label string) string {
	t.Helper()
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, label)
	clean = strings.Trim(clean, "_")
	if clean == "" {
		clean = "t"
	}
	if len(clean) > 24 {
		clean = clean[:24]
	}
	name := fmt.Sprintf("%s%s_%d_%d", DatabasePrefix, clean, time.Now().UnixNano()%1_000_000_000, nameCounter.Add(1))
	AssertSafeName(t, name)
	return name
}

// AssertSafeName is called before every create, connect, and drop. It is
// deliberately redundant: the name is re-checked at each step rather than
// trusted because it was checked once.
func AssertSafeName(t testing.TB, name string) {
	t.Helper()
	for _, forbidden := range ForbiddenDatabaseNames {
		if strings.EqualFold(name, forbidden) {
			t.Fatalf("refusing to touch protected database %q", name)
		}
	}
	if !NamePattern.MatchString(name) {
		t.Fatalf("database name %q does not match %s; refusing to touch it", name, NamePattern)
	}
	if strings.ContainsAny(name, `"'%;\ `) {
		t.Fatalf("database name %q contains characters that must never reach a CREATE/DROP statement", name)
	}
}

// AdminDSN returns the maintenance DSN after proving it is safe. It never reads
// DATABASE_URL and never loads .env.
func AdminDSN(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv(AdminDSNEnvVar)
	if dsn == "" {
		dsn = defaultAdminDSN
	}
	assertNoPassword(t, dsn)

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("%s is not parseable: %v", AdminDSNEnvVar, err)
	}
	switch config.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		t.Fatalf("%s host %q is not local; integration tests only run against a local database", AdminDSNEnvVar, config.Host)
	}
	// The maintenance connection must be the postgres database, never pjsk.
	if !strings.EqualFold(config.Database, "postgres") {
		t.Fatalf("%s must point at the postgres maintenance database, got %q", AdminDSNEnvVar, config.Database)
	}
	return dsn
}

// assertNoPassword rejects a DSN string that embeds a password, in either URL
// or keyword/value form. Checking the string (not the parsed config) matters:
// pgx fills config.Password in from pgpass, which is the mechanism we want.
func assertNoPassword(t testing.TB, dsn string) {
	t.Helper()
	if parsed, err := url.Parse(dsn); err == nil && parsed.User != nil {
		if _, has := parsed.User.Password(); has {
			t.Fatalf("%s must not embed a password; rely on the PostgreSQL pgpass file instead", AdminDSNEnvVar)
		}
	}
	if strings.Contains(strings.ToLower(dsn), "password=") {
		t.Fatalf("%s must not embed a password= keyword; rely on the PostgreSQL pgpass file instead", AdminDSNEnvVar)
	}
}

func testDatabaseDSN(t testing.TB, dbName string) string {
	t.Helper()
	AssertSafeName(t, dbName)
	config, err := pgx.ParseConfig(AdminDSN(t))
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable", config.User, config.Host, config.Port, dbName)
}

func openAdminConn(t testing.TB, ctx context.Context) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, AdminDSN(t))
	if err != nil {
		t.Skipf("no local PostgreSQL available for integration tests: %v", err)
	}
	var current string
	if err := conn.QueryRow(ctx, "select current_database()").Scan(&current); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("could not confirm the maintenance database: %v", err)
	}
	if strings.EqualFold(current, "pjsk") {
		_ = conn.Close(ctx)
		t.Fatal("maintenance connection landed on the production database pjsk; refusing to run")
	}
	return conn
}

func createDatabase(t testing.TB, dbName string) {
	t.Helper()
	AssertSafeName(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn := openAdminConn(t, ctx)
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx, "select exists(select 1 from pg_database where datname = $1)", dbName).Scan(&exists); err != nil {
		t.Fatalf("could not check whether %s exists: %v", dbName, err)
	}
	if exists {
		t.Fatalf("test database %s already exists; refusing to reuse or drop someone else's database", dbName)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("create database %s template template0", pgx.Identifier{dbName}.Sanitize())); err != nil {
		t.Fatalf("create database %s: %v", dbName, err)
	}
	t.Logf("created isolated test database %s", dbName)

	t.Cleanup(func() { DropDatabase(t, dbName) })
}

// DropDatabase drops exactly one database by exact name, after terminating only
// the backends connected to that one database. Never a wildcard, never more
// than the single name it was given.
func DropDatabase(t testing.TB, dbName string) {
	t.Helper()
	AssertSafeName(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, AdminDSN(t))
	if err != nil {
		t.Errorf("CLEANUP FAILED: could not connect to drop test database %s: %v - drop it by hand", dbName, err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx, "select exists(select 1 from pg_database where datname = $1)", dbName).Scan(&exists); err != nil {
		t.Errorf("CLEANUP FAILED: could not check %s: %v - drop it by hand", dbName, err)
		return
	}
	if !exists {
		return
	}
	if _, err := conn.Exec(ctx,
		"select pg_terminate_backend(pid) from pg_stat_activity where datname = $1 and pid <> pg_backend_pid()",
		dbName); err != nil {
		t.Logf("could not terminate backends on %s (continuing): %v", dbName, err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("drop database %s", pgx.Identifier{dbName}.Sanitize())); err != nil {
		t.Errorf("CLEANUP FAILED: could not drop test database %s: %v - drop it by hand", dbName, err)
		return
	}
	t.Logf("dropped isolated test database %s", dbName)
}

// assertConnectedDatabase proves at runtime that the pool really is on the
// throwaway database and not on production.
func assertConnectedDatabase(t testing.TB, ctx context.Context, pool *pgxpool.Pool, want string) {
	t.Helper()
	var current string
	if err := pool.QueryRow(ctx, "select current_database()").Scan(&current); err != nil {
		t.Fatalf("could not confirm the connected database: %v", err)
	}
	if strings.EqualFold(current, "pjsk") {
		t.Fatal("connected to the production database pjsk; refusing to run")
	}
	if current != want {
		t.Fatalf("connected to %q, expected the isolated test database %q", current, want)
	}
}

// migrationsFS locates backend/migrations relative to THIS source file, so it
// resolves the same way no matter which package's directory the test runs in.
// It mirrors production: main.go calls RunMigrations with the "migrations"
// subdirectory, and io/fs rejects paths like "./x.sql", so the runner must be
// given a real subdirectory name, never ".".
func migrationsFS(t testing.TB) (fs.FS, string) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate the testdb source file to resolve backend/migrations")
	}
	// thisFile = <repo>/backend/internal/testdb/testdb.go
	backendDir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	migrationsDir := filepath.Join(backendDir, "migrations")
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("could not read %s: %v", migrationsDir, err)
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			count++
		}
	}
	if count == 0 {
		t.Fatalf("no migrations found in %s", migrationsDir)
	}
	return os.DirFS(backendDir), "migrations"
}
