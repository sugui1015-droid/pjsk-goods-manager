package users

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pjsk/backend/internal/admin"
)

type stubStore struct {
	listResponse   ListResponse
	listFilters    Filters
	detailResponse DetailResponse
	detailID       string
	detailErr      error
	mergeRequest   MergeRequest
	mergeAdminID   string
	mergeErr       error
	previewErr     error
}

func (s *stubStore) ListUsers(_ context.Context, filters Filters) (ListResponse, error) {
	s.listFilters = filters
	return s.listResponse, nil
}

func (s *stubStore) GetUserDetail(_ context.Context, id string) (DetailResponse, error) {
	s.detailID = id
	return s.detailResponse, s.detailErr
}

func (s *stubStore) PreviewMerge(_ context.Context, _ string, _ string) (MergePreviewResponse, error) {
	return MergePreviewResponse{}, s.previewErr
}

func (s *stubStore) MergeUsers(_ context.Context, request MergeRequest, adminID string) (MergeResponse, error) {
	s.mergeRequest = request
	s.mergeAdminID = adminID
	return MergeResponse{SourceUserID: request.SourceUserID, TargetUserID: request.TargetUserID}, s.mergeErr
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

type authStore struct{}

func (authStore) FindByUsername(context.Context, string) (admin.Admin, error) {
	return admin.Admin{}, admin.ErrNotFound
}

func (authStore) CreateSession(context.Context, string, string, time.Time) error { return nil }

func (authStore) FindBySession(context.Context, string) (admin.Admin, error) {
	return admin.Admin{ID: "admin-1", Username: "admin", Status: "active"}, nil
}

func (authStore) DeleteSession(context.Context, string) error { return nil }

func authenticatedHandler(next http.HandlerFunc) http.Handler {
	adminHandler := admin.NewHandler(authStore{}, time.Hour, false)
	return adminHandler.RequireAuthentication(http.HandlerFunc(next))
}

func authenticatedRequest(method string, path string, body *bytes.Buffer) *http.Request {
	request := httptest.NewRequest(method, path, body)
	request.AddCookie(&http.Cookie{Name: "pjsk_admin_session", Value: "session-token"})
	return request
}

func TestMergeRequiresAdmin(t *testing.T) {
	handler := NewHandler(&stubStore{})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/users/merge", bytes.NewBufferString(`{}`))
	recorder := httptest.NewRecorder()
	handler.Merge(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestMergeRejectsMissingReason(t *testing.T) {
	handler := NewHandler(&stubStore{})
	body := bytes.NewBufferString(`{"source_user_id":"6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a","target_user_id":"7c2f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6b","reason":"  "}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/users/merge", body)
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Merge).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestMergePassesAdminID(t *testing.T) {
	store := &stubStore{}
	handler := NewHandler(store)
	body := bytes.NewBufferString(`{"source_user_id":"6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a","target_user_id":"7c2f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6b","reason":"duplicate cn"}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/users/merge", body)
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Merge).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if store.mergeAdminID != "admin-1" {
		t.Fatalf("admin id = %q, want admin-1", store.mergeAdminID)
	}
	if store.mergeRequest.Reason != "duplicate cn" {
		t.Fatalf("reason = %q", store.mergeRequest.Reason)
	}
}

func TestMergeMapsValidationErrors(t *testing.T) {
	store := &stubStore{mergeErr: ErrMergeTargetNotActive}
	handler := NewHandler(store)
	body := bytes.NewBufferString(`{"source_user_id":"6b1f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6a","target_user_id":"7c2f6ec1-8b5a-4b2e-b3f0-1f2e3d4c5b6b","reason":"dup"}`)
	request := authenticatedRequest(http.MethodPost, "/api/admin/users/merge", body)
	recorder := httptest.NewRecorder()
	authenticatedHandler(handler.Merge).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestMergePreviewValidatesParams(t *testing.T) {
	handler := NewHandler(&stubStore{})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/users/merge-preview?source_id=bad&target_cn=succ", nil)
	recorder := httptest.NewRecorder()
	handler.MergePreview(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
