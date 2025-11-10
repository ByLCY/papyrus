package layout

import (
    "math"
    "testing"
)

// TestPtMmRoundTrip 验证 pt↔mm 换算的往返精度（允许极小的浮点误差）。
func TestPtMmRoundTrip(t *testing.T) {
    samples := []float64{0, 0.001, 1, 12, 14.4, 72, 96, 144, 1000}
    for _, pt := range samples {
        mm := pt * PtToMm
        back := mm * MmToPt
        if diff := math.Abs(back-pt); diff > 1e-9 {
            t.Fatalf("pt→mm→pt 往返误差过大: in=%gpt mm=%g back=%g diff=%g", pt, mm, back, diff)
        }
    }
    for _, mm := range samples {
        pt := mm * MmToPt
        back := pt * PtToMm
        if diff := math.Abs(back-mm); diff > 1e-9 {
            t.Fatalf("mm→pt→mm 往返误差过大: in=%gmm pt=%g back=%g diff=%g", mm, pt, back, diff)
        }
    }
}

// TestLengthToConversions 覆盖 Length 在常见单位上的转换正确性（到 mm/pt）。
func TestLengthToConversions(t *testing.T) {
    // 1 in = 25.4 mm
    in := Length{Value: 1, Unit: UnitIN}
    if got := in.ToMM(); math.Abs(got-25.4) > 1e-9 {
        t.Fatalf("1in 转 mm 期望 25.4，实际 %g", got)
    }
    // 2.54 cm = 25.4 mm
    cm := Length{Value: 2.54, Unit: UnitCM}
    if got := cm.ToMM(); math.Abs(got-25.4) > 1e-9 {
        t.Fatalf("2.54cm 转 mm 期望 25.4，实际 %g", got)
    }
    // 12 pt → mm
    pt := Length{Value: 12, Unit: UnitPT}
    if got := pt.ToMM(); math.Abs(got-12*PtToMm) > 1e-9 {
        t.Fatalf("12pt 转 mm 期望 %g，实际 %g", 12*PtToMm, got)
    }
    // 10 mm → pt
    mm := Length{Value: 10, Unit: UnitMM}
    if got := mm.ToPT(); math.Abs(got-10*MmToPt) > 1e-9 {
        t.Fatalf("10mm 转 pt 期望 %g，实际 %g", 10*MmToPt, got)
    }
}

// TestLineHeightResolve 验证行高解析：倍数与绝对值两种语义在目标单位（mm）下的解析结果。
func TestLineHeightResolve(t *testing.T) {
    fontSizePT := Length{Value: 12, Unit: UnitPT}
    // 倍数：1.2x
    lhFactor := LineHeightSpec{Kind: LineHeightFactor, Factor: 1.2}
    gotMM := lhFactor.Resolve(fontSizePT, UnitMM)
    wantMM := 12 * 1.2 * PtToMm
    if diff := math.Abs(gotMM-wantMM); diff > 1e-9 {
        t.Fatalf("1.2x 解析为 mm 错误: got=%g want=%g diff=%g", gotMM, wantMM, diff)
    }
    // 绝对：18pt
    lhAbsPT := LineHeightSpec{Kind: LineHeightAbsolute, Len: Length{Value: 18, Unit: UnitPT}}
    gotMM = lhAbsPT.Resolve(fontSizePT, UnitMM)
    wantMM = 18 * PtToMm
    if diff := math.Abs(gotMM-wantMM); diff > 1e-9 {
        t.Fatalf("18pt 行高解析为 mm 错误: got=%g want=%g diff=%g", gotMM, wantMM, diff)
    }
    // 绝对：6mm
    lhAbsMM := LineHeightSpec{Kind: LineHeightAbsolute, Len: Length{Value: 6, Unit: UnitMM}}
    gotMM = lhAbsMM.Resolve(fontSizePT, UnitMM)
    wantMM = 6
    if diff := math.Abs(gotMM-wantMM); diff > 1e-9 {
        t.Fatalf("6mm 行高解析为 mm 错误: got=%g want=%g diff=%g", gotMM, wantMM, diff)
    }
}
