package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type excelKind string

const (
	excelText    excelKind = "text"
	excelNumber  excelKind = "number"
	excelInteger excelKind = "integer"
)

type excelColumn struct {
	Header   string
	Kind     excelKind
	MinWidth float64
	MaxWidth float64
	Wrap     bool
}

type excelCell struct {
	Text   string
	Number *float64
}

type excelRow []excelCell

func textCell(value string) excelCell    { return excelCell{Text: value} }
func numberCell(value float64) excelCell { return excelCell{Number: &value} }

func writeExcel(w http.ResponseWriter, baseName string, columns []excelColumn, rows []excelRow) error {
	filename := fmt.Sprintf("%s-%s.xlsx", baseName, time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	files := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypesXML},
		{"_rels/.rels", packageRelsXML},
		{"docProps/core.xml", coreXML()},
		{"docProps/app.xml", appXML},
		{"xl/workbook.xml", workbookXML},
		{"xl/_rels/workbook.xml.rels", workbookRelsXML},
		{"xl/styles.xml", stylesXML},
		{"xl/worksheets/sheet1.xml", worksheetXML(columns, rows)},
	}
	for _, item := range files {
		file, err := zipWriter.Create(item.name)
		if err != nil {
			return err
		}
		if _, err := file.Write([]byte(item.body)); err != nil {
			return err
		}
	}
	if err := zipWriter.Close(); err != nil {
		return err
	}
	_, err := w.Write(buffer.Bytes())
	return err
}

func worksheetXML(columns []excelColumn, rows []excelRow) string {
	lastCol := excelColumnName(len(columns))
	lastRow := len(rows) + 1
	ref := fmt.Sprintf("A1:%s%d", lastCol, lastRow)
	var b strings.Builder
	b.WriteString(xml.Header)
	b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	b.WriteString(`<sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>`)
	b.WriteString(`<cols>`)
	for i, col := range columns {
		width := columnWidth(col, rows, i)
		b.WriteString(fmt.Sprintf(`<col min="%d" max="%d" width="%.1f" customWidth="1"/>`, i+1, i+1, width))
	}
	b.WriteString(`</cols><sheetData>`)
	b.WriteString(`<row r="1" ht="22" customHeight="1">`)
	for i, col := range columns {
		writeInlineCell(&b, i+1, 1, 1, col.Header)
	}
	b.WriteString(`</row>`)
	for rowIndex, row := range rows {
		r := rowIndex + 2
		height := 20.0
		if rowNeedsWrap(columns, row) {
			height = 42
		}
		b.WriteString(fmt.Sprintf(`<row r="%d" ht="%.0f" customHeight="1">`, r, height))
		for c := range columns {
			cell := excelCell{}
			if c < len(row) {
				cell = row[c]
			}
			style := styleForColumn(columns[c])
			if (columns[c].Kind == excelNumber || columns[c].Kind == excelInteger) && cell.Number != nil {
				writeNumberCell(&b, c+1, r, style, *cell.Number, columns[c].Kind)
			} else {
				writeInlineCell(&b, c+1, r, style, cell.Text)
			}
		}
		b.WriteString(`</row>`)
	}
	b.WriteString(`</sheetData>`)
	b.WriteString(`<autoFilter ref="` + ref + `"/>`)
	b.WriteString(`</worksheet>`)
	return b.String()
}

func styleForColumn(col excelColumn) int {
	if col.Wrap {
		return 4
	}
	if col.Kind == excelNumber {
		return 3
	}
	if col.Kind == excelInteger {
		return 5
	}
	return 2
}

func rowNeedsWrap(columns []excelColumn, row excelRow) bool {
	for i, col := range columns {
		if !col.Wrap || i >= len(row) {
			continue
		}
		// A cell needs a taller row exactly when its content is wider than
		// the column's capped max width — i.e. it will actually wrap to a
		// second line in Excel, not based on an arbitrary character count.
		limit := col.MaxWidth
		if limit <= 0 {
			limit = 24
		}
		if displayWidth(row[i].Text) > limit {
			return true
		}
	}
	return false
}

