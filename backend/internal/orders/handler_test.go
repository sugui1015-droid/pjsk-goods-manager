package orders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type stubStore struct {
	lastFilters OrderFilters
}

func (s *stubStore) ListOrders(_ context.Context, filters OrderFilters) (OrderListResponse, error) {
	s.lastFilters = filters
	return OrderListResponse{Items: []OrderSummary{}}, nil
}

func (s *stubStore) GetOrder(context.Context, string) (OrderDetailResponse, error) {
	return OrderDetailResponse{}, ErrOrderNotFound
}

// TestListPassesIndependentSeriesCategoryRoleFilters confirms 谷子系列 /
// 谷子种类(分类) / 谷子角色 are read as three independent query params, not
// folded together into the older combined "item" search field.
func TestListPassesIndependentSeriesCategoryRoleFilters(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)

	params := url.Values{}
	params.Set("cn", "CN001")
	params.Set("series", "26感谢祭")
	params.Set("category", "立牌")
	params.Set("role", "miku")
	request := httptest.NewRequest(http.MethodGet, "/api/admin/orders?"+params.Encode(), nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if store.lastFilters.CN != "CN001" {
		t.Fatalf("CN = %q, want CN001", store.lastFilters.CN)
	}
	if store.lastFilters.Series != "26感谢祭" {
		t.Fatalf("Series = %q, want 26感谢祭", store.lastFilters.Series)
	}
	if store.lastFilters.Category != "立牌" {
		t.Fatalf("Category = %q, want 立牌", store.lastFilters.Category)
	}
	if store.lastFilters.Role != "miku" {
		t.Fatalf("Role = %q, want miku", store.lastFilters.Role)
	}
	if store.lastFilters.Item != "" {
		t.Fatalf("Item = %q, want empty (series/category/role must not leak into the legacy combined field)", store.lastFilters.Item)
	}
}

func TestListDefaultsLimitAndCapsAt200(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/orders?limit=9999", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if store.lastFilters.Limit != 100 {
		t.Fatalf("limit = %d, want 100 (over-cap should fall back to default)", store.lastFilters.Limit)
	}
}
