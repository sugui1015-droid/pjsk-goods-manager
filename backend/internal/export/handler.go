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

// maxExportRows caps every export so a single request cannot dump an
// unbounded amount of data.
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

func NewHandler(usersStore usersStore, paymentsStore paymentsStore, pool *pgxpool.Pool) *Handler {
	return &Handler{users: usersStore, payments: paymentsStore, pool: pool}
}

func (h *Handler) Users(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	filters := users.Filters{
		CN:     strings.TrimSpace(query.Get("cn")),
		Status: strings.TrimSpace(query.Get("status")),
		Limit:  maxExportRows,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	response, err := h.users.ListUsers(ctx, filters)
	if err != nil {
		log.Printf("export users: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writer := beginCSV(w, "users")
	_ = writer.Write([]string{"CN", "显示名称", "查询码状态", "用户状态", "订单数", "订单总金额", "已付金额", "剩余金额", "创建时间"})
	for _, item := range response.Items {
		queryCode := "未设置"
		if item.HasQueryCode {
			queryCode = "已设置"
		}
		_ = writer.Write([]string{
			item.CNCode,
			item.DisplayName,
			queryCode,
			item.Status,
			strconv.Itoa(item.OrderCount),
			money(item.TotalAmount),
			money(item.PaidAmount),
			money(item.RemainingAmount),
			item.CreatedAt,
		})
	}
	writer.Flush()
}

func (h *Handler) Payments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	filters := payments.PaymentFilters{
		CN:            strings.TrimSpace(query.Get("cn")),
		PaymentMethod: strings.TrimSpace(query.Get("payment_method")),
		Status:        strings.TrimSpace(query.Get("status")),
		PaidFrom:      strings.TrimSpace(query.Get("paid_from")),
		PaidTo:        strings.TrimSpace(query.Get("paid_to")),
		Limit:         maxExportRows,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	response, err := h.payments.ListPaymentRecords(ctx, filters)
	if err != nil {
		log.Printf("export payments: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	writer := beginCSV(w, "payments")
	_ = writer.Write([]string{"付款时间", "CN", "显示名称", "本金", "手续费", "实付金额", "付款方式", "状态", "操作管理员", "备注", "关联明细数量", "撤销时间", "撤销管理员", "撤销原因"})
	for _, item := range response.Items {
		_ = writer.Write([]string{
			item.PaidAt,
			item.CNCode,
			item.DisplayName,
			money(item.PrincipalAmount),
			money(item.FeeAmount),
			money(item.TotalAmount),
			item.PaymentMethod,
			item.Status,
			item.CreatedBy,
			item.Note,
			strconv.Itoa(item.PaymentItemCount),
			item.VoidedAt,
			item.VoidedBy,
			item.VoidReason,
		})
	}
	writer.Flush()
}

func (h *Handler) OrderItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	cn := strings.TrimSpace(query.Get("cn"))
	project := strings.TrimSpace(query.Get("project"))
	paymentStatus := strings.TrimSpace(query.Get("payment_status"))

	conditions := []string{"o.status <> 'cancelled'", "oi.revoked_at is null"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if cn != "" {
		placeholder := addArg("%" + cn + "%")
		conditions = append(conditions, "(u.cn_code ilike "+placeholder+" or coalesce(u.display_name, '') ilike "+placeholder+")")
	}
	if project != "" {
		placeholder := addArg("%" + project + "%")
		conditions = append(conditions, "p.name ilike "+placeholder)
	}
	if paymentStatus != "" {
		placeholder := addArg(paymentStatus)
		conditions = append(conditions, "oi.payment_status = "+placeholder)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	rows, err := h.pool.Query(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where pay.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments pay on pay.id = pi.payment_id
			group by pi.order_item_id
		)
		select
			u.cn_code,
			coalesce(u.display_name, ''),
			o.order_no,
			p.name,
			case when coalesce(product.category, '') = '' or product.category = '默认分类' then product.name else product.name || '-' || product.category end,
			coalesce(product.character_name, ''),
			coalesce(product.category, ''),
			oi.quantity::float8,
			oi.unit_price::float8,
			oi.amount::float8,
			least(coalesce(paid.paid_amount, 0), oi.amount)::float8,
			greatest(oi.amount - coalesce(paid.paid_amount, 0), 0)::float8,
			oi.payment_status,
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, '')
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
		return
	}
	defer rows.Close()

	writer := beginCSV(w, "order-items")
	_ = writer.Write([]string{"CN", "显示名称", "订单号", "项目", "谷子名称", "角色", "分类", "数量", "单价", "小计", "已付", "剩余未付", "付款状态", "来源文件", "来源 Sheet"})
	for rows.Next() {
		var cnCode, displayName, orderNo, projectName, goodsName, character, category, status, filename, sheet string
		var quantity, unitPrice, amount, paid, remaining float64
		if err := rows.Scan(&cnCode, &displayName, &orderNo, &projectName, &goodsName, &character, &category, &quantity, &unitPrice, &amount, &paid, &remaining, &status, &filename, &sheet); err != nil {
			log.Printf("export order items scan: %v", err)
			return
		}
		_ = writer.Write([]string{
			cnCode,
			displayName,
			orderNo,
			projectName,
			goodsName,
			character,
			category,
			strconv.FormatFloat(quantity, 'f', -1, 64),
			money(unitPrice),
			money(amount),
			money(paid),
			money(remaining),
			status,
			filename,
			sheet,
		})
	}
	if err := rows.Err(); err != nil {
		log.Printf("export order items rows: %v", err)
	}
	writer.Flush()
}

// beginCSV sets download headers, writes a UTF-8 BOM so Excel decodes
// Chinese correctly, and returns a CSV writer over the response.
func beginCSV(w http.ResponseWriter, name string) *csv.Writer {
	filename := fmt.Sprintf("%s-%s.csv", name, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	return csv.NewWriter(w)
}

func money(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}
