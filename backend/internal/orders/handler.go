package orders

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"pjsk/backend/internal/logsafe"
)

type Handler struct {
	store Store
}

type Store interface {
	ListOrders(context.Context, OrderFilters) (OrderListResponse, error)
	OrderFacets(context.Context, FacetRequest) (FacetResponse, error)
	GetOrder(context.Context, string) (OrderDetailResponse, error)
}

// OrderListItem is one row of the admin order table: a single goods line of a
// single CN's order — never a whole order.
//
// Each product column holds one value. The list deliberately has no
// item_names/series_codes/... arrays: an aggregated row cannot say which of its
// values matched a filter, which is exactly the confusion this shape removes.
// An item with quantity 3 is still one row whose Quantity is 3.
//
// OrderID exists so a row can link through to its order's detail page. It is a
// navigation key, not a column to display, and it is the only identifier here:
// no import batch id, no SKU, no file hash. Those stay in the detail page's
// collapsed technical section.
type OrderListItem struct {
	ItemID        string  `json:"item_id"`
	OrderID       string  `json:"order_id"`
	OrderNo       string  `json:"order_no"`
	Status        string  `json:"status"`
	PaymentStatus string  `json:"payment_status"`
	CNCode        string  `json:"cn_code"`
	DisplayName   string  `json:"display_name,omitempty"`
	ProjectName   string  `json:"project_name"`
	ItemName      string  `json:"item_name"`
	SeriesCode    string  `json:"series_code"`
	Category      string  `json:"category"`
	CharacterName string  `json:"character_name"`
	Quantity      float64 `json:"quantity"`
	UnitPrice     float64 `json:"unit_price"`
	TotalAmount   float64 `json:"total_amount"`
	PaidAmount    float64 `json:"paid_amount"`
	UnpaidAmount  float64 `json:"unpaid_amount"`
	CreatedAt     string  `json:"created_at"`
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
	ID              string  `json:"id"`
	ProductID       string  `json:"product_id"`
	ProductName     string  `json:"product_name"`
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
	ImportBatchID   string  `json:"import_batch_id,omitempty"`
	ImportFilename  string  `json:"import_filename,omitempty"`
	SourceSheet     string  `json:"source_sheet,omitempty"`
	SourceRowKey    string  `json:"source_row_key,omitempty"`
	CreatedAt       string  `json:"created_at"`
}

type OrderDetail struct {
	OrderSummary
	Items []OrderItem `json:"items"`
}

// OrderListResponse is one page of the filtered result set.
//
// Every count is in detail rows, not orders: Total is every goods line matching
// the filters, PageSize is goods lines per page. One order's items may
// therefore straddle a page boundary, which is the natural consequence of
// paging the same unit the table displays.
type OrderListResponse struct {
	Items      []OrderListItem `json:"items"`
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
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

	filters, err := FiltersFromQuery(r.URL.Query())
	if err != nil {
		writeFilterError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.ListOrders(ctx, filters)
	if err != nil {
		log.Printf("list admin orders: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// Facets serves the candidate values for one column's filter popover.
func (h *Handler) Facets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	request, err := FacetRequestFromQuery(r.URL.Query())
	if err != nil {
		writeFilterError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.OrderFacets(ctx, request)
	if err != nil {
		var badRequestErr *BadRequestError
		if errors.As(err, &badRequestErr) {
			writeError(w, http.StatusBadRequest, badRequestErr.Message)
			return
		}
		log.Printf("list admin order facets: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// writeFilterError reports a rejected parameter. Only BadRequestError messages
// reach the client; anything else is treated as internal so no database detail
// leaks through the filter surface.
func writeFilterError(w http.ResponseWriter, err error) {
	var badRequestErr *BadRequestError
	if errors.As(err, &badRequestErr) {
		writeError(w, http.StatusBadRequest, badRequestErr.Message)
		return
	}
	log.Printf("parse admin order filters: %s", logsafe.Category(err))
	writeError(w, http.StatusInternalServerError, "internal server error")
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
		log.Printf("get admin order detail: %s", logsafe.Category(err))
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

func (s *PostgresStore) ListOrders(ctx context.Context, filters OrderFilters) (OrderListResponse, error) {
	response := OrderListResponse{
		Items:    []OrderListItem{},
		Page:     filters.Page,
		PageSize: filters.PageSize,
	}

	countQuery, countArgs := buildCountQuery(filters)
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&response.Total); err != nil {
		return OrderListResponse{}, err
	}
	response.TotalPages = (response.Total + filters.PageSize - 1) / filters.PageSize

	listQuery, listArgs := buildListQuery(filters)
	rows, err := s.pool.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return OrderListResponse{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var item OrderListItem
		if err := rows.Scan(
			&item.ItemID,
			&item.OrderID,
			&item.OrderNo,
			&item.Status,
			&item.PaymentStatus,
			&item.CNCode,
			&item.DisplayName,
			&item.ProjectName,
			&item.ItemName,
			&item.SeriesCode,
			&item.Category,
			&item.CharacterName,
			&item.Quantity,
			&item.UnitPrice,
			&item.TotalAmount,
			&item.PaidAmount,
			&item.UnpaidAmount,
			&item.CreatedAt,
		); err != nil {
			return OrderListResponse{}, err
		}
		item.UnitPrice = round2(item.UnitPrice)
		item.TotalAmount = round2(item.TotalAmount)
		item.PaidAmount = round2(item.PaidAmount)
		item.UnpaidAmount = round2(item.UnpaidAmount)
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
			product.id::text,
			product.name,
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
			coalesce(oi.import_batch_id::text, ''),
			coalesce(ib.original_filename, ''),
			coalesce(oi.source_sheet, ''),
			coalesce(oi.source_row_key, ''),
			to_char(oi.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		from order_items oi
		join products product on product.id = oi.product_id
		left join import_batches ib on ib.id = oi.import_batch_id
		left join paid_by_item paid on paid.order_item_id = oi.id
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
			&item.PaidAmount,
			&item.RemainingAmount,
			&item.PaymentStatus,
			&item.ImportBatchID,
			&item.ImportFilename,
			&item.SourceSheet,
			&item.SourceRowKey,
			&item.CreatedAt,
		); err != nil {
			return OrderDetailResponse{}, err
		}
		item.Amount = round2(item.Amount)
		item.PaidAmount = round2(item.PaidAmount)
		item.RemainingAmount = round2(item.RemainingAmount)
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

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode admin orders JSON response: %v", err)
	}
}
