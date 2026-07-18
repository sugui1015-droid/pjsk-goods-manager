package users

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pagination bounds for the user list. PageSize is capped so no caller can ask
// the database for an unbounded result set.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Facet candidate pagination bounds.
const (
	DefaultFacetPageSize = 50
	MaxFacetPageSize     = 200
)

// BadRequestError marks a caller-supplied parameter as invalid. The handler
// turns it into a 400 with the message; it never wraps a database error, so the
// message is always safe to return.
type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &BadRequestError{Message: fmt.Sprintf(format, args...)}
}

// Filters is the full filter state behind the user list, the facet popovers and
// the user export. Value columns are sets: a WPS header popover lets the user
// tick several values at once.
//
// A nil slice means the column is unfiltered — deliberately distinct from a
// one-element slice holding "", which filters *for* blanks.
type Filters struct {
	CN               []string
	Name             []string
	Status           []string
	HasQueryCode     []string
	HasRecoveryEmail []string

	// Range bounds stay validated decimal strings and are cast to numeric in
	// SQL. They are never parsed into float64: money comparisons must not
	// inherit binary floating-point rounding.
	OrderCountMin string
	OrderCountMax string
	TotalMin      string
	TotalMax      string
	PaidMin       string
	PaidMax       string
	UnpaidMin     string
	UnpaidMax     string

	// Normalised to RFC3339; the "To" bounds are exclusive.
	LastLoginFrom string
	LastLoginTo   string
	// LastLoginBlank filters for users who have never logged in.
	LastLoginBlank bool
	CreatedFrom    string
	CreatedTo      string

	Page     int
	PageSize int
}

