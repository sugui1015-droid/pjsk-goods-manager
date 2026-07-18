package orders

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func facetRequest(t *testing.T, rawQuery string) FacetRequest {
	t.Helper()
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	request, err := FacetRequestFromQuery(query)
	if err != nil {
		t.Fatalf("FacetRequestFromQuery(%q): %v", rawQuery, err)
	}
	return request
}

func facetSQL(t *testing.T, rawQuery string) (string, []any) {
	t.Helper()
	sql, args, ok := buildFacetQuery(facetRequest(t, rawQuery))
	if !ok {
		t.Fatalf("buildFacetQuery(%q) rejected the column", rawQuery)
	}
	return sql, args
}

// TestFacetCountsDistinctValuesWithCounts covers the popover's core payload:
// distinct candidate values, each with the number of matching orders.
func TestFacetCountsDistinctValuesWithCounts(t *testing.T) {
	sql, _ := facetSQL(t, "column=cn")
	for _, fragment := range []string{
		"select b.cn_code as value, count(*)::int as count",
		"group by 1",
		"count(*) over ()::int as total",
		"sum(count) filter (where value = '') over ()",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("facet SQL missing %q\n%s", fragment, sql)
		}
	}
}

// TestFacetCountsDetailRowsNotOrders pins the count unit to the same thing the
// list pages through. "初音未来 38" has to mean 38 goods lines, so that ticking
// it yields exactly 38 rows; counting distinct orders instead would promise a
// number the table then contradicts.
func TestFacetCountsDetailRowsNotOrders(t *testing.T) {
	for _, column := range []string{"cn", "series", "category", "role", "item", "status"} {
		sql, _ := facetSQL(t, "column="+column)
		if strings.Contains(sql, "count(distinct b.order_id)") || strings.Contains(sql, "count(distinct b.id)") {
			t.Fatalf("%s facet counts orders rather than detail rows:\n%s", column, sql)
		}
		if !strings.Contains(sql, "count(*)::int as count") {
			t.Fatalf("%s facet must count detail rows:\n%s", column, sql)
		}
	}
}

// TestFacetDoesNotUnnestAggregatedColumns: base rows are single order items, so
// there is no array left to expand. An unnest reappearing here would mean the
// order-grained aggregation had crept back in.
func TestFacetDoesNotUnnestAggregatedColumns(t *testing.T) {
	for _, column := range []string{"series", "category", "role", "item"} {
		sql, _ := facetSQL(t, "column="+column)
		if strings.Contains(sql, "unnest") {
			t.Fatalf("%s facet must not unnest an aggregate:\n%s", column, sql)
		}
	}
}

// TestFacetAppliesOtherColumnsFilters: while a popover is open, its candidates
// must already reflect every other column's active filter.
func TestFacetAppliesOtherColumnsFilters(t *testing.T) {
	sql, args := facetSQL(t, "column=cn&status=paid&series=A&amount_min=10")
	for _, fragment := range []string{"b.status = any(", "b.series_code = any(", "b.total_amount >= "} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("facet must apply other columns' filters, missing %q\n%s", fragment, sql)
		}
	}
	if len(args) < 3 {
		t.Fatalf("expected the other columns' values to bind, got %d args", len(args))
	}
}

