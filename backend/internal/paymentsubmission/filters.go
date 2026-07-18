package paymentsubmission

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var errInvalidTimestamp = errors.New("invalid timestamp")

// BadRequestError marks a caller-supplied parameter as invalid; the handler
// turns it into a 400 with the message. It never wraps a database error, so the
// message is always safe to return.
type BadRequestError struct {
	Message string
}

func (e *BadRequestError) Error() string { return e.Message }

func badRequest(format string, args ...any) error {
	return &BadRequestError{Message: fmt.Sprintf(format, args...)}
}

const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

const (
	DefaultFacetPageSize = 50
	MaxFacetPageSize     = 200
)

var (
	submissionStatuses = []string{StatusSubmitted, StatusApproved, StatusRejected}
	submissionMethods  = []string{MethodAlipay, MethodWechat}

	decimalPattern = regexp.MustCompile(`^\d{1,12}(\.\d{1,4})?$`)

	// chinaLocation anchors naive filter dates to Asia/Shanghai wall-clock time,
	// independent of the server process's OS timezone (mirrors payments).
	chinaLocation = time.FixedZone("CST", 8*60*60)
)

// Filters is the full filter state behind the admin list, the facet popovers and
// pagination. Value columns are sets (a WPS header popover ticks several values);
// blank cells are requested only via <column>_blank=1.
type Filters struct {
	CN            []string
	PaymentMethod []string
	Status        []string
	ReviewedBy    []string

	PrincipalMin string
	PrincipalMax string
	FeeMin       string
	FeeMax       string
	PayableMin   string
	PayableMax   string

	SubmittedFrom string
	SubmittedTo   string
	ReviewedFrom  string
	ReviewedTo    string
	// ReviewedBlank selects submissions that were never reviewed (待核对).
	ReviewedBlank bool

	Page     int
	PageSize int
}

// FiltersFromQuery reads the full filter state out of a URL query.
func FiltersFromQuery(query url.Values) (Filters, error) {
	filters := Filters{
		CN:            valueSet(query, "cn"),
		PaymentMethod: valueSet(query, "payment_method"),
		Status:        valueSet(query, "status"),
		ReviewedBy:    valueSet(query, "reviewed_by"),
	}

	for i, method := range filters.PaymentMethod {
		if method != "" {
			filters.PaymentMethod[i] = normalizeMethod(method)
		}
	}

	if err := requireAllowed("status", filters.Status, submissionStatuses); err != nil {
		return Filters{}, err
	}
	if err := requireAllowed("payment_method", filters.PaymentMethod, submissionMethods); err != nil {
		return Filters{}, err
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
			return Filters{}, err
		}
		*entry.field = value
	}
	for _, pair := range [][2]string{
		{"principal_min", "principal_max"},
		{"fee_min", "fee_max"},
		{"payable_min", "payable_max"},
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
		{"submitted_from", &filters.SubmittedFrom, false},
		{"submitted_to", &filters.SubmittedTo, true},
		{"reviewed_from", &filters.ReviewedFrom, false},
		{"reviewed_to", &filters.ReviewedTo, true},
	}
	for _, entry := range dates {
		value, err := timestampParam(query, entry.param, entry.exclusiveDayEnd)
		if err != nil {
			return Filters{}, err
		}
		*entry.field = value
	}
	for _, pair := range [][2]string{{"submitted_from", "submitted_to"}, {"reviewed_from", "reviewed_to"}} {
		if err := requireOrderedDates(query, pair[0], pair[1]); err != nil {
			return Filters{}, err
		}
	}

	filters.ReviewedBlank = blankFlag(query, "reviewed")

	page, pageSize, err := pagination(query)
	if err != nil {
		return Filters{}, err
	}
	filters.Page, filters.PageSize = page, pageSize

	return filters, nil
}

// normalizeMethod maps common aliases to the canonical alipay/wechat, matching
// the stored values.
func normalizeMethod(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "wechat", "wx", "weixin", "微信":
		return MethodWechat
	case "alipay", "zhifubao", "支付宝":
		return MethodAlipay
	default:
		return normalized
	}
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

// column maps an API filter name to its SQL expression over the base view. A
// name coming off the wire is only ever looked up here — never interpolated.
type column struct {
	name string
	expr string
}

var filterColumns = []column{
	{name: "cn", expr: "b.cn_code"},
	{name: "payment_method", expr: "b.payment_method"},
	{name: "status", expr: "b.status"},
	{name: "reviewed_by", expr: "b.reviewed_by"},
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
	case "payment_method":
		return f.PaymentMethod
	case "status":
		return f.Status
	case "reviewed_by":
		return f.ReviewedBy
	}
	return nil
}

// argList accumulates bind parameters and hands back the placeholder for each.
// Every user-supplied value goes through it; nothing is concatenated into SQL.
type argList struct {
	values []any
}

func (a *argList) add(value any) string {
	a.values = append(a.values, value)
	return "$" + strconv.Itoa(len(a.values))
}

// baseCTE is the single source of truth for what a submission row is: one row
// per payment_submissions record. The list, count, facet counts and detail all
// filter this same view. Image bytes are deliberately never selected here.
const baseCTE = `
with base as (
	select
		ps.id,
		ps.cn_code,
		coalesce(u.display_name, '') as display_name,
		lower(coalesce(ps.payment_method, '')) as payment_method,
		ps.principal_amount,
		ps.fee_amount,
		ps.payable_amount,
		ps.status,
		ps.submitted_at,
		ps.reviewed_at,
		coalesce(reviewer.username, '') as reviewed_by,
		coalesce(ps.reject_reason, '') as reject_reason,
		ps.mime_type,
		ps.byte_size,
		ps.sha256,
		coalesce(ps.original_filename_safe, '') as original_filename_safe,
		ps.user_id,
		ps.linked_payment_id
	from payment_submissions ps
	join users u on u.id = ps.user_id
	left join admins reviewer on reviewer.id = ps.reviewed_by_admin_id
)`

// buildConditions renders the WHERE clause. skipColumn omits one column's own
// value filter so a facet popover reflects every other column's filter without
// narrowing itself. Pass an empty skipColumn for the list, count and detail.
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

	if filters.SubmittedFrom != "" {
		conditions = append(conditions, "b.submitted_at >= "+args.add(filters.SubmittedFrom)+"::timestamptz")
	}
	if filters.SubmittedTo != "" {
		conditions = append(conditions, "b.submitted_at < "+args.add(filters.SubmittedTo)+"::timestamptz")
	}

	if filters.ReviewedBlank {
		conditions = append(conditions, "b.reviewed_at is null")
	}
	if filters.ReviewedFrom != "" {
		conditions = append(conditions, "b.reviewed_at >= "+args.add(filters.ReviewedFrom)+"::timestamptz")
	}
	if filters.ReviewedTo != "" {
		conditions = append(conditions, "b.reviewed_at < "+args.add(filters.ReviewedTo)+"::timestamptz")
	}

	return strings.Join(conditions, " and ")
}

const listColumns = `
	b.id::text,
	b.cn_code,
	b.display_name,
	b.payment_method,
	b.principal_amount::float8,
	b.fee_amount::float8,
	b.payable_amount::float8,
	b.status,
	to_char(b.submitted_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
	coalesce(to_char(b.reviewed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
	b.reviewed_by,
	b.reject_reason`

const listOrder = `
order by b.submitted_at desc, b.id desc`

func buildListQuery(filters Filters) (string, []any) {
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

func buildCountQuery(filters Filters) (string, []any) {
	args := &argList{}
	where := buildConditions(filters, "", args)
	return baseCTE + `
select count(*)::int from base b where ` + where, args.values
}
