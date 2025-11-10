# PDF 渲染流水线

本阶段在 DSL 解析之后增加两层能力：布局计算与 canvas 渲染。以下内容仅覆盖当前示例所需的最小能力，后续可以按模块扩展。

## 1. 数据流

```
DSL 文本 ──解析──> dsl.Document ──layout.Build──> layout.Result ──canvas.Render──> demo.pdf
```

1. `layout.Build` 读取资源、页面尺寸、flow 内的 `text` 命令，计算坐标、行高等信息。
2. `renderer/canvas` 读取布局结果，依次添加页面并绘制文本、图片和表格，字体分为 embed 与文件两种加载方式。

## 2. 布局规则（当前实现）
- 支持多层 `flow`、`absolute` 容器：`flow` 按顺序累积高度，`absolute` 仅影响自身坐标，不改变父流排。
- `text`：行高默认 `fontSize * 1.4`，可通过 `line-height` 覆盖；字号、颜色继承资源中同名字体/颜色。
- `image`：可引用 `resources.image` 或直接路径；未指定尺寸会优先读取资源内配置，否则使用容器宽度。
- `table`：通过 `columns` 指定列数，`header` 与 `row` 内使用 `cell` 描述文本，列宽自动平分并带浅色表头。
- `style`：在 `resources` 中定义 `style Foo extends Bar`，布局阶段会自动将样式属性合并到命令参数里，可复用字体/颜色配置。
- 页面 `margin <length>` 支持 `mm/cm/in/pt/%`，所有内部长度统一换算为毫米。

## 3. 单位与坐标约定（重要）

为了避免单位混用导致的视觉与数值偏差，我们采用“统一约定 + 边界转换”的策略：

- 对外坐标与尺寸（布局与调试 JSON）：统一为毫米（mm）。
- Layout ↔ Typesetter（排版后端）之间的约定：输入与输出全部以 mm 表示。例如 `fontSize`、`lineHeight`、`TextLine.Height/GapBefore/Width` 等。
- 渲染器内部与字体系统交互：使用 pt（points）。仅在以下边界点进行换算：
  - 创建字体面：`fontSize(mm) → pt`。
  - 读取字体度量：`Metrics.Ascent/LineHeight(pt) → mm` 后参与排版数值计算。
  - 文本测宽：`face.TextWidth(…)(pt) → mm` 后与行宽限制（mm）比较。

### 行高与 leading（行前空白）
- `line-height` 支持两种语义：
  - 倍数：`1.2x` → 解析为 `lineHeight = fontSize * 1.2`（mm）。
  - 绝对：`18pt/6mm` → 解析为对应的绝对长度（统一换算为 mm）。
- `TextLine` 的 `GapBefore` 由 `leading = max(lineHeight - textHeight, 0)` 得到，其中 `textHeight` 来自字体度量的行盒高度（pt→mm）。
- 首行 `GapBefore = 0`；第二行及以后使用同一 `leading`。

### 调试原始单位（Raw Units）
- 通过 CLI 开关 `--debug-raw-units` 或 `BuildOptions.Debug.RawUnits = true` 启用。
- 启用后，在文本盒的 JSON 中会出现 `debug.rawUnits` 影子字段，展示作者在 DSL 中的原始语义：
  - `fontSize`: `{ value: 12, unit: "pt" }`
  - `lineHeight`:
    - 倍数：`{ kind: "factor", factor: 1.2 }`
    - 绝对：`{ kind: "absolute", value: 18, unit: "pt" }`
- 这些影子字段仅用于排查问题，不参与任何计算。

## 4. canvas 渲染
- 内置 Inter 字体可通过 `src: "embed:Inter/static/Inter-Regular.ttf"` 引用，无需部署；若需要 PDF Core 14 字体，可写 `src: "builtin:Times-Roman"` 等。所有字体都可指定 `fallback`，失败时会自动回退到嵌入字体。
- 如果 DSL 未声明任何 `font`，引擎会默认尝试加载 `assets/fonts/Noto_Sans_SC/static/NotoSansSC-Regular.ttf`（相对 DSL 路径）；若该文件不存在，则会回退到内置 Inter，确保永远有可用字体。
- 图片采用 `canvas.DrawImage` 绘制，路径默认相对 DSL 文件目录，可配置 `width/height/fit`。
- 表格在渲染阶段自动绘制边框、表头背景，并在每个单元格里复用 `NewTextLine`。
- 基本图形：支持在页面上绘制直线、矩形、圆形。布局结果 `layout.Page` 提供 `lines/rects/circles` 三个字段（单位 mm），矩形与圆支持填充与描边颜色、线宽（mm）。
- 全部元素（文本/图片/表格/图形）都可在调试 JSON 中查看最终坐标，便于排查溢出或分页问题。

## 5. 示例

运行：

```bash
go run . -in examples/demo.papyrus -out output/demo.pdf \
  -data '{"user":{"name":"Papyrus"}}'
```

如需输出调试 JSON 且携带原始单位影子字段：

```bash
go run . -in examples/demo.papyrus -out output/demo.pdf \
  -debug output/stract.json --debug-raw-units
```

效果：在 `output/demo.pdf` 看到一页 A4 文件，包含嵌套 flow 文本、absolute 覆盖的图片以及 3 列表格；在调试 JSON 中可见 `fontSize/lineHeight` 的 mm 数值以及 `debug.rawUnits` 的原始语义。