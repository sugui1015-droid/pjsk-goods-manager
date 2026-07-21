package importpreview

const (
	IssueLevelWarning    = "warning"
	IssueLevelRowError   = "row_error"
	IssueLevelFatalError = "fatal_error"
	IssueLevelNotice     = "notice"
)

type Preview struct {
	ImportBatchID string         `json:"import_batch_id,omitempty"`
	File          FileInfo       `json:"file"`
	Sheets        []SheetSummary `json:"sheets"`
	Batches       []BatchPreview `json:"batches"`
	Errors        []Issue        `json:"errors"`
	Warnings      []Issue        `json:"warnings"`
	Notices       []Issue        `json:"notices"`
}

type FileInfo struct {
	OriginalFilename string `json:"original_filename"`
	SHA256           string `json:"sha256"`
	SizeBytes        int64  `json:"size_bytes"`
	SheetCount       int    `json:"sheet_count"`
	DuplicateFile    bool   `json:"duplicate_file"`
	FilenameConflict bool   `json:"filename_conflict"`
}

type SheetSummary struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Title        string  `json:"title"`
	Index        int     `json:"index"`
	TemplateType string  `json:"template_type"`
	BatchCount   int     `json:"batch_count"`
	RowCount     int     `json:"row_count"`
	ColumnCount  int     `json:"column_count"`
	TableAmount  float64 `json:"table_amount"`
	CalcAmount   float64 `json:"calculated_amount"`
	Difference   float64 `json:"difference"`
}

type BatchPreview struct {
	ID                   string          `json:"id"`
	SheetID              string          `json:"sheet_id"`
	SheetName            string          `json:"sheet_name"`
	SheetTitle           string          `json:"sheet_title,omitempty"`
	BatchName            string          `json:"batch_name"`
	TemplateType         string          `json:"template_type"`
	StartRow             int             `json:"start_row"`
	EndRow               int             `json:"end_row"`
	ContentHash          string          `json:"content_hash"`
	DuplicateInFile      bool            `json:"duplicate_in_file"`
	CalculationPriceType string          `json:"calculation_price_type"`
	PriceTypes           []PriceTypeInfo `json:"price_types"`
	CNCount              int             `json:"cn_count"`
	ItemTypeCount        int             `json:"item_type_count"`
	TotalQuantity        int             `json:"total_quantity"`
	TableAmount          float64         `json:"table_amount"`
	CalculatedAmount     float64         `json:"calculated_amount"`
	Difference           float64         `json:"difference"`
	Details              []DetailPreview `json:"details"`
	Errors               []Issue         `json:"errors"`
	Warnings             []Issue         `json:"warnings"`
	Notices              []Issue         `json:"notices"`
}

type PriceTypeInfo struct {
	Type      string    `json:"type"`
	Row       int       `json:"row"`
	UnitCount int       `json:"unit_count"`
	Values    []float64 `json:"values,omitempty"`
}

type DetailPreview struct {
	ID              string  `json:"id"`
	SheetID         string  `json:"sheet_id"`
	SheetName       string  `json:"sheet_name"`
	SheetTitle      string  `json:"sheet_title,omitempty"`
	BatchName       string  `json:"batch_name"`
	GoodsSeriesName string  `json:"goods_series_name,omitempty"`
	ProductCategory string  `json:"product_category,omitempty"`
	SeriesCode      string  `json:"series_code,omitempty"`
	GroupName       string  `json:"group_name,omitempty"`
	DisplayName     string  `json:"display_name,omitempty"`
	CharacterName   string  `json:"character_name,omitempty"`
	Category        string  `json:"category,omitempty"`
	SeriesName      string  `json:"series_name,omitempty"`
	ItemName        string  `json:"item_name"`
	ColumnIndex     int     `json:"column_index"`
	ColumnName      string  `json:"column_name"`
	RowNumber       int     `json:"row_number"`
	OriginalCN      string  `json:"original_cn"`
	NormalizedCN    string  `json:"normalized_cn"`
	Quantity        int     `json:"quantity"`
	PriceType       string  `json:"price_type"`
	UnitPrice       float64 `json:"unit_price"`
	Amount          float64 `json:"amount"`
	TableRowAmount  float64 `json:"table_row_amount"`
}

