package users

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUserNotFound = errors.New("user not found")

type Handler struct {
	store Store
}

type Store interface {
	ListUsers(context.Context, Filters) (ListResponse, error)
	GetUserDetail(context.Context, string) (DetailResponse, error)
	PreviewMerge(context.Context, string, string) (MergePreviewResponse, error)
	MergeUsers(context.Context, MergeRequest, string) (MergeResponse, error)
}

type Filters struct {
	CN     string
	Status string
	Limit  int
}

type ListItem struct {
	ID              string  `json:"id"`
	CNCode          string  `json:"cn_code"`
	DisplayName     string  `json:"display_name,omitempty"`
	HasQueryCode    bool    `json:"has_query_code"`
	Status          string  `json:"status"`
	OrderCount      int     `json:"order_count"`
	TotalAmount     float64 `json:"total_amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
	CreatedAt       string  `json:"created_at"`
}

type ListSummary struct {
	UserCount       int     `json:"user_count"`
	UsersWithOrders int     `json:"users_with_orders"`
	TotalAmount     float64 `json:"total_amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
}

type ListResponse struct {
	Items   []ListItem  `json:"items"`
	Summary ListSummary `json:"summary"`
}

type DetailOrder struct {
	ID              string            `json:"id"`
	OrderNo         string            `json:"order_no"`
	Status          string            `json:"status"`
	ProjectName     string            `json:"project_name"`
	ItemCount       int               `json:"item_count"`
	TotalAmount     float64           `json:"total_amount"`
	PaidAmount      float64           `json:"paid_amount"`
	RemainingAmount float64           `json:"remaining_amount"`
	CreatedAt       string            `json:"created_at"`
	Items           []DetailOrderItem `json:"items"`
}

type DetailOrderItem struct {
	ID              string  `json:"id"`
	ProductName     string  `json:"product_name"`
	ProductID       string  `json:"product_id,omitempty"`
	CharacterName   string  `json:"character_name,omitempty"`
	Category        string  `json:"category,omitempty"`
	SeriesCode      string  `json:"series_code,omitempty"`
	DisplayName     string  `json:"display_name,omitempty"`
	SKU             string  `json:"sku,omitempty"`
	Quantity        float64 `json:"quantity"`
	UnitPrice       float64 `json:"unit_price"`
	Amount          float64 `json:"amount"`
	PaidAmount      float64 `json:"paid_amount"`
	RemainingAmount float64 `json:"remaining_amount"`
	PaymentStatus   string  `json:"payment_status"`
	ImportFilename  string  `json:"import_filename,omitempty"`
	SourceSheet     string  `json:"source_sheet,omitempty"`
	SourceRowKey    string  `json:"source_row_key,omitempty"`
}

type DetailPayment struct {
	ID              string  `json:"id"`
	PrincipalAmount float64 `json:"principal_amount"`
	FeeAmount       float64 `json:"fee_amount"`
	TotalAmount     float64 `json:"total_amount"`
	PaymentMethod   string  `json:"payment_method,omitempty"`
	Status          string  `json:"status"`
	PaidAt          string  `json:"paid_at"`
	CreatedBy       string  `json:"created_by,omitempty"`
	VoidedAt        string  `json:"voided_at,omitempty"`
	VoidedBy        string  `json:"voided_by,omitempty"`
	VoidReason      string  `json:"void_reason,omitempty"`
}

