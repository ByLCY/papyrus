package layout

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ByLCY/papyrus/binding"
	"github.com/ByLCY/papyrus/dsl"
)

const (
	blockSpacing       = 3.0
	defaultTableRowGap = 0.0
	cellPadding        = 1.2
)

// Build 根据 DSL AST 生成页面、文本、图片与表格的布局结果。
func Build(doc *dsl.Document, data any, opts BuildOptions) (*Result, error) {
	if doc == nil {
		return nil, fmt.Errorf("文档为空")
	}
	if opts.Typesetter == nil {
		return nil, fmt.Errorf("layout: 缺少排版后端 Typesetter")
	}

	res, err := collectResources(doc)
	if err != nil {
		return nil, err
	}
	meta := collectMeta(doc)
	pageSection := firstPage(doc)
	if pageSection == nil {
		return nil, fmt.Errorf("文档中缺少 page 段落")
	}

	pages, err := buildPages(pageSection, res, data, opts)
	if err != nil {
		return nil, err
	}

	return &Result{
		Pages:     pages,
		Resources: res,
		Meta:      meta,
	}, nil
}

func buildPages(section *dsl.PageSection, res ResourceSet, data any, opts BuildOptions) ([]Page, error) {
	width, height, err := resolvePageSize(section.Spec)
	if err != nil {
		return nil, err
	}

	margin := resolveMargin(section.Spec.Params)
	collector := newPageCollector(width, height, margin)

	// 先扫描页眉/页脚定义，计算其高度与元素，更新内容区域。
	if section.Block == nil {
		return nil, fmt.Errorf("page 段落缺少内容")
	}
	var headerDef, footerDef *dsl.Command
	for _, st := range section.Block.Statements {
		if st.Command == nil {
			continue
		}
		switch st.Command.Name {
		case "header":
			headerDef = st.Command
		case "footer":
			footerDef = st.Command
		}
	}
	if headerDef != nil {
		hf, err := buildHeaderFooter(headerDef, width, height, margin, res, data, opts.Typesetter, opts.Debug, "header")
		if err != nil {
			return nil, err
		}
		collector.header = hf
	}
	if footerDef != nil {
		hf, err := buildHeaderFooter(footerDef, width, height, margin, res, data, opts.Typesetter, opts.Debug, "footer")
		if err != nil {
			return nil, err
		}
		collector.footer = hf
	}

	// 根上下文从内容区域顶部开始排版。
	root := &flowContext{
		baseX:          margin.Left,
		baseY:          collector.contentTop(),
		width:          width - margin.Left - margin.Right,
		cursorY:        collector.contentTop(),
		data:           data,
		typesetter:     opts.Typesetter,
		debug:          opts.Debug,
		parent:         nil,
		collector:      collector,
		margin:         margin,
		allowPageBreak: true,
		textWrap:       "anywhere",
	}

	if err := processBlock(section.Block, root, res); err != nil {
		return nil, err
	}

	return collector.pages(), nil
}

// processBlock 会依次处理 block 内的命令，支持 flow、absolute、text、image、table。
func processBlock(block *dsl.Block, ctx *flowContext, res ResourceSet) error {
	for _, stmt := range block.Statements {
		if stmt.Command == nil {
			continue
		}
		cmd := stmt.Command
		switch cmd.Name {
		case "flow":
			if err := handleFlow(cmd, ctx, res); err != nil {
				return err
			}
		case "absolute":
			if err := handleAbsolute(cmd, ctx, res); err != nil {
				return err
			}
		case "text":
			if err := handleText(cmd, ctx, res); err != nil {
				return err
			}
		case "image":
			if err := handleImage(cmd, ctx, res); err != nil {
				return err
			}
		case "table":
			if err := handleTable(cmd, ctx, res); err != nil {
				return err
			}
 	default:
			// 形状命令（page-level 背景图形，坐标为页面坐标，允许在任意层级声明）
			name := strings.ToLower(cmd.Name)
			if name == "line" || name == "rect" || name == "circle" {
				_, attrs := parseArgs(cmd.Args, false)
				switch name {
				case "line":
					if ln, ok := parseLineShape(attrs, res); ok {
						ctx.collector.curr().lines = append(ctx.collector.curr().lines, ln)
					}
				case "rect":
					if rc, ok := parseRectShape(attrs, res); ok {
						ctx.collector.curr().rects = append(ctx.collector.curr().rects, rc)
					}
				case "circle":
					if c, ok := parseCircleShape(attrs, res); ok {
						ctx.collector.curr().circles = append(ctx.collector.curr().circles, c)
					}
				}
				continue
			}
			// 其余命令暂未实现，忽略即可
			continue
		}
	}
	return nil
}

func normalizeWrap(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "auto", "anywhere", "overflow-wrap:anywhere", "overflow-anywhere":
		return "anywhere"
	case "break-word", "word-break:break-word":
		return "break-word"
	case "nowrap", "no-wrap":
		return "nowrap"
	case "normal":
		return "normal"
	default:
		return "anywhere"
	}
}

func handleFlow(cmd *dsl.Command, parent *flowContext, res ResourceSet) error {
	if cmd.Block == nil {
		return fmt.Errorf("flow 语句缺少子内容")
	}
	styleName, attrs := parseArgs(cmd.Args, false)
	attrs = mergeStyleAttributes(styleName, attrs, res.Styles)
	width := parent.width
	if v := attrs["width"]; v != "" {
		if w := parseDimension(v, parent.width); w > 0 && w <= parent.width {
			width = w
		}
	} else if a := strings.ToLower(attrs["align"]); a == "center" || a == "right" || a == "end" {
		if inferred := inferFlowWidth(cmd.Block, res, parent.width, parent.typesetter); inferred > 0 {
			width = math.Min(inferred, parent.width)
		}
	}

	offset := alignOffset(parent.width, width, attrs["align"])

	// 规范化本 flow 的文本对齐方式，供子 text 继承
	flowAlign := strings.ToLower(attrs["align"])
	if flowAlign == "start" {
		flowAlign = "left"
	}
	if flowAlign == "end" {
		flowAlign = "right"
	}
	if flowAlign != "left" && flowAlign != "center" && flowAlign != "right" {
		flowAlign = ""
	}
	// 规范化本 flow 的折行策略，供子 text 继承（默认 anywhere）
	flowWrap := parent.textWrap
	if v, ok := attrs["wrap"]; ok && strings.TrimSpace(v) != "" {
		flowWrap = normalizeWrap(v)
	}

	child := &flowContext{
		baseX:          parent.baseX + offset,
		baseY:          parent.cursorY,
		width:          width,
		cursorY:        parent.cursorY,
		data:           parent.data,
		typesetter:     parent.typesetter,
		debug:          parent.debug,
		parent:         parent,
		collector:      parent.collector,
		margin:         parent.margin,
		allowPageBreak: parent.allowPageBreak,
		textAlign:      flowAlign,
		textWrap:       flowWrap,
	}

	if err := processBlock(cmd.Block, child, res); err != nil {
		return err
	}

	if child.cursorY > parent.cursorY {
		parent.cursorY = child.cursorY + blockSpacing
	}
	return nil
}

