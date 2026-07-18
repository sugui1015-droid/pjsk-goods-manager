package orders

import (
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Pagination bounds for the order list. PageSize is capped so no caller can
// ask the database for an unbounded result set.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Facet candidate pagination bounds. A column like CN can hold thousands of
// distinct values, so the filter popover pages through them too.
const (
	DefaultFacetPageSize = 50
	MaxFacetPageSize     = 200
)

// BadRequestError marks a caller-supplied parameter as invalid. The handler
// turns it into a 400 with the message; it never wraps a database error, so
// the message is always safe to return.
type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &BadRequestError{Message: fmt.Sprintf(format, args...)}
}

// OrderFilters is the full filter state behind the order list, the facet
// popovers and the detail export. Every value-column filter is a set: the WPS
// header popover lets the user tick several values at once, and an empty
// string inside a set means "blank cell" (a product with no series, say).
//
// A nil slice means the column is unfiltered. That is deliberately distinct
// from a one-element slice holding "", which filters *for* blanks.
type OrderFilters struct {
	CN            []string
	Project       []string
	Item          []string
	Series        []string
	Category      []string
	Role          []string
	Status        []string
	PaymentStatus []string

	// Range bounds are kept as validated decimal strings and cast to numeric
	// in SQL. They are never parsed into float64: money comparisons must not
	// inherit binary floating-point rounding.
	QuantityMin string
	QuantityMax string
	AmountMin   string
	AmountMax   string
	PaidMin     string
	PaidMax     string
	UnpaidMin   string
	UnpaidMax   string

	// Normalised to RFC3339 timestamps; CreatedTo is exclusive.
	CreatedFrom string
	CreatedTo   string

	Page     int
	PageSize int
}

