package main

import (
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestStartupTimeoutsAreSeparateBudgets locks the startup timeout policy: the
// connect phase and the migration phase must not share one budget. They did
// share a single 10s context, which meant a fresh database's ~100 migration
// round trips competed with connection setup for the same 10 seconds — fine
// against a local database, tight against a remote one.
//
// This asserts the policy, not the runtime behavior; exercising a real timeout
// needs a database and is designed for the isolated-database stage.
func TestStartupTimeoutsAreSeparateBudgets(t *testing.T) {
	if databaseConnectTimeout <= 0 {
		t.Errorf("databaseConnectTimeout must be positive, got %s", databaseConnectTimeout)
	}
	if databaseMigrationTimeout <= 0 {
		t.Errorf("databaseMigrationTimeout must be positive, got %s", databaseMigrationTimeout)
	}

	// Finite: a migration blocked on a lock must not hang startup forever.
	if databaseMigrationTimeout > 10*time.Minute {
		t.Errorf("databaseMigrationTimeout %s is too large to bound a stuck migration", databaseMigrationTimeout)
	}

	// Migrations need materially more room than a single connection handshake.
	if databaseMigrationTimeout <= databaseConnectTimeout {
		t.Errorf("databaseMigrationTimeout (%s) must exceed databaseConnectTimeout (%s); a shared budget is what this policy exists to prevent",
			databaseMigrationTimeout, databaseConnectTimeout)
	}

	// Connect stays short so a bad DSN or a down database fails fast rather than
	// stalling the service wrapper's restart-with-backoff loop.
	if databaseConnectTimeout > 30*time.Second {
		t.Errorf("databaseConnectTimeout %s is too long to fail fast on an unreachable database", databaseConnectTimeout)
	}
}

// TestEmbeddedMigrationsMatchDisk guards the //go:embed migrations/*.sql
// directive: every .sql file on disk must be embedded (and vice versa), so a
// newly added migration such as 0019_admin_auth_audit_events.sql cannot be
// silently left out of the binary. Fully offline — no database connection.
func TestEmbeddedMigrationsMatchDisk(t *testing.T) {
	embeddedEntries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	embedded := make([]string, 0, len(embeddedEntries))
	for _, entry := range embeddedEntries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			embedded = append(embedded, entry.Name())
		}
	}
	slices.Sort(embedded)

	diskEntries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	disk := make([]string, 0, len(diskEntries))
	for _, entry := range diskEntries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			disk = append(disk, entry.Name())
		}
	}
	slices.Sort(disk)

	if !slices.Equal(embedded, disk) {
		t.Fatalf("embedded migrations do not match disk:\nembedded: %v\ndisk:     %v", embedded, disk)
	}

	for _, required := range []string{
		"0005_import_history.sql",
		"0005_product_series.sql",
		"0019_admin_auth_audit_events.sql",
	} {
		if !slices.Contains(embedded, required) {
			t.Errorf("embedded migrations missing %s", required)
		}
	}
}