func handleAbsolute(cmd *dsl.Command, parent *flowContext, res ResourceSet) error {
	if cmd.Block == nil {
		return fmt.Errorf("absolute 语句缺少子内容")
	}
	styleName, attrs := parseArgs(cmd.Args, false)
	attrs = mergeStyleAttributes(styleName, attrs, res.Styles)
	width := parent.width
	if v := attrs["width"]; v != "" {
		if w := parseDimension(v, parent.width); w > 0 {
			width = w
		}
	}
	offsetX := parseDimension(attrs["x"], parent.width)
	offsetY := parseDimension(attrs["y"], parent.width)

	child := &flowContext{
		baseX:          parent.baseX + offsetX,
		baseY:          parent.baseY + offsetY,
		width:          width,
		cursorY:        parent.baseY + offsetY,
		data:           parent.data,
		typesetter:     parent.typesetter,
		debug:          parent.debug,
		parent:         parent,
		collector:      parent.collector,
		margin:         parent.margin,
		allowPageBreak: false,
	}
	return processBlock(cmd.Block, child, res)
}

func handleText(cmd *dsl.Command, ctx *flowContext, res ResourceSet) error {
	if cmd.Block == nil {
		return fmt.Errorf("text 语句缺少文本块")
	}
	styleName, attrs := parseArgs(cmd.Args, true)
	attrs = mergeStyleAttributes(styleName, attrs, res.Styles)
	// 若未显式设置 align，则继承自父 flow
	if _, ok := attrs["align"]; !ok || strings.TrimSpace(attrs["align"]) == "" {
		if ctx != nil && strings.TrimSpace(ctx.textAlign) != "" {
			attrs["align"] = ctx.textAlign
		}
	}
	content := extractText(cmd.Block)
	if content == "" {
		return fmt.Errorf("text 语句缺少文本内容")
	}

	// 计算折行策略：text 覆盖 flow，默认 anywhere
	effWrap := ctx.textWrap
	if v, ok := attrs["wrap"]; ok && strings.TrimSpace(v) != "" {
		effWrap = normalizeWrap(v)
	}
	tb, height, err := composeTextBox(styleName, attrs, content, ctx.baseX, ctx.cursorY, ctx.width, res, ctx.data, ctx.typesetter, ctx.debug, effWrap)
	if err != nil {
		return err
	}
	ctx.ensureSpace(height)
	tb.X = ctx.baseX
	tb.Y = ctx.cursorY
	if acc := ctx.acc(); acc != nil {
		acc.appendText(tb)
	}
	ctx.cursorY += height + blockSpacing
	return nil
}

func handleImage(cmd *dsl.Command, ctx *flowContext, res ResourceSet) error {
	styleName, attrs := parseArgs(cmd.Args, true)
	attrs = mergeStyleAttributes(styleName, attrs, res.Styles)
	imageName := styleName
	if attrs["image"] != "" {
		imageName = attrs["image"]
	}
	if attrs["src"] != "" {
		imageName = attrs["src"]
	}
	if imageName == "" && len(cmd.Args) > 0 {
		imageName = cmd.Args[0].Value
	}

	imgBox := ImageBox{
		X:       ctx.baseX,
		Y:       ctx.cursorY,
		Fit:     attrs["fit"],
		Opacity: 1,
	}

	if attrs["opacity"] != "" {
		if v, err := strconv.ParseFloat(attrs["opacity"], 64); err == nil {
			imgBox.Opacity = v
		}
	}

	if resImg, ok := res.Images[imageName]; ok {
		imgBox.Path = resImg.Src
		if imgBox.Path == "" {
			imgBox.Path = imageName
		}
		if imgBox.Width == 0 && resImg.Width > 0 {
			imgBox.Width = resImg.Width
		}
		if imgBox.Height == 0 && resImg.Height > 0 {
			imgBox.Height = resImg.Height
		}
	} else {
		imgBox.Path = imageName
	}

	if v := attrs["width"]; v != "" {
		if w := parseDimension(v, ctx.width); w > 0 {
			imgBox.Width = w
		}
	}
	if v := attrs["height"]; v != "" {
		if h := parseDimension(v, ctx.width); h > 0 {
			imgBox.Height = h
		}
	}

	if imgBox.Width == 0 {
		if ctx.width > 0 {
			imgBox.Width = ctx.width
		} else {
			imgBox.Width = 40
		}
	}
	if imgBox.Height == 0 {
		imgBox.Height = imgBox.Width * 0.6
	}

	if imgBox.Path == "" {
		return fmt.Errorf("image 语句缺少资源或 src")
	}

	ctx.ensureSpace(imgBox.Height)
	imgBox.Y = ctx.cursorY
	if acc := ctx.acc(); acc != nil {
		acc.appendImage(imgBox)
	}
	ctx.cursorY = imgBox.Y + imgBox.Height + blockSpacing
	return nil
}

func handleTable(cmd *dsl.Command, ctx *flowContext, res ResourceSet) error {
	if cmd.Block == nil {
		return fmt.Errorf("table 语句缺少内容")
	}
	styleName, attrs := parseArgs(cmd.Args, false)
	attrs = mergeStyleAttributes(styleName, attrs, res.Styles)

	width := ctx.width
	if v := attrs["width"]; v != "" {
		if w := parseDimension(v, ctx.width); w > 0 {
			width = w
		}
	}
	rowGap := defaultTableRowGap
	if v := attrs["row-gap"]; v != "" {
		if g := parseLength(v); g >= 0 {
			rowGap = g
		}
	} else if v := attrs["rowGap"]; v != "" {
		if g := parseLength(v); g >= 0 {
			rowGap = g
		}
	}
	columns := 0
	if v := attrs["columns"]; v != "" {
		if c, err := strconv.Atoi(v); err == nil && c > 0 {
			columns = c
		}
	}

	build := func(baseY float64) (TableBox, float64, error) {
		table := TableBox{
			X:           ctx.baseX,
			Y:           baseY,
			Width:       width,
			RowGap:      rowGap,
			BorderColor: Color{R: 200, G: 200, B: 200},
		}
		currentY := baseY
		colCount := columns
		for _, stmt := range cmd.Block.Statements {
			if stmt.Command == nil {
				continue
			}
			switch stmt.Command.Name {
			case "header":
				row, rowHeight, rowColumns, err := buildTableRow(stmt.Command, res, colCount, width, table.X, currentY, true, ctx.data, ctx.typesetter, ctx.debug)
				if err != nil {
					return TableBox{}, 0, err
				}
				if colCount == 0 {
					colCount = rowColumns
				}
				currentY += rowHeight + table.RowGap
				row.Y = currentY - rowHeight - table.RowGap
				table.Rows = append(table.Rows, row)
			case "row":
				row, rowHeight, _, err := buildTableRow(stmt.Command, res, colCount, width, table.X, currentY, false, ctx.data, ctx.typesetter, ctx.debug)
				if err != nil {
					return TableBox{}, 0, err
				}
				currentY += rowHeight + table.RowGap
				row.Y = currentY - rowHeight - table.RowGap
				table.Rows = append(table.Rows, row)
			}
		}
		if colCount == 0 {
			return TableBox{}, 0, fmt.Errorf("table 需要至少一个单元格")
		}
		colWidth := width / float64(colCount)
		table.ColumnWidths = make([]float64, colCount)
		for i := 0; i < colCount; i++ {
			table.ColumnWidths[i] = colWidth
		}
		if len(table.Rows) > 0 {
			currentY -= table.RowGap
		}
		return table, currentY - baseY, nil
	}

	table, height, err := build(ctx.cursorY)
	if err != nil {
		return err
	}
	if ctx.allowPageBreak && ctx.cursorY+height > ctx.collector.maxContentY() {
		ctx.pageBreak()
		table, height, err = build(ctx.cursorY)
		if err != nil {
			return err
		}
	}

	if acc := ctx.acc(); acc != nil {
		acc.appendTable(table)
	}
	ctx.cursorY += height + blockSpacing
	return nil
}

