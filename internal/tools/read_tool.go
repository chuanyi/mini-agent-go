package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	DefaultReadLimit = 500       // Default max lines to read
	MaxReadBytes     = 30 * 1024 // 30KB max content size
	MaxLineLength    = 2000      // Maximum line length before truncation
)

// ReadTool provides file reading functionality
type ReadTool struct {
	*BaseTool
	workspace string
}

// NewReadTool creates a new file reading tool
func NewReadTool(workspace string) *ReadTool {
	return &ReadTool{
		BaseTool: &BaseTool{
			NameValue: "read",
			DescriptionValue: `Read the contents of a file. For text files only (source code, config, logs, etc.) — not binary files.
Output is truncated to 500 lines or 30KB (whichever is hit first). Use offset/limit for large files.
When you need the full file, continue with offset until complete.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read (relative or absolute)",
					},
					"offset": map[string]interface{}{
						"type":        "integer",
						"description": "Line number to start reading from (1-indexed)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of lines to read",
					},
				},
				"required": []string{"path"},
			},
		},
		workspace: workspace,
	}
}

func (t *ReadTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Get file path
	pathVal, ok := params["path"].(string)
	if !ok || pathVal == "" {
		return ErrorResult("path is required"), nil
	}

	filePath, err := ResolvePath(t.workspace, pathVal)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Check if file exists
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			suggestions := t.findSimilarFiles(filePath)
			if len(suggestions) > 0 {
				return ErrorResult(fmt.Sprintf("File not found: %s\n\nDid you mean one of these?\n%s", filePath, strings.Join(suggestions, "\n"))), nil
			}
			return ErrorResult(fmt.Sprintf("File not found: %s", filePath)), nil
		}
		return ErrorResult(fmt.Sprintf("error accessing file: %v", err)), nil
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		return ErrorResult(fmt.Sprintf("Path is a directory, not a file: %s\nUse ls to list directory contents.", filePath)), nil
	}

	// Check if it's an image file
	if isImage, imageType := isImageFile(filePath); isImage {
		return ErrorResult(fmt.Sprintf("This is an image file (%s). Image reading is not supported.", imageType)), nil
	}

	// Get offset (1-indexed) and limit
	offset := OptionalInt(params, "offset", 1)
	if offset < 1 {
		offset = 1
	}
	limit := OptionalInt(params, "limit", DefaultReadLimit)
	if limit <= 0 {
		limit = DefaultReadLimit
	}

	// Read file with dual truncation (lines + bytes)
	content, startLine, endLine, totalLines, truncated, err := readTextFileWithLimits(filePath, offset, limit)
	if err != nil {
		return ErrorResult(fmt.Sprintf("error reading file: %v", err)), nil
	}

	// Check if content is valid UTF-8
	if !utf8.ValidString(content) {
		return ErrorResult("File appears to be binary (not valid UTF-8)"), nil
	}

	// Empty file
	if totalLines == 0 {
		return &ToolResult{
			Success: true,
			Content: "(empty file)",
			Details: fmt.Sprintf("%s (empty file)", filePath),
		}, nil
	}

	// Format content with line numbers
	numbered := addLineNumbers(content, startLine)

	// Build truncation message
	var truncMsg string
	if truncated {
		truncMsg = fmt.Sprintf("\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startLine, endLine, totalLines, endLine+1)
	}

	// Content for LLM: concise numbered text + truncation indicator
	llmContent := numbered + truncMsg

	// Details for UI: include file path and range header
	uiDetails := fmt.Sprintf("%s (lines %d-%d of %d)\n%s%s", filePath, startLine, endLine, totalLines, numbered, truncMsg)

	return &ToolResult{
		Success: true,
		Content: llmContent,
		Details: uiDetails,
	}, nil
}

// readTextFileWithLimits reads a file with dual truncation: max lines AND max bytes.
// offset is 1-indexed. Returns content, startLine, endLine, totalLines, truncated, error.
func readTextFileWithLimits(filePath string, offset, limit int) (string, int, int, int, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, 0, 0, false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), bufio.MaxScanTokenSize)

	lineNum := 0
	truncated := false
	byteCount := 0

	// Skip to offset (1-indexed: skip offset-1 lines)
	for lineNum < offset-1 && scanner.Scan() {
		lineNum++
	}

	// Read lines up to limit, tracking bytes
	var lines []string
	startLine := offset

	for scanner.Scan() {
		lineNum++
		lineText := scanner.Text()
		if len(lineText) > MaxLineLength {
			lineText = lineText[:MaxLineLength] + "..."
		}

		lineBytes := len(lineText) + 1 // +1 for newline
		if byteCount+lineBytes > MaxReadBytes && len(lines) > 0 {
			truncated = true
			break
		}

		byteCount += lineBytes
		lines = append(lines, lineText)

		if len(lines) >= limit {
			truncated = true
			break
		}
	}

	endLine := startLine + len(lines) - 1
	if len(lines) == 0 {
		endLine = startLine - 1
	}

	// Count remaining lines for total
	totalLines := lineNum
	for scanner.Scan() {
		totalLines++
	}

	if err := scanner.Err(); err != nil {
		return "", 0, 0, 0, false, err
	}

	// Not truncated if we reached the end of file naturally
	if !truncated && totalLines <= endLine {
		truncated = false
	}
	// But if there are lines beyond what we read, it IS truncated
	if totalLines > endLine && endLine >= startLine {
		truncated = true
	}

	return strings.Join(lines, "\n"), startLine, endLine, totalLines, truncated, nil
}

func (t *ReadTool) findSimilarFiles(filePath string) []string {
	dir := filepath.Dir(filePath)
	base := filepath.Base(filePath)

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var suggestions []string
	for _, entry := range dirEntries {
		if strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(base)) ||
			strings.Contains(strings.ToLower(base), strings.ToLower(entry.Name())) {
			suggestions = append(suggestions, filepath.Join(dir, entry.Name()))
			if len(suggestions) >= 3 {
				break
			}
		}
	}

	return suggestions
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")
	var result []string

	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		lineNum := i + startLine
		numStr := fmt.Sprintf("%d", lineNum)

		if len(numStr) >= 6 {
			result = append(result, fmt.Sprintf("%s|%s", numStr, line))
		} else {
			paddedNum := fmt.Sprintf("%6s", numStr)
			result = append(result, fmt.Sprintf("%s|%s", paddedNum, line))
		}
	}

	return strings.Join(result, "\n")
}

func isImageFile(filePath string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return true, "JPEG"
	case ".png":
		return true, "PNG"
	case ".gif":
		return true, "GIF"
	case ".bmp":
		return true, "BMP"
	case ".svg":
		return true, "SVG"
	case ".webp":
		return true, "WebP"
	default:
		return false, ""
	}
}
