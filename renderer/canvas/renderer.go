package canvasrenderer

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/pdf"

	"github.com/ByLCY/papyrus/fonts"
	"github.com/ByLCY/papyrus/layout"
	"github.com/ByLCY/papyrus/renderer"
)

const tableBorderWidth = 0.2

// Renderer draws layout results via github.com/tdewolff/canvas.
type Renderer struct {
	baseDir string

	// injected resources
	fontBlobs  map[string][]byte // by unique name
	imageBlobs map[string][]byte // by unique name

	fontMu         sync.Mutex
	fontFamilies   map[string]*fontFamilyEntry
	fallbackFamily *canvas.FontFamily
}

var (
	_ renderer.Renderer = (*Renderer)(nil)
	_ layout.Typesetter = (*Renderer)(nil)
)

type fontFamilyEntry struct {
	family *canvas.FontFamily
	style  canvas.FontStyle
}

// Options configures the canvas renderer.
type Options struct {
	BaseDir string
	Fonts   map[string]Resource // built-in fonts accessible via built-in:<name>
	Images  map[string]Resource // built-in images accessible via built-in:<name>
}

// Resource can be provided either by Bytes or by Path.
type Resource struct {
	Bytes []byte
	Path  string
}

// NewRenderer creates a canvas-based renderer rooted at baseDir for resolving assets.
func NewRenderer(baseDir string) *Renderer { return NewRendererWithOptions(Options{BaseDir: baseDir}) }

// NewRendererWithOptions creates a renderer with injected resources and optional baseDir.
func NewRendererWithOptions(opts Options) *Renderer {
	r := &Renderer{
		baseDir:        opts.BaseDir,
		fontBlobs:      map[string][]byte{},
		imageBlobs:     map[string][]byte{},
		fontFamilies:   map[string]*fontFamilyEntry{},
		fallbackFamily: nil,
	}
	// ingest fonts
	for name, res := range opts.Fonts {
		if name == "" {
			continue
		}
		if _, ok := r.fontBlobs[name]; ok {
			// last one wins to keep simple; alternatively could panic
		}
		if len(res.Bytes) > 0 {
			r.fontBlobs[name] = res.Bytes
			continue
		}
		if res.Path != "" {
			data, _ := os.ReadFile(res.Path) // ignore error here; will be caught when actually used
			if len(data) > 0 {
				r.fontBlobs[name] = data
			}
		}
	}
	// ingest images
	for name, res := range opts.Images {
		if name == "" {
			continue
		}
		if len(res.Bytes) > 0 {
			r.imageBlobs[name] = res.Bytes
			continue
		}
		if res.Path != "" {
			data, _ := os.ReadFile(res.Path)
			if len(data) > 0 {
				r.imageBlobs[name] = data
			}
		}
	}
	return r
}

// Render renders the result into a PDF byte slice.
func (r *Renderer) Render(result *layout.Result) ([]byte, error) {
	if result == nil {
		return nil, fmt.Errorf("渲染结果为空")
	}
	if len(result.Pages) == 0 {
		return nil, fmt.Errorf("缺少可渲染的页面")
	}

	var buf bytes.Buffer
	writer := pdf.New(&buf, result.Pages[0].Width, result.Pages[0].Height, nil)
	r.applyMeta(writer, result.Meta)
	for i, page := range result.Pages {
		if i > 0 {
			writer.NewPage(page.Width, page.Height)
		}
		c := canvas.New(page.Width, page.Height)
		ctx := canvas.NewContext(c)
		ctx.SetCoordSystem(canvas.CartesianIV) // 使坐标与布局保持左上角为原点

		if err := r.drawPage(ctx, page, result.Resources); err != nil {
			return nil, err
		}
		c.RenderTo(writer)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("写入 PDF 失败: %w", err)
	}
	return buf.Bytes(), nil
}

func (r *Renderer) applyMeta(writer *pdf.PDF, meta layout.DocumentMeta) {
	if writer == nil {
		return
	}
	keywords := strings.Join(meta.Keywords, ", ")
	writer.SetInfo(meta.Title, meta.Subject, keywords, meta.Author, meta.Creator)
}

