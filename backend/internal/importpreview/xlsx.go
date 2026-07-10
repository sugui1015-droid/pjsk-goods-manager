package importpreview

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type workbook struct {
	Sheets []sheet
}

type sheet struct {
	Name     string
	Cells    [][]cell
	RowCount int
	ColCount int
}

type cell struct {
	Text    string
	Number  *float64
	Formula string
}

func readWorkbook(data []byte) (workbook, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return workbook{}, fmt.Errorf("open xlsx: %w", err)
	}

	files := map[string]*zip.File{}
	for _, file := range reader.File {
		files[file.Name] = file
	}

	sharedStrings, err := readSharedStrings(files["xl/sharedStrings.xml"])
	if err != nil {
		return workbook{}, err
	}

	sheetRefs, err := readWorkbookSheets(files)
	if err != nil {
		return workbook{}, err
	}

	result := workbook{}
	for _, ref := range sheetRefs {
		file := files[ref.Path]
		if file == nil {
			return workbook{}, fmt.Errorf("missing worksheet %s", ref.Path)
		}
		parsed, err := readSheet(file, sharedStrings)
		if err != nil {
			return workbook{}, fmt.Errorf("read sheet %s: %w", ref.Name, err)
		}
		parsed.Name = ref.Name
		result.Sheets = append(result.Sheets, parsed)
	}

	return result, nil
}

type workbookSheetRef struct {
	Name string
	Path string
}

func readWorkbookSheets(files map[string]*zip.File) ([]workbookSheetRef, error) {
	workbookFile := files["xl/workbook.xml"]
	relsFile := files["xl/_rels/workbook.xml.rels"]
	if workbookFile == nil || relsFile == nil {
		return nil, fmt.Errorf("xlsx workbook metadata is missing")
	}

	rels, err := readRelationships(relsFile)
	if err != nil {
		return nil, err
	}

	stream, err := workbookFile.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	decoder := xml.NewDecoder(stream)
	var refs []workbookSheetRef
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}

		var name, relID string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "name":
				name = attr.Value
			case "id":
				relID = attr.Value
			}
		}
		target := rels[relID]
		if name == "" || target == "" {
			continue
		}
		refs = append(refs, workbookSheetRef{
			Name: name,
			Path: cleanWorkbookTarget(target),
		})
	}
	return refs, nil
}

func cleanWorkbookTarget(target string) string {
	target = strings.TrimPrefix(target, "/")
	if strings.HasPrefix(target, "xl/") {
		return path.Clean(target)
	}
	return path.Clean("xl/" + target)
}

func readRelationships(file *zip.File) (map[string]string, error) {
	stream, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	type relationship struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	}
	type relationships struct {
		Items []relationship `xml:"Relationship"`
	}
	var parsed relationships
	if err := xml.NewDecoder(stream).Decode(&parsed); err != nil {
		return nil, err
	}

	result := map[string]string{}
	for _, item := range parsed.Items {
		result[item.ID] = item.Target
	}
	return result, nil
}

func readSharedStrings(file *zip.File) ([]string, error) {
	if file == nil {
		return nil, nil
	}
	stream, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	decoder := xml.NewDecoder(stream)
	var values []string
	var builder strings.Builder
	inString := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "si" {
				builder.Reset()
				inString = true
			}
		case xml.CharData:
			if inString {
				builder.Write([]byte(item))
			}
		case xml.EndElement:
			if item.Name.Local == "si" {
				values = append(values, builder.String())
				inString = false
			}
		}
	}
	return values, nil
}