// TestFacetIgnoresItsOwnColumnsFilter is the rule that keeps a popover usable:
// if the CN candidates were narrowed by the CNs already ticked, unticking one
// could never bring it back into the list.
func TestFacetIgnoresItsOwnColumnsFilter(t *testing.T) {
	sql, _ := facetSQL(t, "column=cn&cn=CN001&cn=CN002&status=paid")
	if strings.Contains(sql, "b.cn_code = any(") {
		t.Fatalf("facet must not apply its own column's filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.status = any(") {
		t.Fatalf("facet must still apply the other columns:\n%s", sql)
	}

	// The same rule for a product column: opening 谷子系列 keeps the 谷子种类
	// filter but drops its own.
	sql, _ = facetSQL(t, "column=series&series=A&category=立牌")
	if strings.Contains(sql, "b.series_code = any(") {
		t.Fatalf("facet must not apply its own filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.category = any(") {
		t.Fatalf("facet must still apply other columns:\n%s", sql)
	}
}

// TestFacetSearchIsBoundAndEscaped: the search box filters candidates, and a
// literal % or _ in the term matches itself rather than acting as a wildcard.
func TestFacetSearchIsBoundAndEscaped(t *testing.T) {
	sql, args := facetSQL(t, "column=cn&search=100%25_off")
	if !strings.Contains(sql, "ilike $") {
		t.Fatalf("search must bind as a parameter:\n%s", sql)
	}
	if strings.Contains(sql, "100%") {
		t.Fatalf("search term reached the statement text:\n%s", sql)
	}
	found := false
	for _, arg := range args {
		if value, ok := arg.(string); ok && value == `%100\%\_off%` {
			found = true
		}
	}
	if !found {
		t.Fatalf("search wildcards were not escaped, args = %#v", args)
	}
}

// TestFacetCandidatesArePagedAndCapped: a column like CN can hold thousands of
// values, so the popover must never ask for all of them at once.
func TestFacetCandidatesArePagedAndCapped(t *testing.T) {
	request := facetRequest(t, "column=cn")
	if request.Page != 1 || request.PageSize != DefaultFacetPageSize {
		t.Fatalf("facet defaults = %d/%d", request.Page, request.PageSize)
	}

	sql, args := facetSQL(t, "column=cn&facet_page=3&facet_page_size=20")
	if !strings.Contains(sql, "limit ") || !strings.Contains(sql, "offset ") {
		t.Fatalf("facet must paginate:\n%s", sql)
	}
	if args[len(args)-1] != 40 {
		t.Fatalf("offset = %v, want 40", args[len(args)-1])
	}
	if args[len(args)-2] != 20 {
		t.Fatalf("limit = %v, want 20", args[len(args)-2])
	}

	query, _ := url.ParseQuery("column=cn&facet_page_size=" + strconv.Itoa(MaxFacetPageSize+1))
	if _, err := FacetRequestFromQuery(query); err == nil {
		t.Fatal("facet_page_size over the cap must be rejected")
	}
}

// TestFacetOrdersBlanksLast: the (空白) entry is a real candidate, but it sorts
// to the end rather than to the top of an alphabetical list.
func TestFacetOrdersBlanksLast(t *testing.T) {
	sql, _ := facetSQL(t, "column=series")
	if !strings.Contains(sql, "order by (value = '') asc, value asc") {
		t.Fatalf("facet ordering:\n%s", sql)
	}
}

func TestFacetLabelsAreChineseAndNameBlanks(t *testing.T) {
	if label := facetLabel("series", ""); label != "(空白)" {
		t.Fatalf("blank label = %q", label)
	}
	if label := facetLabel("status", "partially_paid"); label != "部分付款" {
		t.Fatalf("status label = %q", label)
	}
	if label := facetLabel("payment_status", "unpaid"); label != "未付款" {
		t.Fatalf("payment status label = %q", label)
	}
	if label := facetLabel("cn", "CN001"); label != "CN001" {
		t.Fatalf("cn label = %q", label)
	}
}

// TestFacetRejectsTechnicalAndUnknownColumns: technical identifiers are not
// business columns and must never gain a filter surface, and an arbitrary
// column name must never reach the statement.
func TestFacetRejectsTechnicalAndUnknownColumns(t *testing.T) {
	for _, name := range []string{"import_batch_id", "sku", "sha", "id", "file_hash", "b.cn_code; drop table orders", ""} {
		query, _ := url.ParseQuery("column=" + url.QueryEscape(name))
		if _, err := FacetRequestFromQuery(query); err == nil {
			t.Fatalf("column %q must be rejected", name)
		}
	}
}

func TestFacetsEndpointRejectsBadRequests(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)

	for _, rawQuery := range []string{"", "column=sku", "column=cn&status=nonsense", "column=cn&facet_page=0"} {
		recorder := httptest.NewRecorder()
		handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/orders/facets?"+rawQuery, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d for %q, want 400", recorder.Code, rawQuery)
		}
	}
}

func TestFacetsEndpointPassesRequestThrough(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)
	recorder := httptest.NewRecorder()
	handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/orders/facets?column=series&search=感谢&status=paid", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if store.lastFacetReq.Column != "series" || store.lastFacetReq.Search != "感谢" {
		t.Fatalf("facet request = %+v", store.lastFacetReq)
	}
	if len(store.lastFacetReq.Filters.Status) != 1 || store.lastFacetReq.Filters.Status[0] != "paid" {
		t.Fatalf("facet filters = %+v", store.lastFacetReq.Filters)
	}
}
