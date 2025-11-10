package binding

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var exprPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Interpolate 将文本中的 ${path.to.value} 替换为 data 中的值。
// 若 data 为空或路径不存在，则返回原占位符。
func Interpolate(text string, data any) string {
	if data == nil {
		return text
	}
	return exprPattern.ReplaceAllStringFunc(text, func(match string) string {
		groups := exprPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		path := strings.TrimSpace(groups[1])
		if path == "" {
			return match
		}
		if val, ok := resolvePath(data, path); ok {
			return fmt.Sprint(val)
		}
		return match
	})
}

func resolvePath(data any, path string) (any, bool) {
	current := data
	segments := strings.Split(path, ".")
	for _, segment := range segments {
		name, indexes := parseSegment(segment)
		if name != "" {
			var ok bool
			current, ok = descendMap(current, name)
			if !ok {
				return nil, false
			}
		}
		for _, idxStr := range indexes {
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				return nil, false
			}
			var ok bool
			current, ok = descendArray(current, idx)
			if !ok {
				return nil, false
			}
		}
	}
	return current, true
}

func parseSegment(segment string) (string, []string) {
	name := segment
	indexes := []string{}
	if i := strings.Index(segment, "["); i != -1 {
		name = segment[:i]
		rest := segment[i:]
		for len(rest) > 0 {
			if rest[0] != '[' {
				break
			}
			end := strings.IndexByte(rest, ']')
			if end == -1 {
				break
			}
			indexes = append(indexes, rest[1:end])
			rest = rest[end+1:]
		}
	}
	return name, indexes
}

func descendMap(current any, key string) (any, bool) {
	switch c := current.(type) {
	case map[string]interface{}:
		val, ok := c[key]
		return val, ok
	default:
		return nil, false
	}
}

func descendArray(current any, idx int) (any, bool) {
	switch c := current.(type) {
	case []interface{}:
		if idx < 0 || idx >= len(c) {
			return nil, false
		}
		return c[idx], true
	default:
		return nil, false
	}
}
