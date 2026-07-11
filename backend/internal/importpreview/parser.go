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
	"unicode/utf8"
)

const (
	templateMatrix         = "matrix"
	templateStandardImport = "standard_import"
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
					Level:     IssueLevelWarning,
					Code:      "duplicate_block_in_file",
					Message:   "该批次内容与同一文件中的其他批次完全相同，系统会用位置区分。",
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
	if totalPreviewDetails(preview) == 0 {
		preview.Errors = append(preview.Errors, Issue{
			Level:   IssueLevelFatalError,
			Code:    "no_valid_order_items",
			Message: "未识别到任何可导入的有效订单明细，请检查表头、CN 行、角色行、数量和价格格式。",
		})
	}

	return preview, nil
}

func totalPreviewDetails(preview Preview) int {
	total := 0
	for _, batch := range preview.Batches {
		total += len(batch.Details)
	}
	return total
}

func parseSheet(currentSheet sheet, sheetIndex int, filename string) (SheetSummary, []BatchPreview) {
	summary := SheetSummary{
		ID:          sheetID(currentSheet.Name, sheetIndex),
		Name:        currentSheet.Name,
		Title:       sheetTitle(currentSheet),
		Index:       sheetIndex,
		RowCount:    currentSheet.RowCount,
		ColumnCount: currentSheet.ColCount,
	}

	var batches []BatchPreview
	imageNotices := imageFormulaNotices(currentSheet)
	if standard, ok := parseStandardImportSheet(currentSheet, imageNotices, summary.ID, summary.Title); ok {
		summary.Title = standard.SheetTitle
		summary.TemplateType = templateStandardImport
		summary.BatchCount = 1
		summary.TableAmount = standard.TableAmount
		summary.CalcAmount = standard.CalculatedAmount
		summary.Difference = standard.Difference
		batches = append(batches, standard)
		return summary, batches
	}
	if simple, ok := parseSimpleCNAmountSheet(currentSheet, imageNotices, summary.ID, summary.Title); ok {
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
			SheetID:      summary.ID,
			SheetName:    currentSheet.Name,
			SheetTitle:   summary.Title,
			BatchName:    fallbackBatchName(currentSheet, 0, filename),
			TemplateType: templateUnknown,
			StartRow:     1,
			EndRow:       currentSheet.RowCount,
			ContentHash:  hashStrings(currentSheet.Name, "unknown"),
			Notices: append(imageNotices, Issue{
				Level:     IssueLevelNotice,
				Code:      "sheet_not_recognized",
				Message:   "该工作表未匹配到当前支持的导入模板，已跳过业务明细解析。",
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
		batch := parseMatrixBlock(currentSheet, candidate, endRow, filename, summary.ID, summary.Title)
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

func sheetID(name string, index int) string {
	return fmt.Sprintf("sheet-%d-%s", index, hashStrings(name)[:8])
}

func sheetTitle(currentSheet sheet) string {
	for row := 0; row < currentSheet.RowCount && row < 8; row++ {
		for col := 0; col < currentSheet.ColCount; col++ {
			text := cleanTitleText(currentSheet.cellText(row, col))
			if text == "" {
				continue
			}
			if extracted := bracketTitle(text); extracted != "" {
				return extracted
			}
			if isLikelySheetTitle(text) {
				return text
			}
		}
	}
	return currentSheet.Name
}

func bracketTitle(text string) string {
	start := strings.Index(text, "\u3010")
	end := strings.Index(text, "\u3011")
	if start >= 0 && end > start+len("\u3010") {
		return cleanText(text[start+len("\u3010") : end])
	}
	return ""
}

func cleanTitleText(value string) string {
	return strings.Trim(cleanText(value), "\uFF1A: -_\u2014")
}

func isLikelySheetTitle(text string) bool {
	if text == "" || isKindKeyword(text) || isPriceKeyword(text) || isReservedCNLabel(text) || isTotalAmountHeader(normalizeHeader(text)) || isNumericText(text) {
		return false
	}
	normalized := normalizeHeader(text)
	if normalized == "\u5206\u7c7b" || normalized == "\u54c1\u7c7b" || normalized == "category" || normalized == "g" || normalized == "sheet" {
		return false
	}
	return len([]rune(text)) >= 2
}

func detailID(batchID string, row int, col int, originalCN string, itemName string) string {
	return hashStrings(batchID, fmt.Sprint(row+1), fmt.Sprint(col+1), normalizeCN(originalCN), itemName)[:24]
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

func parseMatrixBlock(currentSheet sheet, candidate matrixCandidate, endRow int, filename string, sheetID string, title string) BatchPreview {
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

	itemColumns := itemColumnsForBlock(currentSheet, kindRow, calcPriceRow, labelCol, title)
	batchName := fallbackBatchName(currentSheet, kindRow, filename)
	batch := BatchPreview{
		ID:                   fmt.Sprintf("%s:R%dC%d", currentSheet.Name, kindRow+1, labelCol+1),
		SheetID:              sheetID,
		SheetName:            currentSheet.Name,
		SheetTitle:           title,
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
				Level:     IssueLevelWarning,
				Code:      "duplicate_item_name",
				Message:   fmt.Sprintf("同一批次内同名种类出现 %d 次，系统会用分类和列位置区分，不会直接覆盖。", count),
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
					Level:     IssueLevelRowError,
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
				ID:              detailID(batch.ID, row, item.Col, originalCN, item.Name),
				SheetID:         sheetID,
				SheetName:       currentSheet.Name,
				SheetTitle:      title,
				BatchName:       batchName,
				GoodsSeriesName: title,
				ProductCategory: item.Category,
				SeriesCode:      item.SeriesName,
				DisplayName:     goodsDisplayName(title, item.Category),
				CharacterName:   item.Character,
				Category:        item.Category,
				SeriesName:      item.SeriesName,
				ItemName:        item.Name,
				ColumnIndex:     item.Col + 1,
				ColumnName:      columnName(item.Col + 1),
				RowNumber:       row + 1,
				OriginalCN:      originalCN,
				NormalizedCN:    normalizedCN,
				Quantity:        quantity,
				PriceType:       calcPriceType,
				UnitPrice:       item.UnitPrice,
				Amount:          amount,
				TableRowAmount:  tableRowAmount,
			})
		}
	}

	for normalized, originals := range rawCNByNormalized {
		if len(originals) > 1 {
			batch.Warnings = append(batch.Warnings, Issue{
				Level:     IssueLevelWarning,
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
			Level:     IssueLevelWarning,
			Code:      "amount_mismatch",
			Message:   "表格原金额与程序计算金额差额超过 0.01，请人工核对。",
			SheetName: currentSheet.Name,
			BatchID:   batch.ID,
		})
	}

	batch.ContentHash = hashBatch(batch)
	return batch
}

type itemColumn struct {
	Col        int
	Name       string
	Category   string
	SeriesName string
	Character  string
	UnitPrice  float64
}

func itemColumnsForBlock(currentSheet sheet, kindRow, priceRow, labelCol int, sheetTitle string) []itemColumn {
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
			Col:        col,
			Name:       name,
			Category:   productCategoryForColumn(sheetTitle, categoryByColumn[col]),
			SeriesName: seriesCodeFromText(categoryByColumn[col]),
			Character:  characterNameFromItemName(name),
			UnitPrice:  round2(*price),
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
	if normalized == "\u5206\u7c7b" || normalized == "\u54c1\u7c7b" || normalized == "category" {
		return ""
	}
	return text
}

func seriesCodeFromText(value string) string {
	for _, part := range strings.Split(cleanText(value), "/") {
		part = strings.ToLower(strings.TrimSpace(part))
		if isSeriesLikeCategory(part) {
			return part
		}
	}
	return ""
}
func goodsDisplayName(goodsSeriesName string, productCategory string) string {
	name := cleanText(goodsSeriesName)
	category := cleanText(productCategory)
	if category == "" || category == "\u9ed8\u8ba4\u5206\u7c7b" {
		return name
	}
	if name == "" {
		return category
	}
	return name + "-" + category
}

type characterParseResult struct {
	Raw       string
	Character string
	Variant   string
	Exact     bool
}

func parseCharacterName(itemName string) characterParseResult {
	raw := cleanText(itemName)
	text := strings.ToLower(strings.TrimSpace(raw))
	if text == "" {
		return characterParseResult{Raw: raw}
	}
	if allowedCharacterNames[text] {
		return characterParseResult{Raw: raw, Character: text, Exact: true}
	}
	for _, name := range characterNameOrder {
		if strings.HasSuffix(text, " "+name) {
			variant := strings.TrimSpace(strings.TrimSuffix(text, name))
			variant = strings.TrimSpace(strings.TrimRight(variant, "-_/()[]（）【】"))
			if variant != "" {
				return characterParseResult{Raw: raw, Character: name, Variant: variant}
			}
		}
	}
	for _, name := range characterNameOrder {
		if strings.HasPrefix(text, name) {
			next := strings.TrimPrefix(text, name)
			if next != "" {
				first, _ := utf8.DecodeRuneInString(next)
				if first < 'a' || first > 'z' {
					return characterParseResult{Raw: raw, Character: name}
				}
			}
		}
	}
	return characterParseResult{Raw: raw}
}

func characterNameFromItemName(itemName string) string {
	return parseCharacterName(itemName).Character
}

var characterNameOrder = []string{"meiko", "kaito", "miku", "shiho", "mnr", "airi", "khn", "akt", "toya", "tks", "emu", "nene", "rui", "knd", "mfy", "ena", "mzk", "rin", "len", "luka", "ick", "saki", "hnm", "hrk", "szk", "an"}

var allowedCharacterNames = map[string]bool{
	"miku": true, "rin": true, "len": true, "luka": true, "meiko": true, "kaito": true,
	"ick": true, "saki": true, "hnm": true, "shiho": true, "mnr": true, "hrk": true,
	"airi": true, "szk": true, "khn": true, "an": true, "akt": true, "toya": true,
	"tks": true, "emu": true, "nene": true, "rui": true, "knd": true, "mfy": true,
	"ena": true, "mzk": true,
}

func containsVariantMarker(text string) bool {
	markers := []string{"\u91d1\u7b7e", "\u94f6\u7b7e", "\u7b7e", "\u7206\u914d", "\u7279\u5178", "\u9650\u5b9a", "\u7279\u88c5", "\u666e\u901a", "\u666e\u914d"}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func productCategoryForColumn(sheetTitle string, columnCategory string) string {
	titleCategory := productCategoryFromText(sheetTitle)
	columnCategory = cleanText(columnCategory)
	if columnCategory == "" || isSeriesLikeCategory(columnCategory) {
		return titleCategory
	}
	if titleCategory != "" && productCategoryFromText(columnCategory) == "" {
		return titleCategory
	}
	return columnCategory
}

func productCategoryFromText(value string) string {
	normalized := strings.ToLower(strings.ReplaceAll(cleanText(value), " ", ""))
	if normalized == "" {
		return ""
	}
	known := []struct {
		Needle string
		Name   string
	}{
		{"吧唧", "吧唧"},
		{"badge", "吧唧"},
		{"缶", "吧唧"},
		{"ep", "ep"},
		{"色纸", "色纸"},
		{"色紙", "色纸"},
		{"立牌", "立牌"},
		{"麻将", "麻将"},
		{"麻將", "麻将"},
		{"亚克力", "亚克力"},
		{"亞克力", "亚克力"},
		{"亚克力砖", "亚克力"},
		{"砖", "砖类"},
		{"磚", "砖类"},
		{"扇子", "扇子"},
		{"圆扇", "扇子"},
		{"圓扇", "扇子"},
	}
	for _, item := range known {
		if strings.Contains(normalized, item.Needle) {
			return item.Name
		}
	}
	return ""
}

func isSeriesLikeCategory(value string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(cleanText(value), " ", ""))
	if normalized == "" {
		return false
	}
	if matched, _ := regexp.MatchString(`^\d+[a-z]$`, normalized); matched {
		return true
	}
	if matched, _ := regexp.MatchString(`^\d+(st|nd|rd|th)$`, normalized); matched {
		return true
	}
	return false
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

func parseStandardImportSheet(currentSheet sheet, notices []Issue, sheetID string, title string) (BatchPreview, bool) {
	if !isStandardImportSheet(currentSheet) {
		return BatchPreview{}, false
	}
	goodsSeriesName := bracketTitle(currentSheet.cellText(0, 0))
	if goodsSeriesName == "" || goodsSeriesName == "\u8c37\u5b50\u7cfb\u5217\u540d" {
		goodsSeriesName = cleanStandardTitle(currentSheet.cellText(0, 0))
	}
	if goodsSeriesName == "" || goodsSeriesName == "\u8c37\u5b50\u7cfb\u5217\u540d" {
		goodsSeriesName = title
	}
	defaultProductCategory := cleanStandardCategory(currentSheet.cellText(1, 2))
	batchName := goodsDisplayName(goodsSeriesName, defaultProductCategory)
	if batchName == "" {
		batchName = currentSheet.Name
	}

	itemColumns := []itemColumn{}
	columnErrors := []Issue{}
	categoryByColumn := standardCategoryByColumn(currentSheet, defaultProductCategory)
	for col := 2; col < currentSheet.ColCount; col++ {
		itemName := cleanText(currentSheet.cellText(2, col))
		if itemName == "" || isStandardRolePlaceholder(itemName) {
			continue
		}
		character := parseCharacterName(itemName)
		if character.Character == "" {
			columnErrors = append(columnErrors, Issue{
				Level:     IssueLevelRowError,
				Code:      "invalid_character_header",
				Message:   fmt.Sprintf("角色标题 %q 无法识别为 Project SEKAI 角色；如果该列有人填写数量，对应明细不会导入。", itemName),
				SheetName: currentSheet.Name,
				Column:    columnName(col + 1),
				RowNumber: 3,
			})
			continue
		}
		unitPrice := currentSheet.cellNumber(3, col)
		if unitPrice == nil {
			continue
		}
		itemColumns = append(itemColumns, itemColumn{
			Col:        col,
			Name:       itemName,
			UnitPrice:  round4(*unitPrice),
			Category:   categoryByColumn[col],
			SeriesName: character.Variant,
			Character:  character.Character,
		})
	}

	batch := BatchPreview{
		ID:                   fmt.Sprintf("%s:standard", currentSheet.Name),
		SheetID:              sheetID,
		SheetName:            currentSheet.Name,
		SheetTitle:           goodsSeriesName,
		BatchName:            batchName,
		TemplateType:         templateStandardImport,
		StartRow:             1,
		EndRow:               currentSheet.RowCount,
		CalculationPriceType: "unit_price",
		PriceTypes: []PriceTypeInfo{{
			Type:      "unit_price",
			Row:       4,
			UnitCount: len(itemColumns),
		}},
		ItemTypeCount: len(itemColumns),
		Notices:       append([]Issue{}, notices...),
		Errors:        columnErrors,
	}

	cnRows := map[string]bool{}
	seenCNRows := map[string]int{}
	for row := 5; row < currentSheet.RowCount; row++ {
		originalCN := cleanText(currentSheet.cellText(row, 1))
		if originalCN == "" || isReservedCNLabel(originalCN) || isPriceKeyword(originalCN) {
			continue
		}
		normalizedCN := normalizeCN(originalCN)
		if normalizedCN == "" {
			continue
		}
		if firstRow, ok := seenCNRows[normalizedCN]; ok {
			batch.Warnings = append(batch.Warnings, Issue{
				Level:     IssueLevelWarning,
				Code:      "duplicate_cn_in_standard_sheet",
				Message:   fmt.Sprintf("CN %q 在当前标准模板工作表中重复出现，第 %d 行已跳过，保留第 %d 行。", originalCN, row+1, firstRow),
				SheetName: currentSheet.Name,
				BatchID:   batch.ID,
				RowNumber: row + 1,
			})
			continue
		}
		seenCNRows[normalizedCN] = row + 1
		cnRows[normalizedCN] = true
		tableRowAmount := numberOrZero(currentSheet.cellNumber(row, 0))
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
					Level:     IssueLevelRowError,
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
				ID:              detailID(batch.ID, row, item.Col, originalCN, item.Name),
				SheetID:         sheetID,
				SheetName:       currentSheet.Name,
				SheetTitle:      goodsSeriesName,
				BatchName:       batchName,
				GoodsSeriesName: goodsSeriesName,
				ProductCategory: item.Category,
				SeriesCode:      item.SeriesName,
				DisplayName:     goodsDisplayName(goodsSeriesName, item.Category),
				CharacterName:   item.Character,
				Category:        item.Category,
				SeriesName:      item.SeriesName,
				ItemName:        item.Name,
				ColumnIndex:     item.Col + 1,
				ColumnName:      columnName(item.Col + 1),
				RowNumber:       row + 1,
				OriginalCN:      originalCN,
				NormalizedCN:    normalizedCN,
				Quantity:        quantity,
				PriceType:       "unit_price",
				UnitPrice:       item.UnitPrice,
				Amount:          amount,
				TableRowAmount:  tableRowAmount,
			})
		}
	}

	batch.CNCount = len(cnRows)
	batch.TableAmount = round2(batch.TableAmount)
	batch.CalculatedAmount = round2(batch.CalculatedAmount)
	batch.Difference = round2(batch.TableAmount - batch.CalculatedAmount)
	if math.Abs(batch.Difference) > 0.01 {
		batch.Warnings = append(batch.Warnings, Issue{
			Level:     IssueLevelWarning,
			Code:      "amount_mismatch",
			Message:   "表格原金额与程序计算金额差额超过 0.01，请人工核对。",
			SheetName: currentSheet.Name,
			BatchID:   batch.ID,
		})
	}
	batch.ContentHash = hashBatch(batch)
	return batch, true
}

func standardCategoryByColumn(currentSheet sheet, defaultCategory string) map[int]string {
	categories := map[int]string{}
	current := defaultCategory
	for col := 2; col < currentSheet.ColCount; col++ {
		if category := cleanStandardCategory(currentSheet.cellText(1, col)); category != "" {
			current = category
		}
		categories[col] = current
	}
	return categories
}

func isStandardImportSheet(currentSheet sheet) bool {
	return strings.Contains(currentSheet.cellText(0, 0), "\u6c47\u603b\u8be6\u60c5") &&
		normalizeHeader(currentSheet.cellText(1, 1)) == "\u5206\u7c7b" &&
		isKindKeyword(currentSheet.cellText(2, 1)) &&
		isPriceKeyword(currentSheet.cellText(3, 1)) &&
		isTotalAmountHeader(normalizeHeader(currentSheet.cellText(4, 0))) &&
		isReservedCNLabel(currentSheet.cellText(4, 1))
}

func cleanStandardTitle(value string) string {
	if extracted := bracketTitle(value); extracted != "" {
		return extracted
	}
	text := cleanTitleText(value)
	if index := strings.Index(text, "\u6c47\u603b\u8be6\u60c5"); index >= 0 {
		text = text[:index]
	}
	text = strings.Trim(text, "\uff0c, \uff1a:")
	return cleanText(text)
}

func cleanStandardCategory(value string) string {
	category := cleanText(value)
	if category == "" || category == "\u5236\u54c1\u540d\u6216\u9ed8\u8ba4\u5206\u7c7b" {
		return ""
	}
	return category
}

func isStandardRolePlaceholder(value string) bool {
	normalized := normalizeHeader(value)
	return normalized == "\u53ef\u7ee7\u7eed\u586b\u5199\u89d2\u8272\u540d" || normalized == "\u89d2\u8272\u540d" || normalized == ""
}

func parseSimpleCNAmountSheet(currentSheet sheet, notices []Issue, sheetID string, title string) (BatchPreview, bool) {
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
			SheetID:          sheetID,
			SheetName:        currentSheet.Name,
			SheetTitle:       title,
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
			Level:     IssueLevelNotice,
			Code:      "simple_cn_amount_not_converted",
			Message:   "该工作表只有 CN 和金额列，仅做预览，不转换为订单明细。",
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
					Level:     IssueLevelNotice,
					Code:      "image_formula_ignored",
					Message:   "检测到图片公式，当前仅忽略图片公式，不作为业务数据解析。",
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
			return 0, false, "数量不能为负数。"
		}
		if math.Abs(number-math.Round(number)) > 0.0000001 {
			return 0, false, "数量必须是整数。"
		}
		return int(math.Round(number)), true, ""
	}
	text := strings.TrimSpace(value.Text)
	if text == "" {
		return 0, true, ""
	}
	number, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", ""), 64)
	if err != nil {
		return 0, false, "数量必须是非负整数，不能是文本。"
	}
	if number < 0 {
		return 0, false, "数量不能为负数。"
	}
	if math.Abs(number-math.Round(number)) > 0.0000001 {
		return 0, false, "数量必须是整数。"
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
	return normalized == "\u79cd\u7c7b" || normalized == "\u7a2e\u985e" || normalized == "\u89d2\u8272" || normalized == "\u6b3e\u5f0f" || normalized == "category"
}