func buildTableRow(cmd *dsl.Command, res ResourceSet, columnHint int, tableWidth, baseX, baseY float64, header bool, data any, ts Typesetter, debug DebugOptions) (TableRow, float64, int, error) {
	var row TableRow
	if cmd.Block == nil {
		return row, 0, 0, fmt.Errorf("row/header 缺少 cell 定义")
	}
	row.IsHeader = header
	colIdx := 0
	maxHeight := 0.0
	cells := []TableCell{}

	for _, stmt := range cmd.Block.Statements {
		if stmt.Command == nil || stmt.Command.Name != "cell" {
			continue
		}
		styleName, attrs := parseArgs(stmt.Command.Args, true)
		attrs = mergeStyleAttributes(styleName, attrs, res.Styles)
		content := extractText(stmt.Command.Block)
		if content == "" {
			continue
		}

		columns := columnHint
		if columns == 0 {
			columns = 1
		}
		colWidth := tableWidth / float64(columns)
		x := baseX + float64(colIdx)*colWidth
		cellWidth := colWidth - 2*cellPadding
		if cellWidth <= 0 {
			cellWidth = colWidth
		}
		// 单元格折行策略：默认继承表/flow 含义不易获取，这里按属性值或默认 anywhere
		wrap := normalizeWrap(attrs["wrap"])
		if wrap == "" {
			wrap = "anywhere"
		}
		tb, height, err := composeTextBox(styleName, attrs, content, x+cellPadding, baseY+cellPadding, cellWidth, res, data, ts, debug, wrap)
		if err != nil {
			return row, 0, columnHint, err
		}
		cells = append(cells, TableCell{Text: tb})
		if height > maxHeight {
			maxHeight = height
		}
		colIdx++
	}

	if colIdx == 0 {
		return row, 0, columnHint, fmt.Errorf("row/header 中至少需要一个 cell")
	}

	row.Cells = cells
	row.Height = maxHeight + 2*cellPadding
	return row, row.Height, colIdx, nil
}

type pageAccumulator struct {
	texts   []TextBox
	images  []ImageBox
	tables  []TableBox
	lines   []Line
	rects   []Rect
	circles []Circle
}

func (p *pageAccumulator) appendText(tb TextBox) {
	p.texts = append(p.texts, tb)
}

func (p *pageAccumulator) appendImage(img ImageBox) {
	p.images = append(p.images, img)
}

func (p *pageAccumulator) appendTable(t TableBox) {
	p.tables = append(p.tables, t)
}

type pageCollector struct {
	width   float64
	height  float64
	margin  Margin
	accs    []*pageAccumulator
	current int
	// 页眉/页脚布局结果，应用于所有页面
	header HeaderFooter
	footer HeaderFooter
}

func newPageCollector(width, height float64, margin Margin) *pageCollector {
	pc := &pageCollector{
		width:  width,
		height: height,
		margin: margin,
	}
	pc.newPage()
	return pc
}

func (pc *pageCollector) newPage() *pageAccumulator {
	acc := &pageAccumulator{}
	pc.accs = append(pc.accs, acc)
	pc.current = len(pc.accs) - 1
	return acc
}

func (pc *pageCollector) curr() *pageAccumulator {
	if len(pc.accs) == 0 {
		return pc.newPage()
	}
	return pc.accs[pc.current]
}

func (pc *pageCollector) maxContentY() float64 {
	// 可用内容底部 = 页面高度 - 下边距 - 页脚高度
	return pc.contentBottom()
}

func (pc *pageCollector) contentTop() float64 {
	// Word 逻辑：内容区域顶部 = max(上边距, 页眉高度)
	if pc.header.Height > pc.margin.Top {
		return pc.header.Height
	}
	return pc.margin.Top
}

func (pc *pageCollector) contentBottom() float64 {
	// Word 逻辑：内容区域底部 = 页面高度 - max(下边距, 页脚高度)
	b := pc.margin.Bottom
	if pc.footer.Height > b {
		b = pc.footer.Height
	}
	return pc.height - b
}

func (pc *pageCollector) allPages() []Page {
	out := make([]Page, len(pc.accs))
	for i, acc := range pc.accs {
		out[i] = Page{
			Width:   pc.width,
			Height:  pc.height,
			Margin:  pc.margin,
			Texts:   acc.texts,
			Images:  acc.images,
			Tables:  acc.tables,
			Lines:   acc.lines,
			Rects:   acc.rects,
			Circles: acc.circles,
			Header:  pc.header,
			Footer:  pc.footer,
		}
	}
	return out
}

func (pc *pageCollector) pages() []Page {
	return pc.allPages()
}

type flowContext struct {
	baseX          float64
	baseY          float64
	width          float64
	cursorY        float64
	data           any
	typesetter     Typesetter
	debug          DebugOptions
	parent         *flowContext
	collector      *pageCollector
	margin         Margin
	allowPageBreak bool
	// textAlign 继承自父 flow 的对齐方式（left/center/right），用于未显式声明 align 的子 text。
	textAlign string
	// textWrap 继承自父 flow 的折行方式（anywhere(默认)/break-word/nowrap）。
	textWrap string
}