func readSheet(file *zip.File, sharedStrings []string) (sheet, error) {
	stream, err := file.Open()
	if err != nil {
		return sheet{}, err
	}
	defer stream.Close()

	decoder := xml.NewDecoder(stream)
	rows := map[int]map[int]cell{}
	maxRow, maxCol := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return sheet{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "c" {
			continue
		}
		row, col := cellPosition(start)
		if row == 0 || col == 0 {
			continue
		}
		value, err := readCell(decoder, start, sharedStrings)
		if err != nil {
			return sheet{}, err
		}
		if rows[row] == nil {
			rows[row] = map[int]cell{}
		}
		rows[row][col] = value
		if row > maxRow {
			maxRow = row
		}
		if col > maxCol {
			maxCol = col
		}
	}

	result := sheet{
		Cells:    make([][]cell, maxRow),
		RowCount: maxRow,
		ColCount: maxCol,
	}
	for row := 1; row <= maxRow; row++ {
		result.Cells[row-1] = make([]cell, maxCol)
		for col := 1; col <= maxCol; col++ {
			result.Cells[row-1][col-1] = rows[row][col]
		}
	}
	return result, nil
}

func cellPosition(start xml.StartElement) (int, int) {
	for _, attr := range start.Attr {
		if attr.Name.Local != "r" {
			continue
		}
		return parseCellRef(attr.Value)
	}
	return 0, 0
}

var cellRefPattern = regexp.MustCompile(`^([A-Z]+)([0-9]+)$`)

func parseCellRef(ref string) (int, int) {
	matches := cellRefPattern.FindStringSubmatch(strings.ToUpper(ref))
	if len(matches) != 3 {
		return 0, 0
	}
	row, _ := strconv.Atoi(matches[2])
	col := 0
	for _, r := range matches[1] {
		col = col*26 + int(r-'A'+1)
	}
	return row, col
}

func readCell(decoder *xml.Decoder, start xml.StartElement, sharedStrings []string) (cell, error) {
	cellType := ""
	for _, attr := range start.Attr {
		if attr.Name.Local == "t" {
			cellType = attr.Value
		}
	}

	var formula, rawValue, inlineText strings.Builder
	var inFormula, inValue, inText bool
	for {
		token, err := decoder.Token()
		if err != nil {
			return cell{}, err
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "f":
				inFormula = true
			case "v":
				inValue = true
			case "t":
				inText = true
			}
		case xml.CharData:
			switch {
			case inFormula:
				formula.Write([]byte(item))
			case inValue:
				rawValue.Write([]byte(item))
			case inText:
				inlineText.Write([]byte(item))
			}
		case xml.EndElement:
			switch item.Name.Local {
			case "f":
				inFormula = false
			case "v":
				inValue = false
			case "t":
				inText = false
			case "c":
				return convertCell(cellType, rawValue.String(), inlineText.String(), formula.String(), sharedStrings), nil
			}
		}
	}
}

func convertCell(cellType, rawValue, inlineText, formula string, sharedStrings []string) cell {
	value := strings.TrimSpace(rawValue)
	result := cell{Formula: strings.TrimSpace(formula)}
	switch cellType {
	case "s":
		index, err := strconv.Atoi(value)
		if err == nil && index >= 0 && index < len(sharedStrings) {
			result.Text = strings.TrimSpace(sharedStrings[index])
		}
	case "inlineStr":
		result.Text = strings.TrimSpace(inlineText)
	case "str":
		result.Text = strings.TrimSpace(value)
	default:
		if value != "" {
			if number, err := strconv.ParseFloat(value, 64); err == nil {
				result.Number = &number
				result.Text = strings.TrimSpace(formatNumber(number))
			} else {
				result.Text = strings.TrimSpace(value)
			}
		}
	}
	if result.Text == "" && result.Formula != "" {
		result.Text = "=" + result.Formula
	}
	return result
}

func formatNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}

func columnName(index int) string {
	if index <= 0 {
		return ""
	}
	var chars []byte
	for index > 0 {
		index--
		chars = append(chars, byte('A'+index%26))
		index /= 26
	}
	for i, j := 0, len(chars)-1; i < j; i, j = i+1, j-1 {
		chars[i], chars[j] = chars[j], chars[i]
	}
	return string(chars)
}

func sortedRowNumbers(rows map[int]map[int]cell) []int {
	numbers := make([]int, 0, len(rows))
	for row := range rows {
		numbers = append(numbers, row)
	}
	sort.Ints(numbers)
	return numbers
}
