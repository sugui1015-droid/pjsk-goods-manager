package importpreview

import (
	"context"
	"net/url"
	"strconv"
	"strings"
)

// HistoryFacetRequest describes one filter popover's candidate query.
type HistoryFacetRequest struct {
	Column   string
	Search   string
	Page     int
	PageSize int
	Filters  HistoryFilters
}

// HistoryFacetValue is one candidate row in a filter popover.
type HistoryFacetValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
	Blank bool   `json:"blank"`
}

// HistoryFacetResponse pages through a column's candidate values.
type HistoryFacetResponse struct {
	Column     string              `json:"column"`
	Values     []HistoryFacetValue `json:"values"`
	Total      int                 `json:"total"`
	BlankCount int                 `json:"blank_count"`
	Page       int                 `json:"facet_page"`
	PageSize   int                 `json:"facet_page_size"`
	TotalPages int                 `json:"total_pages"`
	HasMore    bool                `json:"has_more"`
}

// facetableColumns: filename/status/uploaded_by are business columns. file_hash
// and internal ids are deliberately absent — they must never become a filter
// surface.
var facetableColumns = map[string]bool{
	"filename":    true,
	"status":      true,
	"uploaded_by": true,
}

// HistoryFacetRequestFromQuery parses a facet request, reusing the same filter
// parser as the list.
func HistoryFacetRequestFromQuery(query url.Values) (HistoryFacetRequest, error) {
	name := strings.TrimSpace(query.Get("column"))
	if name == "" {
		return HistoryFacetRequest{}, badRequest("column 不能为空")
	}
	if !facetableColumns[name] {
		return HistoryFacetRequest{}, badRequest("column 不支持筛选")
	}

	filterQuery := url.Values{}
	for key, values := range query {
		switch key {
		case "page", "page_size", "column", "search", "facet_page", "facet_page_size":
			continue
		}
		filterQuery[key] = values
	}
	filters, err := HistoryFiltersFromQuery(filterQuery)
	if err != nil {
		return HistoryFacetRequest{}, err
	}

	page := 1
	if raw := strings.TrimSpace(query.Get("facet_page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return HistoryFacetRequest{}, badRequest("facet_page 必须是大于 0 的整数")
		}
		page = parsed
	}
	pageSize := DefaultFacetPageSize
	if raw := strings.TrimSpace(query.Get("facet_page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return HistoryFacetRequest{}, badRequest("facet_page_size 必须是大于 0 的整数")
		}
		if parsed > MaxFacetPageSize {
			return HistoryFacetRequest{}, badRequest("facet_page_size 最大为 %d", MaxFacetPageSize)
		}
		pageSize = parsed
	}

	return HistoryFacetRequest{
		Column:   name,
		Search:   strings.TrimSpace(query.Get("search")),
		Page:     page,
		PageSize: pageSize,
		Filters:  filters,
	}, nil
}

// buildFacetQuery renders the candidate-value query for one column. Counts are
// import-record rows.
func buildFacetQuery(request HistoryFacetRequest) (string, []any, bool) {
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

// ImportFacets returns the candidate values for one filter popover.
func (s *PostgresStore) ImportFacets(ctx context.Context, request HistoryFacetRequest) (HistoryFacetResponse, error) {
	query, args, ok := buildFacetQuery(request)
	if !ok {
		return HistoryFacetResponse{}, badRequest("column 不支持筛选")
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return HistoryFacetResponse{}, err
	}
	defer rows.Close()

	response := HistoryFacetResponse{
		Column:   request.Column,
		Values:   []HistoryFacetValue{},
		Page:     request.Page,
		PageSize: request.PageSize,
	}
	total, blankCount := 0, 0
	for rows.Next() {
		var value string
		var count int
		if err := rows.Scan(&value, &count, &total, &blankCount); err != nil {
			return HistoryFacetResponse{}, err
		}
		response.Values = append(response.Values, HistoryFacetValue{
			Value: value,
			Label: facetLabel(request.Column, value),
			Count: count,
			Blank: value == "",
		})
	}
	if err := rows.Err(); err != nil {
		return HistoryFacetResponse{}, err
	}

	response.Total = total
	response.BlankCount = blankCount
	response.TotalPages = (total + request.PageSize - 1) / request.PageSize
	response.HasMore = request.Page*request.PageSize < total
	return response, nil
}

// facetLabel is what the popover shows. Status stores machine values but must
// read as Chinese; a blank value needs a visible name.
func facetLabel(name, value string) string {
	if value == "" {
		return "(空白)"
	}
	if name == "status" {
		return StatusLabel(value)
	}
	return value
}

// StatusLabel renders an import status in Chinese, matching the frontend's
// existing statusLabel map.
func StatusLabel(status string) string {
	switch status {
	case "pending":
		return "待处理"
	case "previewed":
		return "待确认"
	case "processing":
		return "处理中"
	case "confirmed":
		return "已确认"
	case "completed":
		return "已完成"
	case "partial", "partially_completed":
		return "部分完成"
	case "failed":
		return "失败"
	case "cancelled":
		return "已取消"
	case "reverted":
		return "已撤销"
	}
	return status
}