// LayoutLines 实现 layout.Typesetter 接口，使用贪心换行算法。
// 约定：fontSize/lineHeight 入参均为毫米（mm）。渲染器内部与字体系统交互使用 pt，并在边界做 mm↔pt 换算。
func (r *Renderer) LayoutLines(content string, width float64, font layout.FontResource, fontSize, lineHeight float64, wrap string) ([]layout.TextLine, error) {
	// 将字号从 mm 转为 pt 以创建字体面
	sizePt := toPt(fontSize)
	face, err := r.fontFace(font, sizePt, layout.Color{R: 30, G: 30, B: 30})
	if err != nil {
		return nil, err
	}
	
	// 在贪心换行中，所有宽度比较与累计均使用 mm
	if wrap == "" {
		wrap = "anywhere"
	}
	lines := greedyWrapTokens(content, width, face, wrap)
	textMetrics := face.Metrics()
	textHeight := textMetrics.LineHeight
	if textHeight <= 0 {
		textHeight = lineHeight
	}
	leading := math.Max(lineHeight-textHeight, 0)
	if len(lines) == 0 {
		lines = []layout.TextLine{{
			Content: "",
			Width:   0,
			Height:  textHeight,
		}}
	}
	for i := range lines {
		if lines[i].Height <= 0 {
			lines[i].Height = textHeight
		}
		if i == 0 {
			lines[i].GapBefore = 0
		} else {
			lines[i].GapBefore = leading
		}
	}
	return lines, nil
}

func (r *Renderer) drawPage(ctx *canvas.Context, page layout.Page, resources layout.ResourceSet) error {
	// 先绘制页眉（先形状作为背景，再文本/图片）
	if err := r.drawLines(ctx, page.Header.Lines); err != nil { return err }
	if err := r.drawRects(ctx, page.Header.Rects); err != nil { return err }
	if err := r.drawCircles(ctx, page.Header.Circles); err != nil { return err }
	for _, tb := range page.Header.Texts {
		fontRes := resolveFontResource(tb.Font, resources.Fonts)
		if err := r.drawTextBox(ctx, tb, fontRes); err != nil {
			return err
		}
	}
	if err := r.drawImages(ctx, page.Header.Images); err != nil {
		return err
	}

	// 背景形状（线、矩形、圆）在主体内容之前绘制
	if err := r.drawLines(ctx, page.Lines); err != nil { return err }
	if err := r.drawRects(ctx, page.Rects); err != nil { return err }
	if err := r.drawCircles(ctx, page.Circles); err != nil { return err }

	// 绘制主体内容
	for _, textBox := range page.Texts {
		fontRes := resolveFontResource(textBox.Font, resources.Fonts)
		if err := r.drawTextBox(ctx, textBox, fontRes); err != nil {
			return err
		}
	}
	if err := r.drawImages(ctx, page.Images); err != nil {
		return err
	}
	if err := r.drawTables(ctx, page.Tables, resources.Fonts); err != nil {
		return err
	}

	// 最后绘制页脚（先形状作为背景，再文本与图片）
	if err := r.drawLines(ctx, page.Footer.Lines); err != nil { return err }
	if err := r.drawRects(ctx, page.Footer.Rects); err != nil { return err }
	if err := r.drawCircles(ctx, page.Footer.Circles); err != nil { return err }
	for _, tb := range page.Footer.Texts {
		fontRes := resolveFontResource(tb.Font, resources.Fonts)
		if err := r.drawTextBox(ctx, tb, fontRes); err != nil {
			return err
		}
	}
	if err := r.drawImages(ctx, page.Footer.Images); err != nil {
		return err
	}
	return nil
}

