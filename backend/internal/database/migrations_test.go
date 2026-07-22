package database

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// These tests are fully offline: they never open a database connection. They
// lock in the migration runner's name-selection behavior and the repository's
// migration-file naming conventions, including the historical exception of
// two 0005_* files and no 0006_* file (see HANDOVER.md "Known Issues").

func TestListMigrationNamesSortsByFullFilename(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0010_later.sql":          {Data: []byte("select 1;")},
		"migrations/0005_product_series.sql": {Data: []byte("select 1;")},
		"migrations/0005_import_history.sql": {Data: []byte("select 1;")},
		"migrations/0001_core.sql":           {Data: []byte("select 1;")},
		"migrations/readme.txt":              {Data: []byte("not sql")},
		"migrations/nested/0002_nested.sql":  {Data: []byte("select 1;")},
	}

	names, err := listMigrationNames(fsys, "migrations")
	if err != nil {
		t.Fatalf("listMigrationNames: %v", err)
	}

	want := []string{
		"0001_core.sql",
		"0005_import_history.sql",
		"0005_product_series.sql",
		"0010_later.sql",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("listMigrationNames = %v, want %v", names, want)
	}
}

func TestFeedbackMigrationHasMVPConstraintsAndIndexes(t *testing.T) {
	sqlBytes, err := os.ReadFile("../../migrations/0026_feedbacks.sql")
	if err != nil {
		t.Fatalf("read feedback migration: %v", err)
	}
	sql := strings.ToLower(string(sqlBytes))
	for _, required := range []string{
		"create table if not exists feedbacks",
		"user_id uuid not null references users(id)",
		"char_length(btrim(content)) between 1 and 1000",
		"status in ('new', 'processed')",
		"on feedbacks (status, created_at desc)",
		"on feedbacks (user_id, created_at desc)",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("feedback migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"reply", "attachment", "email", "notification", "handled_by", "status_history"} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("feedback migration contains forbidden MVP field/system %q", forbidden)
		}
	}
}

func TestListMigrationNamesKeepsDuplicateNumericPrefixesDistinct(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0005_import_history.sql": {Data: []byte("select 1;")},
		"migrations/0005_product_series.sql": {Data: []byte("select 1;")},
	}

	names, err := listMigrationNames(fsys, "migrations")
	if err != nil {
		t.Fatalf("listMigrationNames: %v", err)
	}

	if len(names) != 2 {
		t.Fatalf("expected both 0005_* files to load as distinct migrations, got %v", names)
	}
	if names[0] != "0005_import_history.sql" || names[1] != "0005_product_series.sql" {
		t.Fatalf("expected byte-order 0005_import_history.sql before 0005_product_series.sql, got %v", names)
	}
}

func TestRepositoryMigrationDirectoryConventions(t *testing.T) {
	names, err := listMigrationNames(os.DirFS("../../migrations"), ".")
	if err != nil {
		t.Fatalf("listMigrationNames on backend/migrations: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("no migration files found under backend/migrations")
	}

	if !slices.IsSorted(names) {
		t.Fatalf("migration names are not sorted by full filename: %v", names)
	}

	namePattern := regexp.MustCompile(`^\d{4}_[a-z0-9_]+\.sql$`)
	for _, name := range names {
		if !namePattern.MatchString(name) {
			t.Errorf("migration filename %q does not match NNNN_snake_case.sql; non-4-digit prefixes break byte-order == numeric-order", name)
		}
	}

	// The historical exception: exactly two 0005_* files, applied in this
	// relative order, and no other numeric prefix may ever be duplicated.
	importIdx := slices.Index(names, "0005_import_history.sql")
	seriesIdx := slices.Index(names, "0005_product_series.sql")
	if importIdx == -1 || seriesIdx == -1 {
		t.Fatalf("expected both historical 0005_* migrations to be present, got %v", names)
	}
	if importIdx >= seriesIdx {
		t.Fatalf("expected 0005_import_history.sql to sort before 0005_product_series.sql, got indexes %d and %d", importIdx, seriesIdx)
	}

	prefixCounts := make(map[string]int)
	maxPrefix := 0
	for _, name := range names {
		prefix := name[:4]
		prefixCounts[prefix]++
		n, err := strconv.Atoi(prefix)
		if err != nil {
			continue // already reported by the pattern check above
		}
		if n > maxPrefix {
			maxPrefix = n
		}
	}
	for prefix, count := range prefixCounts {
		if count > 1 && prefix != "0005" {
			t.Errorf("numeric prefix %s is used by %d files; only the historical 0005 pair may share a prefix", prefix, count)
		}
	}
	if prefixCounts["0005"] != 2 {
		t.Errorf("expected exactly 2 files with prefix 0005, got %d", prefixCounts["0005"])
	}

	// Numbering must stay contiguous from 0001 to the maximum, except 0006,
	// which is a permanent gap: backfilling 0006 on databases that already
	// ran past it would apply out of order, so it must never be added.
	if prefixCounts["0006"] != 0 {
		t.Error("a 0006_* migration was added; the 0006 gap is a permanent historical exception and must not be backfilled")
	}
	for n := 1; n <= maxPrefix; n++ {
		if n == 6 {
			continue
		}
		prefix := make([]byte, 0, 4)
		prefix = strconv.AppendInt(prefix, int64(n), 10)
		padded := "0000"[:4-len(prefix)] + string(prefix)
		if prefixCounts[padded] == 0 {
			t.Errorf("migration numbering has an unexpected gap: no file with prefix %s", padded)
		}
	}

	if !slices.Contains(names, "0019_admin_auth_audit_events.sql") {
		t.Errorf("expected 0019_admin_auth_audit_events.sql to be loadable, got %v", names)
	}
}