var (
	userStatuses = []string{"active", "disabled", "merged"}
	// Boolean columns are ticked as yes/no rather than true/false so the wire
	// format matches what the popover shows and stays readable in a URL.
	booleanValues = []string{"yes", "no"}

	decimalPattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,4})?$`)
	integerPattern = regexp.MustCompile(`^\d{1,9}$`)
)

// FiltersFromQuery reads the full filter state out of a URL query.
//
// Value columns use repeated parameters (?cn=A&cn=B); a blank-cell selection
// travels as ?<column>_blank=1 so an accidentally empty input can never be
// mistaken for "filter for blanks".
func FiltersFromQuery(query url.Values) (Filters, error) {
	filters := Filters{
		CN:               valueSet(query, "cn"),
		Name:             valueSet(query, "name"),
		Status:           valueSet(query, "status"),
		HasQueryCode:     valueSet(query, "has_query_code"),
		HasRecoveryEmail: valueSet(query, "has_recovery_email"),
	}

	if err := requireAllowed("status", filters.Status, userStatuses); err != nil {
		return Filters{}, err
	}
	if err := requireAllowed("has_query_code", filters.HasQueryCode, booleanValues); err != nil {
		return Filters{}, err
	}
	if err := requireAllowed("has_recovery_email", filters.HasRecoveryEmail, booleanValues); err != nil {
		return Filters{}, err
	}

	orderCountMin, err := numberParam(query, "order_count_min", integerPattern, "必须是非负整数")
	if err != nil {
		return Filters{}, err
	}
	orderCountMax, err := numberParam(query, "order_count_max", integerPattern, "必须是非负整数")
	if err != nil {
		return Filters{}, err
	}
	filters.OrderCountMin, filters.OrderCountMax = orderCountMin, orderCountMax

	money := []struct {
		param string
		field *string
	}{
		{"total_min", &filters.TotalMin},
		{"total_max", &filters.TotalMax},
		{"paid_min", &filters.PaidMin},
		{"paid_max", &filters.PaidMax},
		{"unpaid_min", &filters.UnpaidMin},
		{"unpaid_max", &filters.UnpaidMax},
	}
	for _, entry := range money {
		value, err := numberParam(query, entry.param, decimalPattern, "必须是非负数字")
		if err != nil {
			return Filters{}, err
		}
		*entry.field = value
	}

	for _, pair := range [][2]string{
		{"order_count_min", "order_count_max"},
		{"total_min", "total_max"},
		{"paid_min", "paid_max"},
		{"unpaid_min", "unpaid_max"},
	} {
		if err := requireOrderedRange(query, pair[0], pair[1]); err != nil {
			return Filters{}, err
		}
	}

	dates := []struct {
		param           string
		field           *string
		exclusiveDayEnd bool
	}{
		{"last_login_from", &filters.LastLoginFrom, false},
		{"last_login_to", &filters.LastLoginTo, true},
		{"created_from", &filters.CreatedFrom, false},
		{"created_to", &filters.CreatedTo, true},
	}
	for _, entry := range dates {
		value, err := timestampParam(query, entry.param, entry.exclusiveDayEnd)
		if err != nil {
			return Filters{}, err
		}
		*entry.field = value
	}
	for _, pair := range [][2]string{{"last_login_from", "last_login_to"}, {"created_from", "created_to"}} {
		if err := requireOrderedDates(query, pair[0], pair[1]); err != nil {
			return Filters{}, err
		}
	}

	filters.LastLoginBlank = blankFlag(query, "last_login")

	page, pageSize, err := pagination(query)
	if err != nil {
		return Filters{}, err
	}
	filters.Page, filters.PageSize = page, pageSize

	return filters, nil
}

// valueSet returns nil when the parameter is absent or holds only empty /
// whitespace values. The dedicated <column>_blank=1 flag is the only way to
// request blank cells.
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

func requireAllowed(param string, values []string, allowed []string) error {
	for _, value := range values {
		if value == "" {
			return badRequest("%s 不支持空白值", param)
		}
		if !contains(allowed, value) {
			return badRequest("%s 取值无效", param)
		}
	}
	return nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
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

// requireOrderedRange rejects an inverted range up front. Without it the query
// would silently return nothing and look like "no such users" rather than a
// mistake in the filter.
func requireOrderedRange(query url.Values, minKey, maxKey string) error {
	minRaw := strings.TrimSpace(query.Get(minKey))
	maxRaw := strings.TrimSpace(query.Get(maxKey))
	if minRaw == "" || maxRaw == "" {
		return nil
	}
	minValue, minErr := strconv.ParseFloat(minRaw, 64)
	maxValue, maxErr := strconv.ParseFloat(maxRaw, 64)
	if minErr != nil || maxErr != nil {
		return nil // format errors are reported by numberParam
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
		return nil // format errors are reported by timestampParam
	}
	if from.After(to) {
		return badRequest("%s 不能晚于 %s", fromKey, toKey)
	}
	return nil
}

func parseTimestamp(value string, exclusiveDayEnd bool) (time.Time, error) {
	if day, err := time.Parse("2006-01-02", value); err == nil {
		if exclusiveDayEnd {
			day = day.AddDate(0, 0, 1)
		}
		return day, nil
	}
	return time.Parse(time.RFC3339, value)
}

// timestampParam accepts a plain date or a full RFC3339 timestamp. When
// exclusiveDayEnd is set and the caller passed a plain date, the bound advances
// to the next midnight: the range filter compares with "<", so
// created_to=2026-07-17 must still include everything that happened that day.
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

// column describes one filterable column in terms of the base view below.
// Callers address columns by API name only and the SQL expression is looked up
// from this fixed table — a column name coming off the wire is never
// interpolated into a statement.
type column struct {
	name string
	expr string
}

var filterColumns = []column{
	{name: "cn", expr: "b.cn_code"},
	{name: "name", expr: "b.display_name"},
	{name: "status", expr: "b.status"},
	{name: "has_query_code", expr: "b.has_query_code"},
	{name: "has_recovery_email", expr: "b.has_recovery_email"},
}

func lookupColumn(name string) (column, bool) {
	for _, candidate := range filterColumns {
		if candidate.name == name {
			return candidate, true
		}
	}
	return column{}, false
}

func (f Filters) valuesFor(name string) []string {
	switch name {
	case "cn":
		return f.CN
	case "name":
		return f.Name
	case "status":
		return f.Status
	case "has_query_code":
		return f.HasQueryCode
	case "has_recovery_email":
		return f.HasRecoveryEmail
	}
	return nil
}

// argList accumulates bind parameters and hands back the placeholder for each.
// Every user-supplied value in this package goes through it; nothing is
// concatenated into SQL text.
type argList struct {
	values []any
}

func (a *argList) add(value any) string {
	a.values = append(a.values, value)
	return "$" + strconv.Itoa(len(a.values))
}

// baseCTE is the single source of truth for what a user row is. The list, the
// count, the summary, the facet counts and the export all filter this same
// view, so a filter cannot mean one thing on screen and another in the
// exported file.
//
// Privacy is enforced here, at the source: query_code_hash is reduced to a
// yes/no and the recovery email is reduced to a yes/no. Neither the hash, the
// ciphertext nor the lookup hash is ever selected, so no later change to a DTO
// can leak them.
//
// Money stays numeric throughout; only the outer select casts to float8 for
// JSON, well after every comparison has been made. Only approved payments
// count toward paid, so a voided payment never reads as paid.
const baseCTE = `
with paid_by_item as (
	select
		pi.order_item_id,
		coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
	from payment_items pi
	join payments p on p.id = pi.payment_id
	group by pi.order_item_id
),
user_totals as (
	select
		o.user_id,
		count(distinct o.id)::int as order_count,
		coalesce(sum(oi.amount), 0) as total_amount,
		coalesce(sum(least(coalesce(paid.paid_amount, 0), oi.amount)), 0) as paid_amount
	from orders o
	join order_items oi on oi.order_id = o.id and oi.revoked_at is null
	left join paid_by_item paid on paid.order_item_id = oi.id
	where o.status <> 'cancelled'
	group by o.user_id
),
base as (
	select
		u.id,
		u.cn_code,
		coalesce(u.display_name, '') as display_name,
		u.status,
		case when coalesce(u.query_code_hash, '') <> '' then 'yes' else 'no' end as has_query_code,
		case when re.user_id is not null then 'yes' else 'no' end as has_recovery_email,
		coalesce(t.order_count, 0) as order_count,
		coalesce(t.total_amount, 0) as total_amount,
		coalesce(t.paid_amount, 0) as paid_amount,
		greatest(coalesce(t.total_amount, 0) - coalesce(t.paid_amount, 0), 0) as unpaid_amount,
		u.created_at,
		u.query_code_updated_at,
		u.last_query_login_at
	from users u
	left join user_totals t on t.user_id = u.id
	left join user_recovery_emails re on re.user_id = u.id and re.invalidated_at is null
)`

// buildConditions renders the WHERE clause for the base view.
//
// skipColumn omits one column's own value filter. That is what makes a facet
// popover behave like WPS: while choosing CN values the candidate list must
// still reflect every *other* column's filter, but must not narrow itself to
// the CNs already ticked — otherwise unticking one could never bring it back.
// Pass an empty skipColumn for the list, count, summary and export.
func buildConditions(filters Filters, skipColumn string, args *argList) string {
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
		{"b.order_count", filters.OrderCountMin, filters.OrderCountMax},
		{"b.total_amount", filters.TotalMin, filters.TotalMax},
		{"b.paid_amount", filters.PaidMin, filters.PaidMax},
		{"b.unpaid_amount", filters.UnpaidMin, filters.UnpaidMax},
	}
	for _, r := range numericRanges {
		if r.min != "" {
			conditions = append(conditions, r.expr+" >= "+args.add(r.min)+"::numeric")
		}
		if r.max != "" {
			conditions = append(conditions, r.expr+" <= "+args.add(r.max)+"::numeric")
		}
	}

	if filters.LastLoginBlank {
		// "从未登录" is a real filter target, not an absence of one.
		conditions = append(conditions, "b.last_query_login_at is null")
	}
	if filters.LastLoginFrom != "" {
		conditions = append(conditions, "b.last_query_login_at >= "+args.add(filters.LastLoginFrom)+"::timestamptz")
	}
	if filters.LastLoginTo != "" {
		conditions = append(conditions, "b.last_query_login_at < "+args.add(filters.LastLoginTo)+"::timestamptz")
	}
	if filters.CreatedFrom != "" {
		conditions = append(conditions, "b.created_at >= "+args.add(filters.CreatedFrom)+"::timestamptz")
	}
	if filters.CreatedTo != "" {
		conditions = append(conditions, "b.created_at < "+args.add(filters.CreatedTo)+"::timestamptz")
	}

	return strings.Join(conditions, " and ")
}

const listColumns = `
	b.id::text,
	b.cn_code,
	b.display_name,
	b.has_query_code = 'yes',
	b.has_recovery_email = 'yes',
	b.status,
	b.order_count,
	b.total_amount::float8,
	b.paid_amount::float8,
	b.unpaid_amount::float8,
	to_char(b.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	coalesce(to_char(b.query_code_updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	coalesce(to_char(b.last_query_login_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), '')`

func buildListQuery(filters Filters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	limit := args.add(filters.PageSize)
	offset := args.add((filters.Page - 1) * filters.PageSize)

	return baseCTE + `
select` + listColumns + `
from base b
where ` + where + `
order by b.created_at desc, b.cn_code
limit ` + limit + ` offset ` + offset, args.values
}

func buildCountQuery(filters Filters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select count(*)::int from base b where ` + where, args.values
}

// buildSummaryQuery aggregates over the whole filtered set, not the page.
//
// The summary used to be computed by adding up the rows Go had just scanned.
// That was fine while the list was unpaginated, but once a page holds 50 of 300
// users those tiles would silently describe the page while the header claims
// 300 results. Aggregating in SQL keeps the tiles and "结果：共 N 位用户" telling
// the same story.
func buildSummaryQuery(filters Filters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select
	count(*)::int,
	count(*) filter (where b.order_count > 0)::int,
	coalesce(sum(b.total_amount), 0)::float8,
	coalesce(sum(b.paid_amount), 0)::float8,
	coalesce(sum(b.unpaid_amount), 0)::float8
from base b
where ` + where, args.values
}

// BuildExportQuery renders every user matching the filters, honouring the
// caller's row cap but ignoring list pagination: an export follows the full
// filter result, never the page on screen.
func BuildExportQuery(filters Filters, maxRows int) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	limit := args.add(maxRows)
	return baseCTE + `
select` + listColumns + `
from base b
where ` + where + `
order by b.created_at desc, b.cn_code
limit ` + limit, args.values
}
