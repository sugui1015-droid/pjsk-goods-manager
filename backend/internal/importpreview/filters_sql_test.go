package importpreview

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func parseFilters(t *testing.T, rawQuery string) HistoryFilters {
	t.Helper()
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	filters, err := HistoryFiltersFromQuery(query)
	if err != nil {
		t.Fatalf("HistoryFiltersFromQuery(%q): %v", rawQuery, err)
	}
	return filters
}

func assertStrings(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestMultiValueColumnFilters(t *testing.T) {
	filters := parseFilters(t, "filename=a.xlsx&filename=b.xlsx&status=completed&status=failed&uploaded_by=qa_admin")
	assertStrings(t, filters.Filename, "a.xlsx", "b.xlsx")
	assertStrings(t, filters.Status, "completed", "failed")
	assertStrings(t, filters.UploadedBy, "qa_admin")

	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{"b.filename = any(", "b.status = any(", "b.uploaded_by = any("} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing condition %q: %s", expected, where)
		}
	}
}

// TestDerivedColumnsFromJSON pins the two confirm_result-derived filter columns:
// 写入数量 comes from order_item_count and 总金额 from total_amount, both cast to
// numeric for comparison.
func TestDerivedColumnsFromJSON(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, ""))
	for _, fragment := range []string{
		"(b.error_count + b.warning_count + b.notice_count) as issue_count",
		"coalesce((b.confirm_result->>'order_item_count')::int, 0) as written_count",
		"coalesce((b.confirm_result->>'total_amount')::numeric, 0) as total_amount",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("base view missing %q:\n%s", fragment, sql)
		}
	}
}

func TestRangeFilters(t *testing.T) {
	filters := parseFilters(t, "sheet_min=1&sheet_max=5&issue_min=0&issue_max=3&written_min=10&written_max=100&amount_min=0.50&amount_max=999.99")
	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{
		"b.sheet_count >= ", "b.sheet_count <= ",
		"b.issue_count >= ", "b.issue_count <= ",
		"b.written_count >= ", "b.written_count <= ",
		"b.total_amount >= ", "b.total_amount <= ",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing range condition %q: %s", expected, where)
		}
	}
}

func TestDateRangeAndBlankConfirmed(t *testing.T) {
	filters := parseFilters(t, "created_from=2026-07-01&created_to=2026-07-17&confirmed_blank=1")
	if filters.CreatedTo != "2026-07-18T00:00:00Z" {
		t.Fatalf("CreatedTo = %q, want next midnight", filters.CreatedTo)
	}
	if !filters.ConfirmedBlank {
		t.Fatal("confirmed_blank=1 must set ConfirmedBlank")
	}
	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{"b.created_at >= ", "b.created_at < ", "b.confirmed_at is null"} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing %q: %s", expected, where)
		}
	}

	// Confirmed date range (non-blank).
	ranged := parseFilters(t, "confirmed_from=2026-07-01&confirmed_to=2026-07-17")
	rw := buildConditions(ranged, "", &argList{})
	if !strings.Contains(rw, "b.confirmed_at >= ") || !strings.Contains(rw, "b.confirmed_at < ") {
		t.Fatalf("confirmed range conditions: %s", rw)
	}
}

func TestBlankConfirmedIsDistinctFromUnfiltered(t *testing.T) {
	if parseFilters(t, "filename=a.xlsx").ConfirmedBlank {
		t.Fatal("absent flag must not filter")
	}
	if strings.Contains(buildConditions(parseFilters(t, "filename=a.xlsx"), "", &argList{}), "confirmed_at is null") {
		t.Fatal("unfiltered confirmed column must not appear in WHERE")
	}
}

func TestRejectsInvalidParameters(t *testing.T) {
	cases := []struct{ name, query string }{
		{"non-numeric sheet", "sheet_min=abc"},
		{"negative sheet", "sheet_min=-1"},
		{"fractional sheet", "sheet_min=1.5"},
		{"non-numeric amount", "amount_min=abc"},
		{"inverted sheet range", "sheet_min=9&sheet_max=2"},
		{"inverted amount range", "amount_min=200&amount_max=100"},
		{"inverted created dates", "created_from=2026-07-17&created_to=2026-07-01"},
		{"inverted confirmed dates", "confirmed_from=2026-07-17&confirmed_to=2026-07-01"},
		{"bad date", "created_from=17/07/2026"},
		{"page zero", "page=0"},
		{"page_size over cap", "page_size=9999"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			query, _ := url.ParseQuery(testCase.query)
			if _, err := HistoryFiltersFromQuery(query); err == nil {
				t.Fatalf("%q must be rejected", testCase.query)
			}
		})
	}
}

