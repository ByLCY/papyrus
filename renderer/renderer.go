package renderer

import "github.com/ByLCY/papyrus/layout"

// Renderer 将布局结果输出为最终文件，例如 PDF 或图像。
// Render 返回生成的二进制数据（例如 PDF 字节切片）以及可能的错误。
type Renderer interface {
	Render(result *layout.Result) ([]byte, error)
}
