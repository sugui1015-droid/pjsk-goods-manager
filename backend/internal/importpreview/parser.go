package importpreview

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	templateMatrix         = "matrix"
	templateSimpleCNAmount = "simple_cn_amount"
	templateUnknown        = "unknown"
)

type ParseOptions struct {
	Filename string
	FileHash string
	Size     int64
}

func Parse(data []byte, options ParseOptions) (Preview, error) {
	book, err := readWorkbook(data)
	if err != nil {
		return Preview{}, err
	}

	preview := Preview{
		File: FileInfo{
			OriginalFilename: options.Filename,
			SHA256:           options.FileHash,
			SizeBytes:        options.Size,
			SheetCount:       len(book.Sheets),
		},
	}

	contentHashes := map[string]int{}
	for sheetIndex, currentSheet := range book.Sheets {
		sheetPreview, batches := parseSheet(currentSheet, sheetIndex+1, options.Filename)
		for index := range batches {
			hashCount := contentHashes[batches[index].ContentHash]
			if hashCount > 0 {
				batches[index].DuplicateInFile = true
				batches[index].Warnings = append(batches[index].Warnings, Issue{
					Level:     "warning",
					Code:      "duplicate_block_in_file",
					Message:   "This batch has the same content hash as another batch in the uploaded file.",
					SheetName: batches[index].SheetName,
					BatchID:   batches[index].ID,
				})
			}
			contentHashes[batches[index].ContentHash] = hashCount + 1
			preview.Errors = append(preview.Errors, batches[index].Errors...)
			preview.Warnings = append(preview.Warnings, batches[index].Warnings...)
			preview.Notices = append(preview.Notices, batches[index].Notices...)
		}
		preview.Sheets = append(preview.Sheets, sheetPreview)
		preview.Batches = append(preview.Batches, batches...)
	}

	return preview, nil
}

func parseSheet(currentSheet sheet, sheetIndex int, filename string) (SheetSummary, []BatchPreview) {
	summary := SheetSummary{
		Name:        currentSheet.Name,
		Index:       sheetIndex,
		RowCount:    currentSheet.RowCount,
		ColumnCount: currentSheet.ColCount,
	}

	var batches []BatchPreview
	imageNotices := imageFormulaNotices(currentSheet)
	if simple, ok := parseSimpleCNAmountSheet(currentSheet, imageNotices); ok {
		summary.TemplateType = templateSimpleCNAmount
		summary.BatchCount = 1
		summary.TableAmount = simple.TableAmount
		summary.CalcAmount = simple.CalculatedAmount
		summary.Difference = simple.Difference
		batches = append(batches, simple)
		return summary, batches
	}

	candidates := locateMatrixBlocks(currentSheet)
	if len(candidates) == 0 {
		summary.TemplateType = templateUnknown
		batches = append(batches, BatchPreview{
			ID:           fmt.Sprintf("%s:unknown", currentSheet.Name),
			SheetName:    currentSheet.Name,
			BatchName:    fallbackBatchName(currentSheet, 0, filename),
			TemplateType: templateUnknown,
			StartRow:     1,
			EndRow:       currentSheet.RowCount,
			ContentHash:  hashStrings(currentSheet.Name, "unknown"),
			Notices: append(imageNotices, Issue{
				Level:     "notice",
				Code:      "sheet_not_recognized",
				Message:   "Sheet did not match a supported import template.",
				SheetName: currentSheet.Name,
			}),
		})
		summary.BatchCount = 1
		return summary, batches
	}

	summary.TemplateType = templateMatrix
	for index, candidate := range candidates {
		endRow := currentSheet.RowCount
		if index+1 < len(candidates) && candidates[index+1].KindRow > candidate.KindRow {
			endRow = candidates[index+1].KindRow - 1
		}
		batch := parseMatrixBlock(currentSheet, candidate, endRow, filename)
		if len(imageNotices) > 0 {
			batch.Notices = append(batch.Notices, imageNotices...)
		}
		batches = append(batches, batch)
		summary.TableAmount += batch.TableAmount
		summary.CalcAmount += batch.CalculatedAmount
	}
	summary.BatchCount = len(batches)
	summary.Difference = round2(summary.TableAmount - summary.CalcAmount)
	return summary, batches
}

