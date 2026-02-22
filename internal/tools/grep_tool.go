package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	GrepDefaultLimit   = 100          // Default max matches
	GrepMaxLineLength  = 500          // Max characters per output line
	GrepMaxOutputBytes = 50 * 1024    // 50KB total output cap
	GrepDefaultTimeout = 30 * time.Second
)

// GrepTool provides file content search using ripgrep
type GrepTool struct {
	*BaseTool
	workspace string
}

// NewGrepTool creates a new grep tool
func NewGrepTool(workspace string) *GrepTool {
	return &GrepTool{
		BaseTool: &BaseTool{
			NameValue: "grep",
			DescriptionValue: `Search file contents using ripgrep. Supports regex and literal patterns.
Returns matching lines with file paths and line numbers. Use glob to filter by file type.
For large codebases, use specific paths and glob filters to narrow results.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern (regex by default, or literal with literal=true)",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory or file to search in (default: workspace root)",
					},
					"glob": map[string]interface{}{
						"type":        "string",
						"description": "File glob filter, e.g. \"*.go\", \"*.{js,ts}\"",
					},
					"ignoreCase": map[string]interface{}{
						"type":        "boolean",
						"description": "Case-insensitive search (default: false)",
					},
					"literal": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat pattern as literal string, not regex (default: false)",
					},
					"context": map[string]interface{}{
						"type":        "integer",
						"description": "Number of context lines before and after each match (default: 0)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of matches to return (default: 100)",
					},
				},
				"required": []string{"pattern"},
			},
		},
		workspace: workspace,
	}
}

// rgEvent represents a single JSON event from ripgrep --json output
type rgEvent struct {
	Type string       `json:"type"`
	Data *rgEventData `json:"data,omitempty"`
}

type rgEventData struct {
	Path       *rgText `json:"path,omitempty"`
	LineNumber int     `json:"line_number,omitempty"`
	Lines      *rgText `json:"lines,omitempty"`
}

type rgText struct {
	Text string `json:"text"`
}

// rgMatch holds a parsed match from ripgrep output
type rgMatch struct {
	FilePath   string
	LineNumber int
	LineText   string
}

func (t *GrepTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Extract parameters
	pattern, err := RequireString(params, "pattern")
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	searchPath := OptionalString(params, "path", ".")
	glob := OptionalString(params, "glob", "")
	ignoreCase := OptionalBool(params, "ignoreCase", false)
	literal := OptionalBool(params, "literal", false)
	contextLines := OptionalInt(params, "context", 0)
	limit := OptionalInt(params, "limit", GrepDefaultLimit)

	if limit <= 0 {
		limit = GrepDefaultLimit
	}

	// Resolve and validate search path
	resolvedPath, err := ResolvePath(t.workspace, searchPath)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Check path exists
	if _, err := os.Stat(resolvedPath); err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("Path not found: %s", resolvedPath)), nil
		}
		return ErrorResult(fmt.Sprintf("Error accessing path: %v", err)), nil
	}

	// Find rg binary
	rgPath, err := resolveBinaryPath("rg")
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Build rg command arguments
	args := []string{
		"--json",
		"--line-number",
		"--color=never",
		"--hidden",
	}

	if ignoreCase {
		args = append(args, "--ignore-case")
	}
	if literal {
		args = append(args, "--fixed-strings")
	}
	if glob != "" {
		args = append(args, "--glob", glob)
	}

	args = append(args, pattern, resolvedPath)

	// Execute rg with timeout
	execCtx, cancel := context.WithTimeout(ctx, GrepDefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, rgPath, args...)
	output, err := cmd.Output()

	// Handle exit codes
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			switch {
			case exitCode == 1:
				// No matches found
				return ContentResult("No matches found."), nil
			case exitCode >= 2:
				// Error (bad regex, permission issue, etc.)
				stderr := string(exitErr.Stderr)
				if stderr == "" {
					stderr = err.Error()
				}
				return ErrorResult(fmt.Sprintf("ripgrep error: %s", strings.TrimSpace(stderr))), nil
			}
		}
		if execCtx.Err() == context.DeadlineExceeded {
			return ErrorResult("grep timed out after 30 seconds"), nil
		}
		return ErrorResult(fmt.Sprintf("failed to execute ripgrep: %v", err)), nil
	}

	// Parse JSON output
	matches, err := parseRgOutput(string(output), limit)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse ripgrep output: %v", err)), nil
	}

	if len(matches) == 0 {
		return ContentResult("No matches found."), nil
	}

	// Format output
	var result string
	truncatedByLimit := false

	if contextLines > 0 {
		result, truncatedByLimit = formatMatchesWithContext(matches, contextLines, limit, t.workspace)
	} else {
		result, truncatedByLimit = formatMatches(matches, t.workspace)
	}

	// Byte-level truncation
	truncatedByBytes := false
	if len(result) > GrepMaxOutputBytes {
		result = result[:GrepMaxOutputBytes]
		// Find last newline to avoid cutting mid-line
		if lastNL := strings.LastIndex(result, "\n"); lastNL > 0 {
			result = result[:lastNL]
		}
		truncatedByBytes = true
	}

	// Append truncation notices
	if truncatedByLimit {
		result += fmt.Sprintf("\n\n[Showing first %d matches. Use a more specific pattern or path to narrow results.]", limit)
	}
	if truncatedByBytes {
		result += "\n\n[Output truncated at 50KB. Use a more specific pattern or path to narrow results.]"
	}

	// Details for UI: prepend search summary
	relPath := toRelativePath(resolvedPath, t.workspace)
	summary := fmt.Sprintf("grep %q in %s", pattern, relPath)
	if glob != "" {
		summary += fmt.Sprintf(" (glob: %s)", glob)
	}
	return &ToolResult{
		Success: true,
		Content: result,
		Details: summary + "\n" + result,
	}, nil
}

// parseRgOutput parses ripgrep --json output and returns up to limit matches
func parseRgOutput(output string, limit int) ([]rgMatch, error) {
	var matches []rgMatch
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), bufio.MaxScanTokenSize)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event rgEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // skip malformed lines
		}

		if event.Type != "match" || event.Data == nil {
			continue
		}

		filePath := ""
		if event.Data.Path != nil {
			filePath = event.Data.Path.Text
		}

		lineText := ""
		if event.Data.Lines != nil {
			lineText = strings.TrimRight(event.Data.Lines.Text, "\r\n")
		}

		matches = append(matches, rgMatch{
			FilePath:   filePath,
			LineNumber: event.Data.LineNumber,
			LineText:   lineText,
		})

		if len(matches) >= limit {
			break
		}
	}

	return matches, scanner.Err()
}

// formatMatches formats matches without context lines (simple mode)
func formatMatches(matches []rgMatch, workspace string) (string, bool) {
	var sb strings.Builder
	truncated := false

	for i, m := range matches {
		if i > 0 {
			sb.WriteByte('\n')
		}

		relPath := toRelativePath(m.FilePath, workspace)
		line := m.LineText
		if len(line) > GrepMaxLineLength {
			line = line[:GrepMaxLineLength] + "..."
		}

		sb.WriteString(fmt.Sprintf("%s:%d: %s", relPath, m.LineNumber, line))
	}

	return sb.String(), truncated
}

// formatMatchesWithContext formats matches with surrounding context lines
func formatMatchesWithContext(matches []rgMatch, contextLines, limit int, workspace string) (string, bool) {
	// Group matches by file
	type fileGroup struct {
		filePath string
		matches  []rgMatch
	}

	groupMap := make(map[string]*fileGroup)
	var groupOrder []string

	for _, m := range matches {
		if g, ok := groupMap[m.FilePath]; ok {
			g.matches = append(g.matches, m)
		} else {
			groupMap[m.FilePath] = &fileGroup{filePath: m.FilePath, matches: []rgMatch{m}}
			groupOrder = append(groupOrder, m.FilePath)
		}
	}

	// File content cache
	fileCache := make(map[string][]string)

	var sb strings.Builder
	truncated := len(matches) >= limit

	for gi, fp := range groupOrder {
		group := groupMap[fp]
		relPath := toRelativePath(fp, workspace)

		// Load file lines if not cached
		lines, ok := fileCache[fp]
		if !ok {
			var err error
			lines, err = readFileLines(fp)
			if err != nil {
				// Can't read file, fall back to match-only output
				for mi, m := range group.matches {
					if gi > 0 || mi > 0 {
						sb.WriteByte('\n')
					}
					line := m.LineText
					if len(line) > GrepMaxLineLength {
						line = line[:GrepMaxLineLength] + "..."
					}
					sb.WriteString(fmt.Sprintf("%s:%d: %s", relPath, m.LineNumber, line))
				}
				sb.WriteString(fmt.Sprintf("\n(unable to read file for context: %s)", fp))
				continue
			}
			fileCache[fp] = lines
		}

		// For each match, compute context range and emit lines
		for mi, m := range group.matches {
			if gi > 0 || mi > 0 {
				sb.WriteString("\n--\n")
			}

			startLine := m.LineNumber - contextLines
			if startLine < 1 {
				startLine = 1
			}
			endLine := m.LineNumber + contextLines
			if endLine > len(lines) {
				endLine = len(lines)
			}

			for ln := startLine; ln <= endLine; ln++ {
				if ln > startLine {
					sb.WriteByte('\n')
				}

				lineText := ""
				if ln-1 < len(lines) {
					lineText = lines[ln-1]
				}
				if len(lineText) > GrepMaxLineLength {
					lineText = lineText[:GrepMaxLineLength] + "..."
				}

				if ln == m.LineNumber {
					sb.WriteString(fmt.Sprintf("%s:%d: %s", relPath, ln, lineText))
				} else {
					sb.WriteString(fmt.Sprintf("%s-%d- %s", relPath, ln, lineText))
				}
			}
		}
	}

	return sb.String(), truncated
}

// readFileLines reads all lines from a file
func readFileLines(filePath string) ([]string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), bufio.MaxScanTokenSize)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// toRelativePath converts an absolute path to workspace-relative if possible
func toRelativePath(absPath, workspace string) string {
	if workspace == "" {
		return absPath
	}
	rel, err := filepath.Rel(workspace, absPath)
	if err != nil {
		return absPath
	}
	// Use forward slashes for consistent output
	return filepath.ToSlash(rel)
}