func (r *Renderer) drawTextBox(ctx *canvas.Context, tb layout.TextBox, fontRes layout.FontResource) error {
	// TextBox 的坐标/字号/行高均为 mm；创建字体面需要 pt，这里做一次 mm→pt。
	face, err := r.fontFace(fontRes, toPt(tb.FontSize), tb.Color)
	if err != nil {
		return err
	}

	lines := tb.Lines
	if len(lines) == 0 {
		lines = []layout.TextLine{
			{
				Content: tb.Content,
				Width:   tb.Width,
				Height:  tb.LineHeight,
			},
		}
	}

	// 处理水平对齐：left（默认）/center/right。
	align := strings.ToLower(tb.Align)
	var textAlign canvas.TextAlign
	var anchorX float64
	switch align {
	case "center":
		textAlign = canvas.Center
		anchorX = tb.X + tb.Width/2
	case "right", "end":
		textAlign = canvas.Right
		anchorX = tb.X + tb.Width
	default:
		textAlign = canvas.Left
		anchorX = tb.X
	}

	cursorY := tb.Y
	for _, line := range lines {
		cursorY += line.GapBefore
		textLine := canvas.NewTextLine(face, line.Content, textAlign)

		lineHeight := line.Height
		if lineHeight <= 0 {
			if tb.FontSize > 0 {
				lineHeight = tb.FontSize
			} else {
				lineHeight = tb.LineHeight
			}
		}

		// 基线位置：以行顶部（cursorY，mm）加上字体上升部（Ascent，pt→mm）
		metrics := face.Metrics()
		baseline := cursorY + metrics.Ascent

		// 根据对齐方式在 anchorX 位置绘制文本
		ctx.DrawText(anchorX, baseline, textLine)
		cursorY += lineHeight
	}
	return nil
}

func (r *Renderer) drawImages(ctx *canvas.Context, images []layout.ImageBox) error {
	for _, img := range images {
		if img.Path == "" {
			continue
		}
		orig := img.Path
		var (
			imgData image.Image
			err     error
		)
		// built-in resources take precedence
		if strings.HasPrefix(orig, "built-in:") || strings.HasPrefix(orig, "builtin:") {
			name := strings.TrimPrefix(strings.TrimPrefix(orig, "built-in:"), "builtin:")
			blob, ok := r.imageBlobs[name]
			if !ok {
				return fmt.Errorf("找不到内置图片资源 built-in:%s", name)
			}
			imgData, _, err = image.Decode(bytes.NewReader(blob))
			if err != nil {
				return fmt.Errorf("解码内置图片 built-in:%s 失败: %w", name, err)
			}
		} else if strings.HasPrefix(orig, "embed:") {
			// 当前未内置图片资源，按找不到资源处理
			return fmt.Errorf("图片资源 %s 未找到（embed 仅支持内置字体，暂不支持图片）", orig)
		} else {
			// path based
			if r.baseDir == "" && !filepath.IsAbs(orig) {
				return fmt.Errorf("未指定资源目录时不允许直接使用路径：%s（请改用 built-in: 或 embed:）", orig)
			}
			path := orig
			if !filepath.IsAbs(path) {
				path = filepath.Join(r.baseDir, path)
			}
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("读取图片 %s 失败: %w", orig, err)
			}
			imgData, _, err = image.Decode(file)
			file.Close()
			if err != nil {
				return fmt.Errorf("解码图片 %s 失败: %w", orig, err)
			}
		}

		width := img.Width
		if width <= 0 {
			if imgData.Bounds().Dx() > 0 {
				width = float64(imgData.Bounds().Dx()) / 4.0
			} else {
				width = 40.0
			}
		}
		dpmm := float64(imgData.Bounds().Dx()) / width
		if dpmm <= 0 {
			dpmm = 1
		}
		ctx.DrawImage(img.X, img.Y, imgData, canvas.DPMM(dpmm))
	}
	return nil
}

func (r *Renderer) drawTables(ctx *canvas.Context, tables []layout.TableBox, fonts map[string]layout.FontResource) error {
	for _, table := range tables {
		if len(table.ColumnWidths) == 0 {
			continue
		}
		for _, row := range table.Rows {
			x := table.X
			for idx, cell := range row.Cells {
				colIdx := idx
				if colIdx >= len(table.ColumnWidths) {
					colIdx = len(table.ColumnWidths) - 1
				}
				colWidth := table.ColumnWidths[colIdx]
				fill := canvas.White
				if row.IsHeader {
					fill = canvas.Hex("#f8f8f8")
				}
				ctx.SetFillColor(fill)
				ctx.SetStrokeColor(colorFromLayout(table.BorderColor))
				ctx.SetStrokeWidth(tableBorderWidth)
				ctx.DrawPath(x, row.Y, canvas.Rectangle(colWidth, row.Height))
				
				fontRes := resolveFontResource(cell.Text.Font, fonts)
				textBox := cell.Text
				textBox.X += tableBorderWidth
				textBox.Y += tableBorderWidth
				if err := r.drawTextBox(ctx, textBox, fontRes); err != nil {
					return err
				}
				x += colWidth
			}
		}
	}
	return nil
}