var (
	orderStatuses   = []string{"draft", "submitted", "partially_paid", "paid", "cancelled"}
	paymentStatuses = []string{"unpaid", "partial", "paid"}

	// Bounded decimal: enough digits for any realistic amount, no exponent
	// notation, no leading "+", so it always casts cleanly to numeric.
	decimalPattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,4})?$`)
)

// FiltersFromQuery reads the full filter state out of a URL query.
//
// Value columns use repeated parameters (?series=A&series=B). A repeated
// parameter is used rather than a delimiter-joined string because real values
// contain commas and vertical bars; url.Values already models the repetition.
// Empty and whitespace-only values are ignored. A blank-cell selection travels
// separately as ?series_blank=1, so an accidentally empty input can never turn
// into a real filter and no reserved sentinel can collide with business data.
func FiltersFromQuery(query url.Values) (OrderFilters, error) {
	filters := OrderFilters{
		CN:            valueSet(query, "cn"),
		Project:       valueSet(query, "project"),
		Item:          valueSet(query, "item"),
		Series:        valueSet(query, "series"),
		Category:      valueSet(query, "category"),
		Role:          valueSet(query, "role"),
		Status:        valueSet(query, "status"),
		PaymentStatus: valueSet(query, "payment_status"),
	}

	if err := requireAllowed("status", filters.Status, orderStatuses); err != nil {
		return OrderFilters{}, err
	}
	if err := requireAllowed("payment_status", filters.PaymentStatus, paymentStatuses); err != nil {
		return OrderFilters{}, err
	}

	ranges := []struct {
		param string
		field *string
	}{
		{"quantity_min", &filters.QuantityMin},
		{"quantity_max", &filters.QuantityMax},
		{"amount_min", &filters.AmountMin},
		{"amount_max", &filters.AmountMax},
		{"paid_min", &filters.PaidMin},
		{"paid_max", &filters.PaidMax},
		{"unpaid_min", &filters.UnpaidMin},
		{"unpaid_max", &filters.UnpaidMax},
	}
	for _, r := range ranges {
		value, err := decimalParam(query, r.param)
		if err != nil {
			return OrderFilters{}, err
		}
		*r.field = value
	}
	for _, bounds := range []struct {
		name string
		min  string
		max  string
	}{
		{"quantity", filters.QuantityMin, filters.QuantityMax},
		{"amount", filters.AmountMin, filters.AmountMax},
		{"paid", filters.PaidMin, filters.PaidMax},
		{"unpaid", filters.UnpaidMin, filters.UnpaidMax},
	} {
		if err := validateDecimalRange(bounds.name, bounds.min, bounds.max); err != nil {
			return OrderFilters{}, err
		}
	}

	from, err := timestampParam(query, "created_from", false)
	if err != nil {
		return OrderFilters{}, err
	}
	to, err := timestampParam(query, "created_to", true)
	if err != nil {
		return OrderFilters{}, err
	}
	filters.CreatedFrom = from
	filters.CreatedTo = to
	if from != "" && to != "" && from >= to {
		return OrderFilters{}, badRequest("created_from 必须早于或等于 created_to")
	}

	page, pageSize, err := pagination(query)
	if err != nil {
		return OrderFilters{}, err
	}
	filters.Page = page
	filters.PageSize = pageSize

	return filters, nil
}

func validateDecimalRange(name, min, max string) error {
	if min == "" || max == "" {
		return nil
	}
	minValue, minOK := new(big.Rat).SetString(min)
	maxValue, maxOK := new(big.Rat).SetString(max)
	if !minOK || !maxOK {
		return badRequest("%s 范围格式无效", name)
	}
	if minValue.Cmp(maxValue) > 0 {
		return badRequest("%s_min 不能大于 %s_max", name, name)
	}
	return nil
}

// valueSet returns nil when the parameter is absent or contains only empty /
// whitespace values. The dedicated <column>_blank=1 flag is the only way to
// request blank cells.
func valueSet(query url.Values, key string) []string {
	raw := query[key]
	seen := map[string]bool{}
	values := []string{}
	for _, value := range raw {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		values = append(values, trimmed)
	}
	blankFlag := strings.TrimSpace(query.Get(key + "_blank"))
	if blankFlag == "1" || strings.EqualFold(blankFlag, "true") {
		values = append(values, "")
	}
	if len(values) == 0 {
		return nil
	}
	return values
}

func requireAllowed(param string, values []string, allowed []string) error {
	for _, value := range values {
		if value == "" {
			// Status columns are never blank in the data; treat an explicit
			// blank selection as an unknown value rather than silently
			// matching nothing.
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

func decimalParam(query url.Values, key string) (string, error) {
	value := strings.TrimSpace(query.Get(key))
	if value == "" {
		return "", nil
	}
	if !decimalPattern.MatchString(value) {
		return "", badRequest("%s 必须是非负数字", key)
	}
	return value, nil
}

// timestampParam accepts either a plain date or a full RFC3339 timestamp.
//
// When exclusiveDayEnd is set and the caller passed a plain date, the bound
// advances to the next midnight: the range filter compares with "<", so
// created_to=2026-07-17 must still include everything that happened during
// the 17th rather than cutting off at 00:00.
func timestampParam(query url.Values, key string, exclusiveDayEnd bool) (string, error) {
	value := strings.TrimSpace(query.Get(key))
	if value == "" {
		return "", nil
	}
	if day, err := time.Parse("2006-01-02", value); err == nil {
		if exclusiveDayEnd {
			day = day.AddDate(0, 0, 1)
		}
		return day.Format(time.RFC3339), nil
	}
	if stamp, err := time.Parse(time.RFC3339, value); err == nil {
		return stamp.Format(time.RFC3339), nil
	}
	return "", badRequest("%s 时间格式无效", key)
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
// Callers address columns by API name only, and the SQL expression is looked up
// from this fixed table — a column name coming off the wire is never
// interpolated into a statement.
//
// Every column is single-valued because a base row is a single order item. No
// column holds an aggregated array, so no filter can match a row on the
// strength of a *sibling* item's value.
type column struct {
	name string
	expr string
}

var filterColumns = []column{
	{name: "cn", expr: "b.cn_code"},
	{name: "project", expr: "b.project_name"},
	{name: "item", expr: "b.item_name"},
	{name: "series", expr: "b.series_code"},
	{name: "category", expr: "b.category"},
	{name: "role", expr: "b.character_name"},
	{name: "status", expr: "b.status"},
	{name: "payment_status", expr: "b.payment_status"},
}

func lookupColumn(name string) (column, bool) {
	for _, candidate := range filterColumns {
		if candidate.name == name {
			return candidate, true
		}
	}
	return column{}, false
}

func (f OrderFilters) valuesFor(name string) []string {
	switch name {
	case "cn":
		return f.CN
	case "project":
		return f.Project
	case "item":
		return f.Item
	case "series":
		return f.Series
	case "category":
		return f.Category
	case "role":
		return f.Role
	case "status":
		return f.Status
	case "payment_status":
		return f.PaymentStatus
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

// baseCTE is the single source of truth for what a row *is*: one row per order
// item — one CN's one goods line. The list, the facet counts and the export all
// filter this same view, so a filter cannot mean one thing on screen and
// another in the exported file.
//
// This grain is the point. An earlier version grouped by order and array_agg'd
// the four product columns into one row, which meant filtering 角色=初音未来
// returned the whole order and still displayed its 宁宁 and 真冬 items beside
// the match. Here every product column is single-valued, so a row either
// matches a filter on its own merits or is not returned at all.
//
// Quantity is NOT expanded: an item with quantity 3 stays one row showing 3.
//
// Money stays numeric throughout; only the outer select casts to float8 for
// JSON, well after every comparison has been made. paid is clamped to the
// item's own amount and only 'approved' payments count, so a voided payment
// never shows as paid.
const baseCTE = `
with item_paid as (
	select
		pi.order_item_id,
		coalesce(sum(pi.applied_amount) filter (where pay.status = 'approved'), 0) as paid
	from payment_items pi
	join payments pay on pay.id = pi.payment_id
	group by pi.order_item_id
),
base as (
	select
		oi.id as item_id,
		o.id as order_id,
		o.order_no,
		o.status,
		u.cn_code,
		coalesce(u.display_name, '') as display_name,
		p.name as project_name,
		pr.name as item_name,
		coalesce(pr.series_code, '') as series_code,
		coalesce(pr.category, '') as category,
		coalesce(pr.character_name, '') as character_name,
		oi.quantity,
		oi.unit_price,
		oi.amount as total_amount,
		least(coalesce(ip.paid, 0), oi.amount) as paid_amount,
		greatest(oi.amount - coalesce(ip.paid, 0), 0) as unpaid_amount,
		case
			when least(coalesce(ip.paid, 0), oi.amount) <= 0.004 then 'unpaid'
			when greatest(oi.amount - coalesce(ip.paid, 0), 0) <= 0.004 then 'paid'
			else 'partial'
		end as payment_status,
		o.created_at,
		pr.sort_order
	from order_items oi
	join orders o on o.id = oi.order_id
	join users u on u.id = o.user_id
	join projects p on p.id = o.project_id
	join products pr on pr.id = oi.product_id
	left join item_paid ip on ip.order_item_id = oi.id
	where oi.revoked_at is null
)`

// buildConditions renders the WHERE clause for the base view.
//
// skipColumn omits one column's own value filter. That is what makes a facet
// popover behave like WPS: while choosing CN values the candidate list must
// still reflect every *other* column's filter, but must not narrow itself to
// the CNs already ticked — otherwise unticking one could never bring it back.
// Pass an empty skipColumn for the list and the export, where every filter
// applies.
func buildConditions(filters OrderFilters, skipColumn string, args *argList) string {
	conditions := []string{"1 = 1"}

	for _, col := range filterColumns {
		if col.name == skipColumn {
			continue
		}
		values := filters.valuesFor(col.name)
		if len(values) == 0 {
			continue
		}
		// Equality against this row's own single value. A blank selection
		// arrives as "" and matches the coalesced blank cells.
		placeholder := args.add(values)
		conditions = append(conditions, col.expr+" = any("+placeholder+"::text[])")
	}

	// All bounds are the detail row's own numbers, not its order's totals.
	numericRanges := []struct {
		expr string
		min  string
		max  string
	}{
		{"b.quantity", filters.QuantityMin, filters.QuantityMax},
		{"b.total_amount", filters.AmountMin, filters.AmountMax},
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

	if filters.CreatedFrom != "" {
		conditions = append(conditions, "b.created_at >= "+args.add(filters.CreatedFrom)+"::timestamptz")
	}
	if filters.CreatedTo != "" {
		conditions = append(conditions, "b.created_at < "+args.add(filters.CreatedTo)+"::timestamptz")
	}

	return strings.Join(conditions, " and ")
}

// buildListQuery renders one page of the filtered order list, and
// buildCountQuery the matching total. They are separate statements rather than
// a window function because a page past the end returns no rows, and the page
// still has to be able to say how many results the filters actually matched.
func buildListQuery(filters OrderFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	limit := args.add(filters.PageSize)
	offset := args.add((filters.Page - 1) * filters.PageSize)

	// Ordered so a CN's items stay together and in a stable, meaningful
	// sequence; item_id breaks ties so paging can never repeat or skip a row.
	query := baseCTE + `
