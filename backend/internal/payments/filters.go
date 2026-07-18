package payments

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// errInvalidTimestamp is internal to parsing; callers turn it into a 400 with a
// per-parameter Chinese message.
var errInvalidTimestamp = errors.New("invalid timestamp")

// BadRequestError marks a caller-supplied parameter as invalid. writePaymentError
// turns it into a 400 with the message; it never wraps a database error, so the
// message is always safe to return.
type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &BadRequestError{Message: fmt.Sprintf(format, args...)}
}

// Pagination bounds for the payment list. PageSize is capped so no caller can
// ask the database for an unbounded result set.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// Facet candidate pagination bounds.
const (
	DefaultFacetPageSize = 50
	MaxFacetPageSize     = 200
)

var (
	paymentStatuses = []string{"submitted", "approved", "rejected", "cancelled", "voided"}
	paymentMethods  = []string{"alipay", "wechat", "bank", "cash", "other"}

	decimalPattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,4})?$`)
)

// FiltersFromQuery reads the full filter state out of a URL query.
//
// Value columns use repeated parameters (?cn=A&cn=B); a blank selection travels
// as ?<column>_blank=1 so an accidentally empty input can never be mistaken for
// "filter for blanks".
func FiltersFromQuery(query url.Values) (PaymentFilters, error) {
	filters := PaymentFilters{
		CN:            valueSet(query, "cn"),
		PaymentMethod: valueSet(query, "payment_method"),
		Status:        valueSet(query, "status"),
		CreatedBy:     valueSet(query, "created_by"),
	}

	// Payment methods are normalised the same way the stored values are, so a
	// facet value picked off the screen always matches what is in the column.
	for i, method := range filters.PaymentMethod {
		if method != "" {
			filters.PaymentMethod[i] = normalizePaymentMethodFilter(method)
		}
	}

	if err := requireAllowed("status", filters.Status, paymentStatuses); err != nil {
		return PaymentFilters{}, err
	}
	if err := requireAllowed("payment_method", filters.PaymentMethod, paymentMethods); err != nil {
		return PaymentFilters{}, err
	}

	money := []struct {
		param string
		field *string
	}{
		{"principal_min", &filters.PrincipalMin},
		{"principal_max", &filters.PrincipalMax},
		{"fee_min", &filters.FeeMin},
		{"fee_max", &filters.FeeMax},
		{"payable_min", &filters.PayableMin},
		{"payable_max", &filters.PayableMax},
	}
	for _, entry := range money {
		value, err := decimalParam(query, entry.param)
		if err != nil {
			return PaymentFilters{}, err
		}
		*entry.field = value
	}
	for _, pair := range [][2]string{
		{"principal_min", "principal_max"},
		{"fee_min", "fee_max"},
		{"payable_min", "payable_max"},
	} {
		if err := requireOrderedRange(query, pair[0], pair[1]); err != nil {
			return PaymentFilters{}, err
		}
	}

	dates := []struct {
		param           string
		field           *string
		exclusiveDayEnd bool
	}{
		{"paid_from", &filters.PaidFrom, false},
		{"paid_to", &filters.PaidTo, true},
		{"voided_from", &filters.VoidedFrom, false},
		{"voided_to", &filters.VoidedTo, true},
	}
	for _, entry := range dates {
		value, err := timestampParam(query, entry.param, entry.exclusiveDayEnd)
		if err != nil {
			return PaymentFilters{}, err
		}
		*entry.field = value
	}
	for _, pair := range [][2]string{{"paid_from", "paid_to"}, {"voided_from", "voided_to"}} {
		if err := requireOrderedDates(query, pair[0], pair[1]); err != nil {
			return PaymentFilters{}, err
		}
	}

	// A blank 撤销时间 means "not voided" — a filter target in its own right.
	filters.VoidedBlank = blankFlag(query, "voided")

	page, pageSize, err := pagination(query)
	if err != nil {
		return PaymentFilters{}, err
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
		if !containsValue(allowed, value) {
			return badRequest("%s 取值无效", param)
		}
	}
	return nil
}

func containsValue(values []string, value string) bool {
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

// requireOrderedRange rejects an inverted range up front. Without it the query
// would silently return nothing and look like "no such payments" rather than a
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
		return nil // format errors are reported by decimalParam
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

// parseTimestamp resolves a filter bound to an absolute instant.
//
// A value that already carries an offset is taken as-is. A naive value (a plain
// date, or the datetime-local shape the old filter form submitted) is anchored
// to Asia/Shanghai, matching the existing normalizeChinaTimestampParam
// behaviour: without a zone Postgres would resolve the string using the
// database session's timezone, which is not guaranteed to be China time, and
// the admin's intended wall-clock day would silently shift.
func parseTimestamp(value string, exclusiveDayEnd bool) (time.Time, error) {
	if stamp, err := time.Parse(time.RFC3339, value); err == nil {
		return stamp, nil
	}
	if day, err := time.ParseInLocation("2006-01-02", value, chinaLocation); err == nil {
		if exclusiveDayEnd {
			day = day.AddDate(0, 0, 1)
		}
		return day, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if stamp, err := time.ParseInLocation(layout, value, chinaLocation); err == nil {
			return stamp, nil
		}
	}
	return time.Time{}, errInvalidTimestamp
}

// timestampParam accepts a plain date, a datetime-local value or a full RFC3339
// timestamp. When exclusiveDayEnd is set and the caller passed a plain date, the
// bound advances to the next midnight: the range filter compares with "<", so
// paid_to=2026-07-17 must still include everything that happened that day.
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
	{name: "payment_method", expr: "b.payment_method"},
	{name: "status", expr: "b.status"},
	{name: "created_by", expr: "b.created_by"},
}

func lookupColumn(name string) (column, bool) {
	for _, candidate := range filterColumns {
		if candidate.name == name {
			return candidate, true
		}
	}
	return column{}, false
}

func (f PaymentFilters) valuesFor(name string) []string {
	switch name {
	case "cn":
		return f.CN
	case "payment_method":
		return f.PaymentMethod
	case "status":
		return f.Status
	case "created_by":
		return f.CreatedBy
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

// baseCTE is the single source of truth for what a payment row is: one row per
// payments record. The list, the count, the facet counts and the export all
// filter this same view, so a filter cannot mean one thing on screen and
// another in the exported file.
//
// The three money columns are the stored values, never recomputed: principal is
// submitted_amount, fee is fee_amount and payable is payable_amount. A WeChat
// payment therefore keeps the fee that was actually charged at the time rather
// than one re-derived from today's rate, and a voided payment keeps its
// original amounts while its status carries the fact that it no longer counts.
//
// Money stays numeric throughout; only the outer select casts to float8 for
// JSON, well after every comparison has been made.
const baseCTE = `
with base as (
	select
		p.id,
		u.cn_code,
		coalesce(u.display_name, '') as display_name,
		p.submitted_amount as principal_amount,
		p.fee_amount,
		p.payable_amount,
		lower(coalesce(p.payment_method, '')) as payment_method,
		p.status,
		coalesce(p.paid_at, p.approved_at, p.submitted_at) as paid_at,
		coalesce(a.username, '') as created_by,
		coalesce(p.note, '') as note,
		(select count(*)::int from payment_items pi_count where pi_count.payment_id = p.id) as payment_item_count,
		p.created_at,
		p.voided_at,
		coalesce(voider.username, '') as voided_by,
		coalesce(p.void_reason, '') as void_reason
	from payments p
	join users u on u.id = p.user_id
	left join admins a on a.id = coalesce(p.created_by, p.approved_by)
	left join admins voider on voider.id = p.voided_by_admin_id
)`

// buildConditions renders the WHERE clause for the base view.
//
// skipColumn omits one column's own value filter. That is what makes a facet
// popover behave like WPS: while choosing CN values the candidate list must
// still reflect every *other* column's filter, but must not narrow itself to
// the CNs already ticked — otherwise unticking one could never bring it back.
// Pass an empty skipColumn for the list, count and export.
func buildConditions(filters PaymentFilters, skipColumn string, args *argList) string {
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
		{"b.principal_amount", filters.PrincipalMin, filters.PrincipalMax},
		{"b.fee_amount", filters.FeeMin, filters.FeeMax},
		{"b.payable_amount", filters.PayableMin, filters.PayableMax},
	}
	for _, r := range numericRanges {
		if r.min != "" {
			conditions = append(conditions, r.expr+" >= "+args.add(r.min)+"::numeric")
		}
		if r.max != "" {
			conditions = append(conditions, r.expr+" <= "+args.add(r.max)+"::numeric")
		}
	}

	if filters.PaidFrom != "" {
		conditions = append(conditions, "b.paid_at >= "+args.add(filters.PaidFrom)+"::timestamptz")
	}
	if filters.PaidTo != "" {
		conditions = append(conditions, "b.paid_at < "+args.add(filters.PaidTo)+"::timestamptz")
	}

	if filters.VoidedBlank {
		// "未撤销" is a real filter target, not an absence of one.
		conditions = append(conditions, "b.voided_at is null")
	}
	if filters.VoidedFrom != "" {
		conditions = append(conditions, "b.voided_at >= "+args.add(filters.VoidedFrom)+"::timestamptz")
	}
	if filters.VoidedTo != "" {
		conditions = append(conditions, "b.voided_at < "+args.add(filters.VoidedTo)+"::timestamptz")
	}

	return strings.Join(conditions, " and ")
}

const listColumns = `
	b.id::text,
	b.cn_code,
	b.display_name,
	b.principal_amount::float8,
	b.fee_amount::float8,
	b.payable_amount::float8,
	b.payment_method,
	b.status,
	to_char(b.paid_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	b.created_by,
	b.note,
	b.payment_item_count,
	to_char(b.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	coalesce(to_char(b.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	b.voided_by,
	b.void_reason`

const listOrder = `
order by b.paid_at desc, b.created_at desc, b.id desc`

func buildListQuery(filters PaymentFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	limit := args.add(filters.PageSize)
	offset := args.add((filters.Page - 1) * filters.PageSize)

	return baseCTE + `
select` + listColumns + `
from base b
where ` + where + listOrder + `
limit ` + limit + ` offset ` + offset, args.values
}

func buildCountQuery(filters PaymentFilters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select count(*)::int from base b where ` + where, args.values
}

// BuildExportQuery renders every payment matching the filters, honouring the
// caller's row cap but ignoring list pagination: an export follows the full
// filter result, never the page on screen.
func BuildExportQuery(filters PaymentFilters, maxRows int) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	limit := args.add(maxRows)
	return baseCTE + `
select` + listColumns + `
from base b
where ` + where + listOrder + `
limit ` + limit, args.values
}