// drawLines 绘制直线列表（毫米单位）
func (r *Renderer) drawLines(ctx *canvas.Context, lines []layout.Line) error {
	for _, ln := range lines {
		w := ln.Width
		if w <= 0 {
			w = tableBorderWidth
		}
		ctx.SetStrokeColor(colorFromLayout(ln.Color))
		ctx.SetStrokeWidth(w)
		p := &canvas.Path{}
		p.MoveTo(0, 0)
		p.LineTo(ln.X2-ln.X1, ln.Y2-ln.Y1)
		ctx.DrawPath(ln.X1, ln.Y1, p)
	}
	return nil
}

// drawRects 绘制矩形
func (r *Renderer) drawRects(ctx *canvas.Context, rects []layout.Rect) error {
	for _, rc := range rects {
		w := rc.StrokeWidth
		if w <= 0 {
			w = tableBorderWidth
		}
		if rc.FillColor != nil {
			ctx.SetFillColor(colorFromLayout(*rc.FillColor))
		} else {
			ctx.SetFillColor(color.RGBA{0, 0, 0, 0})
		}
		ctx.SetStrokeColor(colorFromLayout(rc.StrokeColor))
		ctx.SetStrokeWidth(w)
		ctx.DrawPath(rc.X, rc.Y, canvas.Rectangle(rc.Width, rc.Height))
	}
	return nil
}

// drawCircles 绘制圆形
func (r *Renderer) drawCircles(ctx *canvas.Context, circles []layout.Circle) error {
	for _, c := range circles {
		w := c.StrokeWidth
		if w <= 0 {
			w = tableBorderWidth
		}
		if c.FillColor != nil {
			ctx.SetFillColor(colorFromLayout(*c.FillColor))
		} else {
			ctx.SetFillColor(color.RGBA{0, 0, 0, 0})
		}
		ctx.SetStrokeColor(colorFromLayout(c.StrokeColor))
		ctx.SetStrokeWidth(w)
		ctx.DrawPath(c.CX-c.R, c.CY-c.R, canvas.Circle(c.R))
	}
	return nil
}

func (r *Renderer) fontFace(font layout.FontResource, size float64, col layout.Color) (*canvas.FontFace, error) {
	family, style, err := r.ensureFontFamily(font)
	if err != nil {
		return nil, err
	}
	return family.Face(size, colorFromLayout(col), style, canvas.FontNormal), nil
}

func (r *Renderer) ensureFontFamily(font layout.FontResource) (*canvas.FontFamily, canvas.FontStyle, error) {
	key := fontCacheKey(font)
	r.fontMu.Lock()
	defer r.fontMu.Unlock()

	if entry, ok := r.fontFamilies[key]; ok {
		return entry.family, entry.style, nil
	}

	style := parseFontStyle(font.Style)
	familyName := font.Family
	if familyName == "" {
		familyName = font.Name
	}
	if familyName == "" {
		familyName = "Body"
	}
	family := canvas.NewFontFamily(familyName)

	if err := r.loadFontIntoFamily(family, font, style); err != nil {
		fallback, fbStyle, fbErr := r.fallback()
		if fbErr != nil {
			return nil, canvas.FontRegular, err
		}
		r.fontFamilies[key] = &fontFamilyEntry{family: fallback, style: fbStyle}
		return fallback, fbStyle, nil
	}

	entry := &fontFamilyEntry{family: family, style: style}
	r.fontFamilies[key] = entry
	return family, style, nil
}

func (r *Renderer) loadFontIntoFamily(family *canvas.FontFamily, font layout.FontResource, style canvas.FontStyle) error {
	data, err := r.loadFontBytes(font)
	if err != nil {
		return err
	}
	return family.LoadFont(data, 0, style)
}

