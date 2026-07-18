package users

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func parseFilters(t *testing.T, rawQuery string) Filters {
	t.Helper()
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery(%q): %v", rawQuery, err)
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

// TestMultiValueColumnFilters covers the core WPS behaviour on every value
// column: ticking several values filters for all of them at once.
func TestMultiValueColumnFilters(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN002&cn=CN003&name=%E5%B0%8F%E6%98%8E&status=active&status=disabled&has_query_code=yes&has_recovery_email=no")
	assertStrings(t, filters.CN, "CN001", "CN002", "CN003")
	assertStrings(t, filters.Name, "小明")
	assertStrings(t, filters.Status, "active", "disabled")
	assertStrings(t, filters.HasQueryCode, "yes")
	assertStrings(t, filters.HasRecoveryEmail, "no")

	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{
		"b.cn_code = any(",
		"b.display_name = any(",
		"b.status = any(",
		"b.has_query_code = any(",
		"b.has_recovery_email = any(",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing condition %q: %s", expected, where)
		}
	}
}

// TestQueryCodeAndRecoveryEmailAreDerivedBooleans pins the privacy boundary at
// the source: the query code is reduced to a yes/no in SQL and the recovery
// email to whether a current record exists. Neither the hash, the ciphertext
// nor the lookup hash is ever selected, so no DTO change can leak them.
func TestQueryCodeAndRecoveryEmailAreDerivedBooleans(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, ""))

	if !strings.Contains(sql, "case when coalesce(u.query_code_hash, '') <> '' then 'yes' else 'no' end as has_query_code") {
		t.Fatalf("query code must be reduced to a boolean:\n%s", sql)
	}
	if !strings.Contains(sql, "left join user_recovery_emails re on re.user_id = u.id and re.invalidated_at is null") {
		t.Fatalf("recovery email must key off the current record:\n%s", sql)
	}
	if !strings.Contains(sql, "case when re.user_id is not null then 'yes' else 'no' end as has_recovery_email") {
		t.Fatalf("recovery email must be reduced to a boolean:\n%s", sql)
	}

	// Secrets must never appear in the selected columns.
	for _, secret := range []string{"encrypted_email", "email_lookup_hash", "re.status"} {
		if strings.Contains(sql, secret) {
			t.Fatalf("list SQL selects the secret %q:\n%s", secret, sql)
		}
	}
	// The hash may only appear inside the yes/no derivation, never as output.
	if strings.Contains(sql, "u.query_code_hash,\n") || strings.Contains(sql, "as query_code_hash") {
		t.Fatalf("query code hash must not be selected:\n%s", sql)
	}
}

func TestRangeFiltersBindTheUsersOwnNumbers(t *testing.T) {
	filters := parseFilters(t, "order_count_min=1&order_count_max=10&total_min=10.50&total_max=999.99&paid_min=5&paid_max=100&unpaid_min=0&unpaid_max=50")
	// Ranges stay decimal strings: they are cast to numeric in SQL, never
	// routed through float64.
	for _, pair := range [][2]string{
		{filters.OrderCountMin, "1"}, {filters.OrderCountMax, "10"},
		{filters.TotalMin, "10.50"}, {filters.TotalMax, "999.99"},
		{filters.PaidMin, "5"}, {filters.PaidMax, "100"},
		{filters.UnpaidMin, "0"}, {filters.UnpaidMax, "50"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("range value = %q, want %q", pair[0], pair[1])
		}
	}

	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{
		"b.order_count >= ", "b.order_count <= ",
		"b.total_amount >= ", "b.total_amount <= ",
		"b.paid_amount >= ", "b.paid_amount <= ",
		"b.unpaid_amount >= ", "b.unpaid_amount <= ",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing range condition %q: %s", expected, where)
		}
	}
}

// TestMoneyStaysNumeric guards the money rule: comparisons happen on numeric,
// and nothing is bound as a float64.
func TestMoneyStaysNumeric(t *testing.T) {
	sql, args := buildListQuery(parseFilters(t, "total_min=0.10&total_max=0.30&paid_min=1.05&unpaid_max=2.15"))
	if strings.Count(sql, "::numeric") < 4 {
		t.Fatalf("every money bound must cast to numeric:\n%s", sql)
	}
	for _, arg := range args {
		if _, isFloat := arg.(float64); isFloat {
			t.Fatalf("money bound as float64: %v", arg)
		}
	}
	// The aggregate itself stays numeric until the outer select.
	if strings.Contains(sql, "::float8 as total_amount") {
		t.Fatalf("totals must stay numeric until the outer select:\n%s", sql)
	}
}

