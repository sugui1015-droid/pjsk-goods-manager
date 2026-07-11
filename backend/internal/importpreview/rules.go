package importpreview

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidImportRules = errors.New("invalid import adjustment rules")

const maxCorrectedCategoryLength = 40

func applyImportRules(preview Preview, rules ConfirmRules) (Preview, error) {
	excludedSheets := map[string]bool{}
	for _, id := range rules.ExcludedSheetIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			excludedSheets[id] = true
		}
	}

	categoryRules := make([]CategoryCorrectionRule, 0, len(rules.CategoryRules))
	excludedItems := map[string]bool{}
	for _, id := range rules.ExcludedItemIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			excludedItems[id] = true
		}
	}

	for _, rule := range rules.CategoryRules {
		category := cleanText(rule.Category)
		if category == "" {
			continue
		}
		if len([]rune(category)) > maxCorrectedCategoryLength {
			return Preview{}, fmt.Errorf("%w: category is too long", ErrInvalidImportRules)
		}
		rule.Category = category
		rule.DetailIDs = append(rule.DetailIDs, rule.ItemIDs...)
		categoryRules = append(categoryRules, rule)
	}

	adjusted := preview
	adjusted.Batches = nil
	adjusted.Sheets = nil
	adjusted.Errors = nil
	adjusted.Warnings = nil
	adjusted.Notices = nil

	activeSheetIDs := map[string]bool{}
	for _, batch := range preview.Batches {
		if excludedSheets[sheetRuleKey(batch.SheetID, batch.SheetName)] || excludedSheets[batch.SheetName] {
			continue
		}
		batch = applyBatchRules(batch, rules.ExcludedCNs, excludedItems, categoryRules)
		if len(batch.Details) == 0 && batch.TemplateType == templateMatrix {
			continue
		}
		recalculateBatch(&batch)
		activeSheetIDs[sheetRuleKey(batch.SheetID, batch.SheetName)] = true
		adjusted.Batches = append(adjusted.Batches, batch)
		adjusted.Errors = append(adjusted.Errors, batch.Errors...)
		adjusted.Warnings = append(adjusted.Warnings, batch.Warnings...)
		adjusted.Notices = append(adjusted.Notices, batch.Notices...)
	}

	for _, sheet := range preview.Sheets {
		key := sheetRuleKey(sheet.ID, sheet.Name)
		if excludedSheets[key] || excludedSheets[sheet.Name] || !activeSheetIDs[key] {
			continue
		}
		recalculateSheet(&sheet, adjusted.Batches)
		adjusted.Sheets = append(adjusted.Sheets, sheet)
	}
	adjusted.File.SheetCount = len(adjusted.Sheets)
	return adjusted, nil
}

func applyBatchRules(batch BatchPreview, exclusions []CNExclusionRule, excludedItems map[string]bool, categoryRules []CategoryCorrectionRule) BatchPreview {
	filtered := batch.Details[:0]
	for _, detail := range batch.Details {
		if excludedItems[detail.ID] || shouldExcludeDetail(batch, detail, exclusions) {
			continue
		}
		detail.Category = correctedCategory(batch, detail, categoryRules)
		detail.ProductCategory = detail.Category
		detail.DisplayName = goodsDisplayName(detail.GoodsSeriesName, detail.ProductCategory)
		filtered = append(filtered, detail)
	}
	batch.Details = filtered
	return batch
}

func shouldExcludeDetail(batch BatchPreview, detail DetailPreview, exclusions []CNExclusionRule) bool {
	for _, rule := range exclusions {
		cn := normalizeCN(rule.CN)
		if cn == "" || cn != detail.NormalizedCN {
			continue
		}
		if !sheetMatches(rule.SheetID, detail.SheetID, detail.SheetName) {
			continue
		}
		if rule.BatchID != "" && rule.BatchID != batch.ID {
			continue
		}
		return true
	}
	return false
}

func correctedCategory(batch BatchPreview, detail DetailPreview, rules []CategoryCorrectionRule) string {
	category := detail.Category
	for _, rule := range rules {
		if len(rule.DetailIDs) > 0 && !containsString(rule.DetailIDs, detail.ID) {
			continue
		}
		if len(rule.DetailIDs) == 0 {
			if !sheetMatches(rule.SheetID, detail.SheetID, detail.SheetName) {
				continue
			}
			if rule.BatchID != "" && rule.BatchID != batch.ID {
				continue
			}
		}
		category = rule.Category
	}
	return category
}

func recalculateBatch(batch *BatchPreview) {
	batch.CNCount = 0
	batch.ItemTypeCount = 0
	batch.TotalQuantity = 0
	batch.TableAmount = 0
	batch.CalculatedAmount = 0
	batch.Difference = 0

	cnSet := map[string]bool{}
	itemSet := map[string]bool{}
	rowAmountSet := map[string]float64{}
	for _, detail := range batch.Details {
		cnSet[detail.NormalizedCN] = true
		itemSet[hashStrings(detail.Category, detail.ItemName, detail.ColumnName, detail.PriceType, fmt.Sprintf("%.2f", detail.UnitPrice))] = true
		batch.TotalQuantity += detail.Quantity
		batch.CalculatedAmount = round2(batch.CalculatedAmount + detail.Amount)
		rowKey := fmt.Sprintf("%s:%d:%s", detail.SheetName, detail.RowNumber, detail.NormalizedCN)
		if _, ok := rowAmountSet[rowKey]; !ok {
			rowAmountSet[rowKey] = detail.TableRowAmount
		}
	}
	for _, amount := range rowAmountSet {
		batch.TableAmount = round2(batch.TableAmount + amount)
	}
	batch.CNCount = len(cnSet)
	batch.ItemTypeCount = len(itemSet)
	batch.CalculatedAmount = round2(batch.CalculatedAmount)
	batch.TableAmount = round2(batch.TableAmount)
	batch.Difference = round2(batch.TableAmount - batch.CalculatedAmount)
	batch.ContentHash = hashBatch(*batch)
}

func recalculateSheet(sheet *SheetSummary, batches []BatchPreview) {
	sheet.BatchCount = 0
	sheet.TableAmount = 0
	sheet.CalcAmount = 0
	sheet.Difference = 0
	for _, batch := range batches {
		if sheetRuleKey(batch.SheetID, batch.SheetName) != sheetRuleKey(sheet.ID, sheet.Name) {
			continue
		}
		sheet.BatchCount++
		sheet.TableAmount = round2(sheet.TableAmount + batch.TableAmount)
		sheet.CalcAmount = round2(sheet.CalcAmount + batch.CalculatedAmount)
	}
	sheet.Difference = round2(sheet.TableAmount - sheet.CalcAmount)
}

func sheetMatches(ruleSheetID string, detailSheetID string, detailSheetName string) bool {
	if ruleSheetID == "" {
		return true
	}
	return ruleSheetID == detailSheetID || ruleSheetID == detailSheetName
}

func sheetRuleKey(id string, name string) string {
	if strings.TrimSpace(id) != "" {
		return id
	}
	return name
}