type matrixCandidate struct {
	KindRow  int
	LabelCol int
}

func locateMatrixBlocks(currentSheet sheet) []matrixCandidate {
	var candidates []matrixCandidate
	occupied := map[string]bool{}
	for row := 0; row+2 < currentSheet.RowCount; row++ {
		for col := 0; col < currentSheet.ColCount; col++ {
			if !looksLikeKindCell(currentSheet, row, col) {
				continue
			}
			if !looksLikePriceLabel(currentSheet, row+1, col) {
				continue
			}
			if countTextRight(currentSheet, row, col+1) < 1 || countNumbersRight(currentSheet, row+1, col+1) < 1 {
				continue
			}
			key := fmt.Sprintf("%d:%d", row, col)
			if occupied[key] {
				continue
			}
			candidates = append(candidates, matrixCandidate{KindRow: row, LabelCol: col})
			occupied[key] = true
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].KindRow == candidates[j].KindRow {
			return candidates[i].LabelCol < candidates[j].LabelCol
		}
		return candidates[i].KindRow < candidates[j].KindRow
	})
	return candidates
}

func parseMatrixBlock(currentSheet sheet, candidate matrixCandidate, endRow int, filename string) BatchPreview {
	kindRow := candidate.KindRow
	labelCol := candidate.LabelCol
	totalCol := labelCol - 1
	if totalCol < 0 {
		totalCol = 0
	}

	totalRow := kindRow + 2
	priceRows := []int{kindRow + 1}
	dataStart := totalRow + 1
	for dataStart < endRow && isAdjustmentPriceRow(currentSheet, dataStart, labelCol) {
		priceRows = append(priceRows, dataStart)
		dataStart++
	}

	priceTypes := make([]PriceTypeInfo, 0, len(priceRows))
	for _, row := range priceRows {
		priceTypes = append(priceTypes, priceTypeInfo(currentSheet, row, labelCol))
	}
	calcPriceRow := priceRows[0]
	calcPriceType := priceLabel(currentSheet.cellText(calcPriceRow, labelCol))
	if calcPriceType == "" {
		calcPriceType = "unit_price"
	}

	itemColumns := itemColumnsForBlock(currentSheet, kindRow, calcPriceRow, labelCol)
	batchName := fallbackBatchName(currentSheet, kindRow, filename)
	batch := BatchPreview{
		ID:                   fmt.Sprintf("%s:R%dC%d", currentSheet.Name, kindRow+1, labelCol+1),
		SheetName:            currentSheet.Name,
		BatchName:            batchName,
		TemplateType:         templateMatrix,
		StartRow:             kindRow + 1,
		EndRow:               endRow,
		CalculationPriceType: calcPriceType,
		PriceTypes:           priceTypes,
		ItemTypeCount:        len(itemColumns),
	}

	seenItemNames := map[string]int{}
	for _, item := range itemColumns {
		key := normalizeCN(item.Name)
		seenItemNames[key]++
	}
	for name, count := range seenItemNames {
		if name != "" && count > 1 {
			batch.Warnings = append(batch.Warnings, Issue{
				Level:     "warning",
				Code:      "duplicate_item_name",
				Message:   fmt.Sprintf("The same item name appears %d times in this batch; category and column position are used to keep them separate.", count),
				SheetName: currentSheet.Name,
				BatchID:   batch.ID,
			})
		}
	}

	cnRows := map[string]bool{}
	rawCNByNormalized := map[string]map[string]bool{}
	for row := dataStart; row < endRow; row++ {
		originalCN := cleanText(currentSheet.cellText(row, labelCol))
		if originalCN == "" || isReservedCNLabel(originalCN) || isPriceKeyword(originalCN) {
			continue
		}
		normalizedCN := normalizeCN(originalCN)
		if normalizedCN == "" {
			continue
		}
		cnRows[normalizedCN] = true
		if rawCNByNormalized[normalizedCN] == nil {
			rawCNByNormalized[normalizedCN] = map[string]bool{}
		}
		rawCNByNormalized[normalizedCN][originalCN] = true
		tableRowAmount := numberOrZero(currentSheet.cellNumber(row, totalCol))
		if tableRowAmount > 0 {
			batch.TableAmount += tableRowAmount
		}

		for _, item := range itemColumns {
			quantityCell := currentSheet.cell(row, item.Col)
			if isBlank(quantityCell) {
				continue
			}
			quantity, quantityOK, quantityErr := parseQuantity(quantityCell)
			if !quantityOK {
				batch.Errors = append(batch.Errors, Issue{
					Level:     "error",
					Code:      "invalid_quantity",
					Message:   quantityErr,
					SheetName: currentSheet.Name,
					BatchID:   batch.ID,
					RowNumber: row + 1,
					Column:    columnName(item.Col + 1),
				})
				continue
			}
			if quantity == 0 {
				continue
			}
			amount := round2(float64(quantity) * item.UnitPrice)
			batch.TotalQuantity += quantity
			batch.CalculatedAmount += amount
			batch.Details = append(batch.Details, DetailPreview{
				SheetName:      currentSheet.Name,
				BatchName:      batchName,
				Category:       item.Category,
				ItemName:       item.Name,
				ColumnIndex:    item.Col + 1,
				ColumnName:     columnName(item.Col + 1),
				RowNumber:      row + 1,
				OriginalCN:     originalCN,
				NormalizedCN:   normalizedCN,
				Quantity:       quantity,
				PriceType:      calcPriceType,
				UnitPrice:      item.UnitPrice,
				Amount:         amount,
				TableRowAmount: tableRowAmount,
			})
		}
	}

	for normalized, originals := range rawCNByNormalized {
		if len(originals) > 1 {
			batch.Warnings = append(batch.Warnings, Issue{
				Level:     "warning",
				Code:      "possible_duplicate_cn",
				Message:   fmt.Sprintf("CN values normalize to %q but have different original spellings; they were not permanently merged.", normalized),
				SheetName: currentSheet.Name,
				BatchID:   batch.ID,
			})
		}
	}

	batch.CNCount = len(cnRows)
	batch.TableAmount = round2(batch.TableAmount)
	batch.CalculatedAmount = round2(batch.CalculatedAmount)
	batch.Difference = round2(batch.TableAmount - batch.CalculatedAmount)
	if math.Abs(batch.Difference) > 0.01 {
		batch.Warnings = append(batch.Warnings, Issue{
			Level:     "warning",
			Code:      "amount_mismatch",
			Message:   "Table amount and calculated amount differ by more than 0.01.",
			SheetName: currentSheet.Name,
			BatchID:   batch.ID,
		})
	}

	batch.ContentHash = hashBatch(batch)
	return batch
}

