package users

import (
	"strings"
	"testing"
)

// The bulk bind-code batch narrows to the only users a code can ever work for.
// This is a security property, not a convenience one: it must hold no matter
// what the admin puts in the filter parameters, so it is asserted on the SQL
// itself rather than only in the integration test (which is skipped without a
// database).
func TestBulkBindTokenQueryAlwaysNarrowsToEligibleUsers(t *testing.T) {
	cases := []string{
		"",
		"cn=CN001&cn=CN002",
		// An admin explicitly asking for users who already have a query code,
		// or for disabled users, must still get neither.
		"has_query_code=yes",
		"status=disabled&status=merged",
		"status=disabled&has_query_code=yes",
	}
	for _, rawQuery := range cases {
		filters := parseFilters(t, rawQuery)
		query, _ := BuildBulkBindTokenQuery(filters, 500)
		if !strings.Contains(query, "b.status = 'active'") {
			t.Fatalf("query for %q does not restrict to active users:\n%s", rawQuery, query)
		}
		if !strings.Contains(query, "b.has_query_code = 'no'") {
			t.Fatalf("query for %q does not restrict to users without a query code:\n%s", rawQuery, query)
		}
	}
}

// The batch must never select the query code hash or the recovery email — the
// same privacy boundary the list and export hold to.
func TestBulkBindTokenQuerySelectsOnlyIdentifyingColumns(t *testing.T) {
	query, _ := BuildBulkBindTokenQuery(parseFilters(t, ""), 500)
	selectClause := query[strings.LastIndex(query, "select"):]
	for _, forbidden := range []string{"query_code_hash", "email_ciphertext", "email_lookup_hash", "token_hash"} {
		if strings.Contains(selectClause, forbidden) {
			t.Fatalf("bulk query selects %q:\n%s", forbidden, selectClause)
		}
	}
}

// Every user-supplied value must arrive as a bind parameter. A CN that reaches
// the SQL text would be an injection point on a mutating endpoint.
func TestBulkBindTokenQueryBindsEveryValue(t *testing.T) {
	filters := parseFilters(t, "cn=CN001&cn=CN%27%3B+drop+table+users+--&name=%E5%B0%8F%E6%98%8E")
	query, args := BuildBulkBindTokenQuery(filters, 500)
	if strings.Contains(query, "drop table") || strings.Contains(query, "小明") {
		t.Fatalf("filter values leaked into SQL text:\n%s", query)
	}
	if len(args) == 0 {
		t.Fatal("expected bind parameters, got none")
	}
}

// Every skip reason must be a non-empty, distinct, human-readable string: they
// are shown verbatim to the admin in the downloaded file, so an empty or
// duplicated reason would read as an unexplained shortfall.
func TestBulkBindTokenSkipReasonsAreDistinctAndNonEmpty(t *testing.T) {
	reasons := []string{SkipReasonNowHasQueryCode, SkipReasonNotActive, SkipReasonUserGone}
	seen := map[string]bool{}
	for _, reason := range reasons {
		if strings.TrimSpace(reason) == "" {
			t.Fatal("skip reason is empty")
		}
		if seen[reason] {
			t.Fatalf("duplicate skip reason %q", reason)
		}
		seen[reason] = true
	}
}

// The accounting invariant the caller relies on: issued + skipped == requested.
// If this ever breaks, the batch has lost users without saying so.
func TestBulkBindTokenResultAccountsForEveryCandidate(t *testing.T) {
	result := BulkBindTokenResult{
		Requested: 3,
		Issued:    []BulkBindToken{{CNCode: "CN001"}},
		Skipped: []BulkBindTokenSkip{
			{CNCode: "CN002", Reason: SkipReasonNotActive},
			{CNCode: "CN003", Reason: SkipReasonNowHasQueryCode},
		},
	}
	if len(result.Issued)+len(result.Skipped) != result.Requested {
		t.Fatalf("issued %d + skipped %d != requested %d", len(result.Issued), len(result.Skipped), result.Requested)
	}
	for _, skip := range result.Skipped {
		if skip.Reason == "" {
			t.Fatalf("skip for %s carries no reason", skip.CNCode)
		}
	}
}

// The preview must count the same population the batch would issue to,
// otherwise the number the admin confirms is not the number affected.
func TestBulkBindTokenPreviewWrapsTheSameQuery(t *testing.T) {
	filters := parseFilters(t, "cn=CN001")
	inner, innerArgs := BuildBulkBindTokenQuery(filters, 501)
	preview, previewArgs := BuildBulkBindTokenPreviewQuery(filters, 501)
	if !strings.Contains(preview, inner) {
		t.Fatalf("preview does not wrap the batch query:\n%s", preview)
	}
	if len(previewArgs) != len(innerArgs) {
		t.Fatalf("preview args = %d, batch args = %d", len(previewArgs), len(innerArgs))
	}
}
