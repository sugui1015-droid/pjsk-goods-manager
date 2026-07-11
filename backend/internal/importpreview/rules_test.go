package importpreview

import "testing"

func TestSheetTitleRecognition(t *testing.T) {
	data := testWorkbook(t,
		testSheet{Name: "bracket", Rows: [][]any{
			{"\u301026\u611f\u8c22\u796d\u5355\u9886\u3011"},
			{nil, "\u79cd\u7c7b", "miku"},
			{nil, "\u5355\u4ef7", 10},
			{"\u603b\u91d1\u989d", "\u6635\u79f0/\u603b\u6570", 1},
			{10, "Succ", 1},
		}},
		testSheet{Name: "plain", Rows: [][]any{
			{"\u6247\u5b50\u4e13\u573a"},
			{nil, "\u79cd\u7c7b", "rin"},
			{nil, "\u5355\u4ef7", 12},
			{"\u603b\u91d1\u989d", "\u6635\u79f0/\u603b\u6570", 1},
			{12, "Alice", 1},
		}},
		testSheet{Name: "empty-first", Rows: [][]any{
			{nil, nil},
			{"\u9ebb\u5c06\u7b2c\u4e8c\u5f39"},
			{nil, "\u79cd\u7c7b", "luka"},
			{nil, "\u5355\u4ef7", 8},
			{"\u603b\u91d1\u989d", "\u6635\u79f0/\u603b\u6570", 1},
			{8, "Bob", 1},
		}},
	)

	preview, err := Parse(data, ParseOptions{Filename: "titles.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"26\u611f\u8c22\u796d\u5355\u9886", "\u6247\u5b50\u4e13\u573a", "\u9ebb\u5c06\u7b2c\u4e8c\u5f39"}
	for i, title := range want {
		if preview.Sheets[i].ID == "" {
			t.Fatalf("sheet %d id is empty", i)
		}
		if preview.Sheets[i].Title != title {
			t.Fatalf("sheet %d title = %q, want %q", i, preview.Sheets[i].Title, title)
		}
		if preview.Batches[i].SheetID != preview.Sheets[i].ID || preview.Batches[i].SheetTitle != title {
			t.Fatalf("batch sheet metadata mismatch: %#v / %#v", preview.Batches[i], preview.Sheets[i])
		}
		if len(preview.Batches[i].Details) > 0 && preview.Batches[i].Details[0].ID == "" {
			t.Fatalf("detail id is empty")
		}
	}
}

func TestApplyImportRulesExcludesSheetAndScopedCN(t *testing.T) {
	preview := rulesFixturePreview()
	adjusted, err := applyImportRules(preview, ConfirmRules{
		ExcludedSheetIDs: []string{"sheet-1"},
		ExcludedCNs: []CNExclusionRule{{
			SheetID: "sheet-2",
			BatchID: "batch-2",
			CN:      "succ",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(adjusted.Sheets) != 1 || adjusted.Sheets[0].ID != "sheet-2" {
		t.Fatalf("sheets = %#v", adjusted.Sheets)
	}
	if len(adjusted.Batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(adjusted.Batches))
	}
	for _, detail := range adjusted.Batches[0].Details {
		if detail.NormalizedCN == "succ" {
			t.Fatalf("succ should be excluded from batch-2: %#v", adjusted.Batches[0].Details)
		}
	}
	if adjusted.Batches[0].CNCount != 1 || adjusted.Batches[0].TotalQuantity != 1 || adjusted.Batches[0].CalculatedAmount != 7 {
		t.Fatalf("unexpected recalculated batch: %#v", adjusted.Batches[0])
	}
}

func TestApplyImportRulesKeepsMixedCNAndCorrectsCategory(t *testing.T) {
	preview := rulesFixturePreview()
	adjusted, err := applyImportRules(preview, ConfirmRules{
		CategoryRules: []CategoryCorrectionRule{
			{DetailIDs: []string{"detail-1"}, Category: " \u5427\u5527 "},
			{SheetID: "sheet-2", Category: "\u62cd\u7acb\u5f97"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	foundMixed := false
	for _, batch := range adjusted.Batches {
		for _, detail := range batch.Details {
			if detail.ID == "detail-1" && detail.Category != "\u5427\u5527" {
				t.Fatalf("detail-1 category = %q", detail.Category)
			}
			if detail.SheetID == "sheet-2" && detail.Category != "\u62cd\u7acb\u5f97" {
				t.Fatalf("sheet-2 category = %q", detail.Category)
			}
			if detail.OriginalCN == "\u306d\u3053(neko)\u2606" {
				foundMixed = true
			}
		}
	}
	if !foundMixed {
		t.Fatal("mixed Japanese/symbol CN should remain")
	}
}

func TestApplyImportRulesRejectsInvalidCategoryAndEmptyResult(t *testing.T) {
	preview := rulesFixturePreview()
	_, err := applyImportRules(preview, ConfirmRules{CategoryRules: []CategoryCorrectionRule{{DetailIDs: []string{"detail-1"}, Category: "12345678901234567890123456789012345678901"}}})
	if err == nil {
		t.Fatal("expected long category error")
	}

	adjusted, err := applyImportRules(preview, ConfirmRules{ExcludedSheetIDs: []string{"sheet-1", "sheet-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(adjusted.Sheets) != 0 || len(adjusted.Batches) != 0 || adjusted.File.SheetCount != 0 {
		t.Fatalf("expected empty adjusted preview, got %#v", adjusted)
	}
}

func TestApplyImportRulesDoesNotAcceptForgedAmounts(t *testing.T) {
	preview := rulesFixturePreview()
	adjusted, err := applyImportRules(preview, ConfirmRules{CategoryRules: []CategoryCorrectionRule{{DetailIDs: []string{"detail-1"}, Category: "ep"}}})
	if err != nil {
		t.Fatal(err)
	}
	if adjusted.Batches[0].Details[0].Quantity != 2 || adjusted.Batches[0].Details[0].UnitPrice != 10 || adjusted.Batches[0].Details[0].Amount != 20 {
		t.Fatalf("rules must not change quantity, price, or amount: %#v", adjusted.Batches[0].Details[0])
	}
}

func rulesFixturePreview() Preview {
	return Preview{
		File: FileInfo{OriginalFilename: "fixture.xlsx", SheetCount: 2},
		Sheets: []SheetSummary{
			{ID: "sheet-1", Name: "Sheet1", Title: "\u5427\u5527\u573a", TemplateType: templateMatrix, BatchCount: 1, TableAmount: 32, CalcAmount: 32},
			{ID: "sheet-2", Name: "Sheet2", Title: "\u8272\u7eb8\u573a", TemplateType: templateMatrix, BatchCount: 1, TableAmount: 17, CalcAmount: 17},
		},
		Batches: []BatchPreview{
			{
				ID: "batch-1", SheetID: "sheet-1", SheetName: "Sheet1", SheetTitle: "\u5427\u5527\u573a", BatchName: "batch one", TemplateType: templateMatrix, TableAmount: 32, CalculatedAmount: 32, Difference: 0,
				Details: []DetailPreview{
					{ID: "detail-1", SheetID: "sheet-1", SheetName: "Sheet1", BatchName: "batch one", Category: "\u9ed8\u8ba4", ItemName: "miku", ColumnName: "C", RowNumber: 5, OriginalCN: "Succ", NormalizedCN: "succ", Quantity: 2, UnitPrice: 10, Amount: 20, TableRowAmount: 20},
					{ID: "detail-2", SheetID: "sheet-1", SheetName: "Sheet1", BatchName: "batch one", Category: "\u9ed8\u8ba4", ItemName: "rin", ColumnName: "D", RowNumber: 6, OriginalCN: "\u306d\u3053(neko)\u2606", NormalizedCN: normalizeCN("\u306d\u3053(neko)\u2606"), Quantity: 1, UnitPrice: 12, Amount: 12, TableRowAmount: 12},
				},
			},
			{
				ID: "batch-2", SheetID: "sheet-2", SheetName: "Sheet2", SheetTitle: "\u8272\u7eb8\u573a", BatchName: "batch two", TemplateType: templateMatrix, TableAmount: 17, CalculatedAmount: 17, Difference: 0,
				Details: []DetailPreview{
					{ID: "detail-3", SheetID: "sheet-2", SheetName: "Sheet2", BatchName: "batch two", Category: "\u9ed8\u8ba4", ItemName: "luka", ColumnName: "C", RowNumber: 5, OriginalCN: "succ", NormalizedCN: "succ", Quantity: 1, UnitPrice: 10, Amount: 10, TableRowAmount: 10},
					{ID: "detail-4", SheetID: "sheet-2", SheetName: "Sheet2", BatchName: "batch two", Category: "\u9ed8\u8ba4", ItemName: "kaito", ColumnName: "D", RowNumber: 6, OriginalCN: "\u82b1\u5b50", NormalizedCN: normalizeCN("\u82b1\u5b50"), Quantity: 1, UnitPrice: 7, Amount: 7, TableRowAmount: 7},
				},
			},
		},
	}
}

func TestApplyImportRulesExcludesSpecificItemsOnly(t *testing.T) {
	preview := rulesFixturePreview()
	adjusted, err := applyImportRules(preview, ConfirmRules{ExcludedItemIDs: []string{"detail-1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range adjusted.Batches {
		for _, detail := range batch.Details {
			if detail.ID == "detail-1" {
				t.Fatal("detail-1 should be excluded")
			}
			if detail.ID == "detail-3" && detail.NormalizedCN == "succ" {
				return
			}
		}
	}
	t.Fatal("same CN in another sheet/batch should remain")
}

func TestApplyImportRulesCategoryItemIDsAlias(t *testing.T) {
	preview := rulesFixturePreview()
	adjusted, err := applyImportRules(preview, ConfirmRules{CategoryRules: []CategoryCorrectionRule{{ItemIDs: []string{"detail-4"}, Category: "\u7acb\u724c"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range adjusted.Batches {
		for _, detail := range batch.Details {
			if detail.ID == "detail-4" {
				if detail.Category != "\u7acb\u724c" {
					t.Fatalf("category = %q", detail.Category)
				}
				return
			}
		}
	}
	t.Fatal("detail-4 not found")
}

func TestApplyImportRulesSameCNDifferentBatchNotExcluded(t *testing.T) {
	preview := rulesFixturePreview()
	adjusted, err := applyImportRules(preview, ConfirmRules{ExcludedCNs: []CNExclusionRule{{SheetID: "sheet-1", BatchID: "batch-1", CN: "SUCC"}}})
	if err != nil {
		t.Fatal(err)
	}
	foundOtherSucc := false
	for _, batch := range adjusted.Batches {
		for _, detail := range batch.Details {
			if detail.ID == "detail-1" {
				t.Fatal("sheet-1 succ should be excluded")
			}
			if detail.ID == "detail-3" {
				foundOtherSucc = true
			}
		}
	}
	if !foundOtherSucc {
		t.Fatal("same CN in another sheet should not be excluded")
	}
}

func TestProductCategoryPrefersSheetTitleOverSeriesCode(t *testing.T) {
	if got := productCategoryForColumn("ep\u91d1\u7b7e\u7206\u914d", "25c"); got != "ep" {
		t.Fatalf("category = %q, want ep", got)
	}
	if got := seriesCodeFromText("25c"); got != "25c" {
		t.Fatalf("series name = %q, want 25c", got)
	}
	if got := productCategoryForColumn("ep\u91d1\u7b7e\u7206\u914d", "\u7acb\u724c"); got != "\u7acb\u724c" {
		t.Fatalf("manual category = %q, want \u7acb\u724c", got)
	}
}

func TestProductNameAndCharacterRules(t *testing.T) {
	batch := BatchPreview{SheetTitle: "ep\u91d1\u7b7e\u7206\u914d", BatchName: "fallback"}
	detail := DetailPreview{ItemName: "mzk\u91d1\u7b7e"}
	if got := productNameForDetail(batch, detail); got != "ep\u91d1\u7b7e\u7206\u914d" {
		t.Fatalf("product name = %q", got)
	}
	if got := characterNameFromItemName("mzk\u91d1\u7b7e"); got != "mzk" {
		t.Fatalf("english character = %q", got)
	}
	if got := characterNameFromItemName("\u521d\u97f3\u672a\u6765"); got != "" {
		t.Fatalf("chinese character should be empty, got %q", got)
	}
	if got := characterNameFromItemName("\u521d\u97f3\u672a\u6765\u91d1\u7b7e"); got != "" {
		t.Fatalf("decorated chinese character should be empty, got %q", got)
	}
	if got := characterNameFromItemName("25c\u91d1\u7b7e"); got != "" {
		t.Fatalf("series-like character should be empty, got %q", got)
	}
}
