package importpreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"pjsk/backend/internal/admin"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxPreviewFileSize = 20 << 20

type Handler struct {
	store Store
}

type Store interface {
	FindImportFile(context.Context, string, string) (ImportFileState, error)
	SavePreview(context.Context, Preview, string) (PreviewState, error)
	ConfirmImport(context.Context, string, string, bool, ConfirmRules) (ConfirmResult, error)
	ListImports(context.Context) (ImportHistoryResponse, error)
	GetImport(context.Context, string) (ImportDetailResponse, error)
	RevokeImport(context.Context, string, string) (RevokeResult, error)
}

type ImportFileState struct {
	DuplicateFile    bool
	FilenameConflict bool
}

type PreviewState struct {
	ImportBatchID    string
	DuplicateFile    bool
	FilenameConflict bool
}

type confirmRequest struct {
	ImportBatchID string       `json:"import_batch_id"`
	AllowWarnings bool         `json:"allow_warnings"`
	Rules         ConfirmRules `json:"rules"`
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录管理员账号")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPreviewFileSize+1)
	if err := r.ParseMultipartForm(maxPreviewFileSize); err != nil {
		writeError(w, http.StatusBadRequest, "上传表单格式不正确")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择要上传的 xlsx 文件")
		return
	}
	defer file.Close()

	if !strings.EqualFold(filepath.Ext(header.Filename), ".xlsx") {
		writeError(w, http.StatusBadRequest, "仅支持 .xlsx 文件")
		return
	}

	data, err := readUploadedFile(file, maxPreviewFileSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fileHash := sha256Hex(data)

	preview, err := Parse(data, ParseOptions{
		Filename: header.Filename,
		FileHash: fileHash,
		Size:     int64(len(data)),
	})
	if err != nil {
		log.Printf("parse xlsx preview: %v", err)
		writeError(w, http.StatusBadRequest, "无法解析该 xlsx 文件")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	state, err := h.store.SavePreview(ctx, preview, account.ID)
	if err != nil {
		log.Printf("save import preview: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	preview.ImportBatchID = state.ImportBatchID
	preview.File.DuplicateFile = state.DuplicateFile
	preview.File.FilenameConflict = state.FilenameConflict
	if state.DuplicateFile {
		preview.Warnings = append(preview.Warnings, Issue{
			Level:   IssueLevelWarning,
			Code:    "duplicate_file",
			Message: "已经存在 SHA-256 完全相同的导入记录，请确认是否重复上传。",
		})
	}
	if state.FilenameConflict {
		preview.Warnings = append(preview.Warnings, Issue{
			Level:   IssueLevelWarning,
			Code:    "same_filename_different_content",
			Message: "存在同名但内容不同的历史导入，请确认是否为更新版本。",
		})
	}

	writeJSON(w, http.StatusOK, preview)
}

func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	account, ok := admin.CurrentAdmin(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "请先登录管理员账号")
		return
	}

	var request confirmRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求内容格式不正确")
		return
	}
	request.ImportBatchID = strings.TrimSpace(request.ImportBatchID)
	if request.ImportBatchID == "" {
		writeError(w, http.StatusBadRequest, "缺少导入批次 ID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	result, err := h.store.ConfirmImport(ctx, request.ImportBatchID, account.ID, request.AllowWarnings, request.Rules)
	if err != nil {
		switch {
		case errors.Is(err, ErrImportNotFound):
			writeError(w, http.StatusNotFound, "找不到导入预览记录")
		case errors.Is(err, ErrImportAlreadyConfirmed):
			writeError(w, http.StatusConflict, "该导入批次已经确认过，不能重复确认")
		case errors.Is(err, ErrImportHasFatalErrors):
			writeError(w, http.StatusBadRequest, "当前预览存在致命错误，无法确认导入")
		case errors.Is(err, ErrWarningsNeedApproval):
			writeError(w, http.StatusConflict, "当前预览存在提醒，请勾选人工确认后再继续")
		case errors.Is(err, ErrNoOrderItems):
			writeError(w, http.StatusBadRequest, "当前预览没有可导入的有效明细")
		case errors.Is(err, ErrInvalidImportRules):
			writeError(w, http.StatusBadRequest, "导入调整规则不合法")
		default:
			log.Printf("confirm import: %v", err)
			writeError(w, http.StatusInternalServerError, "服务器内部错误")
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.ListImports(ctx)
	if err != nil {
		log.Printf("list imports: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) Detail(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/admin/imports/"), "/")
	if path == "" || path == "history" {
		writeError(w, http.StatusNotFound, "找不到导入批次")
		return
	}

	if strings.HasSuffix(path, "/revert") {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
			return
		}
		importBatchID := strings.TrimSuffix(path, "/revert")
		if importBatchID == "" || strings.Contains(importBatchID, "/") {
			writeError(w, http.StatusNotFound, "找不到导入批次")
			return
		}
		account, ok := admin.CurrentAdmin(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "请先登录管理员账号")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		result, err := h.store.RevokeImport(ctx, importBatchID, account.ID)
		if err != nil {
			switch {
			case errors.Is(err, ErrImportNotFound):
				writeError(w, http.StatusNotFound, "找不到导入批次")
			case errors.Is(err, ErrImportAlreadyReverted):
				writeError(w, http.StatusConflict, "该导入批次已经撤销过，不能重复撤销")
			case errors.Is(err, ErrImportNotConfirmed):
				writeError(w, http.StatusConflict, "只有已确认导入的批次才能撤销")
			default:
				log.Printf("revoke import: %v", err)
				writeError(w, http.StatusInternalServerError, "服务器内部错误")
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if strings.Contains(path, "/") {
		writeError(w, http.StatusNotFound, "找不到导入批次")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, err := h.store.GetImport(ctx, path)
	if err != nil {
		if errors.Is(err, ErrImportNotFound) {
			writeError(w, http.StatusNotFound, "找不到导入批次")
			return
		}
		log.Printf("get import detail: %v", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}

	writeJSON(w, http.StatusOK, response)
}
func readUploadedFile(file multipart.File, limit int64) ([]byte, error) {
	reader := io.LimitReader(file, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.New("读取上传文件失败")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("xlsx 文件超过 20MB 限制")
	}
	if len(data) == 0 {
		return nil, errors.New("xlsx 文件为空")
	}
	return data, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) FindImportFile(ctx context.Context, fileHash string, filename string) (ImportFileState, error) {
	var state ImportFileState
	if s.pool == nil {
		return state, nil
	}

	err := s.pool.QueryRow(ctx, `
		select
			exists (
				select 1
				from import_batches
				where file_hash = $1
			) as duplicate_file,
			exists (
				select 1
				from import_batches
				where original_filename = $2
				  and file_hash <> $1
			) as filename_conflict
	`, fileHash, filename).Scan(&state.DuplicateFile, &state.FilenameConflict)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportFileState{}, nil
	}
	return state, err
}

func (s *PostgresStore) SavePreview(ctx context.Context, preview Preview, adminID string) (PreviewState, error) {
	payload, err := json.Marshal(preview)
	if err != nil {
		return PreviewState{}, err
	}

	state, err := s.FindImportFile(ctx, preview.File.SHA256, preview.File.OriginalFilename)
	if err != nil {
		return PreviewState{}, err
	}

	var importBatchID, status string
	err = s.pool.QueryRow(ctx, `
		select id::text, status
		from import_batches
		where file_hash = $1
	`, preview.File.SHA256).Scan(&importBatchID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `
			insert into import_batches (
				original_filename,
				file_hash,
				file_size,
				sheet_count,
				total_rows,
				success_rows,
				failed_rows,
				status,
				imported_by,
				preview_payload,
				error_count,
				warning_count,
				notice_count,
				started_at
			) values ($1, $2, $3, $4, $5, 0, $6, 'previewed', $7::uuid, $8::jsonb, $9, $10, $11, now())
			returning id::text
		`,
			preview.File.OriginalFilename,
			preview.File.SHA256,
			preview.File.SizeBytes,
			preview.File.SheetCount,
			len(preview.Batches),
			len(preview.Errors),
			adminID,
			payload,
			len(preview.Errors),
			len(preview.Warnings),
			len(preview.Notices),
		).Scan(&importBatchID)
		if err != nil {
			return PreviewState{}, err
		}
		return PreviewState{ImportBatchID: importBatchID, DuplicateFile: false, FilenameConflict: state.FilenameConflict}, nil
	}
	if err != nil {
		return PreviewState{}, err
	}

	if status == "previewed" || status == "pending" || status == "failed" || status == "cancelled" {
		_, err = s.pool.Exec(ctx, `
			update import_batches
			set original_filename = $1,
				file_size = $2,
				sheet_count = $3,
				total_rows = $4,
				failed_rows = $5,
				status = 'previewed',
				imported_by = $6::uuid,
				preview_payload = $7::jsonb,
				error_count = $8,
				warning_count = $9,
				notice_count = $10,
				started_at = now()
			where id = $11::uuid
		`,
			preview.File.OriginalFilename,
			preview.File.SizeBytes,
			preview.File.SheetCount,
			len(preview.Batches),
			len(preview.Errors),
			adminID,
			payload,
			len(preview.Errors),
			len(preview.Warnings),
			len(preview.Notices),
			importBatchID,
		)
		if err != nil {
			return PreviewState{}, err
		}
	}

	return PreviewState{ImportBatchID: importBatchID, DuplicateFile: true, FilenameConflict: state.FilenameConflict}, nil
}

func (s *PostgresStore) ListImports(ctx context.Context) (ImportHistoryResponse, error) {
	rows, err := s.pool.Query(ctx, `
		select
			b.id::text,
			b.original_filename,
			b.file_hash,
			coalesce(b.file_size, 0),
			coalesce(b.sheet_count, 0),
			b.total_rows,
			b.status,
			coalesce(importer.username, ''),
			coalesce(confirmer.username, ''),
			to_char(b.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(b.started_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(to_char(b.confirmed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(to_char(b.completed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			b.error_count,
			b.warning_count,
			b.notice_count,
			b.warnings_accepted,
			coalesce(b.confirm_result::text, ''),
			coalesce(revoker.username, ''),
			coalesce(to_char(b.revoked_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(b.revoke_result::text, '')
		from import_batches b
		left join admins importer on importer.id = b.imported_by
		left join admins confirmer on confirmer.id = b.confirmed_by
		left join admins revoker on revoker.id = b.revoked_by
		order by b.created_at desc
		limit 100
	`)
	if err != nil {
		return ImportHistoryResponse{}, err
	}
	defer rows.Close()

	response := ImportHistoryResponse{Items: []ImportHistoryItem{}}
	for rows.Next() {
		item, err := scanImportHistoryItem(rows)
		if err != nil {
			return ImportHistoryResponse{}, err
		}
		response.Items = append(response.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ImportHistoryResponse{}, err
	}
	return response, nil
}

func (s *PostgresStore) GetImport(ctx context.Context, importBatchID string) (ImportDetailResponse, error) {
	row := s.pool.QueryRow(ctx, `
		select
			b.id::text,
			b.original_filename,
			b.file_hash,
			coalesce(b.file_size, 0),
			coalesce(b.sheet_count, 0),
			b.total_rows,
			b.status,
			coalesce(importer.username, ''),
			coalesce(confirmer.username, ''),
			to_char(b.created_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
			coalesce(to_char(b.started_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(to_char(b.confirmed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(to_char(b.completed_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			b.error_count,
			b.warning_count,
			b.notice_count,
			b.warnings_accepted,
			coalesce(b.confirm_result::text, ''),
			coalesce(revoker.username, ''),
			coalesce(to_char(b.revoked_at at time zone 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), ''),
			coalesce(b.revoke_result::text, ''),
			coalesce(b.preview_payload::text, '')
		from import_batches b
		left join admins importer on importer.id = b.imported_by
		left join admins confirmer on confirmer.id = b.confirmed_by
		left join admins revoker on revoker.id = b.revoked_by
		where b.id = $1::uuid
	`, importBatchID)

	item, previewPayload, err := scanImportDetail(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportDetailResponse{}, ErrImportNotFound
	}
	if err != nil {
		return ImportDetailResponse{}, err
	}

	response := ImportDetailResponse{Import: item}
	if previewPayload != "" {
		var preview Preview
		if err := json.Unmarshal([]byte(previewPayload), &preview); err != nil {
			return ImportDetailResponse{}, err
		}
		response.Preview = &preview
	}
	return response, nil
}

type importHistoryScanner interface {
	Scan(dest ...any) error
}

func scanImportHistoryItem(scanner importHistoryScanner) (ImportHistoryItem, error) {
	item, _, err := scanImportHistoryFields(scanner, false)
	return item, err
}

func scanImportDetail(scanner importHistoryScanner) (ImportHistoryItem, string, error) {
	return scanImportHistoryFields(scanner, true)
}

func scanImportHistoryFields(scanner importHistoryScanner, includePreview bool) (ImportHistoryItem, string, error) {
	var item ImportHistoryItem
	var confirmPayload string
	var revokePayload string
	var previewPayload string
	dest := []any{
		&item.ID,
		&item.OriginalFilename,
		&item.FileHash,
		&item.FileSize,
		&item.SheetCount,
		&item.BatchCount,
		&item.Status,
		&item.UploadedBy,
		&item.ConfirmedBy,
		&item.CreatedAt,
		&item.StartedAt,
		&item.ConfirmedAt,
		&item.CompletedAt,
		&item.ErrorCount,
		&item.WarningCount,
		&item.NoticeCount,
		&item.WarningsAccepted,
		&confirmPayload,
		&item.RevokedBy,
		&item.RevokedAt,
		&revokePayload,
	}
	if includePreview {
		dest = append(dest, &previewPayload)
	}
	if err := scanner.Scan(dest...); err != nil {
		return ImportHistoryItem{}, "", err
	}
	if confirmPayload != "" {
		var result ConfirmResult
		if err := json.Unmarshal([]byte(confirmPayload), &result); err != nil {
			return ImportHistoryItem{}, "", err
		}
		item.ConfirmResult = &result
	}
	if revokePayload != "" {
		var result RevokeResult
		if err := json.Unmarshal([]byte(revokePayload), &result); err != nil {
			return ImportHistoryItem{}, "", err
		}
		item.RevokeResult = &result
	}
	return item, previewPayload, nil
}

var (
	ErrImportNotFound         = errors.New("找不到导入预览记录")
	ErrImportAlreadyConfirmed = errors.New("import already confirmed")
	ErrImportHasFatalErrors   = errors.New("import has fatal errors")
	ErrWarningsNeedApproval   = errors.New("warnings need approval")
	ErrNoOrderItems           = errors.New("no order items")
	ErrImportAlreadyReverted  = errors.New("import already reverted")
	ErrImportNotConfirmed     = errors.New("import not confirmed")
)

func (s *PostgresStore) ConfirmImport(ctx context.Context, importBatchID string, adminID string, allowWarnings bool, rules ConfirmRules) (ConfirmResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ConfirmResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var payload []byte
	var status string
	var fileHash string
	var originalFilename string
	err = tx.QueryRow(ctx, `
		select preview_payload, status, file_hash, original_filename
		from import_batches
		where id = $1::uuid
		for update
	`, importBatchID).Scan(&payload, &status, &fileHash, &originalFilename)
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfirmResult{}, ErrImportNotFound
	}
	if err != nil {
		return ConfirmResult{}, err
	}
	if status == "confirmed" || status == "completed" || status == "partial" {
		return ConfirmResult{}, ErrImportAlreadyConfirmed
	}
	if status != "previewed" {
		return ConfirmResult{}, fmt.Errorf("import batch status %q cannot be confirmed", status)
	}
	if len(payload) == 0 {
		return ConfirmResult{}, ErrImportNotFound
	}

	var preview Preview
	if err := json.Unmarshal(payload, &preview); err != nil {
		return ConfirmResult{}, err
	}
	preview, err = applyImportRules(preview, rules)
	if err != nil {
		return ConfirmResult{}, err
	}
	if hasFatalIssues(preview.Errors) {
		return ConfirmResult{}, ErrImportHasFatalErrors
	}
	if len(preview.Warnings) > 0 && !allowWarnings {
		return ConfirmResult{}, ErrWarningsNeedApproval
	}

	orderItemTotal := 0
	for _, batch := range preview.Batches {
		orderItemTotal += len(batch.Details)
	}
	if orderItemTotal == 0 {
		return ConfirmResult{}, ErrNoOrderItems
	}

	if _, err := tx.Exec(ctx, `
		update import_batches
		set status = 'processing', started_at = coalesce(started_at, now())
		where id = $1::uuid
	`, importBatchID); err != nil {
		return ConfirmResult{}, err
	}

	projectID, err := insertProject(ctx, tx, importBatchID, fileHash, originalFilename)
	if err != nil {
		return ConfirmResult{}, err
	}

	userIDs := map[string]string{}
	productIDs := map[string]string{}
	orderIDs := map[string]string{}
	productCount := 0
	orderItemCount := 0
	totalQuantity := 0
	totalAmount := 0.0

	for _, batch := range preview.Batches {
		for _, detail := range batch.Details {
			userID, ok := userIDs[detail.NormalizedCN]
			if !ok {
				userID, err = upsertUser(ctx, tx, detail.NormalizedCN, detail.OriginalCN)
				if err != nil {
					return ConfirmResult{}, err
				}
				userIDs[detail.NormalizedCN] = userID
			}

			productKey := productKey(batch, detail)
			productID, ok := productIDs[productKey]
			if !ok {
				productID, err = upsertProduct(ctx, tx, projectID, productKey, batch, detail)
				if err != nil {
					return ConfirmResult{}, err
				}
				productIDs[productKey] = productID
				productCount++
			}

			orderID, ok := orderIDs[userID]
			if !ok {
				orderID, err = insertOrder(ctx, tx, importBatchID, projectID, userID, detail.NormalizedCN)
				if err != nil {
					return ConfirmResult{}, err
				}
				orderIDs[userID] = orderID
			}

			if err := insertOrderItem(ctx, tx, importBatchID, orderID, productID, batch, detail); err != nil {
				return ConfirmResult{}, err
			}
			orderItemCount++
			totalQuantity += detail.Quantity
			totalAmount = round2(totalAmount + detail.Amount)
		}
	}

	for userID, orderID := range orderIDs {
		_ = userID
		if _, err := tx.Exec(ctx, `
			update orders
			set total_amount = coalesce((
				select sum(amount)
				from order_items
				where order_id = $1::uuid
				  and revoked_at is null
			), 0),
				status = 'submitted',
				updated_at = now()
			where id = $1::uuid
		`, orderID); err != nil {
			return ConfirmResult{}, err
		}
	}

	confirmedAt := time.Now().UTC()
	result := ConfirmResult{
		ImportBatchID:     importBatchID,
		ProjectID:         projectID,
		Status:            "confirmed",
		CNCount:           len(userIDs),
		ProductCount:      productCount,
		OrderCount:        len(orderIDs),
		OrderItemCount:    orderItemCount,
		TotalQuantity:     totalQuantity,
		TotalAmount:       round2(totalAmount),
		WarningsAccepted:  allowWarnings,
		ConfirmedAt:       confirmedAt.Format(time.RFC3339),
		SkippedErrorCount: len(preview.Errors),
	}
	resultPayload, err := json.Marshal(result)
	if err != nil {
		return ConfirmResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		update import_batches
		set status = 'confirmed',
			success_rows = $2,
			failed_rows = $8,
			completed_at = $3,
			confirmed_by = $4::uuid,
			confirmed_at = $3,
			confirmed_project_id = $5::uuid,
			confirm_result = $6::jsonb,
			warnings_accepted = $7
		where id = $1::uuid
	`, importBatchID, orderItemCount, confirmedAt, adminID, projectID, resultPayload, allowWarnings, len(preview.Errors)); err != nil {
		return ConfirmResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return ConfirmResult{}, err
	}
	return result, nil
}

func hasFatalIssues(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Level == IssueLevelFatalError {
			return true
		}
	}
	return false
}

func (s *PostgresStore) RevokeImport(ctx context.Context, importBatchID string, adminID string) (RevokeResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RevokeResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `
		select status
		from import_batches
		where id = $1::uuid
		for update
	`, importBatchID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return RevokeResult{}, ErrImportNotFound
	}
	if err != nil {
		return RevokeResult{}, err
	}
	if status == "reverted" || status == "cancelled" {
		return RevokeResult{}, ErrImportAlreadyReverted
	}
	if status != "confirmed" {
		return RevokeResult{}, ErrImportNotConfirmed
	}

	var result RevokeResult
	err = tx.QueryRow(ctx, `
		select
			count(distinct o.user_id)::int,
			count(distinct oi.order_id)::int,
			count(oi.id)::int,
			coalesce(sum(oi.quantity), 0)::int,
			coalesce(sum(oi.amount), 0)::float8
		from order_items oi
		join orders o on o.id = oi.order_id
		where oi.import_batch_id = $1::uuid
		  and oi.revoked_at is null
	`, importBatchID).Scan(
		&result.AffectedCNCount,
		&result.OrderCount,
		&result.OrderItemCount,
		&result.TotalQuantity,
		&result.TotalAmount,
	)
	if err != nil {
		return RevokeResult{}, err
	}
	if result.OrderItemCount == 0 {
		return RevokeResult{}, ErrImportAlreadyReverted
	}

	rows, err := tx.Query(ctx, `
		select distinct order_id::text
		from order_items
		where import_batch_id = $1::uuid
		  and revoked_at is null
	`, importBatchID)
	if err != nil {
		return RevokeResult{}, err
	}
	orderIDs := []string{}
	for rows.Next() {
		var orderID string
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return RevokeResult{}, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return RevokeResult{}, err
	}
	rows.Close()

	revokedAt := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		update order_items
		set revoked_at = $2,
			revoked_by = $3::uuid,
			revoke_reason = '管理员撤销导入批次'
		where import_batch_id = $1::uuid
		  and revoked_at is null
	`, importBatchID, revokedAt, adminID); err != nil {
		return RevokeResult{}, err
	}

	for _, orderID := range orderIDs {
		if _, err := tx.Exec(ctx, `
			update orders
			set total_amount = coalesce((
				select sum(amount)
				from order_items
				where order_id = $1::uuid
				  and revoked_at is null
			), 0),
				status = case when exists (
					select 1
					from order_items
					where order_id = $1::uuid
					  and revoked_at is null
				) then status else 'cancelled' end,
				updated_at = now()
			where id = $1::uuid
		`, orderID); err != nil {
			return RevokeResult{}, err
		}
	}

	result.ImportBatchID = importBatchID
	result.Status = "reverted"
	result.TotalAmount = round2(result.TotalAmount)
	result.RevokedBy = adminID
	result.RevokedAt = revokedAt.Format(time.RFC3339)
	payload, err := json.Marshal(result)
	if err != nil {
		return RevokeResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		update import_batches
		set status = 'reverted',
			revoked_by = $2::uuid,
			revoked_at = $3,
			revoke_result = $4::jsonb,
			completed_at = $3
		where id = $1::uuid
	`, importBatchID, adminID, revokedAt, payload); err != nil {
		return RevokeResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return RevokeResult{}, err
	}
	return result, nil
}

type dbTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func insertProject(ctx context.Context, tx dbTx, importBatchID string, fileHash string, filename string) (string, error) {
	code := "import-" + strings.ReplaceAll(importBatchID, "-", "")[:20]
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "Imported Excel " + fileHash[:8]
	}
	var projectID string
	err := tx.QueryRow(ctx, `
		insert into projects (code, name, description, status, opened_at)
		values ($1, $2, $3, 'active', now())
		returning id::text
	`, code, name, "Confirmed Excel import "+fileHash).Scan(&projectID)
	return projectID, err
}

func upsertUser(ctx context.Context, tx dbTx, normalizedCN string, originalCN string) (string, error) {
	var userID string
	err := tx.QueryRow(ctx, `
		insert into users (cn_code, display_name, status)
		values ($1, $2, 'active')
		on conflict (cn_code) do update
		set display_name = coalesce(users.display_name, excluded.display_name),
			updated_at = now()
		returning id::text
	`, normalizedCN, originalCN).Scan(&userID)
	return userID, err
}

func productKey(batch BatchPreview, detail DetailPreview) string {
	parts := []string{
		batch.ID,
		productNameForDetail(batch, detail),
		detail.ProductCategory,
		detail.SeriesCode,
		detail.CharacterName,
		detail.ColumnName,
		detail.PriceType,
		fmt.Sprintf("%.2f", detail.UnitPrice),
	}
	return hashStrings(parts...)
}

func productNameForDetail(batch BatchPreview, detail DetailPreview) string {
	name := strings.TrimSpace(detail.GoodsSeriesName)
	if name != "" {
		return name
	}
	name = strings.TrimSpace(batch.SheetTitle)
	if name != "" {
		return name
	}
	name = strings.TrimSpace(batch.BatchName)
	if name != "" {
		return name
	}
	return detail.ItemName
}

func upsertProduct(ctx context.Context, tx dbTx, projectID string, sku string, batch BatchPreview, detail DetailPreview) (string, error) {
	productName := productNameForDetail(batch, detail)
	characterName := detail.CharacterName
	if characterName == "" {
		characterName = characterNameFromItemName(detail.ItemName)
	}
	category := detail.ProductCategory
	if category == "" {
		category = detail.Category
	}
	var productID string
	err := tx.QueryRow(ctx, `
		insert into products (project_id, sku, name, character_name, category, series_code, unit_price, status, sort_order)
		values ($1::uuid, $2, $3, $4, $5, $6, $7, 'active', $8)
		on conflict (project_id, sku) do update
		set name = excluded.name,
			character_name = excluded.character_name,
			category = excluded.category,
			series_code = excluded.series_code,
			unit_price = excluded.unit_price,
			updated_at = now()
		returning id::text
	`, projectID, sku, productName, characterName, category, detail.SeriesCode, detail.UnitPrice, detail.ColumnIndex).Scan(&productID)
	return productID, err
}

func insertOrder(ctx context.Context, tx dbTx, importBatchID string, projectID string, userID string, normalizedCN string) (string, error) {
	orderNo := "IMP-" + strings.ReplaceAll(importBatchID, "-", "")[:12] + "-" + hashStrings(normalizedCN)[:10]
	var orderID string
	err := tx.QueryRow(ctx, `
		insert into orders (project_id, user_id, order_no, status, total_amount, note)
		values ($1::uuid, $2::uuid, $3, 'draft', 0, $4)
		returning id::text
	`, projectID, userID, orderNo, "Created from Excel import "+importBatchID).Scan(&orderID)
	return orderID, err
}

func insertOrderItem(ctx context.Context, tx dbTx, importBatchID string, orderID string, productID string, batch BatchPreview, detail DetailPreview) error {
	sourceRowKey := fmt.Sprintf("%s!%s%d", detail.SheetName, detail.ColumnName, detail.RowNumber)
	legacyRecordID := hashStrings(importBatchID, detail.SheetName, batch.ID, detail.NormalizedCN, detail.ColumnName, fmt.Sprint(detail.RowNumber), detail.ItemName)
	_, err := tx.Exec(ctx, `
		insert into order_items (
			order_id,
			product_id,
			quantity,
			unit_price,
			amount,
			payment_status,
			import_batch_id,
			source_sheet,
			source_row_key,
			legacy_record_id
		) values ($1::uuid, $2::uuid, $3, $4, $5, 'unpaid', $6::uuid, $7, $8, $9)
	`, orderID, productID, detail.Quantity, detail.UnitPrice, detail.Amount, importBatchID, detail.SheetName, sourceRowKey, legacyRecordID)
	return err
}

type errorResponse struct {
	Error string `json:"error"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode import preview JSON response: %v", err)
	}
}
