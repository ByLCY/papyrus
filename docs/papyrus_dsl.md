# Papyrus DSL 设计（用于 Go 生成 PDF）

> 建议将 DSL 文件保存为 `.papyrus` 扩展名，便于 CLI 识别。

## 1. 设计目标
- **人类可读**：面向设计/产品同学，避免 Go 语法噪音。
- **与 PDF API 解耦**：DSL 只表达意图；解释器可以绑定不同的渲染后端（当前实现使用 `github.com/tdewolff/canvas`）。
- **可组合**：布局、样式、数据绑定都可复用，支持复杂文档。
- **可验证**：语法/语义层面提前捕获错误（缺字体、图片尺寸非法等）。

## 2. 执行架构
```
papyrus.dsl ──(lexer+parser)──> AST ──(验证/归一化)──> 渲染计划 ──(PDF 后端)──> output.pdf
```
1. **Lexer/Parser**：自定义语法 -> AST。
2. **语义阶段**：解析尺寸单位、继承样式、数据绑定、资源引用。
3. **渲染阶段**：将页面命令映射为具体的 Renderer（当前默认 canvas）调用；统一接口便于扩展其它后端。

## 3. 语法概览
- 文件使用 UTF-8，`\n` 结尾。
- 注释：`# 注释` 或 `/* ... */`。
- 标识符：`[A-Za-z_][\w-]*`。字符串支持双引号，内部用 `\"` 转义。
- 数值：整数或小数，可带单位 `pt|mm|cm|in|%`，默认 `pt`。
- 颜色：`#RRGGBB[AA]`。
- 字符串插值：`${path.to.value}`，用于绑定运行时数据。

### 3.1 顶层结构
```
doc Papyrus v0 {
  meta { ... }
  resources { ... }
  page-set name { ... }
  page ... { ... }
}
```

### 3.2 EBNF 摘要
```
document      = "doc" ident "v" version "{" section* "}" ;
section       = meta | resources | pageSet | page ;
meta          = "meta" block ;
resources     = "resources" block ;
pageSet       = "page-set" ident block ;     # 复用页面定义
page          = "page" pageHeader block ;
pageHeader    = sizeSpec orientation? marginSpec? ;
# 在 page 的 block 顶层可使用 header/footer 定义页眉/页脚：
# headerBlock  = "header" (heightSpec)? block ;
# footerBlock  = "footer" (heightSpec)? block ;
# heightSpec   = "height" number ;
# header 子元素（text/image）支持 align 属性覆盖默认居中：align := left | center | right
block         = "{" statement* "}" ;
statement     = layout | drawCmd | control ;
layout        = ("flow" | "absolute" | "grid") layoutOpts block ;
drawCmd       = text | image | rect | line | circle | table ;
control       = ifStmt | forStmt | letStmt ;
```

## 4. 语义元素

### 4.1 meta
```papyrus
meta {
  title:    "Q1 Report"
  author:   "Papyrus Bot"
  subject:  "Quarterly Summary"
  keywords: ["finance", "q1", "internal"]
}
```

