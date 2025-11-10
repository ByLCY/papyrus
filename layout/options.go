package layout

// BuildOptions 配置布局阶段所需的依赖，例如排版后端。
type BuildOptions struct {
	Typesetter Typesetter
	Debug      DebugOptions
}

// DebugOptions 控制调试相关输出。
type DebugOptions struct {
	RawUnits bool // 在调试 JSON 中输出 debug.rawUnits 影子字段
}

// Typesetter 负责根据字体与宽度约束将文本拆成可绘制的行。
type Typesetter interface {
	LayoutLines(content string, width float64, font FontResource, fontSize float64, lineHeight float64, wrap string) ([]TextLine, error)
}
