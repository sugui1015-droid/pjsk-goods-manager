package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/admin"
)

type fakeStore struct {
	getCNPayment       func(context.Context, string) (CNPaymentResponse, error)
	createPayment      func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error)
	listPaymentRecords func(context.Context, PaymentFilters) (PaymentListResponse, error)
	getPaymentDetail   func(context.Context, string) (PaymentDetailResponse, error)
}

func (s fakeStore) GetCNPayment(ctx context.Context, cn string) (CNPaymentResponse, error) {
	if s.getCNPayment == nil {
		return CNPaymentResponse{}, nil
	}
	return s.getCNPayment(ctx, cn)
}

func (s fakeStore) CreatePayment(ctx context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
	if s.createPayment == nil {
		return CreatePaymentResponse{}, nil
	}
	return s.createPayment(ctx, request, adminID)
}

func (s fakeStore) ListPaymentRecords(ctx context.Context, filters PaymentFilters) (PaymentListResponse, error) {
	if s.listPaymentRecords == nil {
		return PaymentListResponse{}, nil
	}
	return s.listPaymentRecords(ctx, filters)
}

func (s fakeStore) GetPaymentDetail(ctx context.Context, paymentID string) (PaymentDetailResponse, error) {
	if s.getPaymentDetail == nil {
		return PaymentDetailResponse{}, nil
	}
	return s.getPaymentDetail(ctx, paymentID)
}

func TestCNRequiresCN(t *testing.T) {
	handler := NewHandler(fakeStore{
		getCNPayment: func(_ context.Context, cn string) (CNPaymentResponse, error) {
			if cn != "" {
				t.Fatalf("cn = %q, want empty", cn)
			}
			return CNPaymentResponse{}, ErrCNRequired
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/payments/cn", nil)
	response := httptest.NewRecorder()

	handler.CN(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestCreateRequiresAdmin(t *testing.T) {
	handler := NewHandler(fakeStore{})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/payments", bytes.NewBufferString(`{}`))
	response := httptest.NewRecorder()

	handler.Create(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestCreatePassesAdminAndReturnsDuplicate(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(_ context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
			if adminID != "admin-1" {
				t.Fatalf("adminID = %q, want admin-1", adminID)
			}
			if request.CN != "CN001" || request.IdempotencyKey != "key-1" {
				t.Fatalf("unexpected request: %#v", request)
			}
			return CreatePaymentResponse{
				PaymentID: "payment-1",
				Status:    "approved",
				Duplicate: true,
			}, nil
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","payment_method":"bank","paid_at":"2026-07-12T12:30:00Z","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload CreatePaymentResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Duplicate || payload.PaymentID != "payment-1" {
		t.Fatalf("payload = %#v, want duplicate payment-1", payload)
	}
}

func TestCreateMapsValidationErrors(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrOverPayment
		},
	})
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":99}]}`))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestCreateRejectsEmptyIdempotencyKey(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrIdempotencyKey
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrIdempotencyKey.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrIdempotencyKey.Error())
	}
}

func TestCreateRejectsInvalidPaidAt(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrPaymentTime
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN001","paid_at":"not-a-valid-time","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrPaymentTime.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrPaymentTime.Error())
	}
}

func TestCreateRejectsUnknownCN(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, ErrUserNotFound
		},
	})
	body := bytes.NewBufferString(`{"cn":"CN999","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":10}]}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", body)
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	var payload errorResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error != ErrUserNotFound.Error() {
		t.Fatalf("error = %q, want %q", payload.Error, ErrUserNotFound.Error())
	}
}

func TestCreateMapsUnknownErrors(t *testing.T) {
	handler := NewHandler(fakeStore{
		createPayment: func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error) {
			return CreatePaymentResponse{}, errors.New("database down")
		},
	})
	request := authenticatedRequest(http.MethodPost, "/api/admin/payments", bytes.NewBufferString(`{"cn":"CN001","idempotency_key":"key-1","items":[{"order_item_id":"item-1","amount":1}]}`))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Create).ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestListPassesFilters(t *testing.T) {
	handler := NewHandler(fakeStore{
		listPaymentRecords: func(_ context.Context, filters PaymentFilters) (PaymentListResponse, error) {
			if filters.CN != "CN001" || filters.PaymentMethod != "Alipay" || filters.Status != "approved" || filters.PaidFrom != "2026-07-12T10:00" || filters.PaidTo != "2026-07-13T10:00" {
				t.Fatalf("filters = %#v", filters)
			}
			if filters.Limit != 100 {
				t.Fatalf("limit = %d, want 100", filters.Limit)
			}
			return PaymentListResponse{Items: []PaymentListItem{{ID: "payment-1", CNCode: "CN001", Amount: 10, PaymentItemCount: 2}}}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments?cn=CN001&payment_method=Alipay&status=approved&paid_from=2026-07-12T10:00&paid_to=2026-07-13T10:00", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.List).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var payload PaymentListResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].ID != "payment-1" {
		t.Fatalf("payload = %#v, want payment-1", payload)
	}
}

func TestListRejectsInvalidTime(t *testing.T) {
	handler := NewHandler(fakeStore{
		listPaymentRecords: func(context.Context, PaymentFilters) (PaymentListResponse, error) {
			t.Fatal("store must not be called for invalid time")
			return PaymentListResponse{}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments?paid_from=not-a-time", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.List).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDetailRejectsInvalidID(t *testing.T) {
	handler := NewHandler(fakeStore{
		getPaymentDetail: func(context.Context, string) (PaymentDetailResponse, error) {
			t.Fatal("store must not be called for invalid payment id")
			return PaymentDetailResponse{}, nil
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments/not-a-uuid", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Detail).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestDetailMapsNotFound(t *testing.T) {
	handler := NewHandler(fakeStore{
		getPaymentDetail: func(_ context.Context, paymentID string) (PaymentDetailResponse, error) {
			if paymentID != "00000000-0000-0000-0000-000000000000" {
				t.Fatalf("paymentID = %q, want 00000000-0000-0000-0000-000000000000", paymentID)
			}
			return PaymentDetailResponse{}, ErrPaymentNotFound
		},
	})
	request := authenticatedRequest(http.MethodGet, "/api/admin/payments/00000000-0000-0000-0000-000000000000", bytes.NewBufferString(""))
	response := httptest.NewRecorder()

	authenticatedHandler(handler.Detail).ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func authenticatedHandler(next http.HandlerFunc) http.Handler {
	adminHandler := admin.NewHandler(authStore{}, time.Hour, false)
	return adminHandler.RequireAuthentication(http.HandlerFunc(next))
}

func authenticatedRequest(method string, path string, body *bytes.Buffer) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request.AddCookie(&http.Cookie{Name: "pjsk_admin_session", Value: "session-token"})
	return request
}

type authStore struct{}

func (authStore) FindByUsername(context.Context, string) (admin.Admin, error) {
	return admin.Admin{}, admin.ErrNotFound
}

func (authStore) CreateSession(context.Context, string, string, time.Time) error {
	return nil
}

func (authStore) FindBySession(context.Context, string) (admin.Admin, error) {
	return admin.Admin{ID: "admin-1", Username: "admin", Status: "active"}, nil
}

func (authStore) DeleteSession(context.Context, string) error {
	return nil
}
