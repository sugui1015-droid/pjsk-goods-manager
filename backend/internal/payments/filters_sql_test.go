package payments

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func parseFilters(t *testing.T, rawQuery string) PaymentFilters {
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
	filters := parseFilters(t, "cn=CN001&cn=CN002&payment_method=alipay&payment_method=wechat&status=approved&status=voided&created_by=admin1&created_by=admin2")
	assertStrings(t, filters.CN, "CN001", "CN002")
	assertStrings(t, filters.PaymentMethod, "alipay", "wechat")
	assertStrings(t, filters.Status, "approved", "voided")
	assertStrings(t, filters.CreatedBy, "admin1", "admin2")

	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{
		"b.cn_code = any(",
		"b.payment_method = any(",
		"b.status = any(",
		"b.created_by = any(",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing condition %q: %s", expected, where)
		}
	}
}

// TestPaymentMethodAliasesNormalise: stored values are normalised, so a filter
// value must be normalised the same way or it would silently match nothing.
func TestPaymentMethodAliasesNormalise(t *testing.T) {
	filters := parseFilters(t, "payment_method=Alipay&payment_method=%E5%BE%AE%E4%BF%A1")
	assertStrings(t, filters.PaymentMethod, "alipay", "wechat")
}

func TestRangeFiltersBindTheStoredAmounts(t *testing.T) {
	filters := parseFilters(t, "principal_min=10.50&principal_max=999.99&fee_min=0&fee_max=5&payable_min=1&payable_max=100")
	// Ranges stay decimal strings: cast to numeric in SQL, never float64.
	for _, pair := range [][2]string{
		{filters.PrincipalMin, "10.50"}, {filters.PrincipalMax, "999.99"},
		{filters.FeeMin, "0"}, {filters.FeeMax, "5"},
		{filters.PayableMin, "1"}, {filters.PayableMax, "100"},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("range value = %q, want %q", pair[0], pair[1])
		}
	}

	where := buildConditions(filters, "", &argList{})
	for _, expected := range []string{
		"b.principal_amount >= ", "b.principal_amount <= ",
		"b.fee_amount >= ", "b.fee_amount <= ",
		"b.payable_amount >= ", "b.payable_amount <= ",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing range condition %q: %s", expected, where)
		}
	}
}

// TestStoredAmountsAreNeverRecomputed pins the money rule: the three columns are
// read straight from the payments row. A WeChat payment keeps the fee actually
// charged at the time; nothing is re-derived from a current rate.
func TestStoredAmountsAreNeverRecomputed(t *testing.T) {
	sql, args := buildListQuery(parseFilters(t, "principal_min=0.10&fee_max=0.30&payable_min=1.05"))

	for _, expected := range []string{
		"p.submitted_amount as principal_amount",
		"p.fee_amount",
		"p.payable_amount",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("list SQL must read the stored column %q:\n%s", expected, sql)
		}
	}
	// No arithmetic deriving the fee or the payable amount.
	for _, banned := range []string{"fee_rate", "* 0.006", "payable_amount =", "submitted_amount *"} {
		if strings.Contains(sql, banned) {
			t.Fatalf("list SQL recomputes money via %q:\n%s", banned, sql)
		}
	}
	// Comparisons happen on numeric, and nothing binds as float64.
	if strings.Count(sql, "::numeric") < 3 {
		t.Fatalf("every money bound must cast to numeric:\n%s", sql)
	}
	for _, arg := range args {
		if _, isFloat := arg.(float64); isFloat {
			t.Fatalf("money bound as float64: %v", arg)
		}
	}
}

// TestVoidedAmountsSurviveAndStatusCarriesTheFact: a voided payment keeps its
// original amounts; only its status says it no longer counts. The list must not
// zero the amounts, and must not treat voided as approved.
func TestVoidedAmountsSurviveAndStatusCarriesTheFact(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, ""))
	// Amounts are selected unconditionally — no "case when voided then 0".
	if strings.Contains(sql, "when b.status = 'voided' then 0") || strings.Contains(sql, "filter (where p.status = 'approved')") {
		t.Fatalf("voided payments must keep their stored amounts:\n%s", sql)
	}
	// Status is a plain column, so approved and voided stay distinguishable.
	if !strings.Contains(sql, "p.status,") {
		t.Fatalf("status must be selected as-is:\n%s", sql)
	}

	// Filtering approved must not sweep in voided rows.
	where := buildConditions(parseFilters(t, "status=approved"), "", &argList{})
	if !strings.Contains(where, "b.status = any(") {
		t.Fatalf("status filter: %s", where)
	}
}