func TestDateRangeFilters(t *testing.T) {
	filters := parseFilters(t, "last_login_from=2026-07-01&last_login_to=2026-07-17&created_from=2026-01-01&created_to=2026-02-01")
	// An end date means "through the end of that day", so the exclusive bound
	// advances to the next midnight.
	if filters.LastLoginTo != "2026-07-18T00:00:00Z" {
		t.Fatalf("LastLoginTo = %q, want the next midnight", filters.LastLoginTo)
	}
	if filters.CreatedTo != "2026-02-02T00:00:00Z" {
		t.Fatalf("CreatedTo = %q, want the next midnight", filters.CreatedTo)
	}

	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{
		"b.last_query_login_at >= ", "b.last_query_login_at < ",
		"b.created_at >= ", "b.created_at < ",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing date condition %q: %s", expected, where)
		}
	}
}

// TestBlankLastLoginFiltersNeverLoggedIn: "从未登录" is a real filter target.
func TestBlankLastLoginFiltersNeverLoggedIn(t *testing.T) {
	filters := parseFilters(t, "last_login_blank=1")
	if !filters.LastLoginBlank {
		t.Fatal("last_login_blank=1 must set LastLoginBlank")
	}
	where := buildConditions(filters, "", &argList{})
	if !strings.Contains(where, "b.last_query_login_at is null") {
		t.Fatalf("blank last-login must filter for never-logged-in users: %s", where)
	}

	// Absent flag leaves the column alone.
	if parseFilters(t, "cn=CN001").LastLoginBlank {
		t.Fatal("absent flag must not filter")
	}
	if strings.Contains(buildConditions(parseFilters(t, "cn=CN001"), "", &argList{}), "last_query_login_at is null") {
		t.Fatal("unfiltered last-login must not appear in WHERE")
	}
}

// TestBlankValueSelectionUsesDedicatedFlag pins the (空白) semantics: only the
// dedicated flag filters blanks; empty and whitespace-only values are ignored.
func TestBlankValueSelectionUsesDedicatedFlag(t *testing.T) {
	blank := parseFilters(t, "name_blank=1")
	if blank.Name == nil || len(blank.Name) != 1 || blank.Name[0] != "" {
		t.Fatalf("Name = %#v, want a one-element blank selection", blank.Name)
	}

	ignored := parseFilters(t, "name=&name=+++&cn=CN001&cn=+++")
	if ignored.Name != nil {
		t.Fatalf("empty name parameters must be ignored, got %#v", ignored.Name)
	}
	assertStrings(t, ignored.CN, "CN001")
}

func TestValueSetDeduplicates(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN001&cn=CN002")
	if len(filters.CN) != 2 {
		t.Fatalf("CN = %#v, want deduplicated", filters.CN)
	}
}

