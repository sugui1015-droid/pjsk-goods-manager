package users

import (
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

// TestFacetCountsUserRows: "已设置 12" must mean twelve users, so that ticking
// it yields exactly twelve rows.
func TestFacetCountsUserRows(t *testing.T) {
	for _, column := range []string{"cn", "name", "status", "has_query_code", "has_recovery_email"} {
		sql, _ := facetSQL(t, "column="+column)
		if !strings.Contains(sql, "count(*)::int as count") {
			t.Fatalf("%s facet must count user rows:\n%s", column, sql)
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
	sql, args := facetSQL(t, "column=cn&status=active&has_query_code=yes&total_min=10&created_from=2026-01-01")
	for _, expected := range []string{
		"b.status = any(",
		"b.has_query_code = any(",
		"b.total_amount >= ",
		"b.created_at >= ",
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
	sql, _ := facetSQL(t, "column=cn&cn=CN001&cn=CN002&status=active")
	if strings.Contains(sql, "b.cn_code = any(") {
		t.Fatalf("facet must not apply its own column's filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.status = any(") {
		t.Fatalf("facet must still apply the other columns:\n%s", sql)
	}

	// Same rule for the boolean columns.
	sql, _ = facetSQL(t, "column=has_query_code&has_query_code=yes&has_recovery_email=no")
	if strings.Contains(sql, "b.has_query_code = any(") {
		t.Fatalf("facet must not apply its own filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.has_recovery_email = any(") {
		t.Fatalf("facet must still apply other columns:\n%s", sql)
	}
}

// TestFacetKeepsRangeAndDateFiltersWhenSkippingItsColumn: only the column's own
// *value* filter is dropped; ranges and dates still narrow the candidates.
func TestFacetKeepsRangeAndDateFiltersWhenSkippingItsColumn(t *testing.T) {
	sql, _ := facetSQL(t, "column=cn&cn=CN001&total_min=10&unpaid_max=5&last_login_blank=1&created_to=2026-07-17")
	for _, expected := range []string{
		"b.total_amount >= ",
		"b.unpaid_amount <= ",
		"b.last_query_login_at is null",
		"b.created_at < ",
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
	sql, _ := facetSQL(t, "column=name")
	if !strings.Contains(sql, "order by (value = '') asc, value asc") {
		t.Fatalf("facet ordering:\n%s", sql)
	}
}

func TestFacetLabelsAreChinese(t *testing.T) {
	cases := map[[2]string]string{
		{"name", ""}:                  "(空白)",
		{"status", "active"}:          "正常",
		{"status", "disabled"}:        "已停用",
		{"status", "merged"}:          "已合并",
		{"has_query_code", "yes"}:     "已设置",
		{"has_query_code", "no"}:      "未设置",
		{"has_recovery_email", "yes"}: "已绑定",
		{"has_recovery_email", "no"}:  "未绑定",
		{"cn", "CN001"}:               "CN001",
	}
	for input, want := range cases {
		if got := facetLabel(input[0], input[1]); got != want {
			t.Fatalf("facetLabel(%q, %q) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

// TestFacetRejectsSecretAndUnknownColumns: secrets and technical identifiers
// must never gain a filter surface, and an arbitrary column name must never
// reach the statement.
func TestFacetRejectsSecretAndUnknownColumns(t *testing.T) {
	for _, name := range []string{
		"id", "query_code_hash", "encrypted_email", "email_lookup_hash",
		"session_id", "recovery_token", "b.cn_code; drop table users", "",
	} {
		query, _ := url.ParseQuery("column=" + url.QueryEscape(name))
		if _, err := FacetRequestFromQuery(query); err == nil {
			t.Fatalf("column %q must be rejected", name)
		}
	}
}

func TestFacetsEndpointRejectsBadRequests(t *testing.T) {
	handler := NewHandler(&stubStore{})
	for _, rawQuery := range []string{"", "column=query_code_hash", "column=cn&status=nonsense", "column=cn&facet_page=0", "column=cn&total_min=200&total_max=100"} {
		recorder := httptest.NewRecorder()
		handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/users/facets?"+rawQuery, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d for %q, want 400", recorder.Code, rawQuery)
		}
	}
}

func TestFacetsEndpointPassesRequestThrough(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)
	recorder := httptest.NewRecorder()
	handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/users/facets?column=cn&search=CN0&status=active&facet_page=2", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if store.facetRequest.Column != "cn" || store.facetRequest.Search != "CN0" || store.facetRequest.Page != 2 {
		t.Fatalf("facet request = %+v", store.facetRequest)
	}
	if len(store.facetRequest.Filters.Status) != 1 || store.facetRequest.Filters.Status[0] != "active" {
		t.Fatalf("facet filters = %+v", store.facetRequest.Filters)
	}
}
