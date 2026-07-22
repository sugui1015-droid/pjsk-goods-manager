package feedback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/query"
)

type fakeStore struct {
	create       func(context.Context, string, string) (Feedback, error)
	list         func(context.Context, ListFilter) (ListResponse, error)
	updateStatus func(context.Context, string, string) (Feedback, error)
}

func (s fakeStore) Create(ctx context.Context, userID, content string) (Feedback, error) {
	if s.create != nil {
		return s.create(ctx, userID, content)
	}
	return Feedback{ID: "feedback-1", Content: content, Status: StatusNew}, nil
}

func (s fakeStore) List(ctx context.Context, filter ListFilter) (ListResponse, error) {
	if s.list != nil {
		return s.list(ctx, filter)
	}
	return ListResponse{Items: []Feedback{}, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (s fakeStore) UpdateStatus(ctx context.Context, id, status string) (Feedback, error) {
	if s.updateStatus != nil {
		return s.updateStatus(ctx, id, status)
	}
	return Feedback{ID: id, Status: status}, nil
}

func userRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(query.ContextWithSessionUser(r.Context(), query.SessionUser{
		UserID: "11111111-1111-1111-1111-111111111111",
		CNCode: "CN001",
	}))
}

func TestUserCreateRequiresSession(t *testing.T) {
	handler := NewHandler(fakeStore{})
	response := httptest.NewRecorder()
	handler.UserCollection(response, httptest.NewRequest(http.MethodPost, "/api/query/feedbacks", strings.NewReader(`{"content":"建议"}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestUserCreateUsesSessionIdentityAndReturnsCreated(t *testing.T) {
	var gotUserID, gotContent string
	handler := NewHandler(fakeStore{create: func(_ context.Context, userID, content string) (Feedback, error) {
		gotUserID, gotContent = userID, content
		return Feedback{ID: "feedback-1", Content: content, Status: StatusNew}, nil
	}})
	response := httptest.NewRecorder()
	handler.UserCollection(response, userRequest(http.MethodPost, "/api/query/feedbacks", `{"content":"  希望增加筛选  "}`))

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", response.Code, response.Body.String())
	}
	if gotUserID != "11111111-1111-1111-1111-111111111111" || gotContent != "希望增加筛选" {
		t.Fatalf("create args = %q/%q", gotUserID, gotContent)
	}
}

func TestUserCreateRejectsClientUserID(t *testing.T) {
	handler := NewHandler(fakeStore{})
	response := httptest.NewRecorder()
	handler.UserCollection(response, userRequest(http.MethodPost, "/api/query/feedbacks", `{"content":"建议","user_id":"someone-else"}`))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestUserCreateRejectsEmptyAnd1001Runes(t *testing.T) {
	handler := NewHandler(fakeStore{})
	for name, content := range map[string]string{
		"empty":    "  \n\t ",
		"too_long": strings.Repeat("意", 1001),
	} {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(createRequest{Content: content})
			response := httptest.NewRecorder()
			handler.UserCollection(response, userRequest(http.MethodPost, "/api/query/feedbacks", string(body)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestUserCreateRejectsOversizedJSONBody(t *testing.T) {
	handler := NewHandler(fakeStore{})
	request := userRequest(http.MethodPost, "/api/query/feedbacks", `{"content":"`+strings.Repeat("x", MaxJSONBodySize)+`"}`)
	response := httptest.NewRecorder()
	handler.UserCollection(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
	}
}

func TestUserCreateRejectsDuplicate(t *testing.T) {
	handler := NewHandler(fakeStore{create: func(context.Context, string, string) (Feedback, error) {
		return Feedback{}, ErrDuplicate
	}})
	response := httptest.NewRecorder()
	handler.UserCollection(response, userRequest(http.MethodPost, "/api/query/feedbacks", `{"content":"重复建议"}`))
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
}

func TestAdminListParsesStatusAndPagination(t *testing.T) {
	var got ListFilter
	handler := NewHandler(fakeStore{list: func(_ context.Context, filter ListFilter) (ListResponse, error) {
		got = filter
		return ListResponse{Items: []Feedback{{ID: "feedback-1", CNCode: "CN001", Content: "建议", Status: StatusNew}}, Page: filter.Page, PageSize: filter.PageSize, Total: 1, TotalPages: 1}, nil
	}})
	response := httptest.NewRecorder()
	handler.AdminCollection(response, httptest.NewRequest(http.MethodGet, "/api/admin/feedbacks?status=new&page=2&page_size=50", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got != (ListFilter{Status: StatusNew, Page: 2, PageSize: 50}) {
		t.Fatalf("filter = %#v", got)
	}
}

func TestAdminUpdateStatusSucceeds(t *testing.T) {
	const id = "11111111-1111-1111-1111-111111111111"
	var gotID, gotStatus string
	handler := NewHandler(fakeStore{updateStatus: func(_ context.Context, currentID, status string) (Feedback, error) {
		gotID, gotStatus = currentID, status
		return Feedback{ID: currentID, Status: status}, nil
	}})
	response := httptest.NewRecorder()
	handler.AdminItem(response, httptest.NewRequest(http.MethodPatch, "/api/admin/feedbacks/"+id+"/status", bytes.NewBufferString(`{"status":"processed"}`)))
	if response.Code != http.StatusOK || gotID != id || gotStatus != StatusProcessed {
		t.Fatalf("status/args = %d %q %q: %s", response.Code, gotID, gotStatus, response.Body.String())
	}
}

func TestAdminUpdateRejectsInvalidStatusAndMissingFeedback(t *testing.T) {
	const path = "/api/admin/feedbacks/11111111-1111-1111-1111-111111111111/status"
	handler := NewHandler(fakeStore{})
	invalid := httptest.NewRecorder()
	handler.AdminItem(invalid, httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"status":"closed"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status = %d, want 400", invalid.Code)
	}

	missingHandler := NewHandler(fakeStore{updateStatus: func(context.Context, string, string) (Feedback, error) {
		return Feedback{}, ErrNotFound
	}})
	missing := httptest.NewRecorder()
	missingHandler.AdminItem(missing, httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"status":"new"}`)))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", missing.Code)
	}
}

