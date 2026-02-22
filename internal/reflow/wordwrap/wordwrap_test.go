package wordwrap

import (
	"strings"
	"testing"

	"github.com/muesli/reflow/ansi"
)

func TestWordWrap_CJK(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit int
		want  int // minimum number of lines expected
	}{
		{
			name:  "Chinese text wraps correctly",
			input: "这是一个很长的中文句子，应该能够正确换行",
			limit: 20,
			want:  2,
		},
		{
			name:  "Japanese text wraps correctly",
			input: "これは日本語のテキストです。正しく折り返されるべきです。",
			limit: 20,
			want:  2,
		},
		{
			name:  "Korean text wraps correctly",
			input: "이것은 한국어 텍스트입니다. 올바르게 줄 바꿈되어야합니다.",
			limit: 20,
			want:  2,
		},
		{
			name:  "Mixed CJK and English",
			input: "This is 中文 mixed with English text that should wrap correctly",
			limit: 25,
			want:  2,
		},
		{
			name:  "Short CJK no wrap",
			input: "短文本",
			limit: 20,
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := String(tt.input, tt.limit)
			lines := strings.Split(strings.TrimRight(result, "\n"), "\n")

			if len(lines) < tt.want {
				t.Errorf("Expected at least %d lines, got %d\nInput: %s\nOutput: %s",
					tt.want, len(lines), tt.input, result)
			}

			// Verify each line doesn't exceed the limit
			for i, line := range lines {
				width := ansi.PrintableRuneWidth(line)
				if width > tt.limit {
					t.Errorf("Line %d exceeds limit: width=%d, limit=%d\nLine: %s",
						i, width, tt.limit, line)
				}
			}
		})
	}
}

func TestWordWrap_CJKWithANSI(t *testing.T) {
	// Test CJK text with ANSI color codes
	input := "\x1b[31m这是一个带颜色的中文句子应该能够正确换行\x1b[0m"
	limit := 20
	result := String(input, limit)

	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) < 2 {
		t.Errorf("Expected colored CJK text to wrap, got %d lines", len(lines))
	}

	// Verify ANSI codes are preserved
	if !strings.Contains(result, "\x1b[31m") {
		t.Error("Color code was lost during wrapping")
	}
}

func TestWordWrap_English(t *testing.T) {
	// Verify English text still works (wraps at word boundaries)
	input := "This is a very long English sentence that should wrap at word boundaries"
	limit := 20
	result := String(input, limit)

	lines := strings.Split(strings.TrimRight(result, "\n"), "\n")
	if len(lines) < 3 {
		t.Errorf("Expected English text to wrap, got %d lines", len(lines))
	}

	// Verify each line doesn't exceed the limit
	for i, line := range lines {
		width := ansi.PrintableRuneWidth(line)
		if width > limit {
			t.Errorf("Line %d exceeds limit: width=%d, limit=%d", i, width, limit)
		}
	}
}