### 4.2 resources
```papyrus
resources {
  font Body { src: "embed:Inter/static/Inter-Regular.ttf" }  # 使用内置 Inter
  font BodyBold { src: "embed:Inter/static/Inter-Bold.ttf" }

  color Primary = #0F62FE
  color Danger  = #DA1E28

  image Logo { src: "assets/logo.png"; dpi: 300 }

  style Heading {
    font: BodyBold
    size: 18pt
    color: Primary
    spacing: { before: 6pt; after: 4pt }
  }

  style SubHeading extends Heading {
    size: 14pt
    color: #333333
  }
}
```
资源 ID 在后续语句中可直接引用。解释器负责在初始化阶段加载字体/图片并传给渲染器。
- `style` 支持属性继承：`style Sub extends Base`，子样式会先拷贝父样式属性再覆盖自身定义。
- 样式属性与命令参数一致（如 `font`, `size`, `color`, `line-height`, `width` 等），文本命令引用样式后即可省略重复的 `size/color` 声明。
- 样式属性与命令参数一致（如 `font`, `size`, `color`, `line-height`, `width` 等），并支持 `line-height: 18pt` 或 `line-height: 1.5x`（字体大小的 1.5 倍）。文本命令引用样式后即可省略重复的 `size/color` 声明。
- 字体可指定 `fallback`，当自定义字体缺失或加载失败时会退回到另一个字体（例如 `fallback: "embed:Inter/static/Inter-Regular.ttf"`）。
- 程序内置了 [Inter](https://github.com/rsms/inter) 字体，可通过 `src: "embed:Inter/static/Inter-Regular.ttf"` 引用，无需额外部署；若仍需 PDF Core 14 字体，可写 `src: "builtin:Times-Roman"` 等。
- `src` 支持三种写法：普通文件路径（相对 DSL）、`embed:` 前缀引用内置字体，以及 `builtin:<CoreFont>`（使用 PDF 标准字体）。
- 若 DSL 中没有显式 `font` 定义，渲染器会尝试加载 `assets/fonts/Noto_Sans_SC/static/NotoSansSC-Regular.ttf`（相对 DSL 的默认路径），并在加载失败时自动回退至内置 Inter。

### 4.3 页面与布局
```papyrus
page A4 portrait margin 20mm {
  flow {
    text Heading { "Papyrus 示例" }
    text Body size 11pt color #333 { "你好，${user.name}！" }
    rect { width: 100%; height: 0.6pt; fill: Primary }

    grid columns 3 gap 6mm {
      for item in metrics {
        cell {
          text BodyBold size 24pt align center { "${item.value}" }
          text Body align center color #777 { "${item.label}" }
        }
      }
    }
  }
}
```
- `flow`：顺序排版，支持 `wrap`, `padding`, `align` 属性。
- `flow align center/right`：可通过 `align` 指定子内容相对父容器的对齐方式（默认 `left`）。未显式 `width` 时系统会根据内部文本宽度（或子 flow）估算尺寸，再做居中/右对齐。
- `absolute`：自定义坐标 `{ x: 10mm; y: 20mm; width: 50mm }`，适合浮层、页眉页脚等不影响主流排的模块。
- `grid`：`columns|rows`、`gap`、`row-height` 等属性，内部 `cell` 自动设置约束。

### 4.4 绘制命令
| 命令                           | 关键属性                                                                  | 描述                                                      |
|------------------------------|-----------------------------------------------------------------------|---------------------------------------------------------|
| `text styleRef? attrs block` | `font`, `size`, `color`, `line-height`, `align`, `max-width`, `wrap`  | `block` 内部是文本，可含 `${}` 插值与 `\n`。                        |
| `image ref attrs`            | `src`, `fit: cover\| contain \|stretch`, `width`, `height`, `opacity` | `src` 可引用 `resources.image` 或直接路径，支持放入 `flow/absolute`。 |
| `rect` / `line` / `circle`   | `stroke`, `fill`, `radius`, `dash`                                    | 绘制基础形状。                                                 |
| `table columns n { ... }`    | `columns`, `width`, `row-gap`, `header`, `row`、`cell`                 | 仅需声明 `header` 与若干 `row`，列宽自动平分，可用 `row-gap: 2mm` 控制行间距（默认 0）。 |

### 4.5 控制语句
```papyrus
let currency = data.meta.currency

if data.summary != nil {
  text Body { "合计：${formatFloat(data.summary.total, 2)} ${currency}" }
}

for row in data.items {
  text Body { "- ${row.name}: ${row.qty}" }
}
```
- `let`：定义只读别名，可绑定表达式。
- `if`：条件块，支持 `elif`/`else`。
- `for item in expr { ... }`：遍历数组；内置 `loop.index`, `loop.first`, `loop.last`。

### 4.6 文本折行（wrap）
- 属性位置：可用于 `flow` 与 `text`。
- 取值：`anywhere` | `break-word` | `nowrap`
    - `anywhere`（默认）：尽量在空白处分割；若单个词过长仍会在词内断开；不自动插入连字符；尊重显式 `\n`。
    - `break-word`：忽略空白机会，严格按容器宽度连续切分字符（仍尊重显式 `\n`）。
    - `nowrap`：不按宽度折行，仅在显式 `\n` 处分行。
- 继承与优先级：`text.wrap` 优先于所在 `flow.wrap`，未显式设置时继承父级；若全都未设置，则默认 `anywhere`。
- 示例：
```papyrus
flow {                 // wrap 默认 anywhere
  text { "A longlonglong text prefers spaces but may break within words if needed." }
  text wrap: break-word { "ThisWillBeSplitStrictlyByWidthWithoutPreferringSpaces" }
  text wrap: nowrap { "This_will_not_wrap_unless_you_use\\nnewline" }
}
```

### 4.7 表格与图片示例
```papyrus
table columns 3 width 100% row-gap 2mm {
  header {
    cell BodyBold color Accent { "项目" }
    cell BodyBold color Accent { "数量" }
    cell BodyBold color Accent { "金额" }
  }
  row {
    cell Body { "设计稿" }
    cell Body { "12" }
    cell Body { "¥ 8,000" }
  }
}

absolute x 120mm y 20mm width 60mm {
  image Hero width 40mm height 15mm
  text Body size 10pt color #777 { "absolute 不占据主流排高度" }
}
```
`row-gap` 可以接受任何长度单位（如 `pt`, `mm` 或 `1.5` 默认 pt），未设置时行距为 0mm，与相邻单元格共用边框。

### 4.8 调试 JSON
- 运行 CLI 时可追加 `-debug output/layout.json`，系统会把 `layout.Result` 以 JSON 持久化。
- JSON 中包含页面尺寸、文本/图片/表格坐标，可直接用于前端 overlay 或排查布局问题。

### 4.9 字符串插值与数据绑定
- 文本中可以写 `${path.to.value}`，布局阶段会从运行时数据中取值；若路径不存在，则保留原占位符。
- 运行时通过 `-data '{"user":{"name":"Papyrus"}}'` 传入 JSON 数据，路径以传入 JSON 为根。
- 示例：`text Body { "欢迎，${user.name}!" }` 搭配命令 `go run . -data '{"user":{"name":"Papyrus"}}' ...` 即可渲染。

## 5. 示例 DSL
```papyrus
doc Papyrus v1 {
  meta { title: "Invoice ${data.invoiceNo}" }

  resources {
    font Serif   { src: "fonts/PlayfairDisplay-Regular.ttf" }
    font Sans    { src: "fonts/Inter-Regular.ttf" }
    color Accent = #2E86AB

    style Title {
      font: Serif
      size: 22pt
      color: Accent
    }

    style Body {
      font: Sans
      size: 12pt
      color: #444
    }

    style Emphasis extends Body {
      color: Accent
      font: Sans
    }
  }

  page A4 portrait margin 18mm {
    flow {
      text Title { "Papyrus 发票" }
      text Body {
        "客户：${data.customer.name}\n"
        "日期：${formatDate(data.date, \"2006-01-02\")}"
      }

      table data.items {
        columns {
          column 30% { header: "描述"; field: item.desc; align: left }
          column 20% { header: "数量"; field: item.qty;  align: right }
          column 25% { header: "单价"; field: formatMoney(item.price); align: right }
          column 25% { header: "小计"; field: formatMoney(item.amount); align: right }
        }
      }

      flow align right {
        text Body size 10pt color #999 { "合计" }
        text Emphasis size 16pt { "${formatMoney(data.total)}" }
      }
    }
  }
}
```

## 6. Go 实现要点

### 6.1 词法/语法
- 推荐使用 [participle](https://github.com/alecthomas/participle) 或自写 LL(1) 解析器。
- Token 需记录位置信息，便于报错。

### 6.2 AST（示例片段）
```go
type Document struct {
    Version   string
    Meta      Meta
    Resources Resources
    Sections  []Section // Page或PageSet引用
}

type TextCmd struct {
    StyleRef string
    Attrs    AttrMap
    Content  []Inline // PlainText or Interpolation
}
```

### 6.3 渲染管线
```go
type Renderer interface {
    BeginDocument(meta Meta, resources ResResolved) error
    BeginPage(spec PageSpec) error
    DrawText(text TextLayout) error
    // ...其余命令
    EndDocument() error
}

func Render(doc *ast.Document, data any, backend Renderer) error {
    ctx := eval.NewContext(data, doc.Resources)
    for _, page := range doc.Pages {
        layout := layout.Resolve(page, ctx)
        for _, cmd := range layout.Commands {
            if err := backend.Dispatch(cmd); err != nil {
                return err
            }
        }
    }
    return backend.EndDocument()
}
```
- 默认实现 `renderer/canvas`，封装字体加载、颜色转换、绝对坐标绘制。
- 通过接口可切换为 `unidoc`, `pdfcpu` 等。

### 6.4 数据绑定/辅助函数
- 提供内置函数（`formatDate`, `formatMoney`, `uppercase`）。
- 数据上下文采用 `map[string]any` 或结构体，访问使用 JSONPath 风格 `data.items[0].name`。
- 插值解析成表达式 AST，避免运行时 `text/template` 注入风险。

### 6.5 校验
- 语义阶段检查：
  - 字体/图片是否定义。
  - 尺寸、百分比范围。
  - 表格列宽总计是否为 100% 或绝对值不超页面宽度。
  - 控制流变量是否存在。

## 7. 扩展点
- **组件**：`component Badge { params (label, color) ... }`，使用 `use Badge { label: "Paid" }`。
- **层叠样式**：允许 `style` 中 `extends Base`.
- **事件钩子**：`on page.enter { ... }`，可在渲染期注入脚注、页码。
- **调试模式**：输出布局盒树 JSON 以便在前端可视化。

---
该 DSL 让产品与设计直接描述 PDF 意图，Go 侧仅负责解析与渲染，既利用现有 PDF 库的能力，又保持语义清晰、可扩展。***



## 基本图形（line/rect/circle）

- 坐标与尺寸单位统一为 mm，坐标系原点为页面左上角（页面坐标）。
- 支持在页面主体（page 顶层）、`header {}`、`footer {}` 中声明，渲染顺序：
  - Header：形状 → 文本 → 图片
  - Page：形状（背景）→ 正文内容
  - Footer：形状 → 文本 → 图片

### 指令语法

1) line（直线）
- 完整形态：
  - `line x1 <len> y1 <len> x2 <len> y2 <len> [color <name|#hex>] [width <len>]`
- 简化形态（方案 A，新增）：
  - 水平线：`line x <len> y <len> length <len> [color <…>] [width <len>]`
  - 垂直线：`line x <len> y <len> length <len> dir v [color <…>] [width <len>]`
- 说明：
  - 简化形态会在构建阶段被展开为 `x1/y1/x2/y2`；`dir` 省略或 `h` 表示水平。
  - `width`（线宽）缺省或 ≤0 时由渲染器回退到默认值（约 0.2mm）。

2) rect（矩形）
- `rect x <len> y <len> width <len> height <len> [stroke <name|#hex>] [stroke-width <len>] [fill <name|#hex>]`
- 无填充时 `fill` 省略表示透明；描边宽度缺省时由渲染器回退默认值（约 0.2mm）。

3) circle（圆）
- `circle cx <len> cy <len> r <len> [stroke <name|#hex>] [stroke-width <len>] [fill <name|#hex>]`

### 示例

- 页面背景分隔线（简化形态）：
```
line x 25mm y 35mm length 160mm color #000 width 0.3mm
```

- 页眉里的底部分隔线（简化形态，水平）：
```
header height 15mm {
  image Logo width 41.1mm height 7.3mm
  line x 25mm y 14.8mm length 160mm color #000 width 0.2mm
}
```

- 带描边的无填充矩形：
```
rect x 20mm y 50mm width 60mm height 30mm stroke #000 stroke-width 0.2mm
```

- 有填充的圆：
```
circle cx 120mm cy 45mm r 12mm stroke #00f stroke-width 0.2mm fill #E6F0FF
```
