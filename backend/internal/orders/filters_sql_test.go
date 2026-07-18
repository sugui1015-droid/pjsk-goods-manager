package orders

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func parseFilters(t *testing.T, rawQuery string) OrderFilters {
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

// TestBuiltSQLBindsEveryUserValue is the injection guard. Rather than trusting
// review, it feeds SQL metacharacters through every filter and asserts none of
// them reach the statement text: each must arrive as a bind parameter.
func TestBuiltSQLBindsEveryUserValue(t *testing.T) {
	hostile := "'; drop table orders; --"
	query := url.Values{}
	query.Add("cn", hostile)
	query.Add("project", hostile)
	query.Add("item", hostile)
	query.Add("series", hostile)
	query.Add("category", hostile)
	query.Add("role", hostile)
	query.Set("amount_min", "1")
	query.Set("amount_max", "2")
	query.Set("quantity_min", "3")
	query.Set("quantity_max", "4")
	query.Set("paid_min", "5")
	query.Set("paid_max", "6")
	query.Set("unpaid_min", "7")
	query.Set("unpaid_max", "8")
	query.Set("created_from", "2026-01-01")
	query.Set("created_to", "2026-02-01")

	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}

	listSQL, listArgs := buildListQuery(filters)
	countSQL, countArgs := buildCountQuery(filters)
	exportSQL, exportArgs := BuildExportItemIDsQuery(filters)

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

func TestListQueryAppliesEveryFilterAndPaginates(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN002&series=A&status=paid&payment_status=partial&amount_min=10&unpaid_max=5&created_from=2026-01-01&page=3&page_size=25")
	sql, args := buildListQuery(filters)

	for _, fragment := range []string{
		"b.cn_code = any(",
		"b.series_code = any(",
		"b.status = any(",
		"b.payment_status = any(",
		"b.total_amount >= ",
		"b.unpaid_amount <= ",
		"b.created_at >= ",
		// A CN's items stay together and in a stable order; item_id breaks ties
		// so paging can neither repeat nor skip a detail row.
		"order by b.created_at desc, b.order_id desc, b.sort_order, b.item_name, b.item_id",
		"limit ",
		"offset ",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("list SQL missing %q\n%s", fragment, sql)
		}
	}

	// page 3 of 25 starts at offset 50.
	if args[len(args)-1] != 50 {
		t.Fatalf("offset arg = %v, want 50", args[len(args)-1])
	}
	if args[len(args)-2] != 25 {
		t.Fatalf("limit arg = %v, want 25", args[len(args)-2])
	}
}

// TestListQueryUsesNumericNotFloat guards the money rule: every amount bound
// into the statement is cast to numeric, so no comparison is decided by binary
// floating-point rounding.
func TestListQueryUsesNumericNotFloat(t *testing.T) {
	filters := parseFilters(t, "amount_min=0.10&amount_max=0.30&paid_min=1.05&unpaid_max=2.15")
	sql, args := buildListQuery(filters)
	if strings.Count(sql, "::numeric") < 4 {
		t.Fatalf("expected every money bound to cast to numeric:\n%s", sql)
	}
	for _, arg := range args {
		if _, isFloat := arg.(float64); isFloat {
			t.Fatalf("money bound as float64: %v", arg)
		}
	}
}

func TestCountQueryMatchesListFilters(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&status=paid&amount_min=10")
	countSQL, countArgs := buildCountQuery(filters)
	if !strings.Contains(countSQL, "select count(*)::int from base b where") {
		t.Fatalf("count SQL shape:\n%s", countSQL)
	}
	// The count must not paginate; only the three filter values bind.
	if len(countArgs) != 3 {
		t.Fatalf("count args = %d, want 3 (no limit/offset)", len(countArgs))
	}
}

// TestExportQueryCarriesFullFiltersWithoutPagination is the guard for
// "导出跟随完整筛选": the export selects the matching detail rows under the same
// conditions as the list, with no limit/offset, so a download is never just the
// visible page — and is row-for-row what the table shows.
func TestExportQueryCarriesFullFiltersWithoutPagination(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN002&series=A&status=paid&amount_min=10&page=2&page_size=25")
	sql, args := BuildExportItemIDsQuery(filters)

	if strings.Contains(sql, "limit ") || strings.Contains(sql, "offset ") {
		t.Fatalf("export SQL must not paginate:\n%s", sql)
	}
	if !strings.Contains(sql, "select b.item_id from base b") {
		t.Fatalf("export must select detail rows, not orders:\n%s", sql)
	}
	for _, fragment := range []string{"b.cn_code = any(", "b.series_code = any(", "b.status = any(", "b.total_amount >= "} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("export SQL missing %q\n%s", fragment, sql)
		}
	}
	if len(args) != 4 {
		t.Fatalf("export args = %d, want 4 filter values with no pagination", len(args))
	}
}

