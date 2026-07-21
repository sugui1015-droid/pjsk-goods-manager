package export

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/orders"
	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/users"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxExportRows = 50000

type usersStore interface {
	ExportUsers(context.Context, users.Filters, int) ([]users.ListItem, error)
	BulkCreateQueryCodeBindTokens(context.Context, users.Filters, string) (users.BulkBindTokenResult, error)
}

type paymentsStore interface {
	ExportPayments(context.Context, payments.PaymentFilters, int) ([]payments.PaymentListItem, error)
}

type Handler struct {
	users    usersStore
	payments paymentsStore
	pool     *pgxpool.Pool
}

type orderItemExportRow struct {
	CNCode       string
	DisplayName  string
	OrderNo      string
	ProjectName  string
	GoodsName    string
	Character    string
	Category     string
	Quantity     float64
	UnitPrice    float64
	Amount       float64
	Paid         float64
	Remaining    float64
	Status       string
	Filename     string
	SourceSheet  string
	SourceRowKey string
}

func NewHandler(usersStore usersStore, paymentsStore paymentsStore, pool *pgxpool.Pool) *Handler {
	return &Handler{users: usersStore, payments: paymentsStore, pool: pool}
}

func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	items, ok := h.loadUsers(w, r)
	if !ok {
		return
	}
	writer := beginCSV(w, "users")
	_ = writer.Write([]string{"CN", "订单总金额", "有效已付总额", "剩余待付总额", "显示名称", "查询码状态", "用户状态", "订单数", "创建时间"})
	for _, item := range items {
		queryCode := "未设置"
		if item.HasQueryCode {
			queryCode = "已设置"
		}
		_ = writer.Write([]string{item.CNCode, money(item.TotalAmount), money(item.PaidAmount), money(item.RemainingAmount), item.DisplayName, queryCode, userStatusLabel(item.Status), strconv.Itoa(item.OrderCount), formatDisplayTime(item.CreatedAt)})
	}
	writer.Flush()
}

func (h *Handler) UsersExcel(w http.ResponseWriter, r *http.Request) {
	items, ok := h.loadUsers(w, r)
	if !ok {
		return
	}
	columns := []excelColumn{{"CN", excelText, 12, 24, false}, {"订单总金额", excelNumber, 12, 16, false}, {"有效已付总额", excelNumber, 14, 18, false}, {"剩余待付总额", excelNumber, 14, 18, false}, {"显示名称", excelText, 14, 30, true}, {"查询码状态", excelText, 12, 16, false}, {"用户状态", excelText, 10, 16, false}, {"订单数", excelInteger, 8, 12, false}, {"创建时间", excelText, 20, 24, false}}
	rows := []excelRow{}
	for _, item := range items {
		queryCode := "未设置"
		if item.HasQueryCode {
			queryCode = "已设置"
		}
		rows = append(rows, excelRow{textCell(item.CNCode), numberCell(item.TotalAmount), numberCell(item.PaidAmount), numberCell(item.RemainingAmount), textCell(item.DisplayName), textCell(queryCode), textCell(userStatusLabel(item.Status)), numberCell(float64(item.OrderCount)), textCell(formatDisplayTime(item.CreatedAt))})
	}
	if err := writeExcel(w, "users", columns, rows); err != nil {
		log.Printf("export users xlsx: %s", logsafe.Category(err))
	}
}

func (h *Handler) Payments(w http.ResponseWriter, r *http.Request) {
	items, ok := h.loadPayments(w, r)
	if !ok {
		return
	}
	writer := beginCSV(w, "payments")
	_ = writer.Write([]string{"CN", "实付金额", "交肾状态", "本金", "手续费", "付款方式", "付款时间", "显示名称", "备注", "关联明细数量", "操作管理员", "撤销时间", "撤销管理员", "撤销原因"})
	for _, item := range items {
		_ = writer.Write([]string{item.CNCode, money(item.TotalAmount), paymentStatusLabel(item.Status), money(item.PrincipalAmount), money(item.FeeAmount), paymentMethodLabel(item.PaymentMethod), formatDisplayTime(item.PaidAt), item.DisplayName, item.Note, strconv.Itoa(item.PaymentItemCount), item.CreatedBy, formatDisplayTime(item.VoidedAt), item.VoidedBy, item.VoidReason})
	}
	writer.Flush()
}

