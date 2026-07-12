package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubStore struct {
	listResponse   ListResponse
	listFilters    Filters
	detailResponse DetailResponse
	detailID       string
	detailErr      error
}

func (s *stubStore) ListUsers(_ context.Context, filters Filters) (ListResponse, error) {
	s.listFilters = filters
	return s.listResponse, nil
}

func (s *stubStore) GetUserDetail(_ context.Context, id string) (DetailResponse, error) {
	s.detailID = id
	return s.detailResponse, s.detailErr
}

func TestListPassesFiltersAndDefaultsLimit(t *testing.T) {
	store := &stubStore{listResponse: ListResponse{Items: []ListItem{{CNCode: "succ"}}}}
	handler := NewHandler(store)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/users?cn=su&status=active", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if store.listFilters.CN != "su" || store.listFilters.Status != "active" || store.listFilters.Limit != 200 {
		t.Fatalf("filters = %+v", store.listFilters)
	}

	var payload ListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].CNCode != "succ" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestListCapsLimit(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/users?limit=9999", nil)
	recorder := httptest.NewRecorder()
	handler.List(recorder, request)

	if store.listFilters.Limit != 200 {
		t.Fatalf("limit = %d, want capped 200", store.listFilters.Limit)
	}
}

func TestDetailRejectsInvalidID(t *testing.T) {
	handler := NewHandler(&stubStore{})

	request := httptest.NewRequest(http.MethodGet, "/api/admin/users/not-a-uuid", nil)
	recorder := httptest.NewRecorder()
	handler.Detail(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestDetailMapsNotFound(t *testing.T) {
	store := &stubStore{detailErr: ErrUserNotFound}
	handler := NewHandler(store)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a", nil)
	recorder := httptest.NewRecorder()
	handler.Detail(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if store.detailID != "6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a" {
		t.Fatalf("detail id = %q", store.detailID)
	}
}

func TestDetailRejectsWrongMethod(t *testing.T) {
	handler := NewHandler(&stubStore{})

	request := httptest.NewRequest(http.MethodPost, "/api/admin/users/6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a", nil)
	recorder := httptest.NewRecorder()
	handler.Detail(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}
