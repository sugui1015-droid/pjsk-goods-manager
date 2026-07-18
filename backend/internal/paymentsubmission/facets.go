package paymentsubmission

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"pjsk/backend/internal/payments"
)

// FacetRequest describes one filter popover's candidate query.
type FacetRequest struct {
	Column   string
	Search   string
	Page     int
	PageSize int
	Filters  Filters
}

// FacetValue is one candidate row in a filter popover.
type FacetValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Blank bool   `json:"blank"`
}

// FacetResponse pages through a column's candidate values. Total is the number
// of distinct candidates matching the search, not the number of submissions.
type FacetResponse struct {
	Column     string       `json:"column"`
	Values     []FacetValue `json:"values"`
	Total      int          `json:"total"`
	BlankCount int          `json:"blank_count"`
	Page       int          `json:"facet_page"`
	PageSize   int          `json:"facet_page_size"`
	TotalPages int          `json:"total_pages"`
	HasMore    bool         `json:"has_more"`
}

// facetableColumns are the columns a user can pick values for. Technical
// identifiers (submission id, sha, internal user/payment ids) are deliberately
// absent — they are not business columns and must never become a filter surface.
var facetableColumns = map[string]bool{
	"cn":             true,
	"payment_method": true,
	"status":         true,
	"reviewed_by":    true,
}

// FacetRequestFromQuery parses a facet request, reusing the same filter parser
// as the list so both sides always agree on the current filter state.
func FacetRequestFromQuery(query url.Values) (FacetRequest, error) {
	name := strings.TrimSpace(query.Get("column"))
	if name == "" {
		return FacetRequest{}, badRequest("column 不能为空")
	}
	if !facetableColumns[name] {
		return FacetRequest{}, badRequest("column 不支持筛选")
	}

	filterQuery := url.Values{}
	for key, values := range query {
		switch key {
		case "page", "page_size", "column", "search", "facet_page", "facet_page_size":
			continue
		}
		filterQuery[key] = values
	}
	filters, err := FiltersFromQuery(filterQuery)
	if err != nil {
		return FacetRequest{}, err
	}

	page := 1
	if raw := strings.TrimSpace(query.Get("facet_page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return FacetRequest{}, badRequest("facet_page 必须是大于 0 的整数")
		}
		page = parsed
	}
	pageSize := DefaultFacetPageSize
	if raw := strings.TrimSpace(query.Get("facet_page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return FacetRequest{}, badRequest("facet_page_size 必须是大于 0 的整数")
		}
		if parsed > MaxFacetPageSize {
			return FacetRequest{}, badRequest("facet_page_size 最大为 %d", MaxFacetPageSize)
		}
		pageSize = parsed
	}

	return FacetRequest{
		Column:   name,
		Search:   strings.TrimSpace(query.Get("search")),
		Page:     page,
		PageSize: pageSize,
		Filters:  filters,
	}, nil
}

// buildFacetQuery renders the candidate-value query for one column. Candidates
// come from the fully filtered result set with this column's own selection
// skipped. Counts are submission rows.
func buildFacetQuery(request FacetRequest) (string, []any, bool) {
	col, ok := lookupColumn(request.Column)
	if !ok {
		return "", nil, false
	}

	args := &argList{}
	where := buildConditions(request.Filters, request.Column, args)

	searchClause := ""
	if request.Search != "" {
		searchClause = " and " + col.expr + " ilike " + args.add("%"+escapeLike(request.Search)+"%") + " escape '\\'"
	}

	limit := args.add(request.PageSize)
	offset := args.add((request.Page - 1) * request.PageSize)

	query := baseCTE + `,
candidates as (
	select ` + col.expr + ` as value, count(*)::int as count
	from base b
	where ` + where + searchClause + `
	group by 1
)
select
	value,
	count,
	count(*) over ()::int as total,
	coalesce(sum(count) filter (where value = '') over (), 0)::int as blank_count
from candidates
order by (value = '') asc, value asc
limit ` + limit + ` offset ` + offset

	return query, args.values, true
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// Facets returns the candidate values for one filter popover.
func (s *PostgresStore) Facets(ctx context.Context, request FacetRequest) (FacetResponse, error) {
	query, args, ok := buildFacetQuery(request)
	if !ok {
		return FacetResponse{}, badRequest("column 不支持筛选")
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return FacetResponse{}, err
	}
	defer rows.Close()

	response := FacetResponse{
		Column:   request.Column,
		Values:   []FacetValue{},
		Page:     request.Page,
		PageSize: request.PageSize,
	}
	total, blankCount := 0, 0
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count, &total, &blankCount); err != nil {
			return FacetResponse{}, err
		}
		response.Values = append(response.Values, FacetValue{
			Value: value,
			Label: facetLabel(request.Column, value),
			Count: count,
			Blank: value == "",
		})
	}
	if err := rows.Err(); err != nil {
		return FacetResponse{}, err
	}

	response.Total = total
	response.BlankCount = blankCount
	response.TotalPages = (total + request.PageSize - 1) / request.PageSize
	response.HasMore = request.Page*request.PageSize < total
	return response, nil
}

// facetLabel is what the popover shows for a value.
func facetLabel(name, value string) string {
	if value == "" {
		return "(空白)"
	}
	switch name {
	case "status":
		return StatusLabel(value)
	case "payment_method":
		// Reuse the canonical Chinese method labels rather than a second copy.
		return payments.MethodLabel(value)
	}
	return value
}

// StatusLabel renders a submission status in Chinese.
func StatusLabel(status string) string {
	switch status {
	case StatusSubmitted:
		return "待核对"
	case StatusApproved:
		return "已通过"
	case StatusRejected:
		return "已驳回"
	}
	return status
}
