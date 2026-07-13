package export

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pjsk/backend/internal/payments"
	"pjsk/backend/internal/users"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maxExportRows = 50000

type usersStore interface {
	ListUsers(context.Context, users.Filters) (users.ListResponse, error)
}

type paymentsStore interface {
	ListPaymentRecords(context.Context, payments.PaymentFilters) (payments.PaymentListResponse, error)
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
	response, ok := h.loadUsers(w, r)
	if !ok {
		return
	}
	writer := beginCSV(w, "users")
	_ = writer.Write([]string{"CN", "订单总金额", "有效已付总额", "剩余待付总额", "显示名称", "查询码状态", "用户状态", "订单数", "创建时间"})
	for _, item := range response.Items {
		queryCode := "未设置"
		if item.HasQueryCode {
			queryCode = "已设置"
		}
		_ = writer.Write([]string{item.CNCode, money(item.TotalAmount), money(item.PaidAmount), money(item.RemainingAmount), item.DisplayName, queryCode, userStatusLabel(item.Status), strconv.Itoa(item.OrderCount), formatDisplayTime(item.CreatedAt)})
	}
	writer.Flush()
}

func (h *Handler) UsersExcel(w http.ResponseWriter, r *http.Request) {
	response, ok := h.loadUsers(w, r)
	if !ok {
		return
	}
	columns := []excelColumn{{"CN", excelText, 12, 24, false}, {"订单总金额", excelNumber, 12, 16, false}, {"有效已付总额", excelNumber, 14, 18, false}, {"剩余待付总额", excelNumber, 14, 18, false}, {"显示名称", excelText, 14, 30, true}, {"查询码状态", excelText, 12, 16, false}, {"用户状态", excelText, 10, 16, false}, {"订单数", excelInteger, 8, 12, false}, {"创建时间", excelText, 20, 24, false}}
	rows := []excelRow{}
	for _, item := range response.Items {
		queryCode := "未设置"
		if item.HasQueryCode {
			queryCode = "已设置"
		}
		rows = append(rows, excelRow{textCell(item.CNCode), numberCell(item.TotalAmount), numberCell(item.PaidAmount), numberCell(item.RemainingAmount), textCell(item.DisplayName), textCell(queryCode), textCell(userStatusLabel(item.Status)), numberCell(float64(item.OrderCount)), textCell(formatDisplayTime(item.CreatedAt))})
	}
	if err := writeExcel(w, "users", columns, rows); err != nil {
		log.Printf("export users xlsx: %v", err)
	}
}

func (h *Handler) Payments(w http.ResponseWriter, r *http.Request) {
	response, ok := h.loadPayments(w, r)
	if !ok {
		return
	}
	writer := beginCSV(w, "payments")
	_ = writer.Write([]string{"CN", "实付金额", "交肾状态", "本金", "手续费", "付款方式", "付款时间", "显示名称", "备注", "关联明细数量", "操作管理员", "撤销时间", "撤销管理员", "撤销原因"})
	for _, item := range response.Items {
		_ = writer.Write([]string{item.CNCode, money(item.TotalAmount), paymentStatusLabel(item.Status), money(item.PrincipalAmount), money(item.FeeAmount), paymentMethodLabel(item.PaymentMethod), formatDisplayTime(item.PaidAt), item.DisplayName, item.Note, strconv.Itoa(item.PaymentItemCount), item.CreatedBy, formatDisplayTime(item.VoidedAt), item.VoidedBy, item.VoidReason})
	}
	writer.Flush()
}

func (h *Handler) PaymentsExcel(w http.ResponseWriter, r *http.Request) {
	response, ok := h.loadPayments(w, r)
	if !ok {
		return
	}
	columns := []excelColumn{{"CN", excelText, 12, 24, false}, {"实付金额", excelNumber, 10, 14, false}, {"交肾状态", excelText, 10, 14, false}, {"本金", excelNumber, 10, 14, false}, {"手续费", excelNumber, 10, 14, false}, {"付款方式", excelText, 10, 14, false}, {"付款时间", excelText, 20, 24, false}, {"显示名称", excelText, 14, 28, true}, {"备注", excelText, 16, 36, true}, {"关联明细数量", excelInteger, 12, 16, false}, {"操作管理员", excelText, 12, 18, false}, {"撤销时间", excelText, 20, 24, false}, {"撤销管理员", excelText, 12, 18, false}, {"撤销原因", excelText, 16, 36, true}}
	rows := []excelRow{}
	for _, item := range response.Items {
		rows = append(rows, excelRow{textCell(item.CNCode), numberCell(item.TotalAmount), textCell(paymentStatusLabel(item.Status)), numberCell(item.PrincipalAmount), numberCell(item.FeeAmount), textCell(paymentMethodLabel(item.PaymentMethod)), textCell(formatDisplayTime(item.PaidAt)), textCell(item.DisplayName), textCell(item.Note), numberCell(float64(item.PaymentItemCount)), textCell(item.CreatedBy), textCell(formatDisplayTime(item.VoidedAt)), textCell(item.VoidedBy), textCell(item.VoidReason)})
	}
	if err := writeExcel(w, "payments", columns, rows); err != nil {
		log.Printf("export payments xlsx: %v", err)
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
		log.Printf("export order items xlsx: %v", err)
	}
}

