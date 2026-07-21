package importpreview

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseStandardImportTemplate(t *testing.T) {
	goodsSeries := "\u4e09\u4e3d\u9e25"
	category := "\u8272\u7eb8"
	secondCategory := "\u5fbd\u7ae0"
	data := testWorkbook(t, testSheet{
		Name: "standard",
		Rows: [][]any{
			{"\u3010" + goodsSeries + "\u3011\u6c47\u603b\u8be6\u60c5\uff0c\u5236\u8868\u65f6\u95f4\uff1a\u7531\u5bfc\u51fa\u7a0b\u5e8f\u81ea\u52a8\u751f\u6210"},
			{nil, "\u5206\u7c7b", category, nil, secondCategory},
			{nil, "\u79cd\u7c7b", "miku", "rin", "len", "notrole"},
			{nil, "\u5355\u4ef7", 40, 50, 60, 70},
			{"\u603b\u91d1\u989d", "\u6635\u79f0/\u603b\u6570", 1, 1, 1, 1},
			{150, "Succ", 1, 1, 1, 1},
			{150, "succ", 1, 1, 1, 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "standard.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(preview.Batches))
	}
	batch := preview.Batches[0]
	if batch.TemplateType != templateStandardImport {
		t.Fatalf("template = %s", batch.TemplateType)
	}
	if batch.SheetTitle != goodsSeries || batch.BatchName != goodsSeries+"-"+category {
		t.Fatalf("title/batch = %q / %q", batch.SheetTitle, batch.BatchName)
	}
	if batch.CNCount != 1 || batch.TotalQuantity != 3 || len(batch.Details) != 3 {
		t.Fatalf("summary cn=%d qty=%d details=%d", batch.CNCount, batch.TotalQuantity, len(batch.Details))
	}
	if got := batch.Details[0].GoodsSeriesName; got != goodsSeries {
		t.Fatalf("goods series = %q", got)
	}
	if got := batch.Details[0].ProductCategory; got != category {
		t.Fatalf("product category = %q", got)
	}
	if got := batch.Details[1].ProductCategory; got != category {
		t.Fatalf("continued product category = %q", got)
	}
	if got := batch.Details[2].ProductCategory; got != secondCategory {
		t.Fatalf("second product category = %q", got)
	}
	if got := batch.Details[0].DisplayName; got != goodsSeries+"-"+category {
		t.Fatalf("display name = %q", got)
	}
	if got := batch.Details[0].CharacterName; got != "miku" {
		t.Fatalf("character = %q", got)
	}
	if len(batch.Errors) == 0 || batch.Errors[0].Code != "invalid_character_header" {
		t.Fatalf("expected invalid character error, got %#v", batch.Errors)
	}
	if len(batch.Warnings) == 0 || batch.Warnings[0].Code != "duplicate_cn_in_standard_sheet" {
		t.Fatalf("expected duplicate CN warning, got %#v", batch.Warnings)
	}
}

func TestParseStandardImportCompositeCharacters(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "standard-composite",
		Rows: [][]any{
			{"【新队服卡套单领】汇总详情"},
			{nil, "分类", "卡套", nil, nil},
			{nil, "种类", "25h miku", "ln luka", "vbs meiko"},
			{nil, "单价", 85.8, 23.32, 22.88},
			{"总金额", "昵称/总数", 1, 2, 1},
			{155.32, "Yoru", 1, 2, 0},
			{22.88, "无桦", 0, 0, 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "composite.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	batch := preview.Batches[0]
	if len(batch.Errors) != 0 {
		t.Fatalf("expected no character errors, got %#v", batch.Errors)
	}
	if len(batch.Details) != 3 || batch.TotalQuantity != 4 || batch.CalculatedAmount != 155.32 {
		t.Fatalf("summary details=%d qty=%d amount=%.2f", len(batch.Details), batch.TotalQuantity, batch.CalculatedAmount)
	}
	// 角色列头前缀是团名（group_name），不是系列；系列来自 B2「分类」那一行。
	checks := map[string]struct{ character, group string }{
		"25h miku":  {"miku", "25h"},
		"ln luka":   {"luka", "ln"},
		"vbs meiko": {"meiko", "vbs"},
	}
	for _, detail := range batch.Details {
		want := checks[detail.ItemName]
		if detail.CharacterName != want.character || detail.GroupName != want.group {
			t.Fatalf("%s character=%q group=%q", detail.ItemName, detail.CharacterName, detail.GroupName)
		}
		if detail.SeriesCode != "卡套" {
			t.Fatalf("%s series = %q, want 卡套", detail.ItemName, detail.SeriesCode)
		}
	}
}

func TestParseCharacterNameComposite(t *testing.T) {
	tests := map[string]struct{ character, variant string }{
		"miku":     {"miku", ""},
		"25h miku": {"miku", "25h"},
		"ln luka":  {"luka", "ln"},
		"mmj rin":  {"rin", "mmj"},
		"限定":       {"", ""},
	}
	for input, want := range tests {
		got := parseCharacterName(input)
		if got.Character != want.character || got.Variant != want.variant {
			t.Fatalf("%q => character=%q variant=%q", input, got.Character, got.Variant)
		}
	}
}

func TestGoodsDisplayNameSkipsDefaultCategory(t *testing.T) {
	if got := goodsDisplayName("\u4e09\u4e3d\u9e25\u8272\u7eb8", "\u9ed8\u8ba4\u5206\u7c7b"); got != "\u4e09\u4e3d\u9e25\u8272\u7eb8" {
		t.Fatalf("display name = %q", got)
	}
	if got := goodsDisplayName("\u751f\u65e5\u7fbd\u6392", "\u5fbd\u7ae0"); got != "\u751f\u65e5\u7fbd\u6392-\u5fbd\u7ae0" {
		t.Fatalf("display name = %q", got)
	}
}

func TestParseNormalMatrix(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "normal",
		Rows: [][]any{
			{"感谢祭单领"},
			{nil, "分类", "吧砖", nil},
			{nil, "种类", "miku", "rin"},
			{nil, "单价", 10, 20},
			{"总金额", "昵称/总数", 1, 2},
			{50, " Alice  A ", 1, 2},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "normal.xlsx", FileHash: "hash", Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(preview.Batches))
	}
	batch := preview.Batches[0]
	if batch.TemplateType != templateMatrix {
		t.Fatalf("template = %s", batch.TemplateType)
	}
	if batch.CNCount != 1 || batch.ItemTypeCount != 2 || batch.TotalQuantity != 3 {
		t.Fatalf("unexpected summary: %#v", batch)
	}
	if batch.CalculatedAmount != 50 || batch.TableAmount != 50 || batch.Difference != 0 {
		t.Fatalf("amounts = table %.2f calc %.2f diff %.2f", batch.TableAmount, batch.CalculatedAmount, batch.Difference)
	}
	if batch.Details[0].NormalizedCN != "alice a" {
		t.Fatalf("normalized CN = %q", batch.Details[0].NormalizedCN)
	}
}

func TestParseNestedCategoryHeaders(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "nested",
		Rows: [][]any{
			{"多层分类"},
			{nil, nil, "扇子", nil, "砖类", nil},
			{nil, "分类", "圆扇", nil, "钻砖", nil},
			{nil, "种类", "miku", "rin", "luka", "kaito"},
			{nil, "单价", 10, 20, 30, 40},
			{"总金额", "昵称/总数", 1, 1, 1, 1},
			{100, "Alice", 1, 1, 1, 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "nested.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	batch := preview.Batches[0]
	if got := batch.Details[0].Category; got != "扇子 / 圆扇" {
		t.Fatalf("first category = %q", got)
	}
	if got := batch.Details[2].Category; got != "砖类 / 钻砖" {
		t.Fatalf("third category = %q", got)
	}
}
func TestParseHorizontalMultiBlock(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "multi",
		Rows: [][]any{
			{"横向多区块"},
			{nil, "分类", "A区", nil, nil, "分类", "B区"},
			{nil, "种类", "miku", "rin", nil, "种类", "luka"},
			{nil, "单价", 10, 20, nil, "单价", 30},
			{"总金额", "昵称/总数", 1, 1, nil, "昵称/总数", 1},
			{60, "Alice", 1, 1, nil, " alice ", 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "multi.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Batches) != 2 {
		t.Fatalf("batches = %d, want 2", len(preview.Batches))
	}
	totalDetails := len(preview.Batches[0].Details) + len(preview.Batches[1].Details)
	if totalDetails != 3 {
		t.Fatalf("details = %d, want 3", totalDetails)
	}
}

func TestParseVerticalMultiBatch(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "vertical",
		Rows: [][]any{
			{"A 汇总详情"},
			{nil, "种类", "miku"},
			{nil, "单价", 10},
			{"总金额", "昵称/总数", 1},
			{10, "Alice", 1},
			{nil, nil, nil},
			{"B 汇总详情"},
			{nil, "种类", "rin"},
			{nil, "单价", 20},
			{"总金额", "昵称/总数", 1},
			{20, "Bob", 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "vertical.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Batches) != 2 {
		t.Fatalf("batches = %d, want 2", len(preview.Batches))
	}
	if preview.Batches[0].BatchName == preview.Batches[1].BatchName {
		t.Fatalf("batch names should differ: %q", preview.Batches[0].BatchName)
	}
}

func TestParseAdjustmentRows(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "prices",
		Rows: [][]any{
			{"调价表"},
			{nil, "种类", "miku", "rin"},
			{nil, "单价", 10, 20},
			{"总金额", "昵称/总数", 1, 1},
			{nil, "调价", 1, -1},
			{nil, "一调", 2, 0},
			{nil, "二调", 3, 1},
			{nil, "实际退差单价", 11, 19},
			{30, "Alice", 1, 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "prices.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	batch := preview.Batches[0]
	if batch.CalculationPriceType != "unit_price" {
		t.Fatalf("calculation price type = %s", batch.CalculationPriceType)
	}
	if len(batch.PriceTypes) != 5 {
		t.Fatalf("price type count = %d, want 5", len(batch.PriceTypes))
	}
	if batch.CalculatedAmount != 30 {
		t.Fatalf("calculated amount = %.2f", batch.CalculatedAmount)
	}
}

func TestParseDuplicateItemsAndInvalidQuantity(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "duplicates",
		Rows: [][]any{
			{"重复种类"},
			{nil, "种类", "miku", "miku", "rin"},
			{nil, "单价", 10, 10, 20},
			{"总金额", "昵称/总数", 1, 1, 1},
			{20, "Alice", 1.5, -1, "text"},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "duplicates.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Batches[0].Warnings) == 0 {
		t.Fatal("expected duplicate item warning")
	}
	if len(preview.Batches[0].Errors) != 3 {
		t.Fatalf("errors = %d, want 3", len(preview.Batches[0].Errors))
	}
}

func TestParseSimpleCNAmountSheet(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "simple",
		Rows: [][]any{
			{"收款码", nil, nil, formula(`_xlfn.DISPIMG("ID_1",1)`)},
			{"cn", "总肾", "g"},
			{"hoshino", 2.03, 16.9},
			{"Alice", 4, 35},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "simple.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Batches[0].TemplateType != templateSimpleCNAmount {
		t.Fatalf("template = %s", preview.Batches[0].TemplateType)
	}
	if len(preview.Batches[0].Details) != 0 {
		t.Fatal("simple amount sheet should not become order-item details")
	}
	if len(preview.Batches[0].Notices) < 2 {
		t.Fatalf("notices = %d, want image and simple-sheet notices", len(preview.Batches[0].Notices))
	}
}

func TestParseProvidedWorkbook(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "excel", "26感谢祭单领.xlsx")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("provided workbook not available: %v", err)
	}
	preview, err := Parse(data, ParseOptions{Filename: filepath.Base(path), Size: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	templateCounts := map[string]int{}
	for _, batch := range preview.Batches {
		templateCounts[batch.TemplateType]++
	}
	t.Logf("provided workbook summary: sheets=%d batches=%d templates=%v errors=%d warnings=%d notices=%d", len(preview.Sheets), len(preview.Batches), templateCounts, len(preview.Errors), len(preview.Warnings), len(preview.Notices))
	for _, batch := range preview.Batches {
		t.Logf("batch %s sheet=%s template=%s cn=%d items=%d qty=%d table=%.2f calculated=%.2f diff=%.2f warnings=%d notices=%d", batch.ID, batch.SheetName, batch.TemplateType, batch.CNCount, batch.ItemTypeCount, batch.TotalQuantity, batch.TableAmount, batch.CalculatedAmount, batch.Difference, len(batch.Warnings), len(batch.Notices))
	}
	for _, sheet := range preview.Sheets {
		t.Logf("sheet %s template=%s batches=%d rows=%d cols=%d table=%.2f calculated=%.2f diff=%.2f", sheet.Name, sheet.TemplateType, sheet.BatchCount, sheet.RowCount, sheet.ColumnCount, sheet.TableAmount, sheet.CalcAmount, sheet.Difference)
	}

	if len(preview.Sheets) != 5 {
		t.Fatalf("sheet count = %d, want 5", len(preview.Sheets))
	}
	if len(preview.Batches) < 5 {
		t.Fatalf("batch count = %d, want at least 5", len(preview.Batches))
	}
	foundSimple := false
	for _, batch := range preview.Batches {
		if batch.TemplateType == templateSimpleCNAmount {
			foundSimple = true
			break
		}
	}
	if !foundSimple {
		t.Fatal("expected simple_cn_amount sheet")
	}
}

// 新版《模板.xlsx》在原字段后面用括号加了中文说明（例如「分类(系列号)」「种类（角色名）」）。
// 括号内是给填表人看的备注，不是导出程序的原始字段，导入必须按括号外的原字段识别。
func TestParseStandardImportWithAnnotatedHeaders(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "vol.63色纸",
		Rows: [][]any{
			{"【三丽鸥】汇总详情"},
			{nil, "分类(系列号)", "色纸"},
			{nil, "种类（角色名）", "miku", "rin"},
			{nil, "单价", 40, 50},
			{"总金额", "昵称/总数", 1, 1},
			{90, "Succ", 1, 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "模板.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(preview.Batches))
	}
	batch := preview.Batches[0]
	if batch.TemplateType != templateStandardImport {
		t.Fatalf("template = %s, want %s", batch.TemplateType, templateStandardImport)
	}
	if len(batch.Errors) != 0 {
		t.Fatalf("unexpected errors: %#v", batch.Errors)
	}
	if len(batch.Details) != 2 || batch.TotalQuantity != 2 || batch.CalculatedAmount != 90 {
		t.Fatalf("details=%d qty=%d amount=%.2f", len(batch.Details), batch.TotalQuantity, batch.CalculatedAmount)
	}
	if got := batch.Details[0].CharacterName; got != "miku" {
		t.Fatalf("character = %q, want miku", got)
	}
	if got := batch.Details[0].ProductCategory; got != "色纸" {
		t.Fatalf("product category = %q, want 色纸", got)
	}
	if batch.SheetName != "vol.63色纸" {
		t.Fatalf("sheet name = %q", batch.SheetName)
	}
	if batch.SheetTitle != "三丽鸥" {
		t.Fatalf("sheet title = %q, want 三丽鸥", batch.SheetTitle)
	}
}

// 最终字段语义：系列来自 B2「分类(系列号)」的值，团名来自角色列头前缀，两者不混用。
func TestParseStandardImportSeriesAndGroupAreSeparate(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "vol.63色纸",
		Rows: [][]any{
			{"【三丽鸥】汇总详情"},
			{nil, "分类(系列号)", "63色纸"},
			{nil, "种类（角色名）", "25h miku", "ln luka", "szk"},
			{nil, "单价", 10, 20, 30},
			{"总金额", "昵称/总数", 1, 1, 1},
			{60, "Alice", 1, 1, 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "series.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	batch := preview.Batches[0]
	if len(batch.Details) != 3 {
		t.Fatalf("details = %d, want 3", len(batch.Details))
	}
	want := map[string]struct{ series, group, character string }{
		"25h miku": {"63色纸", "25h", "miku"},
		"ln luka":  {"63色纸", "ln", "luka"},
		"szk":      {"63色纸", "", "szk"},
	}
	for _, detail := range batch.Details {
		expected, ok := want[detail.ItemName]
		if !ok {
			t.Fatalf("unexpected item %q", detail.ItemName)
		}
		if detail.SeriesCode != expected.series {
			t.Fatalf("%s series = %q, want %q", detail.ItemName, detail.SeriesCode, expected.series)
		}
		if detail.GroupName != expected.group {
			t.Fatalf("%s group = %q, want %q", detail.ItemName, detail.GroupName, expected.group)
		}
		if detail.CharacterName != expected.character {
			t.Fatalf("%s character = %q, want %q", detail.ItemName, detail.CharacterName, expected.character)
		}
		// 团名不能取到系列值，系列也不能取到团名前缀。
		if detail.GroupName == "63色纸" {
			t.Fatalf("%s group must not take the series value", detail.ItemName)
		}
	}
}

// A1 仍是模板占位词时，谷子名称最终兜底到工作表名，绝不能写入占位词本身。
func TestParseStandardImportPlaceholderTitleFallsBackToSheetName(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "vol.63色纸",
		Rows: [][]any{
			{"【谷子名字】汇总详情"},
			{nil, "分类(系列号)", "63色纸"},
			{nil, "种类（角色名）", "szk"},
			{nil, "单价", 1},
			{"总金额", "昵称/总数", 1},
			{1, "柴", 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "placeholder.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	batch := preview.Batches[0]
	if len(batch.Details) != 1 {
		t.Fatalf("details = %d, want 1", len(batch.Details))
	}
	detail := batch.Details[0]
	if detail.GoodsSeriesName != "vol.63色纸" {
		t.Fatalf("goods name = %q, want 工作表名 vol.63色纸", detail.GoodsSeriesName)
	}
	for _, got := range []string{detail.GoodsSeriesName, detail.DisplayName, batch.SheetTitle, productNameForDetail(batch, detail)} {
		if strings.Contains(got, "谷子名字") {
			t.Fatalf("占位词泄漏到业务字段: %q", got)
		}
	}
}

// 工作表名称和标题里的谷子名字都可以由用户改动，不能因此拒绝导入。
func TestParseStandardImportRenamedSheetAndTitle(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "vol.99 亚克力",
		Rows: [][]any{
			{"【新谷子名】汇总详情"},
			{nil, "分类（系列号）", "亚克力"},
			{nil, "种类(角色名)", "luka"},
			{nil, "单价", 30},
			{"总金额", "昵称/总数", 1},
			{30, "Bob", 1},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "renamed.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	batch := preview.Batches[0]
	if batch.TemplateType != templateStandardImport {
		t.Fatalf("template = %s", batch.TemplateType)
	}
	if batch.SheetTitle != "新谷子名" || len(batch.Details) != 1 {
		t.Fatalf("title = %q details = %d", batch.SheetTitle, len(batch.Details))
	}
}

// 结构被破坏或完全无关的表格仍然要给出清楚的错误。
func TestParseUnrelatedSheetStillFails(t *testing.T) {
	data := testWorkbook(t, testSheet{
		Name: "无关表",
		Rows: [][]any{
			{"客户名单"},
			{"姓名", "电话"},
			{"张三", "123"},
		},
	})

	preview, err := Parse(data, ParseOptions{Filename: "unrelated.xlsx"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range preview.Errors {
		if issue.Code == "no_valid_order_items" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected no_valid_order_items error, got %#v", preview.Errors)
	}
}

func TestStripHeaderAnnotation(t *testing.T) {
	cases := map[string]string{
		"分类(系列号)":   "分类",
		"种类（角色名）":   "种类",
		"分类 (系列号)":  "分类",
		"种类（角色名(英文)）": "种类",
		"单价":        "单价",
		"昵称/总数":     "昵称/总数",
		"(仅备注)":     "(仅备注)",
		"":          "",
	}
	for input, want := range cases {
		if got := stripHeaderAnnotation(input); got != want {
			t.Fatalf("stripHeaderAnnotation(%q) = %q, want %q", input, got, want)
		}
	}
}

// 前端下载的模板必须就是仓库里的这份文件，且能被解析器认出是标准模板结构。
func TestShippedTemplateFileIsStandardStructure(t *testing.T) {
	path := filepath.Join("..", "..", "..", "frontend", "public", "templates", "pjsk-goods-import-template.xlsx")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	book, err := readWorkbook(data)
	if err != nil {
		t.Fatalf("read workbook: %v", err)
	}
	if len(book.Sheets) == 0 {
		t.Fatal("template has no sheets")
	}
	sheet := book.Sheets[0]
	if sheet.Name != "vol.63色纸" {
		t.Fatalf("sheet name = %q", sheet.Name)
	}
	if !isStandardImportSheet(sheet) {
		t.Fatalf("shipped template not recognised as standard import: B2=%q B3=%q B4=%q A5=%q B5=%q",
			sheet.cellText(1, 1), sheet.cellText(2, 1), sheet.cellText(3, 1), sheet.cellText(4, 0), sheet.cellText(4, 1))
	}
	// 模板里的示例行只有占位文字、没有数量，不能产生任何订单明细。
	preview, err := Parse(data, ParseOptions{Filename: "pjsk-goods-import-template.xlsx"})
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}
	for _, batch := range preview.Batches {
		if len(batch.Details) != 0 {
			t.Fatalf("blank template produced %d details", len(batch.Details))
		}
	}
}

type testSheet struct {
	Name string
	Rows [][]any
}

type formula string

func testWorkbook(t *testing.T, sheets ...testSheet) []byte {
	t.Helper()
	var output bytes.Buffer
	zipWriter := zip.NewWriter(&output)
	writeZipFile(t, zipWriter, "[Content_Types].xml", contentTypesXML(len(sheets)))
	writeZipFile(t, zipWriter, "_rels/.rels", rootRelsXML())
	writeZipFile(t, zipWriter, "xl/workbook.xml", workbookXML(sheets))
	writeZipFile(t, zipWriter, "xl/_rels/workbook.xml.rels", workbookRelsXML(len(sheets)))
	for index, sheet := range sheets {
		writeZipFile(t, zipWriter, fmt.Sprintf("xl/worksheets/sheet%d.xml", index+1), worksheetXML(sheet.Rows))
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writeZipFile(t *testing.T, zipWriter *zip.Writer, name string, content string) {
	t.Helper()
	writer, err := zipWriter.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
}

func contentTypesXML(sheetCount int) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	builder.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	builder.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	builder.WriteString(`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`)
	for i := 1; i <= sheetCount; i++ {
		builder.WriteString(fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i))
	}
	builder.WriteString(`</Types>`)
	return builder.String()
}

func rootRelsXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
}

func workbookXML(sheets []testSheet) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for index, sheet := range sheets {
		builder.WriteString(fmt.Sprintf(`<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, xmlEscape(sheet.Name), index+1, index+1))
	}
	builder.WriteString(`</sheets></workbook>`)
	return builder.String()
}

func workbookRelsXML(sheetCount int) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := 1; i <= sheetCount; i++ {
		builder.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i, i))
	}
	builder.WriteString(`</Relationships>`)
	return builder.String()
}

func worksheetXML(rows [][]any) string {
	var builder strings.Builder
	builder.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for rowIndex, row := range rows {
		builder.WriteString(fmt.Sprintf(`<row r="%d">`, rowIndex+1))
		for colIndex, value := range row {
			if value == nil {
				continue
			}
			ref := columnName(colIndex+1) + fmt.Sprint(rowIndex+1)
			switch typed := value.(type) {
			case string:
				builder.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, xmlEscape(typed)))
			case formula:
				builder.WriteString(fmt.Sprintf(`<c r="%s"><f>%s</f></c>`, ref, xmlEscape(string(typed))))
			case int:
				builder.WriteString(fmt.Sprintf(`<c r="%s"><v>%d</v></c>`, ref, typed))
			case float64:
				builder.WriteString(fmt.Sprintf(`<c r="%s"><v>%v</v></c>`, ref, typed))
			default:
				builder.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, xmlEscape(fmt.Sprint(value))))
			}
		}
		builder.WriteString(`</row>`)
	}
	builder.WriteString(`</sheetData></worksheet>`)
	return builder.String()
}

func xmlEscape(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	value = strings.ReplaceAll(value, `"`, "&quot;")
	value = strings.ReplaceAll(value, "'", "&apos;")
	return value
}