func isPriceKeyword(value string) bool {
	return priceLabel(value) != ""
}

func priceLabel(value string) string {
	normalized := normalizeHeader(value)
	switch normalized {
	case "\u5355\u4ef7", "\u55ae\u50f9", "\u4ef7\u683c", "\u50f9\u683c", "\u5747\u4ef7", "price":
		return "unit_price"
	case "\u8c03\u4ef7", "\u8abf\u50f9":
		return "adjustment"
	case "\u4e00\u8c03", "\u4e00\u8abf":
		return "adjustment_1"
	case "\u4e8c\u8c03", "\u4e8c\u8abf":
		return "adjustment_2"
	case "\u5b9e\u9645\u9000\u5dee\u5355\u4ef7", "\u5be6\u969b\u9000\u5dee\u55ae\u50f9":
		return "actual_refund_difference_unit_price"
	}
	return ""
}

func isReservedCNLabel(value string) bool {
	normalized := normalizeHeader(value)
	switch normalized {
	case "\u6635\u79f0/\u603b\u6570", "\u6635\u79f0\u603b\u6570", "\u6635\u79f0", "\u66b1\u7a31/\u7e3d\u6578", "\u66b1\u7a31\u7e3d\u6578", "\u66b1\u7a31", "\u603b\u6570", "\u7e3d\u6578", "cn":
		return true
	default:
		return false
	}
}

func isTotalAmountHeader(value string) bool {
	switch value {
	case "\u603b\u80be", "\u7e3d\u80be", "\u603b\u91d1\u989d", "\u7e3d\u91d1\u984d", "\u91d1\u989d", "\u91d1\u984d", "\u5408\u8ba1", "\u5408\u8a08", "\u603b\u8ba1", "\u7e3d\u8a08":
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

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
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
			detail.GoodsSeriesName,
			detail.ProductCategory,
			detail.SeriesCode,
			detail.DisplayName,
			detail.CharacterName,
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
