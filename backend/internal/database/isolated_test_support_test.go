package database

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Shared safety gates and helpers for the isolated-PostgreSQL integration
// tests. These tests create and drop their own throwaway databases, so every
// guard here exists to make it impossible to aim them at anything else —
// above all at the production database.
//
// They are OFF by default: without PJSK_RUN_ISOLATED_MIGRATION_TESTS=1 every
// test skips, so `go test ./...` on a machine with no database still passes.
//
// No password appears anywhere in this file. The admin DSN carries no password;
// pgx resolves credentials through PostgreSQL's own pgpass file (the
// github.com/jackc/pgpassfile dependency), exactly like psql -w.

const (
	isolatedTestEnvVar  = "PJSK_RUN_ISOLATED_MIGRATION_TESTS"
	isolatedTestDSNVar  = "PJSK_ISOLATED_TEST_ADMIN_DSN"
	isolatedTestDSNBase = "postgres://postgres@127.0.0.1:5432/postgres?sslmode=disable"
	testDatabasePrefix  = "pjsk_migration_test_"
)

// The only database names these tests may ever create, connect to, or drop.
var testDatabaseNamePattern = regexp.MustCompile(`^pjsk_migration_test_[a-z0-9_]+$`)

// Names that must never be touched, whatever else happens.
var forbiddenDatabaseNames = []string{"pjsk", "postgres", "template0", "template1"}

// requireIsolatedTestEnv skips unless the explicit opt-in is set.
func requireIsolatedTestEnv(t *testing.T) {
	t.Helper()
	if os.Getenv(isolatedTestEnvVar) != "1" {
		t.Skipf("%s is not set to 1; skipping isolated PostgreSQL integration test", isolatedTestEnvVar)
	}
}

// assertSafeTestDatabaseName is called before every create, connect, and drop.
// It is deliberately redundant: a name is re-checked at each step rather than
// trusted because it was checked once.
func assertSafeTestDatabaseName(t *testing.T, name string) {
	t.Helper()
	for _, forbidden := range forbiddenDatabaseNames {
		if strings.EqualFold(name, forbidden) {
			t.Fatalf("refusing to touch protected database %q", name)
		}
	}
	if !testDatabaseNamePattern.MatchString(name) {
		t.Fatalf("database name %q does not match %s; refusing to touch it", name, testDatabaseNamePattern)
	}
	if strings.ContainsAny(name, `"'%;\ `) {
		t.Fatalf("database name %q contains characters that must never reach a CREATE/DROP statement", name)
	}
}

// assertDSNCarriesNoPassword fails if the DSN string embeds a password, in
// either URL or keyword/value form.
func assertDSNCarriesNoPassword(t *testing.T, dsn string) {
	t.Helper()
	if parsed, err := url.Parse(dsn); err == nil && parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			t.Fatal("admin DSN must not embed a password; rely on the PostgreSQL pgpass file instead")
		}
	}
	if strings.Contains(strings.ToLower(dsn), "password=") {
		t.Fatal("admin DSN must not embed a password= keyword; rely on the PostgreSQL pgpass file instead")
	}
}

// adminDSN returns the DSN of the maintenance connection, after proving it
// points at a local machine and not at the production database.
func adminDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(isolatedTestDSNVar)
	if dsn == "" {
		dsn = isolatedTestDSNBase
	}

	// The DSN *string* must carry no password, so no password can appear in
	// test code, a command line, or a log. Check the string itself rather than
	// the parsed config: pgx populates config.Password from PostgreSQL's pgpass
	// file, which is exactly the mechanism we want it to use.
	assertDSNCarriesNoPassword(t, dsn)

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("admin DSN is not parseable: %v", err)
	}
	switch config.Host {
	case "127.0.0.1", "localhost", "::1":
	default:
		t.Fatalf("admin DSN host %q is not local; these tests only run against a local database", config.Host)
	}
	for _, forbidden := range forbiddenDatabaseNames {
		if strings.EqualFold(config.Database, forbidden) && !strings.EqualFold(config.Database, "postgres") {
			t.Fatalf("admin DSN points at protected database %q", config.Database)
		}
	}
	if strings.EqualFold(config.Database, "pjsk") {
		t.Fatal("admin DSN points at the production database pjsk; refusing to run")
	}
	return dsn
}

// testDatabaseDSN rewrites the admin DSN to target a throwaway test database.
func testDatabaseDSN(t *testing.T, dbName string) string {
	t.Helper()
	assertSafeTestDatabaseName(t, dbName)
	config, err := pgx.ParseConfig(adminDSN(t))
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable",
		config.User, config.Host, config.Port, dbName)
}

