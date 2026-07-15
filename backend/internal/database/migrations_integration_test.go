package database

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Isolated-PostgreSQL integration tests for the migration runner. Every test
// creates and drops its own throwaway pjsk_migration_test_* database and skips
// entirely unless PJSK_RUN_ISOLATED_MIGRATION_TESTS=1. The production database
// is never connected to; see isolated_test_support_test.go for the guards.

// Test 1: a brand-new empty database applies every repository migration.
func TestIsolatedMigrationsFreshDatabase(t *testing.T) {
	requireIsolatedTestEnv(t)

	dbName := uniqueTestDatabaseName(t, "fresh")
	createIsolatedTestDatabase(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := openTestPool(t, ctx, dbName)
	migrationFS := os.DirFS(repoFSRoot)

	if err := RunMigrations(ctx, pool, migrationFS, migrationsSubdir); err != nil {
		t.Fatalf("RunMigrations on a fresh database: %v", err)
	}

	if !tableExists(t, ctx, pool, "schema_migrations") {
		t.Fatal("schema_migrations was not created")
	}

	// Two-way comparison of the FULL filename sets, not just a count.
	repo := repoMigrationNames(t)
	applied := appliedVersions(t, ctx, pool)

	for _, name := range repo {
		if !slices.Contains(applied, name) {
			t.Errorf("repository migration %q was not applied", name)
		}
	}
	for _, name := range applied {
		if !slices.Contains(repo, name) {
			t.Errorf("schema_migrations contains %q, which is not a repository migration", name)
		}
	}
	if len(applied) != len(repo) {
		t.Errorf("applied %d migrations, repository has %d", len(applied), len(repo))
	}

	// The historical exception, verified against a real database.
	importIdx := slices.Index(applied, "0005_import_history.sql")
	seriesIdx := slices.Index(applied, "0005_product_series.sql")
	if importIdx == -1 || seriesIdx == -1 {
		t.Fatalf("both 0005 migrations must be recorded, got %v", applied)
	}
	if importIdx >= seriesIdx {
		t.Errorf("0005_import_history.sql must sort before 0005_product_series.sql, got %d and %d", importIdx, seriesIdx)
	}
	for _, name := range applied {
		if strings.HasPrefix(name, "0006") {
			t.Errorf("a 0006 migration was applied (%q); the gap is permanent", name)
		}
	}
	if !slices.Contains(applied, "0019_admin_auth_audit_events.sql") {
		t.Error("0019_admin_auth_audit_events.sql was not applied")
	}

	// Effects of each 0005 and of 0019 must actually be present.
	if !columnExists(t, ctx, pool, "import_batches", "warnings_accepted") {
		t.Error("0005_import_history.sql effect missing: import_batches.warnings_accepted")
	}
	if !columnExists(t, ctx, pool, "products", "series_code") {
		t.Error("0005_product_series.sql effect missing: products.series_code")
	}
	if !tableExists(t, ctx, pool, "admin_auth_audit_events") {
		t.Error("0019 effect missing: admin_auth_audit_events table")
	}
	for _, table := range []string{"users", "orders", "order_items", "payments", "payment_items", "admins"} {
		if !tableExists(t, ctx, pool, table) {
			t.Errorf("expected business table %q to exist", table)
		}
	}

	// Second run must be a no-op: no new rows, no schema change.
	beforeTables := countPublicTables(t, ctx, pool)
	if err := RunMigrations(ctx, pool, migrationFS, migrationsSubdir); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
	afterApplied := appliedVersions(t, ctx, pool)
	if len(afterApplied) != len(applied) {
		t.Errorf("second run changed schema_migrations from %d to %d rows", len(applied), len(afterApplied))
	}
	if got := countPublicTables(t, ctx, pool); got != beforeTables {
		t.Errorf("second run changed the table count from %d to %d", beforeTables, got)
	}
}

func countPublicTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, "select count(*) from information_schema.tables where table_schema = 'public' and table_type = 'BASE TABLE'").Scan(&n); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	return n
}