func (h *Handler) PaymentsExcel(w http.ResponseWriter, r *http.Request) {
	items, ok := h.loadPayments(w, r)
	if !ok {
		return
	}
	columns := []excelColumn{{"CN", excelText, 12, 24, false}, {"实付金额", excelNumber, 10, 14, false}, {"交肾状态", excelText, 10, 14, false}, {"本金", excelNumber, 10, 14, false}, {"手续费", excelNumber, 10, 14, false}, {"付款方式", excelText, 10, 14, false}, {"付款时间", excelText, 20, 24, false}, {"显示名称", excelText, 14, 28, true}, {"备注", excelText, 16, 36, true}, {"关联明细数量", excelInteger, 12, 16, false}, {"操作管理员", excelText, 12, 18, false}, {"撤销时间", excelText, 20, 24, false}, {"撤销管理员", excelText, 12, 18, false}, {"撤销原因", excelText, 16, 36, true}}
	rows := []excelRow{}
	for _, item := range items {
		rows = append(rows, excelRow{textCell(item.CNCode), numberCell(item.TotalAmount), textCell(paymentStatusLabel(item.Status)), numberCell(item.PrincipalAmount), numberCell(item.FeeAmount), textCell(paymentMethodLabel(item.PaymentMethod)), textCell(formatDisplayTime(item.PaidAt)), textCell(item.DisplayName), textCell(item.Note), numberCell(float64(item.PaymentItemCount)), textCell(item.CreatedBy), textCell(formatDisplayTime(item.VoidedAt)), textCell(item.VoidedBy), textCell(item.VoidReason)})
	}
	if err := writeExcel(w, "payments", columns, rows); err != nil {
		log.Printf("export payments xlsx: %s", logsafe.Category(err))
	}
}

func (h *Handler) OrderItems(w http.ResponseWriter, r *http.Request) {
	rows, ok := h.loadOrderItemRows(w, r)
	if !ok {
		return
	}
	writer := beginCSV(w, "order-items")
	_ = writer.Write(orderItemHeaders())
	for _, row := range rows {
		_ = writer.Write([]string{row.CNCode, money(row.Paid), money(row.Amount), money(row.Remaining), itemPaymentStatusLabel(row.Status), row.GoodsName, row.Character, row.Category, strconv.FormatFloat(row.Quantity, 'f', -1, 64), money(row.UnitPrice), row.DisplayName, row.ProjectName, row.OrderNo, row.Filename, row.SourceSheet, row.SourceRowKey})
	}
	writer.Flush()
}

func (h *Handler) OrderItemsExcel(w http.ResponseWriter, r *http.Request) {
	items, ok := h.loadOrderItemRows(w, r)
	if !ok {
		return
	}
	columns := []excelColumn{{"CN", excelText, 12, 24, false}, {"已付", excelNumber, 10, 12, false}, {"小计", excelNumber, 10, 12, false}, {"剩余", excelNumber, 10, 12, false}, {"付款状态", excelText, 10, 14, false}, {"谷子名称", excelText, 20, 36, true}, {"角色", excelText, 10, 18, false}, {"分类", excelText, 10, 18, false}, {"数量", excelInteger, 8, 10, false}, {"单价", excelNumber, 10, 12, false}, {"显示名称", excelText, 14, 28, true}, {"项目", excelText, 16, 30, true}, {"订单号", excelText, 14, 28, true}, {"来源文件", excelText, 18, 34, true}, {"来源 Sheet", excelText, 12, 18, false}, {"来源位置", excelText, 14, 28, true}}
	rows := []excelRow{}
	for _, row := range items {
		rows = append(rows, excelRow{textCell(row.CNCode), numberCell(row.Paid), numberCell(row.Amount), numberCell(row.Remaining), textCell(itemPaymentStatusLabel(row.Status)), textCell(row.GoodsName), textCell(row.Character), textCell(row.Category), numberCell(row.Quantity), numberCell(row.UnitPrice), textCell(row.DisplayName), textCell(row.ProjectName), textCell(row.OrderNo), textCell(row.Filename), textCell(row.SourceSheet), textCell(row.SourceRowKey)})
	}
	if err := writeExcel(w, "order-items", columns, rows); err != nil {
		log.Printf("export order items xlsx: %s", logsafe.Category(err))
	}
}