// buildHeaderFooter 负责解析与布局页眉/页脚内容（仅支持 text/image）。
// kind 取值 "header" 或 "footer"，用于计算纵向基准。
func buildHeaderFooter(cmd *dsl.Command, pageW, pageH float64, margin Margin, res ResourceSet, data any, ts Typesetter, debug DebugOptions, kind string) (HeaderFooter, error) {
	var hf HeaderFooter
	if cmd == nil || cmd.Block == nil {
		return hf, nil
	}
	_, attrs := parseArgs(cmd.Args, false)
	contentWidth := pageW - margin.Left - margin.Right

	// 临时容器用于收集元素
	var texts []TextBox
	var images []ImageBox
	var lines []Line
	var rects []Rect
	var circles []Circle
	cursorY := 0.0

	// 布局内部的 text/image/shape，按顺序自上而下堆叠（shape 不参与 header 内容高度计算）
	for _, st := range cmd.Block.Statements {
		if st.Command == nil {
			continue
		}
		switch st.Command.Name {
		case "text":
			styleName, tattrs := parseArgs(st.Command.Args, true)
			all := mergeStyleAttributes(styleName, tattrs, res.Styles)
			content := extractText(st.Command.Block)
			wrap := normalizeWrap(all["wrap"])
			if wrap == "" {
				wrap = "anywhere"
			}
			tb, h, err := composeTextBox(styleName, all, content, margin.Left, 0, contentWidth, res, data, ts, debug, wrap)
			if err != nil {
				return hf, err
			}
			// 页眉文本水平对齐（默认 center，可被子元素 align 属性覆盖：left/center/right）
			if kind == "header" {
				// 统一让容器覆盖 header 可用宽度，便于渲染阶段按 tb.Align 计算锚点
				tb.X = margin.Left
				tb.Width = contentWidth
				align := strings.ToLower(tattrs["align"])
				if align == "start" {
					align = "left"
				} else if align == "end" {
					align = "right"
				}
				if align != "left" && align != "right" && align != "center" {
					align = "center" // 默认值：保持向后兼容
				}
				tb.Align = align
			}
			tb.Y = cursorY + tb.Y // composeTextBox 的 Y 为 0，这里使用累积偏移
			texts = append(texts, tb)
			cursorY += h + blockSpacing
		case "image":
			styleName, iattrs := parseArgs(st.Command.Args, true)
			iattrs = mergeStyleAttributes(styleName, iattrs, res.Styles)
			imageName := styleName
			if iattrs["image"] != "" {
				imageName = iattrs["image"]
			}
			if iattrs["src"] != "" {
				imageName = iattrs["src"]
			}
			if imageName == "" && len(st.Command.Args) > 0 {
				imageName = st.Command.Args[0].Value
			}
			img := ImageBox{X: margin.Left, Y: cursorY, Fit: iattrs["fit"], Opacity: 1}
			if v := iattrs["opacity"]; v != "" {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					img.Opacity = f
				}
			}
			if resImg, ok := res.Images[imageName]; ok {
				img.Path = resImg.Src
				if img.Path == "" {
					img.Path = imageName
				}
				if img.Width == 0 && resImg.Width > 0 {
					img.Width = resImg.Width
				}
				if img.Height == 0 && resImg.Height > 0 {
					img.Height = resImg.Height
				}
			} else {
				img.Path = imageName
			}
			if v := iattrs["width"]; v != "" {
				if w := parseDimension(v, contentWidth); w > 0 {
					img.Width = w
				}
			}
			if v := iattrs["height"]; v != "" {
				if h := parseDimension(v, contentWidth); h > 0 {
					img.Height = h
				}
			}
			if img.Width == 0 {
				img.Width = contentWidth
			}
			if img.Height == 0 {
				img.Height = img.Width * 0.6
			}
			// 页眉图片水平对齐（默认 center，可被子元素 align 属性覆盖：left/center/right）
			if kind == "header" {
				align := strings.ToLower(iattrs["align"])
				if align == "start" {
					align = "left"
				} else if align == "end" {
					align = "right"
				}
				if align == "left" {
					img.X = margin.Left
				} else if align == "right" {
					img.X = margin.Left + contentWidth - img.Width
				} else { // 默认 center
					img.X = margin.Left + (contentWidth-img.Width)/2
				}
			}
			images = append(images, img)
			cursorY += img.Height + blockSpacing
		case "line", "rect", "circle":
			_, a := parseArgs(st.Command.Args, false)
			name := strings.ToLower(st.Command.Name)
			switch name {
			case "line":
				if ln, ok := parseLineShape(a, res); ok { lines = append(lines, ln) }
			case "rect":
				if rc, ok := parseRectShape(a, res); ok { rects = append(rects, rc) }
			case "circle":
				if c, ok := parseCircleShape(a, res); ok { circles = append(circles, c) }
			}
			// 形状不改变 header 内 content cursor
		}
	}
	if cursorY > 0 {
		cursorY -= blockSpacing // 去掉最后一项后的额外间距
	}

	// contentHeight 表示页眉/页脚内部内容自身高度（不包含额外区域）
	contentHeight := cursorY
	// areaHeight 表示占用的区域高度：显式给定则使用之，否则等于内容高度
	areaHeight := contentHeight
	if v := attrs["height"]; v != "" {
		if h := parseDimension(v, contentWidth); h > 0 {
			areaHeight = h
		}
	}

	// 将相对 Y 转换为绝对页面坐标（页眉/页脚不受上下边距限制）
	// 要求：页眉按底边对齐，即内容底部贴合从顶部量起的 areaHeight
	baseY := 0.0
	if kind == "header" {
		// header 从页面顶部开始，内容整体下移到区域底部对齐
		baseY = areaHeight - contentHeight
		if baseY < 0 {
			baseY = 0 // 内容高于区域时，允许溢出到上方，不再额外上移
		}
	} else if kind == "footer" {
		// footer 区域从页面底部向上占用 areaHeight
		baseY = pageH - areaHeight
	}
	for i := range texts {
		texts[i].Y += baseY
	}
	for i := range images {
		images[i].Y += baseY
	}

 hf.Height = areaHeight
 hf.Texts = texts
 hf.Images = images
 hf.Lines = lines
 hf.Rects = rects
 hf.Circles = circles
 return hf, nil
}

func (ctx *flowContext) ensureSpace(height float64) {
	if !ctx.allowPageBreak {
		return
	}
	if ctx.collector == nil {
		return
	}
	if ctx.cursorY+height <= ctx.collector.maxContentY() {
		return
	}
	ctx.pageBreak()
}

func (ctx *flowContext) pageBreak() {
	if ctx.collector == nil {
		return
	}
	if ctx.parent != nil {
		ctx.parent.pageBreak()
		ctx.baseY = ctx.parent.cursorY
		ctx.cursorY = ctx.baseY
		return
	}
	ctx.collector.newPage()
	ctx.baseX = ctx.margin.Left
	// 新页从内容区域顶部开始（考虑页眉高度）
	ctx.baseY = ctx.collector.contentTop()
	ctx.cursorY = ctx.baseY
}

