package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"pjsk/backend/internal/logsafe"
	"pjsk/backend/internal/query"
)

type Handler struct {
	store Store
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

type createRequest struct {
	Content string `json:"content"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *Handler) UserCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	user, ok := query.CurrentSessionUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录")
		return
	}

	var request createRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	content, err := normalizeContent(request.Content)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := h.store.Create(ctx, user.UserID, content)
	if err != nil {
		if errors.Is(err, ErrDuplicate) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		// Never log request content. The safe category is sufficient to diagnose
		// storage failures without persisting user-authored text.
		log.Printf("feedback create: user_id=%s result=error err=%s", user.UserID, logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	log.Printf("feedback create: id=%s user_id=%s result=success", item.ID, user.UserID)
	writeJSON(w, http.StatusCreated, map[string]any{"feedback": item})
}

func (h *Handler) AdminCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	filter, err := parseListFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	response, err := h.store.List(ctx, filter)
	if err != nil {
		log.Printf("feedback admin list: %s", logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

var statusPathPattern = regexp.MustCompile(`^/api/admin/feedbacks/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/status/?$`)

func (h *Handler) AdminItem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	match := statusPathPattern.FindStringSubmatch(r.URL.Path)
	if len(match) != 2 {
		writeError(w, http.StatusNotFound, ErrNotFound.Error())
		return
	}
	var request statusRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeDecodeError(w, err)
		return
	}
	if !validStatus(request.Status) {
		writeError(w, http.StatusBadRequest, ErrInvalidStatus.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := h.store.UpdateStatus(ctx, match[1], request.Status)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		log.Printf("feedback status update: id=%s result=error err=%s", match[1], logsafe.Category(err))
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	log.Printf("feedback status update: id=%s status=%s result=success", item.ID, item.Status)
	writeJSON(w, http.StatusOK, map[string]any{"feedback": item})
}

func normalizeContent(value string) (string, error) {
	content := strings.TrimSpace(value)
	if content == "" || utf8.RuneCountInString(content) > MaxContentRunes {
		return "", ErrInvalidContent
	}
	return content, nil
}

func validStatus(status string) bool {
	return status == StatusNew || status == StatusProcessed
}

func parseListFilter(values url.Values) (ListFilter, error) {
	filter := ListFilter{Status: values.Get("status"), Page: 1, PageSize: DefaultPageSize}
	if filter.Status != "" && !validStatus(filter.Status) {
		return ListFilter{}, ErrInvalidStatus
	}
	var err error
	if value := values.Get("page"); value != "" {
		filter.Page, err = strconv.Atoi(value)
		if err != nil || filter.Page < 1 {
			return ListFilter{}, errors.New("page 必须为正整数")
		}
	}
	if value := values.Get("page_size"); value != "" {
		filter.PageSize, err = strconv.Atoi(value)
		if err != nil || filter.PageSize < 1 || filter.PageSize > MaxPageSize {
			return ListFilter{}, errors.New("page_size 必须为1到200之间的整数")
		}
	}
	return filter, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxJSONBodySize)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("请求体只能包含一个 JSON 对象")
	}
	return nil
}

func writeDecodeError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "请求内容过大")
		return
	}
	writeError(w, http.StatusBadRequest, "请求格式不正确")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}
