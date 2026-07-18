package importpreview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func facetRequest(t *testing.T, rawQuery string) HistoryFacetRequest {
	t.Helper()
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	request, err := HistoryFacetRequestFromQuery(query)
	if err != nil {
		t.Fatalf("HistoryFacetRequestFromQuery(%q): %v", rawQuery, err)
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

func TestFacetCountsImportRows(t *testing.T) {
	for _, column := range []string{"filename", "status", "uploaded_by"} {
		sql, _ := facetSQL(t, "column="+column)
		if !strings.Contains(sql, "count(*)::int as count") {
			t.Fatalf("%s facet must count import rows:\n%s", column, sql)
		}
		if !strings.Contains(sql, "count(*) over ()::int as total") {
			t.Fatalf("%s facet must report total:\n%s", column, sql)
		}
		if !strings.Contains(sql, "sum(count) filter (where value = '') over ()") {
			t.Fatalf("%s facet must report blank_count:\n%s", column, sql)
		}
	}
}

func TestFacetAppliesOtherColumnsButNotItsOwn(t *testing.T) {
	sql, _ := facetSQL(t, "column=filename&filename=a.xlsx&status=completed&sheet_min=1&created_from=2026-01-01")
	if strings.Contains(sql, "b.filename = any(") {
		t.Fatalf("facet must not apply its own column:\n%s", sql)
	}
	for _, expected := range []string{"b.status = any(", "b.sheet_count >= ", "b.created_at >= "} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("facet must keep %q:\n%s", expected, sql)
		}
	}
}

func TestFacetSearchIsBoundAndEscaped(t *testing.T) {
	sql, args := facetSQL(t, "column=filename&search=2026%25_report")
	if !strings.Contains(sql, "ilike $") {
		t.Fatalf("search must bind as a parameter:\n%s", sql)
	}
	if strings.Contains(sql, "2026%") {
		t.Fatalf("search term reached the statement text:\n%s", sql)
	}
	found := false
	for _, arg := range args {
		if value, ok := arg.(string); ok && value == `%2026\%\_report%` {
			found = true
		}
	}
	if !found {
		t.Fatalf("search wildcards were not escaped, args = %#v", args)
	}
}

func TestFacetPagedAndCapped(t *testing.T) {
	_, args := facetSQL(t, "column=status&facet_page=3&facet_page_size=20")
	if args[len(args)-1] != 40 || args[len(args)-2] != 20 {
		t.Fatalf("facet pagination args = %v", args[len(args)-2:])
	}
	for _, bad := range []string{"column=status&facet_page_size=201", "column=status&facet_page=0"} {
		query, _ := url.ParseQuery(bad)
		if _, err := HistoryFacetRequestFromQuery(query); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestFacetLabelsAreChinese(t *testing.T) {
	cases := map[[2]string]string{
		{"filename", ""}:         "(空白)",
		{"status", "completed"}:  "已完成",
		{"status", "failed"}:     "失败",
		{"status", "reverted"}:   "已撤销",
		{"filename", "a.xlsx"}:   "a.xlsx",
		{"uploaded_by", "admin"}: "admin",
	}
	for input, want := range cases {
		if got := facetLabel(input[0], input[1]); got != want {
			t.Fatalf("facetLabel(%q, %q) = %q, want %q", input[0], input[1], got, want)
		}
	}
}

// TestFacetRejectsTechnicalAndUnknownColumns: file_hash and internal ids must
// never gain a filter surface.
func TestFacetRejectsTechnicalAndUnknownColumns(t *testing.T) {
	for _, name := range []string{"id", "file_hash", "confirm_result", "b.filename; drop table import_batches", ""} {
		query, _ := url.ParseQuery("column=" + url.QueryEscape(name))
		if _, err := HistoryFacetRequestFromQuery(query); err == nil {
			t.Fatalf("column %q must be rejected", name)
		}
	}
}

// stubStore satisfies the full Store interface for endpoint tests; only the two
// list/facet methods do anything.
type stubStore struct {
	lastFacet   HistoryFacetRequest
	lastFilters HistoryFilters
}

func (s *stubStore) FindImportFile(context.Context, string, string) (ImportFileState, error) {
	return ImportFileState{}, nil
}
func (s *stubStore) SavePreview(context.Context, Preview, string) (PreviewState, error) {
	return PreviewState{}, nil
}
func (s *stubStore) ConfirmImport(context.Context, string, string, bool, ConfirmRules) (ConfirmResult, error) {
	return ConfirmResult{}, nil
}
func (s *stubStore) ListImports(_ context.Context, filters HistoryFilters) (ImportHistoryResponse, error) {
	s.lastFilters = filters
	return ImportHistoryResponse{Items: []ImportHistoryItem{}}, nil
}
func (s *stubStore) ImportFacets(_ context.Context, request HistoryFacetRequest) (HistoryFacetResponse, error) {
	s.lastFacet = request
	return HistoryFacetResponse{Column: request.Column, Values: []HistoryFacetValue{}}, nil
}
func (s *stubStore) GetImport(context.Context, string) (ImportDetailResponse, error) {
	return ImportDetailResponse{}, nil
}
func (s *stubStore) RevokeImport(context.Context, string, string) (RevokeResult, error) {
	return RevokeResult{}, nil
}

func TestFacetsEndpointRejectsBadRequestsAndPassesThrough(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)

	for _, rawQuery := range []string{"", "column=file_hash", "column=filename&sheet_min=abc", "column=filename&facet_page=0"} {
		recorder := httptest.NewRecorder()
		handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/imports/facets?"+rawQuery, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d for %q, want 400", recorder.Code, rawQuery)
		}
	}

	recorder := httptest.NewRecorder()
	handler.Facets(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/imports/facets?column=filename&search=rep&status=completed&facet_page=2", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if store.lastFacet.Column != "filename" || store.lastFacet.Search != "rep" || store.lastFacet.Page != 2 {
		t.Fatalf("facet request = %+v", store.lastFacet)
	}
	if len(store.lastFacet.Filters.Status) != 1 || store.lastFacet.Filters.Status[0] != "completed" {
		t.Fatalf("facet filters = %+v", store.lastFacet.Filters)
	}
}

func TestListEndpointParsesPaginationAndRejectsBadRange(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)

	recorder := httptest.NewRecorder()
	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/imports?page=2&page_size=25&status=completed", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if store.lastFilters.Page != 2 || store.lastFilters.PageSize != 25 {
		t.Fatalf("pagination = %d/%d", store.lastFilters.Page, store.lastFilters.PageSize)
	}

	recorder = httptest.NewRecorder()
	handler.List(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/imports?sheet_min=9&sheet_max=2", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("inverted range status = %d, want 400", recorder.Code)
	}
}