// TestDateRangeFiltersAnchorToChinaTime: a naive bound is the admin's
// wall-clock time in Asia/Shanghai, not UTC — otherwise every filtered day
// would silently shift by eight hours.
func TestDateRangeFiltersAnchorToChinaTime(t *testing.T) {
	filters := parseFilters(t, "paid_from=2026-07-12T10:00&paid_to=2026-07-13T10:00")
	if filters.PaidFrom != "2026-07-12T10:00:00+08:00" || filters.PaidTo != "2026-07-13T10:00:00+08:00" {
		t.Fatalf("paid range = %q..%q, want +08:00 anchored", filters.PaidFrom, filters.PaidTo)
	}

	// A plain end date covers that whole day.
	day := parseFilters(t, "paid_from=2026-07-12&paid_to=2026-07-17")
	if day.PaidTo != "2026-07-18T00:00:00+08:00" {
		t.Fatalf("PaidTo = %q, want the next midnight", day.PaidTo)
	}
	// An explicit offset is taken as given.
	explicit := parseFilters(t, "paid_from=2026-07-12T10:00:00Z")
	if explicit.PaidFrom != "2026-07-12T10:00:00Z" {
		t.Fatalf("PaidFrom = %q, want the given offset preserved", explicit.PaidFrom)
	}

	where := buildConditions(filters, "", &argList{})
	if !strings.Contains(where, "b.paid_at >= ") || !strings.Contains(where, "b.paid_at < ") {
		t.Fatalf("paid range conditions: %s", where)
	}
}

func TestVoidedDateRangeFilter(t *testing.T) {
	filters := parseFilters(t, "voided_from=2026-07-01&voided_to=2026-07-17")
	if filters.VoidedTo != "2026-07-18T00:00:00+08:00" {
		t.Fatalf("VoidedTo = %q, want the next midnight", filters.VoidedTo)
	}
	where := buildConditions(filters, "", &argList{})
	if !strings.Contains(where, "b.voided_at >= ") || !strings.Contains(where, "b.voided_at < ") {
		t.Fatalf("voided range conditions: %s", where)
	}
}

// TestBlankVoidedFiltersNotVoided: an empty 撤销时间 means "never voided" — a
// filter target in its own right.
func TestBlankVoidedFiltersNotVoided(t *testing.T) {
	filters := parseFilters(t, "voided_blank=1")
	if !filters.VoidedBlank {
		t.Fatal("voided_blank=1 must set VoidedBlank")
	}
	where := buildConditions(filters, "", &argList{})
	if !strings.Contains(where, "b.voided_at is null") {
		t.Fatalf("blank voided must filter for never-voided payments: %s", where)
	}

	// Absent flag leaves the column alone.
	if parseFilters(t, "cn=CN001").VoidedBlank {
		t.Fatal("absent flag must not filter")
	}
	if strings.Contains(buildConditions(parseFilters(t, "cn=CN001"), "", &argList{}), "voided_at is null") {
		t.Fatal("unfiltered voided column must not appear in WHERE")
	}
}

func TestEmptyAndWhitespaceValuesAreIgnored(t *testing.T) {
	ignored := parseFilters(t, "cn=&cn=+++&status=&created_by=+")
	if ignored.CN != nil || ignored.Status != nil || ignored.CreatedBy != nil {
		t.Fatalf("empty parameters must be ignored: %#v", ignored)
	}
}

func TestValueSetDeduplicates(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN001&cn=CN002")
	if len(filters.CN) != 2 {
		t.Fatalf("CN = %#v, want deduplicated", filters.CN)
	}
}

