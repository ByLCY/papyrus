package layout

import (
	"strings"
	"testing"

	"github.com/ByLCY/papyrus/dsl"
)

// buildWithRenderer 是测试辅助：用给定 DSL 文本构建布局结果。
// stubTypesetter 是一个最小实现，仅用于测试，避免引入 renderer 造成循环依赖。
type stubTypesetter struct{}

func (s *stubTypesetter) LayoutLines(content string, width float64, font FontResource, fontSize float64, lineHeight float64, wrap string) ([]TextLine, error) {
	// 极简策略：按空格分词，尽量生成多行；不依赖具体宽度。
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return []TextLine{{Content: "", Width: 0, Height: fontSize}}, nil
	}
	// 分成最多三行：1/2/其余
	n := len(parts)
	cut1 := n / 3
	if cut1 == 0 {
		cut1 = 1
	}
	cut2 := 2 * n / 3
	if cut2 <= cut1 {
		cut2 = cut1 + 1
	}
	if cut2 > n {
		cut2 = n
	}

	lines := []TextLine{}
	mk := func(seg []string) {
		if len(seg) == 0 {
			return
		}
		lines = append(lines, TextLine{Content: strings.Join(seg, " "), Width: 0, Height: fontSize})
	}
	mk(parts[:cut1])
	if cut1 < n {
		mk(parts[cut1:cut2])
	}
	if cut2 < n {
		mk(parts[cut2:])
	}
	if len(lines) == 0 {
		lines = []TextLine{{Content: strings.Join(parts, " "), Width: 0, Height: fontSize}}
	}
	// 不设置 GapBefore（保持 0），由 composeTextBox 根据默认 leading 回填。
	return lines, nil
}

func buildWithRenderer(t *testing.T, dslText string, debugRaw bool) *Result {
	t.Helper()
	doc, err := dsl.Parse(strings.NewReader(dslText))
	if err != nil {
		t.Fatalf("解析 DSL 失败: %v", err)
	}
	ts := &stubTypesetter{}
	res, err := Build(doc, nil, BuildOptions{Typesetter: ts, Debug: DebugOptions{RawUnits: debugRaw}})
	if err != nil {
		t.Fatalf("布局计算失败: %v", err)
	}
	return res
}

// TestTextBoxTotalHeightInvariant 断言：TextBox.Height == Σ(line.Height + line.GapBefore)。
func TestTextBoxTotalHeightInvariant(t *testing.T) {
	dslText := `doc T v1 { page A4 portrait margin 10mm { flow { resources { font Body { src: "embed:Inter/static/Inter-Regular.ttf" } style Body { font: Body size: 12pt line-height: 1.2x } } text Body { "long long long long long long long long long long long long long" } } } }`
	res := buildWithRenderer(t, dslText, false)
	if len(res.Pages) == 0 {
		t.Fatalf("无页面输出")
	}
	found := false
	for _, tb := range res.Pages[0].Texts {
		if len(tb.Lines) == 0 {
			continue
		}
		total := 0.0
		for _, ln := range tb.Lines {
			total += ln.GapBefore + ln.Height
		}
		if diff := abs(total - tb.Height); diff > 1e-6 {
			t.Fatalf("TextBox.Height 不变式不成立: got=%g want=%g diff=%g", tb.Height, total, diff)
		}
		found = true
	}
	if !found {
		t.Fatalf("未找到文本框进行校验")
	}
}