func (r *Renderer) loadFontBytes(font layout.FontResource) ([]byte, error) {
	if font.Src == "" {
		return nil, fmt.Errorf("字体 %s 缺少 src", font.Name)
	}
	src := font.Src
	if strings.HasPrefix(src, "built-in:") || strings.HasPrefix(src, "builtin:") {
		name := strings.TrimPrefix(strings.TrimPrefix(src, "built-in:"), "builtin:")
		if blob, ok := r.fontBlobs[name]; ok {
			return blob, nil
		}
		return nil, fmt.Errorf("找不到内置字体资源 built-in:%s", name)
	}
	if strings.HasPrefix(src, "embed:") {
		return fonts.Load(strings.TrimPrefix(src, "embed:"))
	}
	// Path based
	path := src
	if r.baseDir == "" && !filepath.IsAbs(path) {
		return nil, fmt.Errorf("未指定资源目录时不允许直接使用字体路径：%s（请改用 built-in: 或 embed:）", src)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.baseDir, path)
	}
	return os.ReadFile(path)
}

func (r *Renderer) fallback() (*canvas.FontFamily, canvas.FontStyle, error) {
	if r.fallbackFamily != nil {
		return r.fallbackFamily, canvas.FontRegular, nil
	}
	data, err := fonts.Load("Inter/static/Inter-Regular.ttf")
	if err != nil {
		return nil, canvas.FontRegular, err
	}
	family := canvas.NewFontFamily("papyrus-fallback")
	if err := family.LoadFont(data, 0, canvas.FontRegular); err != nil {
		return nil, canvas.FontRegular, err
	}
	r.fallbackFamily = family
	return family, canvas.FontRegular, nil
}

func resolveFontResource(name string, fonts map[string]layout.FontResource) layout.FontResource {
	if font, ok := fonts[name]; ok {
		return font
	}
	if font, ok := fonts["Body"]; ok {
		return font
	}
	for _, font := range fonts {
		return font
	}
	return layout.FontResource{}
}

func parseFontStyle(style string) canvas.FontStyle {
	if style == "" {
		return canvas.FontRegular
	}
	s := strings.ToLower(style)
	result := canvas.FontRegular
	switch {
	case strings.Contains(s, "black"):
		result = canvas.FontBlack
	case strings.Contains(s, "extrabold"):
		result = canvas.FontExtraBold
	case strings.Contains(s, "bold"):
		result = canvas.FontBold
	case strings.Contains(s, "semibold"), strings.Contains(s, "demibold"):
		result = canvas.FontSemiBold
	case strings.Contains(s, "medium"):
		result = canvas.FontMedium
	case strings.Contains(s, "light"):
		result = canvas.FontLight
	default:
		result = canvas.FontRegular
	}
	if strings.Contains(s, "italic") || strings.Contains(s, "oblique") || strings.Contains(style, "I") {
		result |= canvas.FontItalic
	}
	if strings.Contains(style, "B") && !strings.Contains(s, "bold") {
		result = canvas.FontBold | (result & canvas.FontItalic)
	}
	return result
}

func fontCacheKey(font layout.FontResource) string {
	return fmt.Sprintf("%s|%s|%s", font.Name, font.Src, font.Style)
}

func colorFromLayout(c layout.Color) color.Color {
	return canvas.RGBA(float64(c.R)/255.0, float64(c.G)/255.0, float64(c.B)/255.0, 1.0)
}

// toPt 将毫米(mm)转换为点(pt)。
func toPt(mm float64) float64 { return mm * layout.MmToPt }

// toMm 将点(pt)转换为毫米(mm)。
func toMm(pt float64) float64 { return pt * layout.PtToMm }

