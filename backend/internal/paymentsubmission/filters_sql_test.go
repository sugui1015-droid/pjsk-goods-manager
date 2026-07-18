package paymentsubmission

import (
	"net/url"
	"strings"
	"testing"
)

func TestFiltersParseMultiValueAndBlank(t *testing.T) {
	query := url.Values{}
	query.Add("cn", "CN001")
	query.Add("cn", "CN002")
	query.Add("cn", "  ")    // whitespace-only is ignored
	query.Add("cn", "CN001") // duplicate is collapsed
	query.Add("status", "submitted")
	query.Set("reviewed_by_blank", "1")

	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}
	if len(filters.CN) != 2 || filters.CN[0] != "CN001" || filters.CN[1] != "CN002" {
		t.Fatalf("CN = %#v, want [CN001 CN002]", filters.CN)
	}
	// reviewed_by blank cell filter travels as a dedicated flag → a "" value.
	if len(filters.ReviewedBy) != 1 || filters.ReviewedBy[0] != "" {
		t.Fatalf("ReviewedBy = %#v, want [\"\"]", filters.ReviewedBy)
	}
}

func TestFiltersNormaliseMethodAliases(t *testing.T) {
	query := url.Values{}
	query.Add("payment_method", "微信")
	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}
	if len(filters.PaymentMethod) != 1 || filters.PaymentMethod[0] != MethodWechat {
		t.Fatalf("PaymentMethod = %#v, want [wechat]", filters.PaymentMethod)
	}
}

func TestFiltersRejectInvalidStatusAndMethod(t *testing.T) {
	for _, q := range []string{"status=voided", "payment_method=bank", "payment_method=cash"} {
		query, _ := url.ParseQuery(q)
		if _, err := FiltersFromQuery(query); err == nil {
			t.Fatalf("FiltersFromQuery(%q) should be rejected", q)
		}
	}
	// An empty value is ignored, never treated as a value (blank cells use the
	// dedicated <column>_blank flag instead).
	empty, _ := url.ParseQuery("status=")
	filters, err := FiltersFromQuery(empty)
	if err != nil {
		t.Fatalf("empty status must be ignored, not rejected: %v", err)
	}
	if filters.Status != nil {
		t.Fatalf("Status = %#v, want nil for an empty param", filters.Status)
	}
}

func TestFiltersRejectInvertedRanges(t *testing.T) {
	query := url.Values{}
	query.Set("principal_min", "200")
	query.Set("principal_max", "100")
	if _, err := FiltersFromQuery(query); err == nil {
		t.Fatal("inverted principal range must be rejected")
	}

	query = url.Values{}
	query.Set("fee_min", "-1")
	if _, err := FiltersFromQuery(query); err == nil {
		t.Fatal("negative fee must be rejected")
	}
}

func TestFiltersRejectInvertedDates(t *testing.T) {
	query := url.Values{}
	query.Set("submitted_from", "2026-07-18")
	query.Set("submitted_to", "2026-07-01")
	if _, err := FiltersFromQuery(query); err == nil {
		t.Fatal("inverted submitted date range must be rejected")
	}
}

func TestFiltersAnchorNaiveDatesToChinaAndExclusiveDayEnd(t *testing.T) {
	query := url.Values{}
	query.Set("submitted_from", "2026-07-12")
	query.Set("submitted_to", "2026-07-12")
	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}
	if filters.SubmittedFrom != "2026-07-12T00:00:00+08:00" {
		t.Fatalf("SubmittedFrom = %q, want 2026-07-12T00:00:00+08:00", filters.SubmittedFrom)
	}
	// The "to" bound advances to the next midnight so the whole day is included.
	if filters.SubmittedTo != "2026-07-13T00:00:00+08:00" {
		t.Fatalf("SubmittedTo = %q, want 2026-07-13T00:00:00+08:00 (exclusive day end)", filters.SubmittedTo)
	}
}

func TestFiltersReviewedBlank(t *testing.T) {
	query := url.Values{}
	query.Set("reviewed_blank", "1")
	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}
	if !filters.ReviewedBlank {
		t.Fatal("ReviewedBlank should be true")
	}
	args := &argList{}
	where := buildConditions(filters, "", args)
	if !strings.Contains(where, "b.reviewed_at is null") {
		t.Fatalf("where = %q, want reviewed_at is null clause", where)
	}
}

func TestFiltersRejectBadPagination(t *testing.T) {
	for _, q := range []string{"page=0", "page=-1", "page_size=0", "page_size=201", "page_size=abc"} {
		query, _ := url.ParseQuery(q)
		if _, err := FiltersFromQuery(query); err == nil {
			t.Fatalf("FiltersFromQuery(%q) should be rejected", q)
		}
	}
}

func TestListAndCountShareTheSameWhereAndBindEveryValue(t *testing.T) {
	query := url.Values{}
	query.Add("cn", "CN001")
	query.Add("status", "approved")
	query.Set("principal_min", "10")
	query.Set("payable_max", "500")
	query.Set("submitted_from", "2026-07-01")
	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}

	listSQL, listArgs := buildListQuery(filters)
	countSQL, countArgs := buildCountQuery(filters)

	// The count must apply the identical value filters as the list — the two
	// build the same conditions, so the number reported and the rows shown agree.
	for _, clause := range []string{
		"b.cn_code = any(",
		"b.status = any(",
		"b.principal_amount >= ",
		"b.payable_amount <= ",
		"b.submitted_at >= ",
	} {
		if !strings.Contains(listSQL, clause) {
			t.Fatalf("list SQL missing %q", clause)
		}
		if !strings.Contains(countSQL, clause) {
			t.Fatalf("count SQL missing %q", clause)
		}
	}

	// List carries limit+offset in addition to the shared filter args; count
	// carries only the filter args. No user value is ever concatenated: both CN
	// and status arrive as bound []string parameters.
	if len(listArgs) != len(countArgs)+2 {
		t.Fatalf("list args = %d, count args = %d; want list = count+2 (limit, offset)", len(listArgs), len(countArgs))
	}
	if !containsStringSlice(countArgs, "CN001") {
		t.Fatalf("count args %#v must bind CN001 as a parameter, not inline it", countArgs)
	}
}

func TestBaseViewNeverSelectsImageBytes(t *testing.T) {
	// The list/count/facet views must never load image_data — the bytes are
	// served only through the dedicated authenticated image endpoints.
	if strings.Contains(baseCTE, "image_data") {
		t.Fatalf("baseCTE must not select image_data:\n%s", baseCTE)
	}
	listSQL, _ := buildListQuery(Filters{Page: 1, PageSize: 50})
	if strings.Contains(listSQL, "image_data") {
		t.Fatal("list query must not select image_data")
	}
}

func TestMaliciousValueStaysABoundParameter(t *testing.T) {
	query := url.Values{}
	query.Add("cn", "'; drop table payment_submissions;--")
	filters, err := FiltersFromQuery(query)
	if err != nil {
		t.Fatalf("FiltersFromQuery: %v", err)
	}
	sql, args := buildListQuery(filters)
	if strings.Contains(sql, "drop table") {
		t.Fatalf("injection payload leaked into SQL text:\n%s", sql)
	}
	if !containsStringSlice(args, "'; drop table payment_submissions;--") {
		t.Fatalf("payload must be a bound parameter; args = %#v", args)
	}
}

func containsStringSlice(args []any, want string) bool {
	for _, arg := range args {
		if values, ok := arg.([]string); ok {
			for _, v := range values {
				if v == want {
					return true
				}
			}
		}
	}
	return false
}