// TestDebugRawUnitsOutput 验证在开启 Debug.RawUnits 后，JSON 里会输出 debug.rawUnits，且语义正确。
func TestDebugRawUnitsOutput(t *testing.T) {
	// 文档 A：使用倍数行高 1.2x
	dslFactor := `doc D1 v1 {
  resources {
    font Body { src: "embed:Inter/static/Inter-Regular.ttf" }
    style S1 { font: Body size: 12pt line-height: 1.2x }
  }
  page A4 portrait margin 10mm { flow { text S1 { "aaaa bbbb" } } }
}`
	res1 := buildWithRenderer(t, dslFactor, true)
	if len(res1.Pages) == 0 || len(res1.Pages[0].Texts) == 0 {
		t.Fatalf("文档 D1 未生成文本")
	}
	tb1 := res1.Pages[0].Texts[0]
	if tb1.Debug == nil || tb1.Debug.RawUnits == nil || tb1.Debug.RawUnits.LineHeight == nil {
		t.Fatalf("D1 缺少 debug.rawUnits.lineHeight")
	}
	if tb1.Debug.RawUnits.LineHeight.Kind != "factor" || tb1.Debug.RawUnits.LineHeight.Factor <= 0 {
		t.Fatalf("D1 行高应为 factor 语义，实际: %#v", tb1.Debug.RawUnits.LineHeight)
	}
	if tb1.Debug.RawUnits.FontSize == nil || tb1.Debug.RawUnits.FontSize.Unit != "pt" || tb1.Debug.RawUnits.FontSize.Value != 12 {
		t.Fatalf("D1 字号应为 12pt，实际: %#v", tb1.Debug.RawUnits.FontSize)
	}

	// 文档 B：使用绝对行高 6mm
	dslAbs := `doc D2 v1 {
  resources {
    font Body { src: "embed:Inter/static/Inter-Regular.ttf" }
    style Base { font: Body size: 12pt }
  }
  page A4 portrait margin 10mm { flow { text Base line-height 6mm { "cccc dddd" } } }
}`
	res2 := buildWithRenderer(t, dslAbs, true)
	if len(res2.Pages) == 0 || len(res2.Pages[0].Texts) == 0 {
		t.Fatalf("文档 D2 未生成文本")
	}
	tb2 := res2.Pages[0].Texts[0]
	if tb2.Debug == nil || tb2.Debug.RawUnits == nil || tb2.Debug.RawUnits.LineHeight == nil {
		t.Fatalf("D2 缺少 debug.rawUnits.lineHeight")
	}
	if tb2.Debug.RawUnits.LineHeight.Kind != "absolute" || tb2.Debug.RawUnits.LineHeight.Unit != "mm" || tb2.Debug.RawUnits.LineHeight.Value != 6 {
		t.Fatalf("D2 行高应为 6mm 绝对值，实际: %#v", tb2.Debug.RawUnits.LineHeight)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestResolveMarginVariants 验证 margin 参数支持 1、2、3、4+ 个值的语义。
func TestResolveMarginVariants(t *testing.T) {
	// helper to build and return first page margin
	get := func(spec string) Margin {
		dslText := "doc T v1 { page " + spec + " { flow { text { \"x\" } } } }"
		res := buildWithRenderer(t, dslText, false)
		if len(res.Pages) == 0 {
			t.Fatalf("未生成页面")
		}
		return res.Pages[0].Margin
	}

	// 1 个参数：四边相同
	m1 := get("A4 portrait margin 10mm")
	if !(eq(m1.Top, 10) && eq(m1.Right, 10) && eq(m1.Bottom, 10) && eq(m1.Left, 10)) {
		t.Fatalf("1 值语义错误: %+v", m1)
	}

	// 2 个参数：上下，左右
	m2 := get("A4 portrait margin 10mm 5mm")
	if !(eq(m2.Top, 10) && eq(m2.Bottom, 10) && eq(m2.Left, 5) && eq(m2.Right, 5)) {
		t.Fatalf("2 值语义错误: %+v", m2)
	}

	// 3 个参数：上 右 下 左=0
	m3 := get("A4 portrait margin 12mm 8mm 6mm")
	if !(eq(m3.Top, 12) && eq(m3.Right, 8) && eq(m3.Bottom, 6) && eq(m3.Left, 0)) {
		t.Fatalf("3 值语义错误: %+v", m3)
	}

	// 4 个参数：上 右 下 左
	m4 := get("A4 portrait margin 1cm 5mm 2cm 3mm") // 含不同单位
	if !(eq(m4.Top, 10) && eq(m4.Right, 5) && eq(m4.Bottom, 20) && eq(m4.Left, 3)) {
		t.Fatalf("4 值语义错误: %+v", m4)
	}

	// >4 个参数：只取前四个
	m5 := get("A4 portrait margin 1mm 2mm 3mm 4mm 999mm 888mm")
	if !(eq(m5.Top, 1) && eq(m5.Right, 2) && eq(m5.Bottom, 3) && eq(m5.Left, 4)) {
		t.Fatalf(">4 值应忽略多余: %+v", m5)
	}
}

func eq(a, b float64) bool { return abs(a-b) < 1e-6 }

// --- 新增：文本对齐相关测试 ---

// TestTextAlignExplicit 验证在普通 flow 中显式声明 align 生效
func TestTextAlignExplicit(t *testing.T) {
	dslText := `doc T v1 {
  resources {
    font Body { src: "embed:Inter/static/Inter-Regular.ttf" }
    style Body { font: Body size: 12pt }
  }
  page A4 portrait margin 10mm {
    flow {
      text Body align right { "Hello" }
    }
  }
}`
	res := buildWithRenderer(t, dslText, false)
	if len(res.Pages) == 0 || len(res.Pages[0].Texts) == 0 {
		t.Fatalf("未生成文本")
	}
	tb := res.Pages[0].Texts[0]
	if tb.Align != "right" {
		t.Fatalf("显式 align 未生效: got=%q want=\"right\"", tb.Align)
	}
}

// TestTextAlignInheritFlow 验证未显式声明时从父 flow 继承对齐
func TestTextAlignInheritFlow(t *testing.T) {
	dslText := `doc T v1 {
  resources {
    font Body { src: "embed:Inter/static/Inter-Regular.ttf" }
    style Body { font: Body size: 12pt }
  }
  page A4 portrait margin 10mm {
    flow align center {
      text Body { "Hello" }
    }
  }
}`
	res := buildWithRenderer(t, dslText, false)
	if len(res.Pages) == 0 || len(res.Pages[0].Texts) == 0 {
		t.Fatalf("未生成文本")
	}
	tb := res.Pages[0].Texts[0]
	if tb.Align != "center" {
		t.Fatalf("flow 继承对齐未生效: got=%q want=\"center\"", tb.Align)
	}
}

// TestTextAlignAliases 验证 start/end 别名映射
func TestTextAlignAliases(t *testing.T) {
	dslText := `doc T v1 {
  resources {
    font Body { src: "embed:Inter/static/Inter-Regular.ttf" }
    style Body { font: Body size: 12pt }
  }
  page A4 portrait margin 10mm {
    flow {
      text Body align end { "Hello" }
    }
  }
}`
	res := buildWithRenderer(t, dslText, false)
	if len(res.Pages) == 0 || len(res.Pages[0].Texts) == 0 {
		t.Fatalf("未生成文本")
	}
	tb := res.Pages[0].Texts[0]
	if tb.Align != "right" {
		t.Fatalf("align end 未映射为 right: got=%q want=\"right\"", tb.Align)
	}
}