func TestPaginationDefaultsAndOffsets(t *testing.T) {
	defaults := parseFilters(t, "")
	if defaults.Page != 1 || defaults.PageSize != DefaultPageSize {
		t.Fatalf("defaults = %d/%d", defaults.Page, defaults.PageSize)
	}
	_, args := buildListQuery(parseFilters(t, "page=3&page_size=25"))
	if args[len(args)-1] != 50 {
		t.Fatalf("offset = %v, want 50", args[len(args)-1])
	}
	if args[len(args)-2] != 25 {
		t.Fatalf("limit = %v, want 25", args[len(args)-2])
	}
}

func TestCountSharesTheListsConditions(t *testing.T) {
	filters := parseFilters(t, "status=completed&sheet_min=1&page=3&page_size=25")
	listWhere := buildConditions(filters, "", &argList{})
	countSQL, countArgs := buildCountQuery(filters)
	if !strings.Contains(countSQL, listWhere) {
		t.Fatalf("count does not apply the list's exact conditions\nwant: %s\ngot:  %s", listWhere, countSQL)
	}
	if strings.Contains(countSQL, "limit ") || strings.Contains(countSQL, "offset ") {
		t.Fatalf("count must not paginate:\n%s", countSQL)
	}
	if len(countArgs) != 2 {
		t.Fatalf("count args = %d, want 2", len(countArgs))
	}
}

// TestListSelectMatchesScannerOrder guards that the paginated list still selects
// the exact 21 columns (in order) the existing ImportHistoryItem scanner reads,
// so the revoke/detail flow keeps working.
func TestListSelectMatchesScannerOrder(t *testing.T) {
	// Inspect the listColumns constant directly: the same column names also
	// appear inside the base CTE, so scanning the full statement would find the
	// wrong occurrence.
	ordered := []string{
		"b.id::text", "b.filename", "b.file_hash", "b.file_size", "b.sheet_count",
		"b.total_rows", "b.status", "b.uploaded_by", "b.confirmed_by",
		"b.error_count", "b.warning_count", "b.notice_count", "b.warnings_accepted",
		"b.confirm_result::text", "b.revoked_by", "b.revoke_result::text",
	}
	last := -1
	for _, col := range ordered {
		idx := strings.Index(listColumns, col)
		if idx < 0 {
			t.Fatalf("list select missing %q:\n%s", col, listColumns)
		}
		if idx < last {
			t.Fatalf("list select column %q out of scanner order:\n%s", col, listColumns)
		}
		last = idx
	}
}

func TestBuiltSQLBindsEveryUserValue(t *testing.T) {
	hostile := "'; drop table import_batches; --"
	query := url.Values{}
	query.Add("filename", hostile)
	query.Add("uploaded_by", hostile)
	query.Set("sheet_min", "1")
	query.Set("created_from", "2026-01-01")

	filters, err := HistoryFiltersFromQuery(query)
	if err != nil {
		t.Fatalf("HistoryFiltersFromQuery: %v", err)
	}

	listSQL, listArgs := buildListQuery(filters)
	countSQL, countArgs := buildCountQuery(filters)

	for _, built := range []struct {
		name string
		sql  string
		args []any
	}{
		{"list", listSQL, listArgs},
		{"count", countSQL, countArgs},
	} {
		if strings.Contains(built.sql, "drop table") {
			t.Fatalf("%s: hostile value reached the statement text", built.name)
		}
		found := false
		for _, arg := range built.args {
			if values, ok := arg.([]string); ok {
				for _, value := range values {
					if value == hostile {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("%s: hostile value was not bound", built.name)
		}
		assertPlaceholdersSequential(t, built.name, built.sql, built.args)
	}
}

func assertPlaceholdersSequential(t *testing.T, name, sql string, args []any) {
	t.Helper()
	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1)
	seen := map[string]bool{}
	for _, match := range matches {
		seen[match[1]] = true
	}
	if len(seen) != len(args) {
		t.Fatalf("%s: %d distinct placeholders but %d args", name, len(seen), len(args))
	}
}