type DetailResponse struct {
	User            ListItem        `json:"user"`
	Orders          []DetailOrder   `json:"orders"`
	Payments        []DetailPayment `json:"payments"`
	ImportFilenames []string        `json:"import_filenames"`
	Merges          []MergeLogEntry `json:"merges"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	query := r.URL.Query()
	limit, _ := strconv.Atoi(strings.TrimSpace(query.Get("limit")))
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	filters := Filters{
		CN:     strings.TrimSpace(query.Get("cn")),
		Status: strings.TrimSpace(query.Get("status")),
		Limit:  limit,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.ListUsers(ctx, filters)
	if err != nil {
		log.Printf("list users: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Detail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/users/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if !isUUIDLike(id) {
		writeError(w, http.StatusBadRequest, "user id is invalid")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.GetUserDetail(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		log.Printf("user detail: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const paidByItemCTE = `
	paid_by_item as (
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
			coalesce(sum(oi.amount), 0)::float8 as total_amount,
			coalesce(sum(least(coalesce(paid.paid_amount, 0), oi.amount)), 0)::float8 as paid_amount
		from orders o
		join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join paid_by_item paid on paid.order_item_id = oi.id
		where o.status <> 'cancelled'
		group by o.user_id
	)
`

func (s *PostgresStore) ListUsers(ctx context.Context, filters Filters) (ListResponse, error) {
	conditions := []string{"1 = 1"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}

	if filters.CN != "" {
		placeholder := addArg("%" + filters.CN + "%")
		conditions = append(conditions, "(u.cn_code ilike "+placeholder+" or coalesce(u.display_name, '') ilike "+placeholder+")")
	}
	if filters.Status != "" {
		placeholder := addArg(filters.Status)
		conditions = append(conditions, "u.status = "+placeholder)
	}
	limitPlaceholder := addArg(filters.Limit)

	rows, err := s.pool.Query(ctx, `
		with `+paidByItemCTE+`
		select
			u.id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			(coalesce(u.query_code_hash, '') <> ''),
			u.status,
			coalesce(t.order_count, 0),
			coalesce(t.total_amount, 0),
			coalesce(t.paid_amount, 0),
			greatest(coalesce(t.total_amount, 0) - coalesce(t.paid_amount, 0), 0),
			to_char(u.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from users u
		left join user_totals t on t.user_id = u.id
		where `+strings.Join(conditions, " and ")+`
		order by u.created_at desc, u.cn_code
		limit `+limitPlaceholder, args...)
	if err != nil {
		return ListResponse{}, err
	}
	defer rows.Close()

	response := ListResponse{Items: []ListItem{}}
	for rows.Next() {
		var item ListItem
		if err := rows.Scan(
			&item.ID,
			&item.CNCode,
			&item.DisplayName,
			&item.HasQueryCode,
			&item.Status,
			&item.OrderCount,
			&item.TotalAmount,
			&item.PaidAmount,
			&item.RemainingAmount,
			&item.CreatedAt,
		); err != nil {
			return ListResponse{}, err
		}
		item.TotalAmount = round2(item.TotalAmount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
		response.Summary.UserCount++
		if item.OrderCount > 0 {
			response.Summary.UsersWithOrders++
		}
		response.Summary.TotalAmount = round2(response.Summary.TotalAmount + item.TotalAmount)
		response.Summary.PaidAmount = round2(response.Summary.PaidAmount + item.PaidAmount)
		response.Summary.RemainingAmount = round2(response.Summary.RemainingAmount + item.RemainingAmount)
		response.Items = append(response.Items, item)
	}
	return response, rows.Err()
}

func (s *PostgresStore) GetUserDetail(ctx context.Context, userID string) (DetailResponse, error) {
	var detail DetailResponse
	err := s.pool.QueryRow(ctx, `
		with `+paidByItemCTE+`
		select
			u.id::text,
			u.cn_code,
			coalesce(u.display_name, ''),
			(coalesce(u.query_code_hash, '') <> ''),
			u.status,
			coalesce(t.order_count, 0),
			coalesce(t.total_amount, 0),
			coalesce(t.paid_amount, 0),
			greatest(coalesce(t.total_amount, 0) - coalesce(t.paid_amount, 0), 0),
			to_char(u.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from users u
		left join user_totals t on t.user_id = u.id
		where u.id = $1::uuid
	`, userID).Scan(
		&detail.User.ID,
		&detail.User.CNCode,
		&detail.User.DisplayName,
		&detail.User.HasQueryCode,
		&detail.User.Status,
		&detail.User.OrderCount,
		&detail.User.TotalAmount,
		&detail.User.PaidAmount,
		&detail.User.RemainingAmount,
		&detail.User.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DetailResponse{}, ErrUserNotFound
	}
	if err != nil {
		return DetailResponse{}, err
	}
	detail.User.TotalAmount = round2(detail.User.TotalAmount)
	detail.User.PaidAmount = round2(detail.User.PaidAmount)
	detail.User.RemainingAmount = round2(detail.User.RemainingAmount)

	orders, err := s.listOrdersForUser(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.Orders = orders

	payments, err := s.listPaymentsForUser(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.Payments = payments

	filenames, err := s.listImportFilenames(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.ImportFilenames = filenames

	merges, err := s.listMergeLogs(ctx, userID)
	if err != nil {
		return DetailResponse{}, err
	}
	detail.Merges = merges
	return detail, nil
}

func (s *PostgresStore) listOrdersForUser(ctx context.Context, userID string) ([]DetailOrder, error) {
	rows, err := s.pool.Query(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments p on p.id = pi.payment_id
			group by pi.order_item_id
		)
		select
			o.id::text,
			o.order_no,
			o.status,
			p.name,
			count(oi.id)::int,
			coalesce(sum(oi.amount), 0)::float8,
			coalesce(sum(least(coalesce(paid.paid_amount, 0), oi.amount)), 0)::float8,
			to_char(o.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from orders o
		join projects p on p.id = o.project_id
		left join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join paid_by_item paid on paid.order_item_id = oi.id
		where o.user_id = $1::uuid
		  and o.status <> 'cancelled'
		group by o.id, o.order_no, o.status, p.name, o.created_at
		order by o.created_at desc, o.id desc
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := []DetailOrder{}
	for rows.Next() {
		var order DetailOrder
		if err := rows.Scan(
			&order.ID,
			&order.OrderNo,
			&order.Status,
			&order.ProjectName,
			&order.ItemCount,
			&order.TotalAmount,
			&order.PaidAmount,
			&order.CreatedAt,
		); err != nil {
			return nil, err
		}
		order.TotalAmount = round2(order.TotalAmount)
		order.PaidAmount = round2(order.PaidAmount)
		order.RemainingAmount = round2(order.TotalAmount - order.PaidAmount)
		if order.RemainingAmount < 0 {
			order.RemainingAmount = 0
		}
		items, err := s.listOrderItemsForUserDetail(ctx, order.ID)
		if err != nil {
			return nil, err
		}
		order.Items = items
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

func (s *PostgresStore) listOrderItemsForUserDetail(ctx context.Context, orderID string) ([]DetailOrderItem, error) {
	rows, err := s.pool.Query(ctx, `
		with paid_by_item as (
			select
				pi.order_item_id,
				coalesce(sum(pi.applied_amount) filter (where p.status = 'approved'), 0) as paid_amount
			from payment_items pi
			join payments p on p.id = pi.payment_id
			group by pi.order_item_id
		)
		select
			oi.id::text,
			product.name,
			product.id::text,
			coalesce(product.character_name, ''),
			coalesce(product.category, ''),
			coalesce(product.series_code, ''),
			product.name,
			coalesce(product.sku, ''),
			oi.quantity::float8,
			oi.unit_price::float8,
			oi.amount::float8,
			least(coalesce(paid.paid_amount, 0), oi.amount)::float8,
			greatest(oi.amount - coalesce(paid.paid_amount, 0), 0)::float8,
			case
				when coalesce(paid.paid_amount, 0) <= 0 then 'unpaid'
				when coalesce(paid.paid_amount, 0) + 0.004 >= oi.amount then 'paid'
				else 'partial'
			end,
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, ''),
			coalesce(oi.source_row_key, '')
		from order_items oi
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		left join paid_by_item paid on paid.order_item_id = oi.id
		where oi.order_id = $1::uuid
		  and oi.revoked_at is null
		order by product.sort_order, product.name, product.character_name, oi.created_at, oi.id
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []DetailOrderItem{}
	for rows.Next() {
		var item DetailOrderItem
		if err := rows.Scan(
			&item.ID,
			&item.ProductName,
			&item.ProductID,
			&item.CharacterName,
			&item.Category,
			&item.SeriesCode,
			&item.DisplayName,
			&item.SKU,
			&item.Quantity,
			&item.UnitPrice,
			&item.Amount,
			&item.PaidAmount,
			&item.RemainingAmount,
			&item.PaymentStatus,
			&item.ImportFilename,
			&item.SourceSheet,
			&item.SourceRowKey,
		); err != nil {
			return nil, err
		}
		item.Amount = round2(item.Amount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *PostgresStore) listPaymentsForUser(ctx context.Context, userID string) ([]DetailPayment, error) {
	rows, err := s.pool.Query(ctx, `
		select
			p.id::text,
			p.submitted_amount::float8,
			p.fee_amount::float8,
			p.payable_amount::float8,
			coalesce(p.payment_method, ''),
			p.status,
			to_char(coalesce(p.paid_at, p.approved_at, p.submitted_at) at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(a.username, ''),
			coalesce(to_char(p.voided_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(voider.username, ''),
			coalesce(p.void_reason, '')
		from payments p
		left join admins a on a.id = coalesce(p.created_by, p.approved_by)
		left join admins voider on voider.id = p.voided_by_admin_id
		where p.user_id = $1::uuid
		order by coalesce(p.paid_at, p.approved_at, p.submitted_at) desc, p.created_at desc, p.id desc
		limit 200
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	payments := []DetailPayment{}
	for rows.Next() {
		var payment DetailPayment
		if err := rows.Scan(
			&payment.ID,
			&payment.PrincipalAmount,
			&payment.FeeAmount,
			&payment.TotalAmount,
			&payment.PaymentMethod,
			&payment.Status,
			&payment.PaidAt,
			&payment.CreatedBy,
			&payment.VoidedAt,
			&payment.VoidedBy,
			&payment.VoidReason,
		); err != nil {
			return nil, err
		}
		payment.PrincipalAmount = round2(payment.PrincipalAmount)
		payment.FeeAmount = round2(payment.FeeAmount)
		payment.TotalAmount = round2(payment.TotalAmount)
		payments = append(payments, payment)
	}
	return payments, rows.Err()
}

func (s *PostgresStore) listImportFilenames(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		select distinct ib.original_filename
		from order_items oi
		join orders o on o.id = oi.order_id
		join import_batches ib on ib.id = oi.import_batch_id
		where o.user_id = $1::uuid
		  and oi.revoked_at is null
		  and ib.original_filename is not null
		order by ib.original_filename
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	filenames := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		filenames = append(filenames, name)
	}
	return filenames, rows.Err()
}

func isUUIDLike(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
				return false
			}
		}
	}
	return true
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode users JSON response: %v", err)
	}
}
