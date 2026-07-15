package main

import (
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
)

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