// Test 2: with only ONE of the two 0005 migrations already applied, the runner
// must skip that one and apply the other - in both directions.
func TestIsolatedMigrationsSingle0005SkipSemantics(t *testing.T) {
	requireIsolatedTestEnv(t)

	cases := []struct {
		name        string
		alreadyDone string
		mustRun     string
	}{
		{"import_history_present", "0005_import_history.sql", "0005_product_series.sql"},
		{"product_series_present", "0005_product_series.sql", "0005_import_history.sql"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh database per case: no state carried between them.
			dbName := uniqueTestDatabaseName(t, "s5_"+strings.Split(tc.name, "_")[0])
			createIsolatedTestDatabase(t, dbName)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			pool := openTestPool(t, ctx, dbName)
			migrationFS := os.DirFS(repoFSRoot)

			// Reproduce a real historical state: 0001..0004 plus exactly one 0005,
			// applied through the real runner so the rows look genuine.
			partialFS := fstest.MapFS{}
			for _, name := range []string{
				"0001_core_tables.sql", "0002_import_tracking.sql",
				"0003_admin_auth.sql", "0004_import_confirm.sql", tc.alreadyDone,
			} {
				data, err := os.ReadFile("../../migrations/" + name)
				if err != nil {
					t.Fatalf("read %s: %v", name, err)
				}
				partialFS[syntheticFSSubdir+"/"+name] = &fstest.MapFile{Data: data}
			}
			if err := RunMigrations(ctx, pool, partialFS, syntheticFSSubdir); err != nil {
				t.Fatalf("seed historical state: %v", err)
			}
			t.Logf("seeded with: 0001..0004 and %s", tc.alreadyDone)

			seeded := appliedVersions(t, ctx, pool)
			if !slices.Contains(seeded, tc.alreadyDone) {
				t.Fatalf("seed did not record %s: %v", tc.alreadyDone, seeded)
			}
			if slices.Contains(seeded, tc.mustRun) {
				t.Fatalf("seed unexpectedly recorded %s", tc.mustRun)
			}
			var seededAppliedAt time.Time
			if err := pool.QueryRow(ctx, "select applied_at from schema_migrations where version = $1", tc.alreadyDone).Scan(&seededAppliedAt); err != nil {
				t.Fatalf("read applied_at: %v", err)
			}

			// Now run the FULL set.
			if err := RunMigrations(ctx, pool, migrationFS, migrationsSubdir); err != nil {
				t.Fatalf("full RunMigrations: %v", err)
			}

			applied := appliedVersions(t, ctx, pool)

			// The already-applied 0005 must not be replayed: its row is untouched.
			var afterAppliedAt time.Time
			if err := pool.QueryRow(ctx, "select applied_at from schema_migrations where version = $1", tc.alreadyDone).Scan(&afterAppliedAt); err != nil {
				t.Fatalf("re-read applied_at: %v", err)
			}
			if !afterAppliedAt.Equal(seededAppliedAt) {
				t.Errorf("%s was replayed: applied_at moved from %s to %s", tc.alreadyDone, seededAppliedAt, afterAppliedAt)
			}

			// The missing one must now be applied.
			if !slices.Contains(applied, tc.mustRun) {
				t.Errorf("%s was not applied", tc.mustRun)
			}

			// Both 0005 rows present, exactly once each.
			for _, name := range []string{"0005_import_history.sql", "0005_product_series.sql"} {
				count := 0
				for _, v := range applied {
					if v == name {
						count++
					}
				}
				if count != 1 {
					t.Errorf("expected exactly one row for %s, got %d", name, count)
				}
			}

			// Both effects present, and the chain continued to the max version.
			if !columnExists(t, ctx, pool, "import_batches", "warnings_accepted") {
				t.Error("0005_import_history.sql effect missing")
			}
			if !columnExists(t, ctx, pool, "products", "series_code") {
				t.Error("0005_product_series.sql effect missing")
			}
			repo := repoMigrationNames(t)
			if len(applied) != len(repo) {
				t.Errorf("expected %d migrations applied, got %d", len(repo), len(applied))
			}
			if !slices.Contains(applied, repo[len(repo)-1]) {
				t.Errorf("did not continue to the newest migration %s", repo[len(repo)-1])
			}
		})
	}
}

