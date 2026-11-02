package live

import "strings"

const (
	toolResultMaxString = 4000
	toolResultMaxItems  = 20
	toolResultMaxDepth  = 6
)

// trimToolResult bounds tool payloads before they enter the model context and
// the audit record. Sandbox results are excluded by the caller because their
// file contents may be persisted to the private workspace.
func trimToolResult(result map[string]any) map[string]any {
	trimmed, changed := trimToolValue(result, 0)
	value, ok := trimmed.(map[string]any)
	if !ok {
		return map[string]any{"truncated": true, "notice": "工具结果格式已简化"}
	}
	if changed {
		value["_truncated"] = true
	}
	return value
}

func trimToolValue(value any, depth int) (any, bool) {
	if depth > toolResultMaxDepth {
		return nil, true
	}
	switch typed := value.(type) {
	case string:
		if len([]rune(typed)) > toolResultMaxString {
			return truncateRunes(typed, toolResultMaxString) + "…[truncated]", true
		}
		return typed, false
	case []any:
		changed := len(typed) > toolResultMaxItems
		if changed {
			typed = typed[:toolResultMaxItems]
		}
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			trimmed, itemChanged := trimToolValue(item, depth+1)
			out = append(out, trimmed)
			changed = changed || itemChanged
		}
		return out, changed
	case map[string]any:
		changed := false
		out := make(map[string]any, len(typed))
		count := 0
		for key, item := range typed {
			if count >= 40 {
				changed = true
				break
			}
			if strings.HasPrefix(key, "_") {
				continue
			}
			trimmed, itemChanged := trimToolValue(item, depth+1)
			out[key] = trimmed
			changed = changed || itemChanged
			count++
		}
		return out, changed
	default:
		return value, false
	}
}
