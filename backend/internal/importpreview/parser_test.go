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