// Test 3: a migration that fails mid-transaction must leave nothing behind.
func TestIsolatedMigrationFailureRollsBack(t *testing.T) {
	requireIsolatedTestEnv(t)

	dbName := uniqueTestDatabaseName(t, "rollback")
	createIsolatedTestDatabase(t, dbName)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool := openTestPool(t, ctx, dbName)

	// A synthetic FS only - the real migrations directory is never modified.
	goodFirst := "0001_rollback_first.sql"
	failing := "0002_rollback_failing.sql"
	brokenFS := fstest.MapFS{
		syntheticFSSubdir + "/" + goodFirst: &fstest.MapFile{Data: []byte(`create table rollback_first (id int primary key);`)},
		// DDL succeeds, then a guaranteed failure, in the SAME migration file
		// and therefore the same transaction.
		syntheticFSSubdir + "/" + failing: &fstest.MapFile{Data: []byte(`
create table rollback_should_not_exist (id int primary key);
alter table rollback_first add column added_by_failing_migration int;
select * from a_table_that_does_not_exist;
`)},
	}

	err := RunMigrations(ctx, pool, brokenFS, syntheticFSSubdir)
	if err == nil {
		t.Fatal("expected RunMigrations to fail on the broken migration")
	}
	t.Logf("RunMigrations failed as expected on %s", failing)

	applied := appliedVersions(t, ctx, pool)
	if !slices.Contains(applied, goodFirst) {
		t.Errorf("the migration committed before the failure must survive, got %v", applied)
	}
	if slices.Contains(applied, failing) {
		t.Errorf("the failed migration must not be recorded, got %v", applied)
	}
	if tableExists(t, ctx, pool, "rollback_should_not_exist") {
		t.Error("the failed migration's table must not survive; the transaction did not roll back")
	}
	if columnExists(t, ctx, pool, "rollback_first", "added_by_failing_migration") {
		t.Error("the failed migration's column must not survive; the transaction did not roll back")
	}
	if !tableExists(t, ctx, pool, "rollback_first") {
		t.Error("the previously committed migration's table must survive")
	}

	// Fixing the migration and re-running must succeed and resume cleanly.
	fixedFS := fstest.MapFS{
		syntheticFSSubdir + "/" + goodFirst: brokenFS[syntheticFSSubdir+"/"+goodFirst],
		syntheticFSSubdir + "/" + failing:   &fstest.MapFile{Data: []byte(`create table rollback_now_ok (id int primary key);`)},
	}
	if err := RunMigrations(ctx, pool, fixedFS, syntheticFSSubdir); err != nil {
		t.Fatalf("re-run after fixing the migration: %v", err)
	}
	afterApplied := appliedVersions(t, ctx, pool)
	if !slices.Contains(afterApplied, failing) {
		t.Errorf("the fixed migration should now be recorded, got %v", afterApplied)
	}
	if !tableExists(t, ctx, pool, "rollback_now_ok") {
		t.Error("the fixed migration's table should exist")
	}
	firstCount := 0
	for _, v := range afterApplied {
		if v == goodFirst {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Errorf("the already-applied migration must not be replayed; %s appears %d times", goodFirst, firstCount)
	}
}

// Test 4: a context deadline during a migration must not half-apply it, and a
// re-run with a fresh context must resume from the unfinished migration.
func TestIsolatedMigrationTimeoutResumes(t *testing.T) {
	requireIsolatedTestEnv(t)

	dbName := uniqueTestDatabaseName(t, "timeout")
	createIsolatedTestDatabase(t, dbName)

	setupCtx, setupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer setupCancel()
	pool := openTestPool(t, setupCtx, dbName)

	fastFirst := "0001_timeout_fast.sql"
	slowSecond := "0002_timeout_slow.sql"
	// The slow migration creates a table and then sleeps well past the deadline,
	// inside one transaction. The sleep is short so nothing hangs.
	slowFS := fstest.MapFS{
		syntheticFSSubdir + "/" + fastFirst:  &fstest.MapFile{Data: []byte(`create table timeout_fast (id int primary key);`)},
		syntheticFSSubdir + "/" + slowSecond: &fstest.MapFile{Data: []byte("create table timeout_slow (id int primary key);\nselect pg_sleep(1.5);")},
	}

	// Deadline far shorter than the sleep.
	shortCtx, shortCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer shortCancel()

	start := time.Now()
	err := RunMigrations(shortCtx, pool, slowFS, syntheticFSSubdir)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected RunMigrations to fail on the context deadline")
	}
	t.Logf("RunMigrations hit the deadline after %s: %v", elapsed.Round(time.Millisecond), err)
	if elapsed > 30*time.Second {
		t.Errorf("timeout took %s; it should abort promptly", elapsed)
	}

	// Inspect with a fresh, healthy context.
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer checkCancel()

	applied := appliedVersions(t, checkCtx, pool)
	if !slices.Contains(applied, fastFirst) {
		t.Errorf("the migration committed before the deadline must survive, got %v", applied)
	}
	if slices.Contains(applied, slowSecond) {
		t.Errorf("the timed-out migration must not be recorded, got %v", applied)
	}
	if tableExists(t, checkCtx, pool, "timeout_slow") {
		t.Error("the timed-out migration's table must not survive; there must be no half-commit")
	}
	if !tableExists(t, checkCtx, pool, "timeout_fast") {
		t.Error("the previously committed migration's table must survive")
	}

	// Resume with a generous context: the unfinished migration completes, the
	// finished one is not replayed.
	var fastAppliedAt time.Time
	if err := pool.QueryRow(checkCtx, "select applied_at from schema_migrations where version = $1", fastFirst).Scan(&fastAppliedAt); err != nil {
		t.Fatalf("read applied_at: %v", err)
	}

	resumeCtx, resumeCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer resumeCancel()
	if err := RunMigrations(resumeCtx, pool, slowFS, syntheticFSSubdir); err != nil {
		t.Fatalf("resume run: %v", err)
	}

	afterApplied := appliedVersions(t, resumeCtx, pool)
	if !slices.Contains(afterApplied, slowSecond) {
		t.Errorf("the resumed migration should now be recorded, got %v", afterApplied)
	}
	if !tableExists(t, resumeCtx, pool, "timeout_slow") {
		t.Error("the resumed migration's table should now exist")
	}
	var fastAfter time.Time
	if err := pool.QueryRow(resumeCtx, "select applied_at from schema_migrations where version = $1", fastFirst).Scan(&fastAfter); err != nil {
		t.Fatalf("re-read applied_at: %v", err)
	}
	if !fastAfter.Equal(fastAppliedAt) {
		t.Errorf("the already-applied migration was replayed: applied_at moved from %s to %s", fastAppliedAt, fastAfter)
	}
	if len(afterApplied) != 2 {
		t.Errorf("expected exactly 2 migrations recorded, got %d (%v)", len(afterApplied), afterApplied)
	}
}
