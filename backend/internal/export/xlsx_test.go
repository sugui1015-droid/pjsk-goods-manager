package export

import (
	"strings"
	"testing"
)

func TestDisplayWidthWeightsCJKDouble(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  float64
	}{
		{"ascii", "ABCDE", 5},
		{"chinese", "柴柴柴", 6},
		{"japanese_hiragana", "ひらがな", 8},
		{"emoji", "😀😀", 4},
		{"mixed", "CN柴", 4}, // "C","N" = 1 each, "柴" = 2
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := displayWidth(c.input); got != c.want {
				t.Fatalf("displayWidth(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}

func TestColumnWidthWidensForLongChineseName(t *testing.T) {
	col := excelColumn{Header: "CN", Kind: excelText, MinWidth: 12, MaxWidth: 24, Wrap: false}
	shortRows := []excelRow{{textCell("柴")}}
	longRows := []excelRow{{textCell("三丽鸥联名限定角色显示名称示例")}} // 14 CJK chars = 28 width units

	shortWidth := columnWidth(col, shortRows, 0)
	longWidth := columnWidth(col, longRows, 0)

	if shortWidth != col.MinWidth {
		t.Fatalf("short name width = %v, want min width %v", shortWidth, col.MinWidth)
	}
	if longWidth != col.MaxWidth {
		t.Fatalf("long CJK name width = %v, want capped at max width %v", longWidth, col.MaxWidth)
	}
	if longWidth <= shortWidth {
		t.Fatalf("expected long CJK content to widen the column beyond the short-name width, got long=%v short=%v", longWidth, shortWidth)
	}
}

func TestColumnWidthRespectsCapWithoutBreakingCJKMidCharacter(t *testing.T) {
	// A column with no explicit cap should still fall back to a sane width;
	// with a cap, width must never exceed MaxWidth regardless of how long
	// the technical identifier content is.
	col := excelColumn{Header: "标识", Kind: excelText, MinWidth: 8, MaxWidth: 20, Wrap: true}
	rows := []excelRow{{textCell(strings.Repeat("a", 200))}}
	width := columnWidth(col, rows, 0)
	if width != col.MaxWidth {
		t.Fatalf("width = %v, want capped at %v for an extremely long ASCII identifier", width, col.MaxWidth)
	}
}

func TestRowNeedsWrapUsesColumnMaxWidth(t *testing.T) {
	columns := []excelColumn{
		{Header: "CN", Kind: excelText, MinWidth: 12, MaxWidth: 24, Wrap: false},
		{Header: "显示名称", Kind: excelText, MinWidth: 14, MaxWidth: 20, Wrap: true},
	}
	shortRow := excelRow{textCell("柴"), textCell("短名")}
	longRow := excelRow{textCell("柴"), textCell("三丽鸥联名限定角色显示名称示例超长文本")}

	if rowNeedsWrap(columns, shortRow) {
		t.Fatal("short display name should not require a taller row")
	}
	if !rowNeedsWrap(columns, longRow) {
		t.Fatal("display name wider than the column's max width should require a taller row")
	}
}

func TestFormatDisplayTimeConvertsUTCToChinaLocalTime(t *testing.T) {
	got := formatDisplayTime("2026-07-12T15:39:00Z")
	want := "2026-07-12 23:39:00" // UTC+8
	if got != want {
		t.Fatalf("formatDisplayTime = %q, want %q", got, want)
	}
}

func TestFormatDisplayTimeCrossesMidnight(t *testing.T) {
	got := formatDisplayTime("2026-07-12T16:28:00Z")
	want := "2026-07-13 00:28:00" // UTC+8 rolls over to the next day
	if got != want {
		t.Fatalf("formatDisplayTime = %q, want %q", got, want)
	}
}

func TestFormatDisplayTimeHandlesEmptyAndInvalid(t *testing.T) {
	if got := formatDisplayTime(""); got != "" {
		t.Fatalf("formatDisplayTime(empty) = %q, want empty", got)
	}
	if got := formatDisplayTime("not-a-date"); got != "not-a-date" {
		t.Fatalf("formatDisplayTime(invalid) = %q, want the original value unchanged", got)
	}
}
