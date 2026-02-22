package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// EditTool provides file editing functionality
type EditTool struct {
	*BaseTool
	workspace string
}

// NewEditTool creates a new file editing tool
func NewEditTool(workspace string) *EditTool {
	return &EditTool{
		BaseTool: &BaseTool{
			NameValue: "edit",
			DescriptionValue: `Edit a file by replacing exact text. The old_string must match exactly (including whitespace).
Use this for precise, surgical edits. If exact match fails, a fuzzy match ignoring whitespace differences is attempted.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit (relative or absolute)",
					},
					"old_string": map[string]interface{}{
						"type":        "string",
						"description": "Exact text to find and replace (must match file content)",
					},
					"new_string": map[string]interface{}{
						"type":        "string",
						"description": "New text to replace the old text with (empty to delete)",
					},
					"replace_all": map[string]interface{}{
						"type":        "boolean",
						"description": "Replace all occurrences of old_string (default false)",
						"default":     false,
					},
				},
				"required": []string{"path", "old_string", "new_string"},
			},
		},
		workspace: workspace,
	}
}

func (t *EditTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	pathVal, ok := params["path"].(string)
	if !ok || pathVal == "" {
		return ErrorResult("path is required"), nil
	}

	filePath, err := ResolvePath(t.workspace, pathVal)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	oldString, _ := params["old_string"].(string)
	newString, _ := params["new_string"].(string)
	replaceAll := OptionalBool(params, "replace_all", false)

	if oldString == "" {
		return ErrorResult("old_string is required. To create a new file, use the write tool"), nil
	}

	// Check file
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("file not found: %s", filePath)), nil
		}
		return ErrorResult(fmt.Sprintf("failed to access file: %v", err)), nil
	}
	if fileInfo.IsDir() {
		return ErrorResult(fmt.Sprintf("path is a directory, not a file: %s", filePath)), nil
	}

	// Read file
	rawContent, err := os.ReadFile(filePath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err)), nil
	}

	oldContent, isCrlf := ToUnixLineEndings(string(rawContent))

	// Validate UTF-8 encoding
	if !utf8.ValidString(oldContent) {
		return ErrorResult("File is not valid UTF-8. Only UTF-8 encoded files are supported."), nil
	}

	var newContent string

	if replaceAll {
		count := strings.Count(oldContent, oldString)
		if count == 0 {
			return ErrorResult("old_string not found in file. Make sure it matches exactly, including whitespace and line breaks"), nil
		}
		newContent = strings.ReplaceAll(oldContent, oldString, newString)
	} else {
		newContent, err = replaceExactOrFuzzy(oldContent, oldString, newString)
		if err != nil {
			return ErrorResult(err.Error()), nil
		}
	}

	// Convert back to CRLF if needed
	if isCrlf {
		newContent, _ = ToWindowsLineEndings(newContent)
	}

	// Write file
	if err := os.WriteFile(filePath, []byte(newContent), 0o644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	// Generate diff for UI
	relPath := strings.TrimPrefix(filePath, t.workspace+string(os.PathSeparator))
	diffText, additions, removals := GenerateDiff(oldContent, newContent, relPath)

	// Content for LLM: concise confirmation
	llmContent := fmt.Sprintf("Successfully replaced text in %s.", filePath)

	// Details for UI: diff
	uiDetails := fmt.Sprintf("%s (+%d -%d)\n", filePath, additions, removals)
	if diffText != "" {
		uiDetails += diffText
	}

	return &ToolResult{
		Success: true,
		Content: llmContent,
		Details: uiDetails,
	}, nil
}

// replaceExactOrFuzzy tries exact match first, then fuzzy match with whitespace tolerance.
func replaceExactOrFuzzy(content, oldString, newString string) (string, error) {
	// Try exact match
	index := strings.Index(content, oldString)
	if index != -1 {
		// Check uniqueness
		lastIndex := strings.LastIndex(content, oldString)
		if index != lastIndex {
			return "", fmt.Errorf("old_string appears multiple times in the file. Please provide more context to ensure a unique match, or set replace_all to true")
		}
		return content[:index] + newString + content[index+len(oldString):], nil
	}

	// Exact match failed — try fuzzy match (whitespace-tolerant)
	start, end, err := fuzzyFindInContent(content, oldString)
	if err != nil {
		return "", err
	}

	return content[:start] + newString + content[end:], nil
}

// fuzzyFindInContent finds oldString in content with whitespace tolerance.
// Collapses all whitespace runs to a single space, then searches.
// Returns start, end indices in the original content.
func fuzzyFindInContent(content, oldString string) (int, int, error) {
	normContent, contentMap := normalizeWithMapping(content)
	normSearch, _ := normalizeWithMapping(oldString)
	normSearch = strings.TrimSpace(normSearch)

	if normSearch == "" {
		return 0, 0, fmt.Errorf("old_string not found in file. Make sure it matches exactly, including whitespace and line breaks")
	}

	idx := strings.Index(normContent, normSearch)
	if idx == -1 {
		return 0, 0, fmt.Errorf("old_string not found in file. Make sure it matches exactly, including whitespace and line breaks")
	}

	// Check uniqueness in normalized content
	lastIdx := strings.LastIndex(normContent, normSearch)
	if idx != lastIdx {
		return 0, 0, fmt.Errorf("old_string appears multiple times in the file (fuzzy match). Please provide more context to ensure a unique match, or set replace_all to true")
	}

	origStart := contentMap[idx]
	origEnd := contentMap[idx+len(normSearch)]

	return origStart, origEnd, nil
}

// normalizeWithMapping collapses all whitespace runs to a single space,
// returning the normalized string and a mapping from normalized index → original index.
// A sentinel entry maps len(normalized) → len(original).
func normalizeWithMapping(s string) (string, []int) {
	result := make([]byte, 0, len(s))
	mapping := make([]int, 0, len(s)+1)

	inWhitespace := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			if !inWhitespace {
				result = append(result, ' ')
				mapping = append(mapping, i)
				inWhitespace = true
			}
		} else {
			result = append(result, ch)
			mapping = append(mapping, i)
			inWhitespace = false
		}
	}
	// Sentinel: end of normalized → end of original
	mapping = append(mapping, len(s))

	return string(result), mapping
}
