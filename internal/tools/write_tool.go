package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// WriteTool provides file writing functionality
type WriteTool struct {
	*BaseTool
	workspace string
}

// NewWriteTool creates a new file writing tool
func NewWriteTool(workspace string) *WriteTool {
	return &WriteTool{
		BaseTool: &BaseTool{
			NameValue: "write",
			DescriptionValue: `Write content to a file. Creates the file if it doesn't exist, overwrites if it does.
Automatically creates parent directories. Use this only for new files or complete rewrites.
For precise changes to existing files, prefer the edit tool.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to write (relative or absolute)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to write to the file",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		workspace: workspace,
	}
}

func (t *WriteTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	pathVal, ok := params["path"].(string)
	if !ok || pathVal == "" {
		return ErrorResult("path is required"), nil
	}

	contentVal, ok := params["content"].(string)
	if !ok {
		return ErrorResult("content is required"), nil
	}

	filePath, err := ResolvePath(t.workspace, pathVal)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Check if path is a directory
	if info, err := os.Stat(filePath); err == nil && info.IsDir() {
		return ErrorResult(fmt.Sprintf("Path is a directory, not a file: %s", filePath)), nil
	}

	// Create parent directories
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ErrorResult(fmt.Sprintf("error creating directory: %v", err)), nil
	}

	// Read old content for diff (if file exists)
	oldContent := ""
	if oldBytes, err := os.ReadFile(filePath); err == nil {
		if utf8.Valid(oldBytes) {
			oldContent = string(oldBytes)
		}
	}

	// Generate diff for UI
	relPath := strings.TrimPrefix(filePath, t.workspace+string(os.PathSeparator))
	diffText, additions, removals := GenerateDiff(oldContent, contentVal, relPath)

	// Write file
	if err := os.WriteFile(filePath, []byte(contentVal), 0o644); err != nil {
		return ErrorResult(fmt.Sprintf("error writing file: %v", err)), nil
	}

	// Content for LLM: concise confirmation
	llmContent := fmt.Sprintf("Successfully wrote %d bytes to %s.", len(contentVal), filePath)

	// Details for UI: include diff
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
