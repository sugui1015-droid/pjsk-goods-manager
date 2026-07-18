package payments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// TestFacetCountsPaymentRows: "已撤销 3" must mean three payments, so that
// ticking it yields exactly three rows.
func TestFacetCountsPaymentRows(t *testing.T) {
	for _, column := range []string{"cn", "payment_method", "status", "created_by"} {
		sql, _ := facetSQL(t, "column="+column)
		if !strings.Contains(sql, "count(*)::int as count") {
			t.Fatalf("%s facet must count payment rows:\n%s", column, sql)
		}
		if !strings.Contains(sql, "group by 1") {
			t.Fatalf("%s facet must group by the column:\n%s", column, sql)
		}
		if !strings.Contains(sql, "count(*) over ()::int as total") {
			t.Fatalf("%s facet must report the candidate total:\n%s", column, sql)
		}
		if !strings.Contains(sql, "sum(count) filter (where value = '') over ()") {
			t.Fatalf("%s facet must report blank_count:\n%s", column, sql)
		}
	}
}

// TestFacetAppliesOtherColumnsFilters: while a popover is open, its candidates
// must already reflect every other column's active filter, including ranges and
// dates.
func TestFacetAppliesOtherColumnsFilters(t *testing.T) {
	sql, args := facetSQL(t, "column=cn&status=approved&payment_method=wechat&principal_min=10&paid_from=2026-01-01&voided_blank=1")
	for _, expected := range []string{
		"b.status = any(",
		"b.payment_method = any(",
		"b.principal_amount >= ",
		"b.paid_at >= ",
		"b.voided_at is null",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("facet must apply other columns' filters, missing %q\n%s", expected, sql)
		}
	}
	if len(args) < 4 {
		t.Fatalf("expected the other columns' values to bind, got %d args", len(args))
	}
}

// TestFacetIgnoresItsOwnColumnsFilter is the rule that keeps a popover usable:
// if the CN candidates were narrowed by the CNs already ticked, unticking one
// could never bring it back into the list.
func TestFacetIgnoresItsOwnColumnsFilter(t *testing.T) {
	sql, _ := facetSQL(t, "column=cn&cn=CN001&cn=CN002&status=approved")
	if strings.Contains(sql, "b.cn_code = any(") {
		t.Fatalf("facet must not apply its own column's filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.status = any(") {
		t.Fatalf("facet must still apply the other columns:\n%s", sql)
	}

	// Same rule for the status column itself.
	sql, _ = facetSQL(t, "column=status&status=voided&payment_method=alipay")
	if strings.Contains(sql, "b.status = any(") {
		t.Fatalf("facet must not apply its own filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.payment_method = any(") {
		t.Fatalf("facet must still apply other columns:\n%s", sql)
	}
}

// TestFacetKeepsRangeAndDateFiltersWhenSkippingItsColumn: only the column's own
// *value* filter is dropped; ranges and dates still narrow the candidates.
func TestFacetKeepsRangeAndDateFiltersWhenSkippingItsColumn(t *testing.T) {
	sql, _ := facetSQL(t, "column=status&status=approved&principal_min=10&fee_max=5&payable_min=1&paid_to=2026-07-17&voided_from=2026-07-01")
	for _, expected := range []string{
		"b.principal_amount >= ",
		"b.fee_amount <= ",
		"b.payable_amount >= ",
		"b.paid_at < ",
		"b.voided_at >= ",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("facet must keep %q\n%s", expected, sql)
		}
	}
}

// TestFacetSearchIsBoundAndEscaped: a literal % or _ in the term matches itself
// rather than acting as a wildcard.
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

	for _, bad := range []string{"column=cn&facet_page_size=201", "column=cn&facet_page=0", "column=cn&facet_page_size=0"} {
		query, _ := url.ParseQuery(bad)
		if _, err := FacetRequestFromQuery(query); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestFacetOrdersBlanksLast(t *testing.T) {
	sql, _ := facetSQL(t, "column=created_by")
	if !strings.Contains(sql, "order by (value = '') asc, value asc") {
		t.Fatalf("facet ordering:\n%s", sql)
	}
}

func TestFacetLabelsAreChinese(t *testing.T) {
	cases := map[[2]string]string{
		{"created_by", ""}:           "(空白)",
		{"status", "approved"}:       "已交肾",
		{"status", "voided"}:         "已撤销",
		{"status", "submitted"}:      "待处理",
		{"status", "rejected"}:       "已驳回",
		{"status", "cancelled"}:      "已取消",
		{"payment_method", "alipay"}: "支付宝",
		{"payment_method", "wechat"}: "微信",
		{"payment_method", "bank"}:   "银行转账",
		{"payment_method", "cash"}:   "现金",
		{"payment_method", "other"}:  "其他",
		{"cn", "CN001"}:              "CN001",
		{"created_by", "admin"}:      "admin",
	}
	for input, want := range cases {
		if got := facetLabel(input[0], input[1]); got != want {
			t.Fatalf("facetLabel(%q, %q) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

// TestFacetRejectsTechnicalAndUnknownColumns: technical identifiers must never
// gain a filter surface, and an arbitrary column name must never reach the
// statement.
func TestFacetRejectsTechnicalAndUnknownColumns(t *testing.T) {
	for _, name := range []string{
		"id", "idempotency_key", "user_id", "screenshot_storage_path",
		"note", "b.cn_code; drop table payments", "",
	} {
		query, _ := url.ParseQuery("column=" + url.QueryEscape(name))
		if _, err := FacetRequestFromQuery(query); err == nil {
			t.Fatalf("column %q must be rejected", name)
		}
	}
}

func TestFacetsEndpointRejectsBadRequests(t *testing.T) {
	handler := NewHandler(fakeStore{})
	for _, rawQuery := range []string{"", "column=idempotency_key", "column=cn&status=nonsense", "column=cn&facet_page=0", "column=cn&principal_min=200&principal_max=100"} {
		recorder := httptest.NewRecorder()
		handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/payments/facets?"+rawQuery, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d for %q, want 400", recorder.Code, rawQuery)
		}
	}
}

func TestFacetsEndpointPassesRequestThrough(t *testing.T) {
	var seen FacetRequest
	handler := NewHandler(fakeStore{
		paymentFacets: func(_ context.Context, request FacetRequest) (FacetResponse, error) {
			seen = request
			return FacetResponse{Column: request.Column, Values: []FacetValue{}}, nil
		},
	})
	recorder := httptest.NewRecorder()
	handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/payments/facets?column=cn&search=CN0&status=voided&facet_page=2", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if seen.Column != "cn" || seen.Search != "CN0" || seen.Page != 2 {
		t.Fatalf("facet request = %+v", seen)
	}
	if len(seen.Filters.Status) != 1 || seen.Filters.Status[0] != "voided" {
		t.Fatalf("facet filters = %+v", seen.Filters)
	}
}