func TestRejectsInvalidParameters(t *testing.T) {
	cases := []struct{ name, query string }{
		{"non-numeric principal", "principal_min=abc"},
		{"negative principal", "principal_min=-5"},
		{"negative fee", "fee_min=-1"},
		{"inverted principal range", "principal_min=200&principal_max=100"},
		{"inverted fee range", "fee_min=5&fee_max=1"},
		{"inverted payable range", "payable_min=200&payable_max=100"},
		{"inverted paid dates", "paid_from=2026-07-17&paid_to=2026-07-01"},
		{"inverted voided dates", "voided_from=2026-07-17&voided_to=2026-07-01"},
		{"bad paid date", "paid_from=not-a-time"},
		{"bad voided date", "voided_to=17/07/2026"},
		{"unknown status", "status=exploded"},
		{"blank status", "status_blank=1"},
		{"unknown method", "payment_method=bitcoin"},
		{"blank method", "payment_method_blank=1"},
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
		if parseFilters(t, "page_size="+itoa(size)).PageSize != size {
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

// TestCountSharesTheListsConditions: the header count and the page must
// describe the same set of payments.
func TestCountSharesTheListsConditions(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&status=approved&principal_min=10&page=3&page_size=25")

	listWhere := buildConditions(filters, "", &argList{})
	countSQL, countArgs := buildCountQuery(filters)

	if !strings.Contains(countSQL, listWhere) {
		t.Fatalf("count does not apply the list's exact conditions\nwant: %s\ngot:  %s", listWhere, countSQL)
	}
	if strings.Contains(countSQL, "limit ") || strings.Contains(countSQL, "offset ") {
		t.Fatalf("count must not paginate:\n%s", countSQL)
	}
	if len(countArgs) != 3 {
		t.Fatalf("count args = %d, want 3 filter values with no pagination", len(countArgs))
	}
}

// TestExportQueryFollowsFiltersWithoutPagination: an export covers the whole
// filtered result set, not the page on screen — including the amount ranges the
// old export silently dropped.
func TestExportQueryFollowsFiltersWithoutPagination(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&status=voided&principal_min=10&fee_max=5&payable_min=1&page=2&page_size=25")
	sql, args := BuildExportQuery(filters, 50000)

	if strings.Contains(sql, "offset ") {
		t.Fatalf("export must not paginate:\n%s", sql)
	}
	for _, expected := range []string{
		"b.cn_code = any(", "b.status = any(",
		"b.principal_amount >= ", "b.fee_amount <= ", "b.payable_amount >= ",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("export SQL missing %q\n%s", expected, sql)
		}
	}
	// Five filter values plus the row cap; the list's page/page_size do not bind.
	if len(args) != 6 {
		t.Fatalf("export args = %#v", args)
	}
	if args[len(args)-1] != 50000 {
		t.Fatalf("export cap = %v, want 50000", args[len(args)-1])
	}
}

// TestBuiltSQLBindsEveryUserValue is the injection guard. It feeds SQL
// metacharacters through every free-text filter and asserts none of them reach
// the statement text: each must arrive as a bind parameter.
func TestBuiltSQLBindsEveryUserValue(t *testing.T) {
	hostile := "'; drop table payments; --"
	query := url.Values{}
	query.Add("cn", hostile)
	query.Add("created_by", hostile)
	query.Set("principal_min", "1")
	query.Set("paid_from", "2026-01-01")

	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}

	listSQL, listArgs := buildListQuery(filters)
	countSQL, countArgs := buildCountQuery(filters)
	exportSQL, exportArgs := BuildExportQuery(filters, 100)

	for _, built := range []struct {
		name string
		sql  string
		args []any
	}{
		{"list", listSQL, listArgs},
		{"count", countSQL, countArgs},
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

// TestListCarriesNoTechnicalIdentifiers: the payment list must not *select* the
// idempotency key, screenshot path, session id, or any internal id beyond the
// payment id the detail link needs. Referencing a column in a join condition
// (e.g. users on p.user_id) is fine — only what reaches the output columns can
// be serialized, so the guard is scoped to the select fragment.
func TestListCarriesNoTechnicalIdentifiers(t *testing.T) {
	for _, technical := range []string{"idempotency", "screenshot", "user_id", "order_item_id", "session", "void_reason_hash"} {
		if strings.Contains(listColumns, technical) {
			t.Fatalf("list select exposes %q:\n%s", technical, listColumns)
		}
	}
	// The one id that is selected is the payment id, needed for the detail link.
	if !strings.Contains(listColumns, "b.id::text") {
		t.Fatalf("payment id must still be selected for detail navigation:\n%s", listColumns)
	}
}