func (ctx *flowContext) acc() *pageAccumulator {
	if ctx.collector == nil {
		return nil
	}
	return ctx.collector.curr()
}

func collectResources(doc *dsl.Document) (ResourceSet, error) {
	res := ResourceSet{
		Fonts:  map[string]FontResource{},
		Colors: map[string]Color{},
		Images: map[string]ImageResource{},
		Styles: map[string]Style{},
	}
	rawStyles := map[string]Style{}

	for _, section := range doc.Sections {
		if section.Resources == nil || section.Resources.Block == nil {
			continue
		}
		for _, stmt := range section.Resources.Block.Statements {
			if stmt.Command == nil {
				continue
			}
			switch stmt.Command.Name {
			case "font":
				font := parseFontResource(stmt.Command)
				if font.Name != "" {
					res.Fonts[font.Name] = font
				}
			case "color":
				name, value := parseColorResource(stmt.Command)
				if name == "" || value == "" {
					continue
				}
				if c, err := parseColor(value); err == nil {
					res.Colors[name] = c
				}
			case "image":
				image := parseImageResource(stmt.Command)
				if image.Name != "" {
					res.Images[image.Name] = image
				}
			case "style":
				style := parseStyleResource(stmt.Command)
				if style.Name != "" {
					rawStyles[style.Name] = style
				}
			}
		}
	}

	if len(res.Fonts) == 0 {
		res.Fonts["Body"] = FontResource{
			Name:     "Body",
			Src:      "assets/fonts/Noto_Sans_SC/static/NotoSansSC-Regular.ttf",
			Family:   "Body",
			Fallback: "embed:Inter/static/Inter-Regular.ttf",
		}
	}

	resolvedStyles, err := resolveStyles(rawStyles)
	if err != nil {
		return res, err
	}
	res.Styles = resolvedStyles

	return res, nil
}

func collectMeta(doc *dsl.Document) DocumentMeta {
	meta := DocumentMeta{
		Creator: "Papyrus",
	}
	for _, section := range doc.Sections {
		if section.Meta == nil || section.Meta.Block == nil {
			continue
		}
		for _, stmt := range section.Meta.Block.Statements {
			if stmt.Assignment == nil {
				continue
			}
			key := strings.ToLower(stmt.Assignment.Key)
			switch key {
			case "title":
				meta.Title = valueToString(stmt.Assignment.Value)
			case "author":
				meta.Author = valueToString(stmt.Assignment.Value)
			case "subject":
				meta.Subject = valueToString(stmt.Assignment.Value)
			case "creator":
				meta.Creator = valueToString(stmt.Assignment.Value)
			case "keywords":
				meta.Keywords = valueToStringSlice(stmt.Assignment.Value)
			}
		}
	}
	return meta
}

func parseFontResource(cmd *dsl.Command) FontResource {
	if len(cmd.Args) == 0 {
		return FontResource{}
	}
	font := FontResource{
		Name:      cmd.Args[0].Value,
		Family:    cmd.Args[0].Value,
		Base:      cmd.Args[0].Value,
		IsBuiltin: strings.HasPrefix(cmd.Args[0].Value, "builtin:"),
	}

	if cmd.Block == nil {
		return font
	}
	for _, stmt := range cmd.Block.Statements {
		if stmt.Assignment == nil {
			continue
		}
		switch stmt.Assignment.Key {
		case "src":
			if stmt.Assignment.Value.String != nil {
				font.Src = string(*stmt.Assignment.Value.String)
				if strings.HasPrefix(font.Src, "builtin:") {
					font.IsBuiltin = true
					font.Base = strings.TrimPrefix(font.Src, "builtin:")
					if font.Base == "" {
						font.Base = "Times-Roman"
					}
				}
			}
		case "style":
			if stmt.Assignment.Value.String != nil {
				font.Style = string(*stmt.Assignment.Value.String)
			}
		case "fallback":
			if stmt.Assignment.Value.String != nil {
				font.Fallback = string(*stmt.Assignment.Value.String)
			}
		}
	}
	return font
}

func parseImageResource(cmd *dsl.Command) ImageResource {
	if len(cmd.Args) == 0 {
		return ImageResource{}
	}
	image := ImageResource{
		Name: cmd.Args[0].Value,
	}
	if cmd.Block == nil {
		return image
	}

	for _, stmt := range cmd.Block.Statements {
		if stmt.Assignment == nil {
			continue
		}
		switch stmt.Assignment.Key {
		case "src":
			if stmt.Assignment.Value.String != nil {
				image.Src = string(*stmt.Assignment.Value.String)
			}
		case "width":
			if stmt.Assignment.Value.Number != nil {
				image.Width = parseLength(*stmt.Assignment.Value.Number)
			}
		case "height":
			if stmt.Assignment.Value.Number != nil {
				image.Height = parseLength(*stmt.Assignment.Value.Number)
			}
		case "dpi":
			if stmt.Assignment.Value.Number != nil {
				if v, err := strconv.Atoi(*stmt.Assignment.Value.Number); err == nil {
					image.DPI = v
				}
			}
		}
	}
	return image
}

func parseStyleResource(cmd *dsl.Command) Style {
	if len(cmd.Args) == 0 {
		return Style{}
	}
	style := Style{
		Name:  cmd.Args[0].Value,
		Props: map[string]string{},
	}
	if len(cmd.Args) >= 3 && strings.EqualFold(cmd.Args[1].Value, "extends") {
		style.Extends = cmd.Args[2].Value
	}

	if cmd.Block == nil {
		return style
	}

	for _, stmt := range cmd.Block.Statements {
		if stmt.Assignment == nil {
			continue
		}
		val := valueToString(stmt.Assignment.Value)
		if val == "" {
			continue
		}
		style.Props[stmt.Assignment.Key] = val
	}
	return style
}

