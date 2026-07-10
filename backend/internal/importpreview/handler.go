package importpreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxPreviewFileSize = 20 << 20

type Handler struct {
	store Store
}

type Store interface {
	FindImportFile(context.Context, string, string) (ImportFileState, error)
}

type ImportFileState struct {
	DuplicateFile    bool
	FilenameConflict bool
}

func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPreviewFileSize+1)
	if err := r.ParseMultipartForm(maxPreviewFileSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart file upload")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "xlsx file field is required")
		return
	}
	defer file.Close()

	if !strings.EqualFold(filepath.Ext(header.Filename), ".xlsx") {
		writeError(w, http.StatusBadRequest, "only .xlsx files are supported")
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
		writeError(w, http.StatusBadRequest, "xlsx file could not be parsed")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	state, err := h.store.FindImportFile(ctx, fileHash, header.Filename)
	if err != nil {
		log.Printf("check import duplicate: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	preview.File.DuplicateFile = state.DuplicateFile
	preview.File.FilenameConflict = state.FilenameConflict
	if state.DuplicateFile {
		preview.Warnings = append(preview.Warnings, Issue{
			Level:   "warning",
			Code:    "duplicate_file",
			Message: "A previous import batch has the same file SHA-256.",
		})
	}
	if state.FilenameConflict {
		preview.Warnings = append(preview.Warnings, Issue{
			Level:   "warning",
			Code:    "same_filename_different_content",
			Message: "A previous import batch has the same filename but a different file SHA-256; treat it as a possible updated version.",
		})
	}

	writeJSON(w, http.StatusOK, preview)
}

func readUploadedFile(file multipart.File, limit int64) ([]byte, error) {
	reader := io.LimitReader(file, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.New("could not read uploaded file")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("xlsx file is too large")
	}
	if len(data) == 0 {
		return nil, errors.New("xlsx file is empty")
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

func (s *PostgresStore) FindImportFile(
	ctx context.Context,
	fileHash string,
	filename string,
) (ImportFileState, error) {
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

type errorResponse struct {
	Error string `json:"error"`
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
