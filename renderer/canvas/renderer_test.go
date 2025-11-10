package canvasrenderer

import (
	"math"
	"testing"

	"github.com/ByLCY/papyrus/layout"
)

func TestLayoutLinesGreedyWrapsText(t *testing.T) {
	r := NewRenderer(".")
	font := layout.FontResource{
		Name: "Body",
		Src:  "embed:Inter/static/Inter-Regular.ttf",
	}

	// 这里的宽度/字号/行高均为 mm
	fontSizeMM := 12 * layout.PtToMm
	lineHeightMM := fontSizeMM * 1.2

	lines, err := r.LayoutLines("hello world again", 10, font, fontSizeMM, lineHeightMM, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) < 2 {
		t.Fatalf("expected wrapping into multiple lines, got %d", len(lines))
	}
}

func TestGreedyWrapHonorsNewlines(t *testing.T) {
	r := NewRenderer(".")
	font := layout.FontResource{
		Name: "Body",
		Src:  "embed:Inter/static/Inter-Regular.ttf",
	}

	fontSizeMM := 12 * layout.PtToMm
	lineHeightMM := fontSizeMM * 1.2

	lines, err := r.LayoutLines("foo\n\nbar", 100, font, fontSizeMM, lineHeightMM, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines including blank, got %d", len(lines))
	}
	if lines[1].Content != "" {
		t.Fatalf("expected middle line to be blank, got %q", lines[1].Content)
	}
}

// TestLineHeightsInvariant 验证：
// 1) 首行 GapBefore == 0；
// 2) 其余行 GapBefore ≈ max(lineHeight - textHeight, 0)；
// 3) 各行的 Height 与 textHeight 一致（渲染器会用字体度量回填）。
func TestLineHeightsInvariant(t *testing.T) {
	r := NewRenderer(".")
	font := layout.FontResource{
		Name: "Body",
		Src:  "embed:Inter/static/Inter-Regular.ttf",
	}
	fontSizeMM := 12 * layout.PtToMm
	lineHeightMM := fontSizeMM * 1.3

	content := "longlonglong longlonglong longlonglong longlonglong longlonglong"
	lines, err := r.LayoutLines(content, 40, font, fontSizeMM, lineHeightMM, "")
	if err != nil {
		t.Fatalf("LayoutLines error: %v", err)
	}
	if len(lines) < 2 {
		t.Fatalf("expected multiple lines for invariant test, got %d", len(lines))
	}

	// textHeight 以第一行 Height 为准
	textHeight := lines[0].Height
	if textHeight <= 0 {
		t.Fatalf("invalid text height: %g", textHeight)
	}
	wantLeading := math.Max(lineHeightMM-textHeight, 0)

	if lines[0].GapBefore != 0 {
		t.Fatalf("first line GapBefore must be 0, got %g", lines[0].GapBefore)
	}
	const eps = 1e-6
	for i := 1; i < len(lines); i++ {
		if diff := math.Abs(lines[i].GapBefore - wantLeading); diff > eps {
			t.Fatalf("line %d GapBefore mismatch: got=%g want=%g diff=%g", i, lines[i].GapBefore, wantLeading, diff)
		}
		if diff := math.Abs(lines[i].Height - textHeight); diff > eps {
			t.Fatalf("line %d Height mismatch: got=%g want=%g diff=%g", i, lines[i].Height, textHeight, diff)
		}
	}
}

// TestGreedyWrapWidthLimit 验证每行宽度不超过限制（mm）。
func TestGreedyWrapWidthLimit(t *testing.T) {
	r := NewRenderer(".")
	font := layout.FontResource{Src: "embed:Inter/static/Inter-Regular.ttf"}
	fontSizeMM := 12 * layout.PtToMm
	lineHeightMM := fontSizeMM * 1.2

	limit := 30.0 // mm
	content := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	lines, err := r.LayoutLines(content, limit, font, fontSizeMM, lineHeightMM, "")
	if err != nil {
		t.Fatalf("LayoutLines error: %v", err)
	}
	if len(lines) == 0 {
		t.Fatalf("expected at least one line")
	}
	for i, ln := range lines {
		if ln.Width-limit > 1e-6 { // 允许极小的数值误差
			t.Fatalf("line %d width exceeds limit: width=%g limit=%g", i, ln.Width, limit)
		}
	}
}
