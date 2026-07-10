package importpreview

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
	Name         string  `json:"name"`
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
	SheetName            string          `json:"sheet_name"`
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
	SheetName      string  `json:"sheet_name"`
	BatchName      string  `json:"batch_name"`
	Category       string  `json:"category,omitempty"`
	ItemName       string  `json:"item_name"`
	ColumnIndex    int     `json:"column_index"`
	ColumnName     string  `json:"column_name"`
	RowNumber      int     `json:"row_number"`
	OriginalCN     string  `json:"original_cn"`
	NormalizedCN   string  `json:"normalized_cn"`
	Quantity       int     `json:"quantity"`
	PriceType      string  `json:"price_type"`
	UnitPrice      float64 `json:"unit_price"`
	Amount         float64 `json:"amount"`
	TableRowAmount float64 `json:"table_row_amount"`
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

type ConfirmResult struct {
	ImportBatchID  string  `json:"import_batch_id"`
	ProjectID      string  `json:"project_id"`
	Status         string  `json:"status"`
	CNCount        int     `json:"cn_count"`
	ProductCount   int     `json:"product_count"`
	OrderCount     int     `json:"order_count"`
	OrderItemCount int     `json:"order_item_count"`
	TotalQuantity  int     `json:"total_quantity"`
	TotalAmount    float64 `json:"total_amount"`
	ConfirmedAt    string  `json:"confirmed_at"`
}
