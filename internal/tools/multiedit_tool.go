package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// MultiEditOperation represents a single edit operation
type MultiEditOperation struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// MultiEditTool provides functionality to make multiple edits to a single file
type MultiEditTool struct {
	*BaseTool
	workspace string
}

// NewMultiEditTool creates a new multi-edit tool
func NewMultiEditTool(workspace string) *MultiEditTool {
	return &MultiEditTool{
		BaseTool: &BaseTool{
			NameValue: "multiedit",
			DescriptionValue: `Make multiple edits to a single file in one operation. Edits are applied sequentially.
Each edit is a find-and-replace: old_string must match exactly (fuzzy whitespace fallback on failure).
All edits must succeed or none are applied. Prefer this over edit when making several changes to the same file.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit (relative or absolute)",
					},
					"edits": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"old_string": map[string]interface{}{
									"type":        "string",
									"description": "The text to replace",
								},
								"new_string": map[string]interface{}{
									"type":        "string",
									"description": "The text to replace it with",
								},
								"replace_all": map[string]interface{}{
									"type":        "boolean",
									"default":     false,
									"description": "Replace all occurrences (default false)",
								},
							},
							"required":             []string{"old_string", "new_string"},
							"additionalProperties": false,
						},
						"minItems":    1,
						"description": "Array of edit operations to perform sequentially",
					},
				},
				"required": []string{"path", "edits"},
			},
		},
		workspace: workspace,
	}
}

func (t *MultiEditTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	pathVal, ok := params["path"].(string)
	if !ok || pathVal == "" {
		return ErrorResult("path is required"), nil
	}

	filePath, err := ResolvePath(t.workspace, pathVal)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Get edits array
	editsVal, ok := params["edits"].([]interface{})
	if !ok || len(editsVal) == 0 {
		return ErrorResult("at least one edit operation is required"), nil
	}

	// Parse edit operations
	var edits []MultiEditOperation
	for i, editVal := range editsVal {
		editMap, ok := editVal.(map[string]interface{})
		if !ok {
			return ErrorResult(fmt.Sprintf("edit %d: invalid edit operation format", i+1)), nil
		}

		oldStr, _ := editMap["old_string"].(string)
		newStr, _ := editMap["new_string"].(string)
		replAll, _ := editMap["replace_all"].(bool)

		if oldStr == "" {
			return ErrorResult(fmt.Sprintf("edit %d: old_string is required", i+1)), nil
		}

		edits = append(edits, MultiEditOperation{
			OldString:  oldStr,
			NewString:  newStr,
			ReplaceAll: replAll,
		})
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

	currentContent := oldContent

	// Apply all edits sequentially
	for i, edit := range edits {
		newContent, err := applyEdit(currentContent, edit)
		if err != nil {
			return ErrorResult(fmt.Sprintf("edit %d failed: %s", i+1, err.Error())), nil
		}
		currentContent = newContent
	}

	// Convert back to CRLF if needed
	if isCrlf {
		currentContent, _ = ToWindowsLineEndings(currentContent)
	}

	// Write file
	if err := os.WriteFile(filePath, []byte(currentContent), 0o644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err)), nil
	}

	// Generate diff for UI
	relPath := strings.TrimPrefix(filePath, t.workspace+string(os.PathSeparator))
	diffText, additions, removals := GenerateDiff(oldContent, currentContent, relPath)

	// Content for LLM: concise
	llmContent := fmt.Sprintf("Successfully applied %d edits to %s.", len(edits), filePath)

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

// applyEdit applies a single edit operation to content.
func applyEdit(content string, edit MultiEditOperation) (string, error) {
	if edit.ReplaceAll {
		count := strings.Count(content, edit.OldString)
		if count == 0 {
			return "", fmt.Errorf("old_string not found. Make sure it matches exactly, including whitespace and line breaks")
		}
		return strings.ReplaceAll(content, edit.OldString, edit.NewString), nil
	}

	return replaceExactOrFuzzy(content, edit.OldString, edit.NewString)
}