select
	b.item_id::text,
	b.order_id::text,
	b.order_no,
	b.status,
	b.payment_status,
	b.cn_code,
	b.display_name,
	b.project_name,
	b.item_name,
	b.series_code,
	b.category,
	b.character_name,
	b.quantity::float8,
	b.unit_price::float8,
	b.total_amount::float8,
	b.paid_amount::float8,
	b.unpaid_amount::float8,
	to_char(b.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
from base b
where ` + where + `
order by b.created_at desc, b.order_id desc, b.sort_order, b.item_name, b.item_id
limit ` + limit + ` offset ` + offset

	return query, args.values
}

// buildCountQuery counts matching detail rows — the same unit the list pages
// through, so "结果：共 N 项谷子明细" describes exactly what is listed.
func buildCountQuery(filters OrderFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select count(*)::int from base b where ` + where, args.values
}

// BuildExportItemIDsQuery renders the ids of every order *item* matching the
// filters, with no pagination.
//
// The export restricts to these item ids rather than to their orders. That
// distinction is the whole point: restricting by order would re-inflate the
// download to every sibling item of a matching order, so filtering 角色=初音未来
// would still export 宁宁 and 真冬 rows. Since both sides filter the same base
// view, the exported rows match the listed rows exactly.
func BuildExportItemIDsQuery(filters OrderFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select b.item_id from base b where ` + where, args.values
}