func resolveStyles(styles map[string]Style) (map[string]Style, error) {
	resolved := map[string]Style{}
	visiting := map[string]bool{}

	var dfs func(name string) (Style, error)
	dfs = func(name string) (Style, error) {
		if style, ok := resolved[name]; ok {
			return style, nil
		}
		style, ok := styles[name]
		if !ok {
			return Style{}, fmt.Errorf("style %s 未定义", name)
		}
		if visiting[name] {
			return Style{}, fmt.Errorf("style 继承存在循环：%s", name)
		}
		visiting[name] = true

		props := map[string]string{}
		if style.Extends != "" {
			parent, err := dfs(style.Extends)
			if err != nil {
				return Style{}, err
			}
			for k, v := range parent.Props {
				props[k] = v
			}
		}
		for k, v := range style.Props {
			props[k] = v
		}
		style.Props = props
		resolved[name] = style
		delete(visiting, name)
		return style, nil
	}

	for name := range styles {
		if _, err := dfs(name); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func parseColorResource(cmd *dsl.Command) (string, string) {
	if len(cmd.Args) == 0 {
		return "", ""
	}
	name := cmd.Args[0].Value
	value := ""
	if len(cmd.Args) > 1 {
		value = cmd.Args[len(cmd.Args)-1].Value
	}
	return name, value
}

func resolvePageSize(spec dsl.PageSpec) (float64, float64, error) {
	base, ok := pagePresets[strings.ToUpper(spec.Size)]
	if !ok {
		return 0, 0, fmt.Errorf("暂不支持的纸张尺寸：%s", spec.Size)
	}

	width := base[0]
	height := base[1]
	for _, token := range spec.Params {
		switch token.Value {
		case "landscape":
			width, height = height, width
		}
	}
	return width, height, nil
}

var pagePresets = map[string][2]float64{
	"A4": {210, 297},
	"A5": {148, 210},
}

func resolveMargin(params []*dsl.Lexeme) Margin {
	// default 20mm on all sides
	margin := Margin{Top: 20, Right: 20, Bottom: 20, Left: 20}
	for i := 0; i < len(params); i++ {
		token := params[i]
		switch token.Value {
		case "margin":
			// collect up to 4 subsequent values after 'margin'
			vals := []float64{}
			for j := i + 1; j < len(params) && len(vals) < 4; j++ {
				v := parseLength(params[j].Value)
				// accept zero as valid value (e.g., set left=0), but skip NaN via parseLength==0 when value isn't a length
				// here we can't distinguish invalid 0 from valid 0mm; treat any numeric parse as acceptable, including 0
				// however, to avoid consuming unrelated keywords accidentally (e.g., 'portrait'),
				// stop when encountering a non-numeric token: we heuristically check that trimUnit parses to float without error
				// Since parseLength silently returns 0 on error, add an extra guard: require that the raw numeric part is a number
				num := trimUnit(params[j].Value)
				if _, err := strconv.ParseFloat(num, 64); err != nil {
					break
				}
				vals = append(vals, v)
			}
			// apply CSS-like semantics described:
			// 1 value: top/right/bottom/left = v1
			// 2 values: top/bottom = v1; left/right = v2
			// 3 values: top = v1; right = v2; bottom = v3; left = 0
			// 4+ values: top = v1; right = v2; bottom = v3; left = v4 (ignore extras)
			switch len(vals) {
			case 1:
				v := vals[0]
				margin = Margin{Top: v, Right: v, Bottom: v, Left: v}
			case 2:
				margin = Margin{Top: vals[0], Right: vals[1], Bottom: vals[0], Left: vals[1]}
			case 3:
				margin = Margin{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: 0}
			case 4:
				margin = Margin{Top: vals[0], Right: vals[1], Bottom: vals[2], Left: vals[3]}
			}
		}
	}
	return margin
}

func firstPage(doc *dsl.Document) *dsl.PageSection {
	for _, section := range doc.Sections {
		if section.Page != nil {
			return section.Page
		}
	}
	return nil
}

func parseArgs(args []*dsl.Lexeme, allowStyle bool) (string, map[string]string) {
	result := map[string]string{}
	if len(args) == 0 {
		return "", result
	}

	cursor := 0
	var style string
	if allowStyle && args[0].Type == "Ident" {
		style = args[0].Value
		cursor = 1
	}

	for cursor < len(args)-1 {
		key := args[cursor].Value
		val := args[cursor+1].Value
		result[key] = val
		cursor += 2
	}

	return style, result
}

func mergeStyleAttributes(style string, inline map[string]string, styles map[string]Style) map[string]string {
	out := make(map[string]string)
	if style != "" {
		if s, ok := styles[style]; ok {
			for k, v := range s.Props {
				out[k] = v
			}
		}
	}
	for k, v := range inline {
		out[k] = v
	}
	return out
}

func extractText(block *dsl.Block) string {
	if block == nil {
		return ""
	}
	var builder strings.Builder
	for _, stmt := range block.Statements {
		if stmt.Text != nil {
			builder.WriteString(string(stmt.Text.Value))
		}
	}
	return builder.String()
}

func composeTextBox(style string, attrs map[string]string, content string, x, y, width float64, res ResourceSet, data any, ts Typesetter, debug DebugOptions, wrap string) (TextBox, float64, error) {
	attrs = mergeStyleAttributes(style, attrs, res.Styles)
	fontName := attrs["font"]
	if fontName == "" {
		fontName = style
	}
	if fontName == "" {
		fontName = "Body"
	}

	if data != nil {
		content = binding.Interpolate(content, data)
	}

	fontSize := parseLength(attrs["size"]) // mm
	if fontSize <= 0 {                     // default 12pt in mm
		fontSize = 12 * 0.352777
	}
	lineHeight := fontSize * 1.4 // mm by default
	if v := strings.TrimSpace(attrs["line-height"]); v != "" {
		if strings.HasSuffix(v, "x") {
			factor := strings.TrimSuffix(v, "x")
			if f, err := strconv.ParseFloat(factor, 64); err == nil && f > 0 {
				lineHeight = fontSize * f // mm since fontSize is mm
			}
		} else if lh := parseLength(v); lh > 0 { // absolute line-height, convert to mm
			lineHeight = lh
		}
	}

	color := resolveColor(attrs["color"], res)
	fontRes, err := resolveFontResource(fontName, res)
	if err != nil {
		return TextBox{}, 0, err
	}

	lines, err := layoutLines(content, width, fontRes, fontSize, lineHeight, ts, wrap)
	if err != nil {
		return TextBox{}, 0, err
	}

	totalHeight := 0.0
	defaultLeading := math.Max(lineHeight-fontSize, 0)
	for i := range lines {
		if lines[i].Height <= 0 {
			lines[i].Height = fontSize
		}
		if i == 0 {
			lines[i].GapBefore = 0
		} else if lines[i].GapBefore <= 0 {
			lines[i].GapBefore = defaultLeading
		}
		totalHeight += lines[i].GapBefore + lines[i].Height
	}
	if len(lines) == 0 {
		totalHeight = 0
	}

	tb := TextBox{
		Content:    content,
		X:          x,
		Y:          y,
		Width:      width,
		LineHeight: lineHeight,
		Font:       fontName,
		FontSize:   fontSize,
		Color:      color,
		Lines:      lines,
		Height:     totalHeight,
		Wrap:       wrap,
	}
	// 应用对齐属性（支持 start/end 别名），默认 left（省略时不写入 JSON）
	if v := strings.ToLower(strings.TrimSpace(attrs["align"])); v != "" {
		if v == "start" {
			v = "left"
		}
		if v == "end" {
			v = "right"
		}
		if v == "left" || v == "center" || v == "right" {
			tb.Align = v
		}
	}
	// Populate debug.rawUnits when enabled
	if debug.RawUnits {
		var sizeRaw RawLengthJSON
		szSpec := ParseRawLengthStr(attrs["size"]) // preserve original unit
		if szSpec.Unit == UnitNone || szSpec.Value <= 0 {
			// default 12pt as raw when unspecified
			sizeRaw = RawLengthJSON{Value: 12, Unit: "pt"}
		} else {
			sizeRaw = RawLengthJSON{Value: szSpec.Value, Unit: UnitToString(szSpec.Unit)}
		}
		var lhRaw RawLineHeightJSON
		if v := strings.TrimSpace(attrs["line-height"]); v != "" {
			if strings.HasSuffix(v, "x") {
				factor := strings.TrimSuffix(v, "x")
				if f, err := strconv.ParseFloat(factor, 64); err == nil && f > 0 {
					lhRaw = RawLineHeightJSON{Kind: "factor", Factor: f}
				}
			} else {
				lhLen := ParseRawLengthStr(v)
				if lhLen.Unit != UnitNone && lhLen.Value > 0 {
					lhRaw = RawLineHeightJSON{Kind: "absolute", Value: lhLen.Value, Unit: UnitToString(lhLen.Unit)}
				}
			}
		} else {
			// default normal = 1.4x
			lhRaw = RawLineHeightJSON{Kind: "factor", Factor: 1.4}
		}
		tb.Debug = &TextBoxDebug{RawUnits: &RawUnits{FontSize: &sizeRaw, LineHeight: &lhRaw}}
	}
	return tb, totalHeight, nil
}

func resolveFontResource(name string, res ResourceSet) (FontResource, error) {
	if font, ok := res.Fonts[name]; ok {
		return font, nil
	}
	if font, ok := res.Fonts["Body"]; ok {
		return font, nil
	}
	for _, font := range res.Fonts {
		return font, nil
	}
	return FontResource{}, fmt.Errorf("字体 %s 未定义，且没有可用的默认字体", name)
}

func layoutLines(content string, width float64, font FontResource, fontSize, lineHeight float64, ts Typesetter, wrap string) ([]TextLine, error) {
	if ts == nil {
		lines := strings.Split(content, "\n")
		out := make([]TextLine, 0, len(lines))
		textHeight := fontSize
		if textHeight <= 0 {
			textHeight = 12
		}
		leading := math.Max(lineHeight-textHeight, 0)
		for _, l := range lines {
			out = append(out, TextLine{
				Content:   l,
				Width:     width,
				Height:    textHeight,
				GapBefore: leading,
			})
		}
		if len(out) == 0 {
			out = []TextLine{{Content: "", Width: width, Height: textHeight}}
		} else {
			out[0].GapBefore = 0
		}
		return out, nil
	}
	lines, err := ts.LayoutLines(content, width, font, fontSize, lineHeight, wrap)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		height := fontSize
		if height <= 0 {
			height = lineHeight
		}
		lines = []TextLine{{Content: "", Width: width, Height: height}}
	}
	if len(lines) > 0 {
		lines[0].GapBefore = 0
	}
	return lines, nil
}

func parseFontSize(value string) float64 {
	if value == "" {
		return 12
	}
	num := trimUnit(value)
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 12
	}
	return f
}

