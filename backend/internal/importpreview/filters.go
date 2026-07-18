package importpreview

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pagination bounds for the import history list.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Facet candidate pagination bounds.
const (
	DefaultFacetPageSize = 50
	MaxFacetPageSize     = 200
)

// BadRequestError marks a caller-supplied parameter as invalid.
type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &BadRequestError{Message: fmt.Sprintf(format, args...)}
}

// HistoryFilters is the full filter state behind the import history list, the
// facet popovers and (potentially) an export. Value columns are sets; a WPS
// header popover lets the user tick several values at once.
//
// The status column is a free value column sourced from facets rather than a
// fixed allowlist: import statuses have grown across migrations (previewed,
// confirmed, reverted, …) and hard-coding them here would silently reject real
// values.
type HistoryFilters struct {
	Filename   []string
	Status     []string
	UploadedBy []string

	SheetMin   string
	SheetMax   string
	IssueMin   string
	IssueMax   string
	WrittenMin string
	WrittenMax string
	AmountMin  string
	AmountMax  string

	CreatedFrom string
	CreatedTo   string
	// ConfirmedBlank filters for imports that were never confirmed.
	ConfirmedBlank bool
	ConfirmedFrom  string
	ConfirmedTo    string

	Page     int
	PageSize int
}

var (
	decimalPattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,4})?$`)
	integerPattern = regexp.MustCompile(`^\d{1,9}$`)
)

// HistoryFiltersFromQuery reads the full filter state out of a URL query.
func HistoryFiltersFromQuery(query url.Values) (HistoryFilters, error) {
	filters := HistoryFilters{
		Filename:   valueSet(query, "filename"),
		Status:     valueSet(query, "status"),
		UploadedBy: valueSet(query, "uploaded_by"),
	}

	integers := []struct {
		param string
		field *string
	}{
		{"sheet_min", &filters.SheetMin},
		{"sheet_max", &filters.SheetMax},
		{"issue_min", &filters.IssueMin},
		{"issue_max", &filters.IssueMax},
		{"written_min", &filters.WrittenMin},
		{"written_max", &filters.WrittenMax},
	}
	for _, entry := range integers {
		value, err := numberParam(query, entry.param, integerPattern, "必须是非负整数")
		if err != nil {
			return HistoryFilters{}, err
		}
		*entry.field = value
	}

	amountMin, err := numberParam(query, "amount_min", decimalPattern, "必须是非负数字")
	if err != nil {
		return HistoryFilters{}, err
	}
	amountMax, err := numberParam(query, "amount_max", decimalPattern, "必须是非负数字")
	if err != nil {
		return HistoryFilters{}, err
	}
	filters.AmountMin, filters.AmountMax = amountMin, amountMax

	for _, pair := range [][2]string{
		{"sheet_min", "sheet_max"},
		{"issue_min", "issue_max"},
		{"written_min", "written_max"},
		{"amount_min", "amount_max"},
	} {
		if err := requireOrderedRange(query, pair[0], pair[1]); err != nil {
			return HistoryFilters{}, err
		}
	}

	dates := []struct {
		param           string
		field           *string
		exclusiveDayEnd bool
	}{
		{"created_from", &filters.CreatedFrom, false},
		{"created_to", &filters.CreatedTo, true},
		{"confirmed_from", &filters.ConfirmedFrom, false},
		{"confirmed_to", &filters.ConfirmedTo, true},
	}
	for _, entry := range dates {
		value, err := timestampParam(query, entry.param, entry.exclusiveDayEnd)
		if err != nil {
			return HistoryFilters{}, err
		}
		*entry.field = value
	}
	for _, pair := range [][2]string{{"created_from", "created_to"}, {"confirmed_from", "confirmed_to"}} {
		if err := requireOrderedDates(query, pair[0], pair[1]); err != nil {
			return HistoryFilters{}, err
		}
	}

	filters.ConfirmedBlank = blankFlag(query, "confirmed")

	page, pageSize, err := pagination(query)
	if err != nil {
		return HistoryFilters{}, err
	}
	filters.Page, filters.PageSize = page, pageSize

	return filters, nil
}

func valueSet(query url.Values, key string) []string {
	seen := map[string]bool{}
	values := []string{}
	for _, value := range query[key] {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		values = append(values, trimmed)
	}
	if blankFlag(query, key) {
		values = append(values, "")
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func blankFlag(query url.Values, key string) bool {
	flag := strings.TrimSpace(query.Get(key + "_blank"))
	return flag == "1" || strings.EqualFold(flag, "true")
}

func numberParam(query url.Values, key string, pattern *regexp.Regexp, requirement string) (string, error) {
	value := strings.TrimSpace(query.Get(key))
	if value == "" {
		return "", nil
	}
	if !pattern.MatchString(value) {
		return "", badRequest("%s %s", key, requirement)
	}
	return value, nil
}

func requireOrderedRange(query url.Values, minKey, maxKey string) error {
	minRaw := strings.TrimSpace(query.Get(minKey))
	maxRaw := strings.TrimSpace(query.Get(maxKey))
	if minRaw == "" || maxRaw == "" {
		return nil
	}
	minValue, minErr := strconv.ParseFloat(minRaw, 64)
	maxValue, maxErr := strconv.ParseFloat(maxRaw, 64)
	if minErr != nil || maxErr != nil {
		return nil
	}
	if minValue > maxValue {
		return badRequest("%s 不能大于 %s", minKey, maxKey)
	}
	return nil
}

func requireOrderedDates(query url.Values, fromKey, toKey string) error {
	fromRaw := strings.TrimSpace(query.Get(fromKey))
	toRaw := strings.TrimSpace(query.Get(toKey))
	if fromRaw == "" || toRaw == "" {
		return nil
	}
	from, fromErr := parseTimestamp(fromRaw, false)
	to, toErr := parseTimestamp(toRaw, false)
	if fromErr != nil || toErr != nil {
		return nil
	}
	if from.After(to) {
		return badRequest("%s 不能晚于 %s", fromKey, toKey)
	}
	return nil
}

func parseTimestamp(value string, exclusiveDayEnd bool) (time.Time, error) {
	if stamp, err := time.Parse(time.RFC3339, value); err == nil {
		return stamp, nil
	}
	if day, err := time.Parse("2006-01-02", value); err == nil {
		if exclusiveDayEnd {
			day = day.AddDate(0, 0, 1)
		}
		return day, nil
	}
	return time.Time{}, badRequest("invalid timestamp")
}

func timestampParam(query url.Values, key string, exclusiveDayEnd bool) (string, error) {
	value := strings.TrimSpace(query.Get(key))
	if value == "" {
		return "", nil
	}
	stamp, err := parseTimestamp(value, exclusiveDayEnd)
	if err != nil {
		return "", badRequest("%s 时间格式无效", key)
	}
	return stamp.Format(time.RFC3339), nil
}

func pagination(query url.Values) (int, int, error) {
	page := 1
	if raw := strings.TrimSpace(query.Get("page")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, badRequest("page 必须是大于 0 的整数")
		}
		page = parsed
	}
	pageSize := DefaultPageSize
	if raw := strings.TrimSpace(query.Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			return 0, 0, badRequest("page_size 必须是大于 0 的整数")
		}
		if parsed > MaxPageSize {
			return 0, 0, badRequest("page_size 最大为 %d", MaxPageSize)
		}
		pageSize = parsed
	}
	return page, pageSize, nil
}

type column struct {
	name string
	expr string
}

var filterColumns = []column{
	{name: "filename", expr: "b.filename"},
	{name: "status", expr: "b.status"},
	{name: "uploaded_by", expr: "b.uploaded_by"},
}

func lookupColumn(name string) (column, bool) {
	for _, candidate := range filterColumns {
		if candidate.name == name {
			return candidate, true
		}
	}
	return column{}, false
}

func (f HistoryFilters) valuesFor(name string) []string {
	switch name {
	case "filename":
		return f.Filename
	case "status":
		return f.Status
	case "uploaded_by":
		return f.UploadedBy
	}
	return nil
}

type argList struct {
	values []any
}

func (a *argList) add(value any) string {
	a.values = append(a.values, value)
	return "$" + strconv.Itoa(len(a.values))
}

// baseCTE is the single source of truth. It exposes both the raw columns the
// existing ImportHistoryItem scanner needs and the derived columns the filters
// act on (issue_count, written_count, total_amount extracted from the
// confirm_result JSON). The list, count and facet queries all filter this same
// view.
//
// Technical identifiers (file_hash) are still selected because the existing
// ImportHistoryItem carries them for the detail page's technical section — the
// main table simply does not render them.
const baseCTE = `
with base as (
	select
		b.id,
		b.original_filename as filename,
		b.file_hash,
		coalesce(b.file_size, 0) as file_size,
		coalesce(b.sheet_count, 0) as sheet_count,
		b.total_rows,
		b.status,
		coalesce(importer.username, '') as uploaded_by,
		coalesce(confirmer.username, '') as confirmed_by,
		b.created_at,
		b.started_at,
		b.confirmed_at,
		b.completed_at,
		b.error_count,
		b.warning_count,
		b.notice_count,
		b.warnings_accepted,
		b.confirm_result,
		coalesce(revoker.username, '') as revoked_by,
		b.revoked_at,
		b.revoke_result,
		(b.error_count + b.warning_count + b.notice_count) as issue_count,
		coalesce((b.confirm_result->>'order_item_count')::int, 0) as written_count,
		coalesce((b.confirm_result->>'total_amount')::numeric, 0) as total_amount
	from import_batches b
	left join admins importer on importer.id = b.imported_by
	left join admins confirmer on confirmer.id = b.confirmed_by
	left join admins revoker on revoker.id = b.revoked_by
)`

// listColumns matches the ImportHistoryItem scanner order exactly, so
// ListImports can keep using scanImportHistoryItem unchanged.
const listColumns = `
	b.id::text,
	b.filename,
	b.file_hash,
	b.file_size,
	b.sheet_count,
	b.total_rows,
	b.status,
	b.uploaded_by,
	b.confirmed_by,
	to_char(b.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	coalesce(to_char(b.started_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	coalesce(to_char(b.confirmed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	coalesce(to_char(b.completed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	b.error_count,
	b.warning_count,
	b.notice_count,
	b.warnings_accepted,
	coalesce(b.confirm_result::text, ''),
	b.revoked_by,
	coalesce(to_char(b.revoked_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	coalesce(b.revoke_result::text, '')`

func buildConditions(filters HistoryFilters, skipColumn string, args *argList) string {
	conditions := []string{"1 = 1"}

	for _, col := range filterColumns {
		if col.name == skipColumn {
			continue
		}
		values := filters.valuesFor(col.name)
		if len(values) == 0 {
			continue
		}
		placeholder := args.add(values)
		conditions = append(conditions, col.expr+" = any("+placeholder+"::text[])")
	}

	numericRanges := []struct {
		expr string
		min  string
		max  string
	}{
		{"b.sheet_count", filters.SheetMin, filters.SheetMax},
		{"b.issue_count", filters.IssueMin, filters.IssueMax},
		{"b.written_count", filters.WrittenMin, filters.WrittenMax},
		{"b.total_amount", filters.AmountMin, filters.AmountMax},
	}
	for _, r := range numericRanges {
		if r.min != "" {
			conditions = append(conditions, r.expr+" >= "+args.add(r.min)+"::numeric")
		}
		if r.max != "" {
			conditions = append(conditions, r.expr+" <= "+args.add(r.max)+"::numeric")
		}
	}

	if filters.CreatedFrom != "" {
		conditions = append(conditions, "b.created_at >= "+args.add(filters.CreatedFrom)+"::timestamptz")
	}
	if filters.CreatedTo != "" {
		conditions = append(conditions, "b.created_at < "+args.add(filters.CreatedTo)+"::timestamptz")
	}
	if filters.ConfirmedBlank {
		conditions = append(conditions, "b.confirmed_at is null")
	}
	if filters.ConfirmedFrom != "" {
		conditions = append(conditions, "b.confirmed_at >= "+args.add(filters.ConfirmedFrom)+"::timestamptz")
	}
	if filters.ConfirmedTo != "" {
		conditions = append(conditions, "b.confirmed_at < "+args.add(filters.ConfirmedTo)+"::timestamptz")
	}

	return strings.Join(conditions, " and ")
}

func buildListQuery(filters HistoryFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	limit := args.add(filters.PageSize)
	offset := args.add((filters.Page - 1) * filters.PageSize)
	return baseCTE + `
select` + listColumns + `
from base b
where ` + where + `
order by b.created_at desc, b.id desc
limit ` + limit + ` offset ` + offset, args.values
}

func buildCountQuery(filters HistoryFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select count(*)::int from base b where ` + where, args.values
}