type itemColumn struct {
	Col       int
	Name      string
	Category  string
	UnitPrice float64
}

func itemColumnsForBlock(currentSheet sheet, kindRow, priceRow, labelCol int) []itemColumn {
	categoryByColumn := categoryPathsForBlock(currentSheet, kindRow, priceRow, labelCol)
	var items []itemColumn
	for col := labelCol + 1; col < currentSheet.ColCount; col++ {
		if isNextMatrixBlock(currentSheet, kindRow, priceRow, labelCol, col) {
			break
		}

		name := cleanText(currentSheet.cellText(kindRow, col))
		price := currentSheet.cellNumber(priceRow, col)
		if name == "" || price == nil {
			continue
		}
		items = append(items, itemColumn{
			Col:       col,
			Name:      name,
			Category:  categoryByColumn[col],
			UnitPrice: round2(*price),
		})
	}
	return items
}

func categoryPathsForBlock(currentSheet sheet, kindRow, priceRow, labelCol int) map[int]string {
	categoryPartsByColumn := map[int][]string{}
	startRow := kindRow - 4
	if startRow < 0 {
		startRow = 0
	}

	for row := startRow; row < kindRow; row++ {
		rowValues := inheritedHeaderValues(currentSheet, row, kindRow, priceRow, labelCol)
		if len(rowValues) == 0 {
			continue
		}
		for col, value := range rowValues {
			if value == "" || containsString(categoryPartsByColumn[col], value) {
				continue
			}
			categoryPartsByColumn[col] = append(categoryPartsByColumn[col], value)
		}
	}

	categoryByColumn := map[int]string{}
	for col, parts := range categoryPartsByColumn {
		categoryByColumn[col] = strings.Join(parts, " / ")
	}
	return categoryByColumn
}