// TestBlankSelectionIsDistinctFromUnfiltered pins the (空白) semantics: only
// the dedicated blank flag filters blank cells. Empty and whitespace-only
// normal values are ignored.
func TestBlankSelectionIsDistinctFromUnfiltered(t *testing.T) {
	blank := parseFilters(t, "series_blank=1")
	if blank.Series == nil || len(blank.Series) != 1 || blank.Series[0] != "" {
		t.Fatalf("Series = %#v, want a one-element blank selection", blank.Series)
	}
	sql, args := buildListQuery(blank)
	if !strings.Contains(sql, "b.series_code = any(") {
		t.Fatalf("blank selection must still filter the column:\n%s", sql)
	}
	values, ok := args[0].([]string)
	if !ok || len(values) != 1 || values[0] != "" {
		t.Fatalf("blank arg = %#v", args[0])
	}

	unfiltered := parseFilters(t, "cn=CN001")
	if unfiltered.Series != nil {
		t.Fatalf("absent parameter must leave the column unfiltered, got %#v", unfiltered.Series)
	}
	// Checked against the WHERE clause alone: the column is naturally present
	// in the select list either way, so only the conditions can tell whether
	// it is filtered.
	where := buildConditions(unfiltered, "", &argList{})
	if strings.Contains(where, "b.series_code") {
		t.Fatalf("unfiltered column must not appear in WHERE: %s", where)
	}
	if !strings.Contains(where, "b.cn_code") {
		t.Fatalf("filtered column missing from WHERE: %s", where)
	}

	ignored := parseFilters(t, "series=&series=+++&cn=CN001&cn=+++")
	if ignored.Series != nil {
		t.Fatalf("empty series parameters must be ignored, got %#v", ignored.Series)
	}
	assertStrings(t, ignored.CN, "CN001")
}

func TestValueSetDeduplicates(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN001&cn=CN002")
	if len(filters.CN) != 2 {
		t.Fatalf("CN = %#v, want deduplicated", filters.CN)
	}
}

// --- 阶段 4F：明细行粒度 ---

// TestBaseRowIsOneOrderItemNotOneOrder is the regression guard for the bug this
// stage fixes. The list used to group by order and array_agg the four product
// columns, so 角色=初音未来 returned the whole order and still displayed its
// 宁宁 and 真冬 items next to the match. If any of these aggregates reappear,
// that confusion is back.
func TestBaseRowIsOneOrderItemNotOneOrder(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, ""))

	for _, banned := range []string{
		"array_agg",
		"string_agg",
		"item_names",
		"series_codes",
		"categories",
		"character_names",
		"group by o.id",
		"order_agg",
	} {
		if strings.Contains(sql, banned) {
			t.Fatalf("list SQL re-introduces order-level aggregation via %q:\n%s", banned, sql)
		}
	}

	// One row per surviving order item.
	if !strings.Contains(sql, "from order_items oi") {
		t.Fatalf("base must be driven by order_items:\n%s", sql)
	}
	if !strings.Contains(sql, "where oi.revoked_at is null") {
		t.Fatalf("revoked items must stay out of the list:\n%s", sql)
	}
	// Every product column is single-valued.
	for _, singular := range []string{"pr.name as item_name", "as series_code", "as category", "as character_name"} {
		if !strings.Contains(sql, singular) {
			t.Fatalf("base missing single-valued column %q:\n%s", singular, sql)
		}
	}
}

// TestQuantityIsNotExpandedIntoRows: an item with quantity 3 is one row showing
// 3, not three rows. Nothing may generate rows from the quantity.
func TestQuantityIsNotExpandedIntoRows(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, ""))
	if strings.Contains(sql, "generate_series") {
		t.Fatalf("quantity must not be expanded into one row per unit:\n%s", sql)
	}
	if !strings.Contains(sql, "b.quantity::float8") {
		t.Fatalf("the row must carry its own quantity:\n%s", sql)
	}
}

