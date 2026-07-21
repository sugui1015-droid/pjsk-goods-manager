package importpreview

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pjsk/backend/internal/admin"
	"pjsk/backend/internal/logsafe"
)

// confirmErrorStore hands Confirm whatever error the test wants, so the HTTP
// mapping can be checked without a database.
type confirmErrorStore struct {
	stubStore
	err error
}

func (s *confirmErrorStore) ConfirmImport(context.Context, string, string, bool, ConfirmRules) (ConfirmResult, error) {
	return ConfirmResult{}, s.err
}

func confirmRequestFor(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	handler := NewHandler(&confirmErrorStore{err: err})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/imports/confirm", strings.NewReader(`{"import_batch_id":"11111111-1111-1111-1111-111111111111"}`))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(admin.ContextWithAdmin(request.Context(), admin.Admin{ID: "22222222-2222-2222-2222-222222222222", Username: "tester"}))
	recorder := httptest.NewRecorder()
	handler.Confirm(recorder, request)
	return recorder
}

// 撤销过的批次再确认是业务冲突（409），不是 500。
// 这条路径以前落进 default 分支，前端看到「服务器内部错误」，日志只有
// "confirm import: internal error"，完全无法定位。
func TestConfirmRevertedBatchAnswers409(t *testing.T) {
	recorder := confirmRequestFor(t, &ImportNotConfirmableError{Status: "reverted"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "已撤销") {
		t.Fatalf("body = %s, want 状态说明「已撤销」", body)
	}
	// 前端不能看到内部错误措辞。
	if strings.Contains(body, "服务器内部错误") {
		t.Fatalf("body = %s, must not be reported as an internal error", body)
	}
}

func TestConfirmProcessingBatchAnswers409(t *testing.T) {
	recorder := confirmRequestFor(t, &ImportNotConfirmableError{Status: "processing"})
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "处理中") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

// 真正的服务器故障仍然是 500，且响应体里只有通用文案。
func TestConfirmRealFailureStaysInternalAndLeaksNothing(t *testing.T) {
	recorder := confirmRequestFor(t, logsafe.Stage("upsert product", errors.New("dial tcp 10.0.0.5:5432: password=hunter2")))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	body := recorder.Body.String()
	for _, secret := range []string{"password", "hunter2", "10.0.0.5", "upsert product"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %q: %s", secret, body)
		}
	}
}

// 日志侧：Stage 链给出失败阶段，但不带驱动原文/连接串/参数值。
func TestLogsafeDetailNamesTheStageWithoutLeaking(t *testing.T) {
	err := logsafe.Stage("insert order item", errors.New("dial tcp 10.0.0.5:5432: user=pjsk password=hunter2"))
	detail := logsafe.Detail(err)
	if !strings.Contains(detail, "insert order item") {
		t.Fatalf("detail = %q, want the stage label", detail)
	}
	for _, secret := range []string{"hunter2", "password", "10.0.0.5", "pjsk"} {
		if strings.Contains(detail, secret) {
			t.Fatalf("detail leaked %q: %s", secret, detail)
		}
	}

	// 多层阶段按从外到内的顺序拼接。
	nested := logsafe.Stage("confirm import", logsafe.Stage("upsert product", errors.New("boom")))
	if got := logsafe.Detail(nested); !strings.HasPrefix(got, "confirm import: upsert product -> ") {
		t.Fatalf("nested detail = %q", got)
	}

	// Stage 不能破坏 errors.Is：handler 的 sentinel 判断依赖它。
	wrapped := logsafe.Stage("load preview payload", ErrImportNotFound)
	if !errors.Is(wrapped, ErrImportNotFound) {
		t.Fatal("Stage broke the %w chain: errors.Is no longer sees the sentinel")
	}
	if logsafe.Detail(nil) != "" {
		t.Fatal("Detail(nil) must be empty")
	}
}