func inheritedHeaderValues(currentSheet sheet, row, kindRow, priceRow, labelCol int) map[int]string {
	values := map[int]string{}
	lastHeader := ""
	rowHasItemHeader := false
	for col := labelCol + 1; col < currentSheet.ColCount; col++ {
		if isNextMatrixBlock(currentSheet, kindRow, priceRow, labelCol, col) {
			break
		}
		text := cleanCategoryText(currentSheet.cellText(row, col))
		if text != "" {
			lastHeader = text
			rowHasItemHeader = true
		}
		if lastHeader != "" {
			values[col] = lastHeader
		}
	}
	if !rowHasItemHeader {
		return map[int]string{}
	}
	return values
}

func cleanCategoryText(value string) string {
	text := cleanText(value)
	if text == "" || isKindKeyword(text) || isPriceKeyword(text) || isReservedCNLabel(text) || isNumericText(text) {
		return ""
	}
	normalized := normalizeHeader(text)
	if normalized == "分类" || normalized == "分類" || normalized == "category" {
		return ""
	}
	return text
}

func isNextMatrixBlock(currentSheet sheet, kindRow, priceRow, labelCol, col int) bool {
	return col > labelCol+1 && isKindKeyword(currentSheet.cellText(kindRow, col)) && isPriceKeyword(currentSheet.cellText(priceRow, col))
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func priceTypeInfo(currentSheet sheet, row, labelCol int) PriceTypeInfo {
	label := priceLabel(currentSheet.cellText(row, labelCol))
	if label == "" {
		label = fmt.Sprintf("price_row_%d", row+1)
	}
	info := PriceTypeInfo{Type: label, Row: row + 1}
	for col := labelCol + 1; col < currentSheet.ColCount; col++ {
		if number := currentSheet.cellNumber(row, col); number != nil {
			info.UnitCount++
			info.Values = append(info.Values, round2(*number))
		}
	}
	return info
}

func parseSimpleCNAmountSheet(currentSheet sheet, notices []Issue) (BatchPreview, bool) {
	for row := 0; row < currentSheet.RowCount; row++ {
		cnCol, amountCol, gCol := -1, -1, -1
		for col := 0; col < currentSheet.ColCount; col++ {
			label := normalizeHeader(currentSheet.cellText(row, col))
			switch {
			case label == "cn":
				cnCol = col
			case label == "g":
				gCol = col
			case isTotalAmountHeader(label):
				amountCol = col
			}
		}
		if cnCol >= 0 && gCol >= 0 && amountCol < 0 {
			for col := 0; col < currentSheet.ColCount; col++ {
				if col != cnCol && col != gCol && hasNumericDataBelow(currentSheet, row, col) {
					amountCol = col
					break
				}
			}
		}
		if cnCol < 0 || gCol < 0 || amountCol < 0 {
			continue
		}
		cnSet := map[string]bool{}
		tableAmount := 0.0
		for dataRow := row + 1; dataRow < currentSheet.RowCount; dataRow++ {
			cn := cleanText(currentSheet.cellText(dataRow, cnCol))
			if cn == "" {
				continue
			}
			cnSet[normalizeCN(cn)] = true
			tableAmount += numberOrZero(currentSheet.cellNumber(dataRow, amountCol))
		}
		batch := BatchPreview{
			ID:               fmt.Sprintf("%s:simple:R%d", currentSheet.Name, row+1),
			SheetName:        currentSheet.Name,
			BatchName:        currentSheet.Name,
			TemplateType:     templateSimpleCNAmount,
			StartRow:         row + 1,
			EndRow:           currentSheet.RowCount,
			ContentHash:      hashStrings(currentSheet.Name, "simple", fmt.Sprint(row), fmt.Sprint(tableAmount), fmt.Sprint(len(cnSet))),
			CNCount:          len(cnSet),
			TableAmount:      round2(tableAmount),
			Notices:          append([]Issue{}, notices...),
			PriceTypes:       nil,
			Details:          nil,
			ItemTypeCount:    0,
			TotalQuantity:    0,
			CalculatedAmount: 0,
			Difference:       round2(tableAmount),
		}
		batch.Notices = append(batch.Notices, Issue{
			Level:     "notice",
			Code:      "simple_cn_amount_not_converted",
			Message:   "Sheet contains only CN and amount columns, so it is recognized but not converted into order items.",
			SheetName: currentSheet.Name,
			BatchID:   batch.ID,
		})
		return batch, true
	}
	return BatchPreview{}, false
}

func hasNumericDataBelow(currentSheet sheet, headerRow, col int) bool {
	for row := headerRow + 1; row < currentSheet.RowCount; row++ {
		if currentSheet.cellNumber(row, col) != nil {
			return true
		}
	}
	return false
}
func imageFormulaNotices(currentSheet sheet) []Issue {
	var notices []Issue
	for row := 0; row < currentSheet.RowCount; row++ {
		for col := 0; col < currentSheet.ColCount; col++ {
			formula := currentSheet.cell(row, col).Formula
			if strings.Contains(strings.ToUpper(formula), "DISPIMG") {
				notices = append(notices, Issue{
					Level:     "notice",
					Code:      "image_formula_ignored",
					Message:   "Image formula found and ignored for business-data parsing.",
					SheetName: currentSheet.Name,
					RowNumber: row + 1,
					Column:    columnName(col + 1),
				})
			}
		}
	}
	return notices
}

func looksLikeKindCell(currentSheet sheet, row, col int) bool {
	text := currentSheet.cellText(row, col)
	if isKindKeyword(text) {
		return true
	}
	return cleanText(text) != "" &&
		!isNumericText(text) &&
		!isReservedCNLabel(text) &&
		countTextRight(currentSheet, row, col+1) >= 1 &&
		looksLikePriceLabel(currentSheet, row+1, col)
}

func looksLikePriceLabel(currentSheet sheet, row, col int) bool {
	if row < 0 || row >= currentSheet.RowCount || col < 0 || col >= currentSheet.ColCount {
		return false
	}
	text := currentSheet.cellText(row, col)
	return isPriceKeyword(text) && countNumbersRight(currentSheet, row, col+1) >= 1
}

func isAdjustmentPriceRow(currentSheet sheet, row, labelCol int) bool {
	if row < 0 || row >= currentSheet.RowCount {
		return false
	}
	label := currentSheet.cellText(row, labelCol)
	if isReservedCNLabel(label) {
		return false
	}
	if isPriceKeyword(label) && countNumbersRight(currentSheet, row, labelCol+1) >= 1 {
		return true
	}
	if currentSheet.cellText(row, labelCol-1) == "" &&
		cleanText(label) != "" &&
		countNumbersRight(currentSheet, row, labelCol+1) >= 1 &&
		!hasLikelyQuantityCells(currentSheet, row, labelCol+1) {
		return true
	}
	return false
}

func hasLikelyQuantityCells(currentSheet sheet, row, startCol int) bool {
	count := 0
	for col := startCol; col < currentSheet.ColCount; col++ {
		quantity, ok, _ := parseQuantity(currentSheet.cell(row, col))
		if ok && quantity > 0 {
			count++
		}
	}
	return count >= 1
}

func countTextRight(currentSheet sheet, row, startCol int) int {
	count := 0
	if row < 0 || row >= currentSheet.RowCount {
		return 0
	}
	for col := startCol; col < currentSheet.ColCount; col++ {
		text := cleanText(currentSheet.cellText(row, col))
		if text != "" && !isNumericText(text) {
			count++
		}
	}
	return count
}

func countNumbersRight(currentSheet sheet, row, startCol int) int {
	count := 0
	if row < 0 || row >= currentSheet.RowCount {
		return 0
	}
	for col := startCol; col < currentSheet.ColCount; col++ {
		if currentSheet.cellNumber(row, col) != nil {
			count++
		}
	}
	return count
}

func fallbackBatchName(currentSheet sheet, kindRow int, filename string) string {
	for row := kindRow - 1; row >= 0 && row >= kindRow-4; row-- {
		for col := 0; col < currentSheet.ColCount; col++ {
			text := cleanText(currentSheet.cellText(row, col))
			if text != "" && !isKindKeyword(text) && !isPriceKeyword(text) && !isReservedCNLabel(text) {
				return text
			}
		}
	}
	if currentSheet.Name != "" {
		return currentSheet.Name
	}
	return strings.TrimSuffix(filename, ".xlsx")
}

func parseQuantity(value cell) (int, bool, string) {
	if isBlank(value) {
		return 0, true, ""
	}
	if value.Number != nil {
		number := *value.Number
		if number < 0 {
			return 0, false, "Quantity must not be negative."
		}
		if math.Abs(number-math.Round(number)) > 0.0000001 {
			return 0, false, "Quantity must be a whole number."
		}
		return int(math.Round(number)), true, ""
	}
	text := strings.TrimSpace(value.Text)
	if text == "" {
		return 0, true, ""
	}
	number, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", ""), 64)
	if err != nil {
		return 0, false, "Quantity must be a non-negative integer, not text."
	}
	if number < 0 {
		return 0, false, "Quantity must not be negative."
	}
	if math.Abs(number-math.Round(number)) > 0.0000001 {
		return 0, false, "Quantity must be a whole number."
	}
	return int(math.Round(number)), true, ""
}

func normalizeCN(value string) string {
	value = strings.TrimSpace(value)
	value = whitespacePattern.ReplaceAllString(value, " ")
	return strings.ToLower(value)
}

func normalizeHeader(value string) string {
	return strings.ToLower(strings.ReplaceAll(cleanText(value), " ", ""))
}

var whitespacePattern = regexp.MustCompile(`\s+`)

func cleanText(value string) string {
	return strings.TrimSpace(whitespacePattern.ReplaceAllString(value, " "))
}

func isKindKeyword(value string) bool {
	normalized := normalizeHeader(value)
	return normalized == "种类" || normalized == "種類" || normalized == "角色" || normalized == "款式"
}

func isPriceKeyword(value string) bool {
	return priceLabel(value) != ""
}

func priceLabel(value string) string {
	normalized := normalizeHeader(value)
	switch normalized {
	case "单价", "單價", "价格", "價格", "均价", "price":
		return "unit_price"
	case "调价", "調價":
		return "adjustment"
	case "一调", "一調":
		return "adjustment_1"
	case "二调", "二調":
		return "adjustment_2"
	case "实际退差单价", "實際退差單價":
		return "actual_refund_difference_unit_price"
	case "����":
		return "unit_price"
	case "ʵ���˲��":
		return "actual_refund_difference_unit_price"
	}
	return ""
}

func isReservedCNLabel(value string) bool {
	normalized := normalizeHeader(value)
	switch normalized {
	case "昵称/总数", "昵称总数", "昵称", "總數", "总数", "cn", "�ǳ�/����":
		return true
	default:
		return false
	}
}

func isTotalAmountHeader(value string) bool {
	switch value {
	case "总肾", "总金额", "总金額", "金额", "合计", "总计", "����":
		return true
	default:
		return false
	}
}

func isNumericText(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.ParseFloat(strings.ReplaceAll(cleanText(value), ",", ""), 64)
	return err == nil
}

func isBlank(value cell) bool {
	return cleanText(value.Text) == "" && value.Number == nil && value.Formula == ""
}

func numberOrZero(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func hashBatch(batch BatchPreview) string {
	parts := []string{
		batch.SheetName,
		batch.BatchName,
		batch.TemplateType,
		fmt.Sprint(batch.StartRow),
		fmt.Sprint(batch.EndRow),
		batch.CalculationPriceType,
	}
	for _, detail := range batch.Details {
		parts = append(parts,
			detail.NormalizedCN,
			detail.Category,
			detail.ItemName,
			fmt.Sprint(detail.ColumnIndex),
			fmt.Sprint(detail.RowNumber),
			fmt.Sprint(detail.Quantity),
			fmt.Sprintf("%.2f", detail.UnitPrice),
		)
	}
	return hashStrings(parts...)
}

func hashStrings(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (s sheet) cell(row, col int) cell {
	if row < 0 || row >= len(s.Cells) || col < 0 || col >= len(s.Cells[row]) {
		return cell{}
	}
	return s.Cells[row][col]
}

func (s sheet) cellText(row, col int) string {
	return s.cell(row, col).Text
}

func (s sheet) cellNumber(row, col int) *float64 {
	return s.cell(row, col).Number
}
