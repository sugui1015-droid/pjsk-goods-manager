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
	getCNPayment  func(context.Context, string) (CNPaymentResponse, error)
	createPayment func(context.Context, CreatePaymentRequest, string) (CreatePaymentResponse, error)
}

func (s fakeStore) GetCNPayment(ctx context.Context, cn string) (CNPaymentResponse, error) {
	return s.getCNPayment(ctx, cn)
}

func (s fakeStore) CreatePayment(ctx context.Context, request CreatePaymentRequest, adminID string) (CreatePaymentResponse, error) {
	return s.createPayment(ctx, request, adminID)
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