func greedyWrapTokens(content string, width float64, face *canvas.FontFace, wrap string) []layout.TextLine {
	// 说明：本函数内部的所有宽度单位在逻辑上按 mm 处理；canvas 的 TextWidth 返回的值已在现有实现中用于与 width 比较，保持现状避免破坏兼容。
	limit := width
	if limit <= 0 {
		limit = math.MaxFloat64
	}

	// nowrap：仅按显式换行划分，不基于宽度折行
	if wrap == "nowrap" {
		parts := strings.Split(content, "\n")
		lines := make([]layout.TextLine, 0, len(parts))
		for _, p := range parts {
			w := face.TextWidth(p)
			lines = append(lines, layout.TextLine{Content: p, Width: w})
		}
		return lines
	}

	// break-word：忽略空白机会，纯按宽度切分（但仍然尊重显式换行）
	if wrap == "break-word" {
		var lines []layout.TextLine
		var builder strings.Builder
		current := 0.0
		emit := func(force bool) {
			if builder.Len() == 0 {
				if force {
					lines = append(lines, layout.TextLine{Content: "", Width: 0})
				}
				return
			}
			str := builder.String()
			lines = append(lines, layout.TextLine{Content: str, Width: current})
			builder.Reset()
			current = 0
		}
		for _, r := range content {
			if r == '\r' {
				continue
			}
			if r == '\n' {
				emit(true)
				continue
			}
			s := string(r)
			cw := face.TextWidth(s)
			if current > 0 && current+cw > limit {
				emit(false)
			}
			builder.WriteString(s)
			current += cw
			if current > limit {
				emit(false)
			}
		}
		emit(true)
		return lines
	}

	// 默认（anywhere/normal 等）：优先在空白处分割，超过限制时在词内拆分
	tokens := tokenizeContent(content)
	var lines []layout.TextLine
	var builder strings.Builder
	currentWidth := 0.0

	emit := func(force bool) {
		if builder.Len() == 0 {
			if force {
				lines = append(lines, layout.TextLine{Content: "", Width: 0})
			}
			return
		}
		lineStr := builder.String()
		lines = append(lines, layout.TextLine{
			Content: lineStr,
			Width:   currentWidth,
		})
		builder.Reset()
		currentWidth = 0
	}

	appendToken := func(token string) {
		builder.WriteString(token)
		currentWidth += face.TextWidth(token)
	}

	for _, token := range tokens {
		if token == "\n" {
			emit(true)
			continue
		}

		tokenWidth := face.TextWidth(token)
		if currentWidth > 0 && currentWidth+tokenWidth > limit {
			emit(false)
		}
		if tokenWidth <= limit {
			appendToken(token)
			if currentWidth > limit {
				emit(false)
			}
			continue
		}

		for _, chunk := range splitTokenByWidth(token, limit, face) {
			chunkWidth := face.TextWidth(chunk)
			if currentWidth > 0 && currentWidth+chunkWidth > limit {
				emit(false)
			}
			appendToken(chunk)
			if currentWidth > limit {
				emit(false)
			}
		}
	}

	emit(true)
	return lines
}

func tokenizeContent(s string) []string {
	var tokens []string
	var builder strings.Builder
	lastWasSpace := false
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		tokens = append(tokens, builder.String())
		builder.Reset()
	}

	for _, r := range s {
		if r == '\r' {
			continue
		}
		if r == '\n' {
			flush()
			tokens = append(tokens, "\n")
			lastWasSpace = false
			continue
		}
		isSpace := unicode.IsSpace(r)
		if builder.Len() == 0 {
			lastWasSpace = isSpace
		} else if lastWasSpace != isSpace {
			flush()
			lastWasSpace = isSpace
		}
		builder.WriteRune(r)
	}
	flush()
	return tokens
}

func splitTokenByWidth(token string, limit float64, face *canvas.FontFace) []string {
	// 说明：limit 为 mm，需要将 canvas 返回的宽度（pt）转换为 mm 后再比较
	if limit <= 0 || limit == math.MaxFloat64 {
		return []string{token}
	}
	var parts []string
	var builder strings.Builder
	for _, r := range token {
		builder.WriteRune(r)
		if face.TextWidth(builder.String()) > limit && builder.Len() > 1 {
			runes := []rune(builder.String())
			parts = append(parts, string(runes[:len(runes)-1]))
			builder.Reset()
			builder.WriteRune(r)
		}
	}
	if builder.Len() > 0 {
		parts = append(parts, builder.String())
	}
	return parts
}