func resolveColor(value string, res ResourceSet) Color {
	if value == "" {
		return Color{R: 30, G: 30, B: 30}
	}
	if c, ok := res.Colors[value]; ok {
		return c
	}
	if strings.HasPrefix(value, "#") {
		if c, err := parseColor(value); err == nil {
			return c
		}
	}
	return Color{R: 30, G: 30, B: 30}
}

func parseColor(value string) (Color, error) {
	value = strings.TrimPrefix(value, "#")
	switch len(value) {
	case 3:
		r := strings.Repeat(string(value[0]), 2)
		g := strings.Repeat(string(value[1]), 2)
		b := strings.Repeat(string(value[2]), 2)
		return Color{
			R: mustHex(r),
			G: mustHex(g),
			B: mustHex(b),
		}, nil
	case 6, 8:
		return Color{
			R: mustHex(value[0:2]),
			G: mustHex(value[2:4]),
			B: mustHex(value[4:6]),
		}, nil
	default:
		return Color{}, fmt.Errorf("颜色值 %s 无法解析", value)
	}
}

// --- Shapes parsing helpers ---

// parseLineShape supports both full form (x1/y1/x2/y2) and simplified form:
//   line x <len> y <len> length <len> [dir h|v] [color <..>] [width <len>]
func parseLineShape(attrs map[string]string, res ResourceSet) (Line, bool) {
	var ln Line
	// Prefer full form when present
	x1 := parseLength(attrs["x1"]) // mm
	y1 := parseLength(attrs["y1"]) // mm
	x2 := parseLength(attrs["x2"]) // mm
	y2 := parseLength(attrs["y2"]) // mm
	if x1 != 0 || y1 != 0 || x2 != 0 || y2 != 0 {
		ln.X1, ln.Y1, ln.X2, ln.Y2 = x1, y1, x2, y2
		if v := attrs["color"]; v != "" { ln.Color = resolveColor(v, res) } else { ln.Color = Color{0,0,0} }
		ln.Width = parseLength(attrs["width"]) // may be 0
		return ln, true
	}
	// Simplified form
	x := parseLength(attrs["x"]) // mm
	y := parseLength(attrs["y"]) // mm
	length := parseLength(attrs["length"]) // mm
	if (x != 0 || y != 0) && length > 0 {
		d := strings.ToLower(strings.TrimSpace(attrs["dir"]))
		if d == "" || d == "h" || d == "hor" || d == "horizontal" {
			ln.X1, ln.Y1 = x, y
			ln.X2, ln.Y2 = x+length, y
		} else if d == "v" || d == "ver" || d == "vertical" {
			ln.X1, ln.Y1 = x, y
			ln.X2, ln.Y2 = x, y+length
		} else {
			// unknown dir
			return Line{}, false
		}
		if v := attrs["color"]; v != "" { ln.Color = resolveColor(v, res) } else { ln.Color = Color{0,0,0} }
		ln.Width = parseLength(attrs["width"]) // may be 0
		return ln, true
	}
	return Line{}, false
}

func parseRectShape(attrs map[string]string, res ResourceSet) (Rect, bool) {
	var rc Rect
	rc.X = parseLength(attrs["x"]) // mm
	rc.Y = parseLength(attrs["y"]) // mm
	rc.Width = parseLength(attrs["width"]) // mm
	rc.Height = parseLength(attrs["height"]) // mm
	if rc.Width <= 0 || rc.Height <= 0 { return Rect{}, false }
	if v := attrs["stroke"]; v != "" { rc.StrokeColor = resolveColor(v, res) }
	if v := attrs["stroke-width"]; v != "" { rc.StrokeWidth = parseLength(v) }
	if v := attrs["fill"]; v != "" {
		c := resolveColor(v, res)
		rc.FillColor = &c
	}
	return rc, true
}

