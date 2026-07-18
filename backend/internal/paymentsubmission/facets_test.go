package paymentsubmission

import (
	"net/url"
	"strings"
	"testing"
)

func TestFacetRequestRejectsUnknownAndTechnicalColumns(t *testing.T) {
	for _, column := range []string{"", "sha256", "user_id", "linked_payment_id", "id", "image_data"} {
		query := url.Values{}
		if column != "" {
			query.Set("column", column)
		}
		if _, err := FacetRequestFromQuery(query); err == nil {
			t.Fatalf("facet column %q must be rejected", column)
		}
	}
}

func TestFacetableColumnsAreBusinessOnly(t *testing.T) {
	want := map[string]bool{"cn": true, "payment_method": true, "status": true, "reviewed_by": true}
	if len(facetableColumns) != len(want) {
		t.Fatalf("facetableColumns = %#v, want %#v", facetableColumns, want)
	}
	for name := range want {
		if !facetableColumns[name] {
			t.Fatalf("facetableColumns missing %q", name)
		}
	}
}

func TestFacetRequestParsesFiltersAndPagination(t *testing.T) {
	query := url.Values{}
	query.Set("column", "status")
	query.Set("search", "已")
	query.Set("facet_page", "2")
	query.Set("facet_page_size", "25")
	query.Add("cn", "CN001") // an active filter on another column must survive
	request, err := FacetRequestFromQuery(query)
	if err != nil {
		t.Fatalf("FacetRequestFromQuery: %v", err)
	}
	if request.Column != "status" || request.Search != "已" || request.Page != 2 || request.PageSize != 25 {
		t.Fatalf("request = %#v", request)
	}
	if len(request.Filters.CN) != 1 || request.Filters.CN[0] != "CN001" {
		t.Fatalf("request.Filters.CN = %#v, want [CN001]", request.Filters.CN)
	}
}

func TestFacetQuerySkipsOwnColumnButKeepsOthers(t *testing.T) {
	query := url.Values{}
	query.Set("column", "status")
	query.Add("status", "approved") // own column selection is skipped
	query.Add("cn", "CN001")        // other column selection is kept
	request, err := FacetRequestFromQuery(query)
	if err != nil {
		t.Fatalf("FacetRequestFromQuery: %v", err)
	}
	sql, args, ok := buildFacetQuery(request)
	if !ok {
		t.Fatal("buildFacetQuery returned !ok")
	}
	if strings.Contains(sql, "b.status = any(") {
		t.Fatalf("facet on status must skip its own value filter:\n%s", sql)
	}
	if !strings.Contains(sql, "b.cn_code = any(") {
		t.Fatalf("facet must keep other columns' filters:\n%s", sql)
	}
	if !containsStringSlice(args, "CN001") {
		t.Fatalf("CN001 must be a bound param; args = %#v", args)
	}
	// Facet counts are submission rows, so a candidate count means "N submissions".
	if !strings.Contains(sql, "count(*)::int as count") {
		t.Fatalf("facet count must be count(*) rows:\n%s", sql)
	}
}

func TestFacetSearchTermIsEscapedAndBound(t *testing.T) {
	query := url.Values{}
	query.Set("column", "cn")
	query.Set("search", "100%_x")
	request, err := FacetRequestFromQuery(query)
	if err != nil {
		t.Fatalf("FacetRequestFromQuery: %v", err)
	}
	sql, args, ok := buildFacetQuery(request)
	if !ok {
		t.Fatal("buildFacetQuery returned !ok")
	}
	if !strings.Contains(sql, "ilike") || !strings.Contains(sql, "escape '\\'") {
		t.Fatalf("search must use ilike with escape:\n%s", sql)
	}
	found := false
	for _, arg := range args {
		if s, ok := arg.(string); ok && s == `%100\%\_x%` {
			found = true
		}
	}
	if !found {
		t.Fatalf("search wildcards must be escaped and bound; args = %#v", args)
	}
}

func TestFacetLabelsAreChinese(t *testing.T) {
	cases := []struct {
		column string
		value  string
		want   string
	}{
		{"status", "submitted", "待核对"},
		{"status", "approved", "已通过"},
		{"status", "rejected", "已驳回"},
		{"payment_method", "alipay", "支付宝"},
		{"payment_method", "wechat", "微信"},
		{"cn", "", "(空白)"},
		{"cn", "测试CN01", "测试CN01"},
	}
	for _, c := range cases {
		if got := facetLabel(c.column, c.value); got != c.want {
			t.Fatalf("facetLabel(%q,%q) = %q, want %q", c.column, c.value, got, c.want)
		}
	}
}
