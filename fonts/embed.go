package fonts

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed Inter/static/*.ttf
var fontFS embed.FS

// Load 返回内置字体的字节数据，path 可写为 "embed:Inter/static/Inter-Regular.ttf" 或直接 "Inter/static/Inter-Regular.ttf".
func Load(path string) ([]byte, error) {
	path = strings.TrimPrefix(path, "embed:")
	clean := strings.TrimPrefix(path, "Inter/static/")
	target := "Inter/static/" + clean
	data, err := fontFS.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("读取内置字体 %s 失败: %w", target, err)
	}
	return data, nil
}