func uniqueTestDatabaseName(t *testing.T, suffix string) string {
	t.Helper()
	name := fmt.Sprintf("%s%s_%s_%d", testDatabasePrefix, time.Now().UTC().Format("20060102"), suffix, time.Now().UnixNano()%1000000)
	name = strings.ToLower(name)
	assertSafeTestDatabaseName(t, name)
	return name
}

func openAdminConn(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, adminDSN(t))
	if err != nil {
		t.Skipf("no local PostgreSQL available for isolated tests: %v", err)
	}
	// Prove at runtime that the maintenance connection is not the production DB.
	var currentDB string
	if err := conn.QueryRow(ctx, "select current_database()").Scan(&currentDB); err != nil {
		_ = conn.Close(ctx)
		t.Fatalf("could not confirm the maintenance database: %v", err)
	}
	if strings.EqualFold(currentDB, "pjsk") {
		_ = conn.Close(ctx)
		t.Fatal("maintenance connection landed on the production database pjsk; refusing to run")
	}
	return conn
}

// createIsolatedTestDatabase creates one throwaway database and registers
// cleanup twice over: a t.Cleanup (which runs even when a test fails) and an
// explicit drop the caller may also invoke. Dropping is always by exact name.
func createIsolatedTestDatabase(t *testing.T, dbName string) {
	t.Helper()
	assertSafeTestDatabaseName(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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

	// Identifiers cannot be parameterized; the name is regexp-validated above.
	if _, err := conn.Exec(ctx, fmt.Sprintf(`create database %s template template0`, pgx.Identifier{dbName}.Sanitize())); err != nil {
		t.Fatalf("create database %s: %v", dbName, err)
	}
	t.Logf("created isolated test database %s", dbName)

	t.Cleanup(func() { dropIsolatedTestDatabase(t, dbName) })
}

// dropIsolatedTestDatabase drops exactly one database by exact name. It first
// terminates backends connected ONLY to that database. It never uses a
// wildcard and never touches more than the one name it was given.
func dropIsolatedTestDatabase(t *testing.T, dbName string) {
	t.Helper()
	assertSafeTestDatabaseName(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, adminDSN(t))
	if err != nil {
		t.Errorf("CLEANUP FAILED: could not connect to drop test database %s: %v — drop it by hand", dbName, err)
		return
	}
	defer func() { _ = conn.Close(ctx) }()

	var exists bool
	if err := conn.QueryRow(ctx, "select exists(select 1 from pg_database where datname = $1)", dbName).Scan(&exists); err != nil {
		t.Errorf("CLEANUP FAILED: could not check %s: %v — drop it by hand", dbName, err)
		return
	}
	if !exists {
		return
	}

	// Terminate only connections to this one database, by exact name.
	if _, err := conn.Exec(ctx,
		"select pg_terminate_backend(pid) from pg_stat_activity where datname = $1 and pid <> pg_backend_pid()",
		dbName); err != nil {
		t.Logf("could not terminate backends on %s (continuing): %v", dbName, err)
	}

	if _, err := conn.Exec(ctx, fmt.Sprintf(`drop database %s`, pgx.Identifier{dbName}.Sanitize())); err != nil {
		t.Errorf("CLEANUP FAILED: could not drop test database %s: %v — drop it by hand", dbName, err)
		return
	}
	t.Logf("dropped isolated test database %s", dbName)
}

func openTestPool(t *testing.T, ctx context.Context, dbName string) *pgxpool.Pool {
	t.Helper()
	assertSafeTestDatabaseName(t, dbName)
	pool, err := pgxpool.New(ctx, testDatabaseDSN(t, dbName))
	if err != nil {
		t.Fatalf("open pool for %s: %v", dbName, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping %s: %v", dbName, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// appliedVersions returns schema_migrations.version ordered by version.
func appliedVersions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, "select version from schema_migrations order by version")
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()

	versions := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema_migrations: %v", err)
	}
	return versions
}

func tableExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx,
		"select exists(select 1 from information_schema.tables where table_schema = 'public' and table_name = $1)",
		table).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	return exists
}

func columnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx,
		"select exists(select 1 from information_schema.columns where table_schema = 'public' and table_name = $1 and column_name = $2)",
		table, column).Scan(&exists); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	return exists
}

// Mirrors production: main.go embeds the tree and calls RunMigrations with the
// "migrations" subdirectory. io/fs rejects paths like "./x.sql", so the runner
// must always be given a real subdirectory name, never ".".
const (
	repoFSRoot        = "../.."
	migrationsSubdir  = "migrations"
	syntheticFSSubdir = "migrations"
)

// repoMigrationNames lists the real migration files, using the same helper the
// production runner uses, so the comparison is against the real set.
func repoMigrationNames(t *testing.T) []string {
	t.Helper()
	names, err := listMigrationNames(os.DirFS(repoFSRoot), migrationsSubdir)
	if err != nil {
		t.Fatalf("list repository migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no repository migrations found")
	}
	return names
}
