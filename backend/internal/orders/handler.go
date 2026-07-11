package orders

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	store Store
}

type Store interface {
	ListOrders(context.Context, OrderFilters) (OrderListResponse, error)
	GetOrder(context.Context, string) (OrderDetailResponse, error)
}

type OrderFilters struct {
	CN            string
	Project       string
	ProjectID     string
	Item          string
	ImportBatchID string
	Status        string
	CreatedFrom   string
	CreatedTo     string
	Limit         int
}

type OrderSummary struct {
	ID              string   `json:"id"`
	OrderNo         string   `json:"order_no"`
	Status          string   `json:"status"`
	CNCode          string   `json:"cn_code"`
	DisplayName     string   `json:"display_name,omitempty"`
	ProjectID       string   `json:"project_id"`
	ProjectName     string   `json:"project_name"`
	ItemTypeCount   int      `json:"item_type_count"`
	ItemCount       int      `json:"item_count"`
	TotalQuantity   float64  `json:"total_quantity"`
	TotalAmount     float64  `json:"total_amount"`
	ImportBatchIDs  []string `json:"import_batch_ids"`
	ImportFilenames []string `json:"import_filenames"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

type OrderItem struct {
	ID             string  `json:"id"`
	ProductID      string  `json:"product_id"`
	ProductName    string  `json:"product_name"`
	CharacterName  string  `json:"character_name,omitempty"`
	Category       string  `json:"category,omitempty"`
	SeriesCode     string  `json:"series_code,omitempty"`
	DisplayName    string  `json:"display_name,omitempty"`
	SKU            string  `json:"sku,omitempty"`
	Quantity       float64 `json:"quantity"`
	UnitPrice      float64 `json:"unit_price"`
	Amount         float64 `json:"amount"`
	PaymentStatus  string  `json:"payment_status"`
	ImportBatchID  string  `json:"import_batch_id,omitempty"`
	ImportFilename string  `json:"import_filename,omitempty"`
	SourceSheet    string  `json:"source_sheet,omitempty"`
	SourceRowKey   string  `json:"source_row_key,omitempty"`
	CreatedAt      string  `json:"created_at"`
}

type OrderDetail struct {
	OrderSummary
	Items []OrderItem `json:"items"`
}

type OrderListResponse struct {
	Items []OrderSummary `json:"items"`
}

type OrderDetailResponse struct {
	Order OrderDetail `json:"order"`
}

type errorResponse struct {
	Error string `json:"error"`
}

var ErrOrderNotFound = errors.New("order not found")

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	filters := orderFiltersFromRequest(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response, err := h.store.ListOrders(ctx, filters)
	if err != nil {
		log.Printf("list admin orders: %v", err)
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

	orderID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/orders/"), "/")
	if orderID == "" || strings.Contains(orderID, "/") {
		writeError(w, http.StatusNotFound, "order not found")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			writeError(w, http.StatusNotFound, "order not found")
			return
		}
		log.Printf("get admin order detail: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func orderFiltersFromRequest(r *http.Request) OrderFilters {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(strings.TrimSpace(query.Get("limit")))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return OrderFilters{
		CN:            strings.TrimSpace(query.Get("cn")),
		Project:       strings.TrimSpace(query.Get("project")),
		ProjectID:     strings.TrimSpace(query.Get("project_id")),
		Item:          strings.TrimSpace(query.Get("item")),
		ImportBatchID: strings.TrimSpace(query.Get("import_batch_id")),
		Status:        strings.TrimSpace(query.Get("status")),
		CreatedFrom:   strings.TrimSpace(query.Get("created_from")),
		CreatedTo:     strings.TrimSpace(query.Get("created_to")),
		Limit:         limit,
	}
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) ListOrders(ctx context.Context, filters OrderFilters) (OrderListResponse, error) {
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
	if filters.ProjectID != "" {
		placeholder := addArg(filters.ProjectID)
		conditions = append(conditions, "p.id = "+placeholder+"::uuid")
	}
	if filters.Project != "" {
		placeholder := addArg("%" + filters.Project + "%")
		conditions = append(conditions, "(p.name ilike "+placeholder+" or p.code ilike "+placeholder+")")
	}
	if filters.Item != "" {
		placeholder := addArg("%" + filters.Item + "%")
		conditions = append(conditions, "exists (select 1 from order_items oi_filter join products pr_filter on pr_filter.id = oi_filter.product_id where oi_filter.order_id = o.id and oi_filter.revoked_at is null and (pr_filter.name ilike "+placeholder+" or coalesce(pr_filter.category, '') ilike "+placeholder+" or coalesce(pr_filter.series_code, '') ilike "+placeholder+" or coalesce(pr_filter.character_name, '') ilike "+placeholder+"))")
	}
	if filters.ImportBatchID != "" {
		placeholder := addArg(filters.ImportBatchID)
		conditions = append(conditions, "exists (select 1 from order_items oi_import where oi_import.order_id = o.id and oi_import.revoked_at is null and oi_import.import_batch_id = "+placeholder+"::uuid)")
	}
	if filters.Status != "" {
		placeholder := addArg(filters.Status)
		conditions = append(conditions, "o.status = "+placeholder)
	}
	if filters.CreatedFrom != "" {
		placeholder := addArg(filters.CreatedFrom)
		conditions = append(conditions, "o.created_at >= "+placeholder+"::timestamptz")
	}
	if filters.CreatedTo != "" {
		placeholder := addArg(filters.CreatedTo)
		conditions = append(conditions, "o.created_at < "+placeholder+"::timestamptz")
	}
	limitPlaceholder := addArg(filters.Limit)

	query := `
		select
			o.id::text,
			o.order_no,
			o.status,
			u.cn_code,
			coalesce(u.display_name, ''),
			p.id::text,
			p.name,
			count(distinct product.id)::int,
			count(oi.id)::int,
			coalesce(sum(oi.quantity), 0)::float8,
			o.total_amount::float8,
			coalesce(array_agg(distinct oi.import_batch_id::text) filter (where oi.import_batch_id is not null), array[]::text[]),
			coalesce(array_agg(distinct ib.original_filename) filter (where ib.original_filename is not null), array[]::text[]),
			to_char(o.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			to_char(o.updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from orders o
		join users u on u.id = o.user_id
		join projects p on p.id = o.project_id
		left join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		where ` + strings.Join(conditions, " and ") + `
		group by o.id, o.order_no, o.status, u.cn_code, u.display_name, p.id, p.name, o.total_amount, o.created_at, o.updated_at
		order by o.created_at desc, o.id desc
		limit ` + limitPlaceholder

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return OrderListResponse{}, err
	}
	defer rows.Close()

	response := OrderListResponse{Items: []OrderSummary{}}
	for rows.Next() {
		var item OrderSummary
		if err := rows.Scan(
			&item.ID,
			&item.OrderNo,
			&item.Status,
			&item.CNCode,
			&item.DisplayName,
			&item.ProjectID,
			&item.ProjectName,
			&item.ItemTypeCount,
			&item.ItemCount,
			&item.TotalQuantity,
			&item.TotalAmount,
			&item.ImportBatchIDs,
			&item.ImportFilenames,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return OrderListResponse{}, err
		}
		response.Items = append(response.Items, item)
	}
	if err := rows.Err(); err != nil {
		return OrderListResponse{}, err
	}
	return response, nil
}

func (s *PostgresStore) GetOrder(ctx context.Context, orderID string) (OrderDetailResponse, error) {
	row := s.pool.QueryRow(ctx, `
		select
			o.id::text,
			o.order_no,
			o.status,
			u.cn_code,
			coalesce(u.display_name, ''),
			p.id::text,
			p.name,
			count(distinct product.id)::int,
			count(oi.id)::int,
			coalesce(sum(oi.quantity), 0)::float8,
			o.total_amount::float8,
			coalesce(array_agg(distinct oi.import_batch_id::text) filter (where oi.import_batch_id is not null), array[]::text[]),
			coalesce(array_agg(distinct ib.original_filename) filter (where ib.original_filename is not null), array[]::text[]),
			to_char(o.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			to_char(o.updated_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from orders o
		join users u on u.id = o.user_id
		join projects p on p.id = o.project_id
		left join order_items oi on oi.order_id = o.id and oi.revoked_at is null
		left join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		where o.id = $1::uuid
		group by o.id, o.order_no, o.status, u.cn_code, u.display_name, p.id, p.name, o.total_amount, o.created_at, o.updated_at
	`, orderID)

	var summary OrderSummary
	if err := row.Scan(
		&summary.ID,
		&summary.OrderNo,
		&summary.Status,
		&summary.CNCode,
		&summary.DisplayName,
		&summary.ProjectID,
		&summary.ProjectName,
		&summary.ItemTypeCount,
		&summary.ItemCount,
		&summary.TotalQuantity,
		&summary.TotalAmount,
		&summary.ImportBatchIDs,
		&summary.ImportFilenames,
		&summary.CreatedAt,
		&summary.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrderDetailResponse{}, ErrOrderNotFound
		}
		return OrderDetailResponse{}, err
	}

	rows, err := s.pool.Query(ctx, `
		select
			oi.id::text,
			product.id::text,
			product.name,
			coalesce(product.character_name, ''),
			coalesce(product.category, ''),
			coalesce(product.series_code, ''),
			case when coalesce(product.category, '') = '' or product.category = '默认分类' then product.name else product.name || '-' || product.category end,
			coalesce(product.sku, ''),
			oi.quantity::float8,
			oi.unit_price::float8,
			oi.amount::float8,
			oi.payment_status,
			coalesce(oi.import_batch_id::text, ''),
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, ''),
			coalesce(oi.source_row_key, ''),
			to_char(oi.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from order_items oi
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		where oi.order_id = $1::uuid
		  and oi.revoked_at is null
		order by product.sort_order, product.name, oi.created_at, oi.id
	`, orderID)
	if err != nil {
		return OrderDetailResponse{}, err
	}
	defer rows.Close()

	detail := OrderDetail{OrderSummary: summary, Items: []OrderItem{}}
	for rows.Next() {
		var item OrderItem
		if err := rows.Scan(
			&item.ID,
			&item.ProductID,
			&item.ProductName,
			&item.CharacterName,
			&item.Category,
			&item.SeriesCode,
			&item.DisplayName,
			&item.SKU,
			&item.Quantity,
			&item.UnitPrice,
			&item.Amount,
			&item.PaymentStatus,
			&item.ImportBatchID,
			&item.ImportFilename,
			&item.SourceSheet,
			&item.SourceRowKey,
			&item.CreatedAt,
		); err != nil {
			return OrderDetailResponse{}, err
		}
		detail.Items = append(detail.Items, item)
	}
	if err := rows.Err(); err != nil {
		return OrderDetailResponse{}, err
	}
	return OrderDetailResponse{Order: detail}, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode admin orders JSON response: %v", err)
	}
}
