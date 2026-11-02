package live

import (
	"strings"
	"testing"
)

func TestTrimToolResultBoundsStringsAndArrays(t *testing.T) {
	result := map[string]any{
		"text":  strings.Repeat("x", 9000),
		"items": make([]any, 30),
		"ok":    true,
	}
	trimmed := trimToolResult(result)
	if len([]rune(trimmed["text"].(string))) > toolResultMaxString+16 {
		t.Fatalf("string not bounded: %d", len([]rune(trimmed["text"].(string))))
	}
	if len(trimmed["items"].([]any)) != toolResultMaxItems {
		t.Fatalf("items not bounded: %d", len(trimmed["items"].([]any)))
	}
	if trimmed["_truncated"] != true || trimmed["ok"] != true {
		t.Fatalf("truncation marker missing: %#v", trimmed)
	}
}
