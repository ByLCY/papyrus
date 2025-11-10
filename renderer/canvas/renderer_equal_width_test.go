package canvasrenderer

import (
	"testing"

	"github.com/ByLCY/papyrus/layout"
)

// 当第一行宽度与容器宽度恰好相等且后面紧跟一个显式换行时，不应产生额外的空行。
func TestNoBlankLineWhenEqualWidthThenNewline(t *testing.T) {
	r := NewRenderer(".")
	font := layout.FontResource{Src: "embed:Inter/static/Inter-Regular.ttf"}
	fontSizeMM := 12 * layout.PtToMm
	lineHeightMM := fontSizeMM * 1.2

	first := "SAMPLE-A"
	// 用极大宽度先测量第一行宽度（mm）
	measured, err := r.LayoutLines(first, 1e6, font, fontSizeMM, lineHeightMM, "")
	if err != nil {
		t.Fatalf("measure error: %v", err)
	}
	if len(measured) != 1 {
		t.Fatalf("unexpected measured lines: %d", len(measured))
	}
	limit := measured[0].Width
	if limit <= 0 {
		t.Fatalf("invalid measured width: %g", limit)
	}

	// 构造恰好等宽 + 显式换行 + 下一行内容
	content := first + "\n" + "SAMPLE-B"
	lines, err := r.LayoutLines(content, limit, font, fontSizeMM, lineHeightMM, "")
	if err != nil {
		t.Fatalf("LayoutLines error: %v", err)
	}
	if got := len(lines); got != 2 {
		t.Fatalf("expected 2 lines without blank, got %d", got)
	}
	if lines[0].Content != first {
		t.Fatalf("first line mismatch: got=%q want=%q", lines[0].Content, first)
	}
	if lines[1].Content != "SAMPLE-B" {
		t.Fatalf("second line mismatch: got=%q want=%q", lines[1].Content, "SAMPLE-B")
	}
}
