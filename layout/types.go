package layout

// 该文件定义布局结果与资源描述，供布局计算、渲染与调试 JSON 共用。

// Result 保存布局后的页面与资源信息。
type Result struct {
	Pages     []Page       `json:"pages"`
	Resources ResourceSet  `json:"resources"`
	Meta      DocumentMeta `json:"meta"`
}

// ResourceSet 记录解析出的字体、颜色与图片定义。
type ResourceSet struct {
	Fonts  map[string]FontResource  `json:"fonts"`
	Colors map[string]Color         `json:"colors"`
	Images map[string]ImageResource `json:"images"`
	Styles map[string]Style         `json:"styles"`
}

// FontResource 描述字体资源，src 可以是文件路径、内置 embed 路径或 builtin:* 形式。
type FontResource struct {
	Name      string `json:"name"`
	Src       string `json:"src"`
	Style     string `json:"style"`
	Base      string `json:"base"`      // builtin 模式下记录真实字体名
	Family    string `json:"family"`    // 渲染器使用的 Family 名称
	IsBuiltin bool   `json:"isBuiltin"` // 是否为内建字体
	Fallback  string `json:"fallback"`
}

// ImageResource 记录图片资源，宽高统一以毫米为单位保存（方便绝对定位）。
type ImageResource struct {
	Name   string  `json:"name"`
	Src    string  `json:"src"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	DPI    int     `json:"dpi"`
}

// Color 采用 0-255 的 RGB 数值。
type Color struct {
	R int `json:"r"`
	G int `json:"g"`
	B int `json:"b"`
}

// Page 记录页面尺寸、边距与最终可以直接渲染的元素。
// 新增 Header/Footer 支持，二者的坐标均为页面坐标（单位：mm）。
type Page struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Margin Margin  `json:"margin"`
	// 主体内容（受页眉/页脚占用的有效区域内）
	Texts   []TextBox  `json:"texts"`
	Images  []ImageBox `json:"images"`
	Tables  []TableBox `json:"tables"`
	Lines   []Line     `json:"lines,omitempty"`
	Rects   []Rect     `json:"rects,omitempty"`
	Circles []Circle   `json:"circles,omitempty"`
	// 页眉与页脚（会在每一页重复渲染）
	Header HeaderFooter `json:"header"`
	Footer HeaderFooter `json:"footer"`
}

// HeaderFooter 描述页眉/页脚区域的固定高度与元素集合。
type HeaderFooter struct {
	Height  float64    `json:"height"` // 区域高度（mm）
	Texts   []TextBox  `json:"texts"`
	Images  []ImageBox `json:"images"`
	Lines   []Line     `json:"lines,omitempty"`
	Rects   []Rect     `json:"rects,omitempty"`
	Circles []Circle   `json:"circles,omitempty"`
}

// Margin 以毫米为单位。
type Margin struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

// TextBox 表示一个已经排好坐标的文本块。
type TextBox struct {
	Content    string        `json:"content"`
	X          float64       `json:"x"`
	Y          float64       `json:"y"`
	Width      float64       `json:"width"`
	LineHeight float64       `json:"lineHeight"`
	Font       string        `json:"font"`
	FontSize   float64       `json:"fontSize"`
	Color      Color         `json:"color"`
	Lines      []TextLine    `json:"lines"`
	Height     float64       `json:"height"`
	Align      string        `json:"align,omitempty"` // 文本水平对齐方式：left/center/right（默认 left）
	Wrap       string        `json:"wrap,omitempty"`  // 折行策略：anywhere(默认)/break-word/nowrap；当省略时默认为 anywhere
	Debug      *TextBoxDebug `json:"debug,omitempty"`
}

// TextLine 表示排版后的一行文本内容及其宽高。
type TextLine struct {
	Content   string  `json:"content"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
	GapBefore float64 `json:"gapBefore,omitempty"`
}

// TextBoxDebug holds optional debug info displayed only when enabled by BuildOptions.
type TextBoxDebug struct {
	RawUnits *RawUnits `json:"rawUnits,omitempty"`
}

// RawUnits describes original author-specified units for key fields.
type RawUnits struct {
	FontSize   *RawLengthJSON     `json:"fontSize,omitempty"`
	LineHeight *RawLineHeightJSON `json:"lineHeight,omitempty"`
}

// RawLengthJSON is a JSON-friendly representation of Length.
type RawLengthJSON struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// RawLineHeightJSON is a JSON-friendly representation of LineHeightSpec.
type RawLineHeightJSON struct {
	Kind   string  `json:"kind"` // "factor" | "absolute"
	Factor float64 `json:"factor,omitempty"`
	Value  float64 `json:"value,omitempty"`
	Unit   string  `json:"unit,omitempty"`
}

// ImageBox 用于描述图片位置与尺寸。
type ImageBox struct {
	Path    string  `json:"path"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	Fit     string  `json:"fit"`
	Opacity float64 `json:"opacity"`
}

// TableBox 保存简化表格布局信息（平均列宽）。
type TableBox struct {
	X            float64    `json:"x"`
	Y            float64    `json:"y"`
	Width        float64    `json:"width"`
	RowGap       float64    `json:"rowGap"`
	ColumnWidths []float64  `json:"columnWidths"`
	Rows         []TableRow `json:"rows"`
	BorderColor  Color      `json:"borderColor"`
}

// TableRow 记录每一行的高度与单元格。
type TableRow struct {
	Y        float64     `json:"y"`
	Height   float64     `json:"height"`
	IsHeader bool        `json:"isHeader"`
	Cells    []TableCell `json:"cells"`
}

// TableCell 复用 TextBox 作为单元格内容。
type TableCell struct {
	Text TextBox `json:"text"`
}

// 基本图形：直线、矩形、圆形（单位均为 mm）。
// Line 表示一条线段。
type Line struct {
	X1    float64 `json:"x1"`
	Y1    float64 `json:"y1"`
	X2    float64 `json:"x2"`
	Y2    float64 `json:"y2"`
	Color Color   `json:"color"`
	Width float64 `json:"width"` // 线宽（mm），<=0 时由渲染器给默认值
}

// Rect 表示一个矩形（不包含圆角）。
type Rect struct {
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	StrokeColor Color   `json:"strokeColor"`
	StrokeWidth float64 `json:"strokeWidth"`  // mm
	FillColor   *Color  `json:"fillColor,omitempty"` // 为空表示不填充
}

// Circle 表示一个圆。
type Circle struct {
	CX          float64 `json:"cx"`
	CY          float64 `json:"cy"`
	R           float64 `json:"r"`
	StrokeColor Color   `json:"strokeColor"`
	StrokeWidth float64 `json:"strokeWidth"` // mm
	FillColor   *Color  `json:"fillColor,omitempty"`
}

// Style 用于描述可继承的文本样式。
type Style struct {
	Name    string            `json:"name"`
	Extends string            `json:"extends,omitempty"`
	Props   map[string]string `json:"props"`
}

// DocumentMeta 保存 PDF 元信息。
type DocumentMeta struct {
	Title    string   `json:"title"`
	Author   string   `json:"author"`
	Subject  string   `json:"subject"`
	Creator  string   `json:"creator"`
	Keywords []string `json:"keywords"`
}