// runeDisplayWidth approximates how many "character widths" a rune occupies
// in a spreadsheet column, the same way Excel measures its column width unit
// (roughly the width of a Latin digit in the default font). CJK ideographs,
// kana, hangul, fullwidth forms, and most emoji render at roughly twice that
// width, so they must count as 2, not 1 — otherwise columns holding Chinese
// CN codes or product names come out far too narrow.
func runeDisplayWidth(r rune) float64 {
	switch {
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r == 0x2329, r == 0x232A,
		r >= 0x2E80 && r <= 0x303E, // CJK Radicals Supplement .. CJK Symbols/Punctuation
		r >= 0x3041 && r <= 0x33FF, // Hiragana .. CJK Compatibility
		r >= 0x3400 && r <= 0x4DBF, // CJK Unified Ideographs Extension A
		r >= 0x4E00 && r <= 0x9FFF, // CJK Unified Ideographs
		r >= 0xA000 && r <= 0xA4CF, // Yi Syllables/Radicals
		r >= 0xAC00 && r <= 0xD7A3, // Hangul Syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK Compatibility Ideographs
		r >= 0xFE30 && r <= 0xFE4F, // CJK Compatibility Forms
		r >= 0xFF00 && r <= 0xFF60, // Fullwidth Forms
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD, // CJK Unified Ideographs Extension B and beyond
		r >= 0x1F300 && r <= 0x1FAFF: // Misc symbols, pictographs, emoji
		return 2
	default:
		return 1
	}
}

// displayWidth sums the approximate rendered width of a string in Excel
// column-width units.
func displayWidth(s string) float64 {
	total := 0.0
	for _, r := range s {
		total += runeDisplayWidth(r)
	}
	return total
}

func columnWidth(col excelColumn, rows []excelRow, index int) float64 {
	width := displayWidth(col.Header) + 4
	if col.MinWidth > width {
		width = col.MinWidth
	}
	for _, row := range rows {
		if index >= len(row) {
			continue
		}
		var length float64
		if row[index].Number != nil {
			length = float64(len(strconv.FormatFloat(*row[index].Number, 'f', 2, 64)))
		} else {
			length = displayWidth(row[index].Text)
		}
		candidate := length + 3
		if candidate > width {
			width = candidate
		}
	}
	if col.MaxWidth > 0 && width > col.MaxWidth {
		width = col.MaxWidth
	}
	if width < 8 {
		width = 8
	}
	return width
}

func writeInlineCell(b *strings.Builder, col int, row int, style int, value string) {
	ref := excelColumnName(col) + strconv.Itoa(row)
	b.WriteString(fmt.Sprintf(`<c r="%s" t="inlineStr" s="%d"><is><t>%s</t></is></c>`, ref, style, escapeXML(value)))
}

func writeNumberCell(b *strings.Builder, col int, row int, style int, value float64, kind excelKind) {
	ref := excelColumnName(col) + strconv.Itoa(row)
	precision := 2
	if kind == excelInteger {
		precision = 0
	}
	b.WriteString(fmt.Sprintf(`<c r="%s" s="%d"><v>%s</v></c>`, ref, style, strconv.FormatFloat(value, 'f', precision, 64)))
}

func excelColumnName(index int) string {
	name := ""
	for index > 0 {
		index--
		name = string(rune('A'+index%26)) + name
		index /= 26
	}
	return name
}

func escapeXML(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func coreXML() string {
	now := time.Now().UTC().Format(time.RFC3339)
	return xml.Header + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:creator>PJSK Goods Manager</dc:creator><cp:lastModifiedBy>PJSK Goods Manager</cp:lastModifiedBy><dcterms:created xsi:type="dcterms:W3CDTF">` + now + `</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">` + now + `</dcterms:modified></cp:coreProperties>`
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/></Types>`
const packageRelsXML = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`
const workbookXML = `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="导出" sheetId="1" r:id="rId1"/></sheets></workbook>`
const workbookRelsXML = `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`
const appXML = `<?xml version="1.0" encoding="UTF-8"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>PJSK Goods Manager</Application></Properties>`
const stylesXML = `<?xml version="1.0" encoding="UTF-8"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="0.00"/></numFmts><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border><left style="thin"><color auto="1"/></left><right style="thin"><color auto="1"/></right><top style="thin"><color auto="1"/></top><bottom style="thin"><color auto="1"/></bottom><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="6"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="1" xfId="0" applyFont="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf><xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf><xf numFmtId="164" fontId="0" fillId="0" borderId="1" xfId="0" applyNumberFormat="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf><xf numFmtId="0" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf><xf numFmtId="1" fontId="0" fillId="0" borderId="1" xfId="0" applyNumberFormat="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center"/></xf></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