// TestProductFiltersMatchTheRowsOwnValue: filtering 角色 must compare the row's
// own character_name. Array overlap (&&) would mean "some sibling item in this
// order matched", which is exactly what dragged unmatched goods onto screen.
func TestProductFiltersMatchTheRowsOwnValue(t *testing.T) {
	filters := parseFilters(t, "role=初音未来&category=吧唧&series=MK-01&item=初音未来吧唧")
	where := buildConditions(filters, "", &argList{})

	if strings.Contains(where, "&&") {
		t.Fatalf("product filters must not use array overlap: %s", where)
	}
	for _, expected := range []string{
		"b.character_name = any(",
		"b.category = any(",
		"b.series_code = any(",
		"b.item_name = any(",
	} {
		if !strings.Contains(where, expected) {
			t.Fatalf("missing per-row condition %q: %s", expected, where)
		}
	}
}

// TestMoneyAndPaymentStatusAreDetailLevel: each row's money is its own, and its
// payment status is derived from that same row — never from its order's totals.
func TestMoneyAndPaymentStatusAreDetailLevel(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, ""))

	// Per-item amount, paid clamped to the item, unpaid as the remainder.
	for _, fragment := range []string{
		"oi.amount as total_amount",
		"least(coalesce(ip.paid, 0), oi.amount) as paid_amount",
		"greatest(oi.amount - coalesce(ip.paid, 0), 0) as unpaid_amount",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("detail money missing %q:\n%s", fragment, sql)
		}
	}
	// The order's own header total must not be presented as the row's amount.
	if strings.Contains(sql, "o.total_amount") {
		t.Fatalf("row amount must be the item's, not the order's:\n%s", sql)
	}
	// Only approved payments count, so a voided payment never reads as paid.
	if !strings.Contains(sql, "filter (where pay.status = 'approved')") {
		t.Fatalf("paid must count approved payments only:\n%s", sql)
	}

	// Ranges bind to the row's own numbers.
	where := buildConditions(parseFilters(t, "quantity_min=2&amount_max=50&paid_min=1&unpaid_max=9"), "", &argList{})
	for _, expected := range []string{"b.quantity >= ", "b.total_amount <= ", "b.paid_amount >= ", "b.unpaid_amount <= "} {
		if !strings.Contains(where, expected) {
			t.Fatalf("range must bind the row's own value, missing %q: %s", expected, where)
		}
	}
}

// TestPaymentStatusFilterUsesTheRowsOwnStatus: 付款状态 filters the item's own
// paid/unpaid split, so a partially paid order does not drag its fully paid
// items into an "unpaid" result.
func TestPaymentStatusFilterUsesTheRowsOwnStatus(t *testing.T) {
	sql, _ := buildListQuery(parseFilters(t, "payment_status=unpaid"))
	if !strings.Contains(sql, "when least(coalesce(ip.paid, 0), oi.amount) <= 0.004 then 'unpaid'") {
		t.Fatalf("payment status must be derived per item:\n%s", sql)
	}
	where := buildConditions(parseFilters(t, "payment_status=unpaid"), "", &argList{})
	if !strings.Contains(where, "b.payment_status = any(") {
		t.Fatalf("payment status must filter the row: %s", where)
	}
}

// TestCountAndExportSharePreciselyTheListsConditions: the "共 N 项谷子明细"
// count, the page and the export must all describe the same set of rows.
func TestCountAndExportSharePreciselyTheListsConditions(t *testing.T) {
	filters := parseFilters(t, "role=初音未来&cn=CN01&amount_min=10&page=3&page_size=25")

	listWhere := buildConditions(filters, "", &argList{})
	countSQL, _ := buildCountQuery(filters)
	exportSQL, _ := BuildExportItemIDsQuery(filters)

	for name, sql := range map[string]string{"count": countSQL, "export": exportSQL} {
		if !strings.Contains(sql, listWhere) {
			t.Fatalf("%s does not apply the list's exact conditions\nwant: %s\ngot:  %s", name, listWhere, sql)
		}
	}
	// The count is of detail rows.
	if !strings.Contains(countSQL, "select count(*)::int from base b") {
		t.Fatalf("count must count detail rows:\n%s", countSQL)
	}
}