func (h *Handler) loadUsers(w http.ResponseWriter, r *http.Request) (users.ListResponse, bool) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return users.ListResponse{}, false
	}
	query := r.URL.Query()
	filters := users.Filters{CN: strings.TrimSpace(query.Get("cn")), Status: strings.TrimSpace(query.Get("status")), Limit: maxExportRows}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	response, err := h.users.ListUsers(ctx, filters)
	if err != nil {
		log.Printf("export users: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return users.ListResponse{}, false
	}
	return response, true
}

func (h *Handler) loadPayments(w http.ResponseWriter, r *http.Request) (payments.PaymentListResponse, bool) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return payments.PaymentListResponse{}, false
	}
	query := r.URL.Query()
	filters := payments.PaymentFilters{CN: strings.TrimSpace(query.Get("cn")), PaymentMethod: strings.TrimSpace(query.Get("payment_method")), Status: strings.TrimSpace(query.Get("status")), PaidFrom: strings.TrimSpace(query.Get("paid_from")), PaidTo: strings.TrimSpace(query.Get("paid_to")), Limit: maxExportRows}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	response, err := h.payments.ListPaymentRecords(ctx, filters)
	if err != nil {
		log.Printf("export payments: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return payments.PaymentListResponse{}, false
	}
	return response, true
}

func (h *Handler) loadOrderItemRows(w http.ResponseWriter, r *http.Request) ([]orderItemExportRow, bool) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return nil, false
	}
	query := r.URL.Query()
	cn := strings.TrimSpace(query.Get("cn"))
	project := strings.TrimSpace(query.Get("project"))
	series := strings.TrimSpace(query.Get("series"))
	category := strings.TrimSpace(query.Get("category"))
	role := strings.TrimSpace(query.Get("role"))
	paymentStatus := strings.TrimSpace(query.Get("payment_status"))
	unpaidOnly := isTruthy(query.Get("unpaid_only")) || paymentStatus == "unpaid"
	conditions := []string{"o.status <> 'cancelled'", "oi.revoked_at is null"}
	args := []any{}
	addArg := func(value any) string { args = append(args, value); return "$" + strconv.Itoa(len(args)) }
	if cn != "" {
		placeholder := addArg("%" + cn + "%")
		conditions = append(conditions, "(u.cn_code ilike "+placeholder+" or coalesce(u.display_name, '') ilike "+placeholder+")")
	}
	if project != "" {
		placeholder := addArg("%" + project + "%")
		conditions = append(conditions, "p.name ilike "+placeholder)
	}
	// Series (谷子系列), category (谷子种类/分类), and role (谷子角色) are
	// independent filters — an export request combining several narrows the
	// result with AND, keeping the exported rows consistent with whatever
	// the on-screen table filter shows.
	if series != "" {
		placeholder := addArg("%" + series + "%")
		conditions = append(conditions, "coalesce(product.series_code, '') ilike "+placeholder)
	}
	if category != "" {
		placeholder := addArg("%" + category + "%")
		conditions = append(conditions, "coalesce(product.category, '') ilike "+placeholder)
	}
	if role != "" {
		placeholder := addArg("%" + role + "%")
		conditions = append(conditions, "coalesce(product.character_name, '') ilike "+placeholder)
	}
	if unpaidOnly {
		conditions = append(conditions, "greatest(oi.amount - coalesce(paid.paid_amount, 0), 0) > 0.005")
	} else if paymentStatus != "" {
		placeholder := addArg(paymentStatus)
		conditions = append(conditions, `case when coalesce(paid.paid_amount, 0) <= 0 then 'unpaid' when coalesce(paid.paid_amount, 0) + 0.004 >= oi.amount then 'paid' else 'partial' end = `+placeholder)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.pool.Query(ctx, `
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
		where `+strings.Join(conditions, " and ")+`
		order by u.cn_code, o.order_no, product.sort_order, product.name
		limit `+strconv.Itoa(maxExportRows), args...)
	if err != nil {
		log.Printf("export order items: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	defer rows.Close()
	items := []orderItemExportRow{}
	for rows.Next() {
		var item orderItemExportRow
		if err := rows.Scan(&item.CNCode, &item.DisplayName, &item.OrderNo, &item.ProjectName, &item.GoodsName, &item.Character, &item.Category, &item.Quantity, &item.UnitPrice, &item.Amount, &item.Paid, &item.Remaining, &item.Status, &item.Filename, &item.SourceSheet, &item.SourceRowKey); err != nil {
			log.Printf("export order items scan: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return nil, false
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("export order items rows: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, false
	}
	return items, true
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