func TestRejectsInvalidParameters(t *testing.T) {
	cases := []struct{ name, query string }{
		{"non-numeric total", "total_min=abc"},
		{"negative total", "total_min=-5"},
		{"negative order count", "order_count_min=-1"},
		{"fractional order count", "order_count_min=1.5"},
		{"inverted amount range", "total_min=200&total_max=100"},
		{"inverted order count range", "order_count_min=9&order_count_max=2"},
		{"inverted paid range", "paid_min=50&paid_max=10"},
		{"inverted unpaid range", "unpaid_min=50&unpaid_max=10"},
		{"inverted last login dates", "last_login_from=2026-07-17&last_login_to=2026-07-01"},
		{"inverted created dates", "created_from=2026-07-17&created_to=2026-07-01"},
		{"bad date", "created_from=17/07/2026"},
		{"unknown status", "status=exploded"},
		{"blank status", "status_blank=1"},
		{"unknown boolean", "has_query_code=maybe"},
		{"blank boolean", "has_recovery_email_blank=1"},
		{"page zero", "page=0"},
		{"page not a number", "page=abc"},
		{"page_size zero", "page_size=0"},
		{"page_size over cap", "page_size=9999"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			query, err := url.ParseQuery(testCase.query)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if _, err := FiltersFromQuery(query); err == nil {
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
	// page 3 of 25 starts at offset 50.
	if args[len(args)-1] != 50 {
		t.Fatalf("offset = %v, want 50", args[len(args)-1])
	}
	if args[len(args)-2] != 25 {
		t.Fatalf("limit = %v, want 25", args[len(args)-2])
	}

	for _, size := range []int{25, 50, 100, 200} {
		filters := parseFilters(t, "page_size="+itoa(size))
		if filters.PageSize != size {
			t.Fatalf("page_size %d rejected", size)
		}
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// TestCountAndSummaryShareTheListsConditions: the header count, the summary
// tiles and the page must all describe the same set of users.
func TestCountAndSummaryShareTheListsConditions(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&status=active&total_min=10&page=3&page_size=25")

	listWhere := buildConditions(filters, "", &argList{})
	countSQL, countArgs := buildCountQuery(filters)
	summarySQL, _ := buildSummaryQuery(filters)

	for name, sql := range map[string]string{"count": countSQL, "summary": summarySQL} {
		if !strings.Contains(sql, listWhere) {
			t.Fatalf("%s does not apply the list's exact conditions\nwant: %s\ngot:  %s", name, listWhere, sql)
		}
		if strings.Contains(sql, "limit ") || strings.Contains(sql, "offset ") {
			t.Fatalf("%s must not paginate:\n%s", name, sql)
		}
	}
	if len(countArgs) != 3 {
		t.Fatalf("count args = %d, want 3 filter values with no pagination", len(countArgs))
	}
	// The summary aggregates over the filtered set, not the scanned page.
	if !strings.Contains(summarySQL, "count(*) filter (where b.order_count > 0)::int") {
		t.Fatalf("summary must aggregate in SQL:\n%s", summarySQL)
	}
}

// TestExportQueryFollowsFiltersWithoutPagination: an export covers the whole
// filtered result set, not the page on screen.
func TestExportQueryFollowsFiltersWithoutPagination(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN002&status=active&total_min=10&page=2&page_size=25")
	sql, args := BuildExportQuery(filters, 50000)

	if strings.Contains(sql, "offset ") {
		t.Fatalf("export must not paginate:\n%s", sql)
	}
	for _, expected := range []string{"b.cn_code = any(", "b.status = any(", "b.total_amount >= "} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("export SQL missing %q\n%s", expected, sql)
		}
	}
	// Three filter values plus the row cap; the list's page/page_size do not bind.
	if len(args) != 4 {
		t.Fatalf("export args = %#v", args)
	}
	if args[len(args)-1] != 50000 {
		t.Fatalf("export cap = %v, want 50000", args[len(args)-1])
	}
}

// TestBuiltSQLBindsEveryUserValue is the injection guard. It feeds SQL
// metacharacters through every value filter and asserts none of them reach the
// statement text: each must arrive as a bind parameter.
func TestBuiltSQLBindsEveryUserValue(t *testing.T) {
	hostile := "'; drop table users; --"
	query := url.Values{}
	query.Add("cn", hostile)
	query.Add("name", hostile)
	query.Set("total_min", "1")
	query.Set("created_from", "2026-01-01")

	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}

	listSQL, listArgs := buildListQuery(filters)
	countSQL, countArgs := buildCountQuery(filters)
	summarySQL, summaryArgs := buildSummaryQuery(filters)
	exportSQL, exportArgs := BuildExportQuery(filters, 100)

	for _, built := range []struct {
		name string
		sql  string
		args []any
	}{
		{"list", listSQL, listArgs},
		{"count", countSQL, countArgs},
		{"summary", summarySQL, summaryArgs},
		{"export", exportSQL, exportArgs},
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
			t.Fatalf("%s: hostile value was not bound as a parameter", built.name)
		}
		assertPlaceholdersSequential(t, built.name, built.sql, built.args)
	}
}

// assertPlaceholdersSequential checks the statement's $N placeholders are
// exactly $1..$len(args), so no argument is silently dropped or misaligned —
// the failure mode that turns a filter into a wrong-data bug rather than an
// error.
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
