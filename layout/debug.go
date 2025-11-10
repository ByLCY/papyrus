package layout

import (
	"encoding/json"
	"os"
)

// WriteDebugJSON 将布局结果输出为 JSON，便于调试或可视化。
func WriteDebugJSON(res *Result, path string) error {
	if res == nil {
		return nil
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