func parseCircleShape(attrs map[string]string, res ResourceSet) (Circle, bool) {
	var c Circle
	c.CX = parseLength(attrs["cx"]) // mm
	c.CY = parseLength(attrs["cy"]) // mm
	c.R = parseLength(attrs["r"]) // mm
	if c.R <= 0 { return Circle{}, false }
	if v := attrs["stroke"]; v != "" { c.StrokeColor = resolveColor(v, res) }
	if v := attrs["stroke-width"]; v != "" { c.StrokeWidth = parseLength(v) }
	if v := attrs["fill"]; v != "" {
		col := resolveColor(v, res)
		c.FillColor = &col
	}
	return c, true
}

func mustHex(s string) int {
	v, _ := strconv.ParseInt(s, 16, 64)
	return int(v)
}

func parseLength(value string) float64 {
	if value == "" {
		return 0
	}
	unit := ""
	for _, suffix := range []string{"mm", "cm", "in", "pt"} {
		if strings.HasSuffix(value, suffix) {
			unit = suffix
			break
		}
	}
	num := trimUnit(value)
	val, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0
	}
	switch unit {
	case "cm":
		return val * 10
	case "in":
		return val * 25.4
	case "pt":
		return val * 0.352777
	case "mm", "":
		return val
	default:
		return val
	}
}

func parseDimension(value string, reference float64) float64 {
	if value == "" {
		return 0
	}
	if strings.HasSuffix(value, "%") {
		num := strings.TrimSuffix(value, "%")
		if f, err := strconv.ParseFloat(num, 64); err == nil {
			return reference * f / 100
		}
		return 0
	}
	return parseLength(value)
}

func trimUnit(value string) string {
	for _, suffix := range []string{"pt", "mm", "cm", "in", "%"} {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix)
		}
	}
	return value
}

func alignOffset(container, width float64, align string) float64 {
	if container <= width {
		return 0
	}
	switch strings.ToLower(align) {
	case "center", "middle":
		return (container - width) / 2
	case "right", "end":
		return container - width
	default:
		return 0
	}
}

func valueToString(val *dsl.Value) string {
	if val == nil {
		return ""
	}
	switch {
	case val.String != nil:
		return string(*val.String)
	case val.Number != nil:
		return *val.Number
	case val.Color != nil:
		return *val.Color
	case val.Expr != nil:
		var builder strings.Builder
		for _, part := range val.Expr.Parts {
			builder.WriteString(part.Value)
		}
		return builder.String()
	default:
		return ""
	}
}

func valueToStringSlice(val *dsl.Value) []string {
	if val == nil {
		return nil
	}
	if val.Array != nil {
		out := make([]string, 0, len(val.Array.Values))
		for _, item := range val.Array.Values {
			if s := valueToString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if s := valueToString(val); s != "" {
		return []string{s}
	}
	return nil
}

func inferFlowWidth(block *dsl.Block, res ResourceSet, maxWidth float64, ts Typesetter) float64 {
	if block == nil {
		return 0
	}
	var width float64
	for _, stmt := range block.Statements {
		if stmt.Command == nil {
			continue
		}
		switch stmt.Command.Name {
		case "text":
			if w := inferTextWidth(stmt.Command, res, maxWidth, ts); w > width {
				width = w
			}
		case "flow":
			if w := inferFlowWidth(stmt.Command.Block, res, maxWidth, ts); w > width {
				width = w
			}
		case "image":
			_, attrs := parseArgs(stmt.Command.Args, true)
			if v := attrs["width"]; v != "" {
				if w := parseDimension(v, maxWidth); w > width {
					width = w
				}
			}
		case "table":
			_, attrs := parseArgs(stmt.Command.Args, false)
			if v := attrs["width"]; v != "" {
				if w := parseDimension(v, maxWidth); w > width {
					width = w
				}
			}
		}
	}
	return width
}

func inferTextWidth(cmd *dsl.Command, res ResourceSet, maxWidth float64, ts Typesetter) float64 {
	if cmd.Block == nil {
		return 0
	}
	styleName, attrs := parseArgs(cmd.Args, true)
	attrs = mergeStyleAttributes(styleName, attrs, res.Styles)
	if v := attrs["width"]; v != "" {
		return parseDimension(v, maxWidth)
	}
	content := extractText(cmd.Block)
	if content == "" {
		return 0
	}
	// 如果没有 typesetter，则退回粗略估计
	if ts == nil {
		fontSize := parseFontSize(attrs["size"]) // pt
		return estimateTextWidth(content, fontSize)
	}
	// 解析字体与尺寸（单位：mm），用真实排版测量宽度
	fontName := attrs["font"]
	if fontName == "" {
		fontName = styleName
	}
	if fontName == "" {
		fontName = "Body"
	}
	fontRes, err := resolveFontResource(fontName, res)
	if err != nil {
		// 字体解析失败时使用估算，避免影响其他内容
		fontSize := parseFontSize(attrs["size"]) // pt
		return estimateTextWidth(content, fontSize)
	}
	fontSizeMm := parseLength(attrs["size"]) // mm
	if fontSizeMm <= 0 {
		fontSizeMm = 12 * 0.352777
	}
	lineHeightMm := fontSizeMm * 1.4
	if v := strings.TrimSpace(attrs["line-height"]); v != "" {
		if strings.HasSuffix(v, "x") {
			factor := strings.TrimSuffix(v, "x")
			if f, err := strconv.ParseFloat(factor, 64); err == nil && f > 0 {
				lineHeightMm = fontSizeMm * f
			}
		} else if lh := parseLength(v); lh > 0 {
			lineHeightMm = lh
		}
	}
	// 使用极大宽度避免换行，获取每行实际宽度，取最大值
	lines, err := layoutLines(content, math.MaxFloat64, fontRes, fontSizeMm, lineHeightMm, ts, "nowrap")
	if err != nil {
		// 测量失败则退回估算
		fontSize := parseFontSize(attrs["size"]) // pt
		return estimateTextWidth(content, fontSize)
	}
	maxW := 0.0
	for _, ln := range lines {
		if ln.Width > maxW {
			maxW = ln.Width
		}
	}
	if maxW <= 0 {
		// 兜底
		fontSize := parseFontSize(attrs["size"]) // pt
		return estimateTextWidth(content, fontSize)
	}
	return maxW
}

func estimateTextWidth(content string, fontSize float64) float64 {
	if fontSize <= 0 {
		fontSize = 12
	}
	lines := strings.Split(content, "\n")
	maxChars := 0
	for _, line := range lines {
		count := utf8.RuneCountInString(line)
		if count > maxChars {
			maxChars = count
		}
	}
	if maxChars == 0 {
		maxChars = utf8.RuneCountInString(content)
	}
	return fontSize * 0.55 * float64(maxChars+1)
}