// loadUsers reuses the user list's filter parser, so the same parameters select
// the same users here as on screen — including the multi-value column filters,
// ranges and date bounds. List pagination is dropped: an export follows the
// whole filter result, capped only by maxExportRows.
func (h *Handler) loadUsers(w http.ResponseWriter, r *http.Request) ([]users.ListItem, bool) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return nil, false
	}

	filters, ok := parseUserFilters(w, r)
	if !ok {
		return nil, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	items, err := h.users.ExportUsers(ctx, filters, maxExportRows)
	if err != nil {
		log.Printf("export users: %s", logsafe.Category(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	return items, true
}

// parseUserFilters turns the request's query string into user list filters,
// dropping only pagination. Both the plain user export and the bind-code batch
// go through it, so "what the admin sees on screen" and "what the batch acts
// on" cannot drift apart.
func parseUserFilters(w http.ResponseWriter, r *http.Request) (users.Filters, bool) {
	filterQuery := url.Values{}
	for key, values := range r.URL.Query() {
		switch key {
		case "page", "page_size":
			continue
		}
		filterQuery[key] = values
	}
	filters, err := users.FiltersFromQuery(filterQuery)
	if err != nil {
		var badRequest *users.BadRequestError
		if errors.As(err, &badRequest) {
			http.Error(w, badRequest.Message, http.StatusBadRequest)
			return users.Filters{}, false
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return users.Filters{}, false
	}
	return filters, true
}

// BindTokensExcel issues a fresh bind code to every eligible user in the
// current filter result and streams them back as a spreadsheet.
//
// This is the one export that mutates, which is why it is POST and why the
// router puts it behind a fresh re-authentication. The plaintext codes exist
// only in this response body: nothing is written to disk on the server, and
// no code is logged — only the batch size is.
func (h *Handler) BindTokensExcel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	filters, ok := parseUserFilters(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	result, err := h.users.BulkCreateQueryCodeBindTokens(ctx, filters, account.ID)
	if errors.Is(err, users.ErrBulkBindTokenTooMany) {
		http.Error(w, "筛选结果超过单批上限，请缩小筛选范围后重试。", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		log.Printf("bulk bind tokens: %s", logsafe.Category(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if result.Requested == 0 {
		http.Error(w, "当前筛选结果中没有可生成绑定码的用户。", http.StatusUnprocessableEntity)
		return
	}
	// Counts only — never a code. The skip count is logged too, so a shortfall
	// is visible in the server log as well as in the download.
	log.Printf(
		"bulk bind tokens: admin=%s requested=%d issued=%d skipped=%d",
		account.ID, result.Requested, len(result.Issued), len(result.Skipped),
	)

	// The batch outcome rides on the response as headers as well as in the
	// file, so the caller can report "issued N of M, skipped K" without having
	// to parse the spreadsheet it just downloaded.
	w.Header().Set("X-Bind-Token-Requested", strconv.Itoa(result.Requested))
	w.Header().Set("X-Bind-Token-Issued", strconv.Itoa(len(result.Issued)))
	w.Header().Set("X-Bind-Token-Skipped", strconv.Itoa(len(result.Skipped)))
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition, X-Bind-Token-Requested, X-Bind-Token-Issued, X-Bind-Token-Skipped")

	columns := []excelColumn{
		{"CN", excelText, 14, 28, false},
		{"显示名称", excelText, 14, 30, true},
		{"结果", excelText, 10, 14, false},
		{"绑定码", excelText, 14, 20, false},
		{"过期时间", excelText, 20, 24, false},
		{"备注", excelText, 18, 36, true},
	}
	// Skipped users are listed in the same sheet rather than omitted: a file
	// with silently fewer rows than the confirmed preview is exactly the
	// failure mode this column set exists to prevent.
	rows := []excelRow{}
	for _, item := range result.Issued {
		note := ""
		if item.ReplacedUnused {
			note = "该用户此前未使用的绑定码已失效"
		}
		rows = append(rows, excelRow{
			textCell(item.CNCode),
			textCell(item.DisplayName),
			textCell("已生成"),
			textCell(item.BindToken),
			textCell(formatDisplayTime(item.ExpiresAt)),
			textCell(note),
		})
	}
	for _, item := range result.Skipped {
		rows = append(rows, excelRow{
			textCell(item.CNCode),
			textCell(item.DisplayName),
			textCell("已跳过"),
			textCell(""),
			textCell(""),
			textCell(item.Reason),
		})
	}
	if err := writeExcel(w, "bind-tokens", columns, rows); err != nil {
		log.Printf("export bind tokens xlsx: %s", logsafe.Category(err))
	}
}

// loadPayments reuses the payment list's filter parser, so the same parameters
// select the same payments here as on screen.
//
// It previously read only cn/payment_method/status/paid_from/paid_to and
// dropped the principal, fee and payable ranges entirely — so filtering the
// table by amount and then exporting produced more rows than the page showed.
// Going through FiltersFromQuery closes that gap. List pagination is dropped:
// an export follows the whole filter result, capped only by maxExportRows.
func (h *Handler) loadPayments(w http.ResponseWriter, r *http.Request) ([]payments.PaymentListItem, bool) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return nil, false
	}

	filterQuery := url.Values{}
	for key, values := range r.URL.Query() {
		switch key {
		case "page", "page_size":
			continue
		}
		filterQuery[key] = values
	}
	filters, err := payments.FiltersFromQuery(filterQuery)
	if err != nil {
		var badRequest *payments.BadRequestError
		if errors.As(err, &badRequest) {
			http.Error(w, badRequest.Message, http.StatusBadRequest)
			return nil, false
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	items, err := h.payments.ExportPayments(ctx, filters, maxExportRows)
	if err != nil {
		log.Printf("export payments: %s", logsafe.Category(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	return items, true
}

func (h *Handler) loadOrderItemRows(w http.ResponseWriter, r *http.Request) ([]orderItemExportRow, bool) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return nil, false
	}
	filters, unpaidOnly, err := parseOrderItemExportFilters(r.URL.Query())
	if err != nil {
		var badRequest *orders.BadRequestError
		if errors.As(err, &badRequest) {
			http.Error(w, badRequest.Message, http.StatusBadRequest)
			return nil, false
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}

	query, args := buildOrderItemExportQuery(filters, unpaidOnly)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		log.Printf("export order items: %s", logsafe.Category(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	defer rows.Close()
	items := []orderItemExportRow{}
	for rows.Next() {
		var item orderItemExportRow
		if err := rows.Scan(&item.CNCode, &item.DisplayName, &item.OrderNo, &item.ProjectName, &item.GoodsName, &item.Character, &item.Category, &item.Quantity, &item.UnitPrice, &item.Amount, &item.Paid, &item.Remaining, &item.Status, &item.Filename, &item.SourceSheet, &item.SourceRowKey); err != nil {
			log.Printf("export order items scan: %s", logsafe.Category(err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return nil, false
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("export order items rows: %s", logsafe.Category(err))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	return items, true
}

// parseOrderItemExportFilters deliberately drops list pagination. Every other
// order filter goes through the list parser unchanged, including repeated
// values, ranges, dates and dedicated blank flags.
func parseOrderItemExportFilters(query url.Values) (orders.OrderFilters, bool, error) {
	unpaidOnly := isTruthy(query.Get("unpaid_only"))
	filterQuery := url.Values{}
	for key, values := range query {
		switch key {
		case "page", "page_size", "unpaid_only":
			continue
		}
		filterQuery[key] = values
	}
	filters, err := orders.FiltersFromQuery(filterQuery)
	return filters, unpaidOnly, err
}

// buildOrderItemExportQuery selects exactly the detail rows the filtered list
// shows. There is intentionally no LIMIT/OFFSET: exports follow the full filter
// result, never just the visible list page or a silent cap.
//
// The restriction is on order *item* ids, not order ids. Restricting by order
// would re-inflate the download to every sibling item of a matching order, so
// filtering 角色=初音未来 would export 宁宁 and 真冬 rows the table never showed.
// Item ids keep the file row-for-row identical to the list.
func buildOrderItemExportQuery(filters orders.OrderFilters, unpaidOnly bool) (string, []any) {
	itemIDsSQL, args := orders.BuildExportItemIDsQuery(filters)
	conditions := []string{
		"oi.revoked_at is null",
		"oi.id in (" + itemIDsSQL + ")",
	}
	if unpaidOnly {
		// The unpaid-detail export (payments page) narrows further to rows that
		// still owe money, and drops cancelled orders: nobody should be chased
		// for payment on a cancelled order. The order list's own export leaves
		// cancelled rows in, so its row count matches the table exactly.
		conditions = append(conditions,
			"o.status <> 'cancelled'",
			"greatest(oi.amount - coalesce(paid.paid_amount, 0), 0) > 0.005",
		)
	}
	query := `
		with paid_by_item as (
			select pi.order_item_id, coalesce(sum(pi.applied_amount) filter (where pay.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments pay on pay.id = pi.payment_id
			group by pi.order_item_id
		)
		select u.cn_code, coalesce(u.display_name, ''), o.order_no, p.name, product.name, coalesce(product.character_name, ''), coalesce(product.category, ''), oi.quantity::float8, oi.unit_price::float8, oi.amount::float8, least(coalesce(paid.paid_amount, 0), oi.amount)::float8, greatest(oi.amount - coalesce(paid.paid_amount, 0), 0)::float8,
		case when coalesce(paid.paid_amount, 0) <= 0 then 'unpaid' when coalesce(paid.paid_amount, 0) + 0.004 >= oi.amount then 'paid' else 'partial' end,
		coalesce(ib.original_filename, ''), coalesce(oi.source_sheet, ''), coalesce(oi.source_row_key, '')
		from order_items oi
		join orders o on o.id = oi.order_id
		join users u on u.id = o.user_id
		join projects p on p.id = o.project_id
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		left join paid_by_item paid on paid.order_item_id = oi.id
		where ` + strings.Join(conditions, " and ") + `
		order by u.cn_code, o.order_no, product.sort_order, product.name`
	return query, args
}

func orderItemHeaders() []string {
	return []string{"CN", "已付", "小计", "剩余", "付款状态", "谷子名称", "角色", "分类", "数量", "单价", "显示名称", "项目", "订单号", "来源文件", "来源 Sheet", "来源位置"}
}

func beginCSV(w http.ResponseWriter, name string) *csv.Writer {
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	return csv.NewWriter(w)
}

func money(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) }

func isTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