type Issue struct {
	Level     string `json:"level"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	SheetName string `json:"sheet_name,omitempty"`
	BatchID   string `json:"batch_id,omitempty"`
	RowNumber int    `json:"row_number,omitempty"`
	Column    string `json:"column,omitempty"`
}

type ConfirmRules struct {
	ExcludedSheetIDs []string                 `json:"excluded_sheet_ids,omitempty"`
	ExcludedCNs      []CNExclusionRule        `json:"excluded_cns,omitempty"`
	ExcludedItemIDs  []string                 `json:"excluded_item_ids,omitempty"`
	CategoryRules    []CategoryCorrectionRule `json:"category_rules,omitempty"`
}

type CNExclusionRule struct {
	SheetID string `json:"sheet_id,omitempty"`
	BatchID string `json:"batch_id,omitempty"`
	CN      string `json:"cn"`
}

type CategoryCorrectionRule struct {
	SheetID   string   `json:"sheet_id,omitempty"`
	BatchID   string   `json:"batch_id,omitempty"`
	DetailIDs []string `json:"detail_ids,omitempty"`
	ItemIDs   []string `json:"item_ids,omitempty"`
	Category  string   `json:"category"`
}

type PreviewAdjustmentResponse struct {
	Preview Preview `json:"preview"`
}

type ConfirmResult struct {
	ImportBatchID     string  `json:"import_batch_id"`
	ProjectID         string  `json:"project_id"`
	Status            string  `json:"status"`
	CNCount           int     `json:"cn_count"`
	ProductCount      int     `json:"product_count"`
	OrderCount        int     `json:"order_count"`
	OrderItemCount    int     `json:"order_item_count"`
	TotalQuantity     int     `json:"total_quantity"`
	TotalAmount       float64 `json:"total_amount"`
	WarningsAccepted  bool    `json:"warnings_accepted"`
	ConfirmedAt       string  `json:"confirmed_at"`
	SkippedErrorCount int     `json:"skipped_error_count"`
}

type RevokeResult struct {
	ImportBatchID   string  `json:"import_batch_id"`
	Status          string  `json:"status"`
	AffectedCNCount int     `json:"affected_cn_count"`
	OrderCount      int     `json:"order_count"`
	OrderItemCount  int     `json:"order_item_count"`
	TotalQuantity   int     `json:"total_quantity"`
	TotalAmount     float64 `json:"total_amount"`
	RevokedBy       string  `json:"revoked_by"`
	RevokedAt       string  `json:"revoked_at"`
}

type ImportHistoryItem struct {
	ID               string         `json:"id"`
	OriginalFilename string         `json:"original_filename"`
	FileHash         string         `json:"file_hash"`
	FileSize         int64          `json:"file_size"`
	SheetCount       int            `json:"sheet_count"`
	BatchCount       int            `json:"batch_count"`
	Status           string         `json:"status"`
	UploadedBy       string         `json:"uploaded_by,omitempty"`
	ConfirmedBy      string         `json:"confirmed_by,omitempty"`
	CreatedAt        string         `json:"created_at"`
	StartedAt        string         `json:"started_at,omitempty"`
	ConfirmedAt      string         `json:"confirmed_at,omitempty"`
	CompletedAt      string         `json:"completed_at,omitempty"`
	ErrorCount       int            `json:"error_count"`
	WarningCount     int            `json:"warning_count"`
	NoticeCount      int            `json:"notice_count"`
	WarningsAccepted bool           `json:"warnings_accepted"`
	ConfirmResult    *ConfirmResult `json:"confirm_result,omitempty"`
	RevokedBy        string         `json:"revoked_by,omitempty"`
	RevokedAt        string         `json:"revoked_at,omitempty"`
	RevokeResult     *RevokeResult  `json:"revoke_result,omitempty"`
}

// ImportHistoryResponse is one page of the filtered result set. Total counts
// every import matching the filters, not just this page.
type ImportHistoryResponse struct {
	Items      []ImportHistoryItem `json:"items"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
	Total      int                 `json:"total"`
	TotalPages int                 `json:"total_pages"`
}

type ImportDetailResponse struct {
	Import  ImportHistoryItem `json:"import"`
	Preview *Preview          `json:"preview,omitempty"`
}