func TestStoreErrorsDoNotLeakIntoResponse(t *testing.T) {
	handler := NewHandler(fakeStore{create: func(context.Context, string, string) (Feedback, error) {
		return Feedback{}, errors.New("secret feedback database detail")
	}})
	response := httptest.NewRecorder()
	handler.UserCollection(response, userRequest(http.MethodPost, "/api/query/feedbacks", `{"content":"建议"}`))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("response leaked store error: %d %s", response.Code, response.Body.String())
	}
}

func TestFeedbackLogsNeverContainContent(t *testing.T) {
	const sensitiveContent = "PRIVATE_FEEDBACK_BODY_不要记录"
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	handler := NewHandler(fakeStore{})
	success := httptest.NewRecorder()
	body, _ := json.Marshal(createRequest{Content: sensitiveContent})
	handler.UserCollection(success, userRequest(http.MethodPost, "/api/query/feedbacks", string(body)))
	if success.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", success.Code)
	}
	if strings.Contains(logs.String(), sensitiveContent) {
		t.Fatalf("success log contains feedback content: %s", logs.String())
	}

	logs.Reset()
	failingHandler := NewHandler(fakeStore{create: func(context.Context, string, string) (Feedback, error) {
		return Feedback{}, errors.New("database unavailable")
	}})
	failure := httptest.NewRecorder()
	failingHandler.UserCollection(failure, userRequest(http.MethodPost, "/api/query/feedbacks", string(body)))
	if strings.Contains(logs.String(), sensitiveContent) {
		t.Fatalf("failure log contains feedback content: %s", logs.String())
	}
}
