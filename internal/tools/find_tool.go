package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	FindDefaultLimit   = 1000          // Default max results (matches pi-mono)
	FindMaxOutputBytes = 50 * 1024     // 50KB total output cap
	FindDefaultTimeout = 30 * time.Second
)

// FindTool provides file search by glob pattern using fd
type FindTool struct {
	*BaseTool
	workspace string
}

// NewFindTool creates a new find tool
func NewFindTool(workspace string) *FindTool {
	return &FindTool{
		BaseTool: &BaseTool{
			NameValue: "find",
			DescriptionValue: `Search for files and directories by glob pattern using fd.
Returns matching paths relative to workspace. Respects .gitignore by default.
Use this to locate files by name or extension (e.g. "*.go", "**/*.json").`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern to match, e.g. \"*.go\", \"**/*.json\", \"src/**/*.spec.ts\"",
					},
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Directory to search in (default: workspace root)",
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return (default: 1000)",
					},
				},
				"required": []string{"pattern"},
			},
		},
		workspace: workspace,
	}
}

func (t *FindTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Extract parameters
	pattern, err := RequireString(params, "pattern")
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	searchPath := OptionalString(params, "path", ".")
	limit := OptionalInt(params, "limit", FindDefaultLimit)

	if limit <= 0 {
		limit = FindDefaultLimit
	}

	// Resolve and validate search path
	resolvedPath, err := ResolvePath(t.workspace, searchPath)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Check path exists
	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("Path not found: %s", searchPath)), nil
		}
		return ErrorResult(fmt.Sprintf("Error accessing path: %v", err)), nil
	}
	if !info.IsDir() {
		return ErrorResult(fmt.Sprintf("Path is not a directory: %s", searchPath)), nil
	}

	// Find fd binary
	fdPath, err := resolveBinaryPath("fd")
	if err != nil {
		return ErrorResult(err.Error()), nil
	}

	// Build fd command arguments
	args := []string{
		"--glob",
		"--color=never",
		"--hidden",
		"--max-results", fmt.Sprintf("%d", limit),
		pattern,
		resolvedPath,
	}

	// Execute fd with timeout
	execCtx, cancel := context.WithTimeout(ctx, FindDefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, fdPath, args...)
	output, err := cmd.Output()

	// Handle exit codes
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			switch {
			case exitCode == 1:
				// No matches found
				return ContentResult("No files found matching pattern."), nil
			case exitCode >= 2:
				// Error (bad glob syntax, permission issue, etc.)
				stderr := string(exitErr.Stderr)
				if stderr == "" {
					stderr = err.Error()
				}
				return ErrorResult(fmt.Sprintf("fd error: %s", strings.TrimSpace(stderr))), nil
			}
		}
		if execCtx.Err() == context.DeadlineExceeded {
			return ErrorResult("find timed out after 30 seconds"), nil
		}
		return ErrorResult(fmt.Sprintf("failed to execute fd: %v", err)), nil
	}

	// Parse output: one path per line
	rawOutput := string(output)
	if strings.TrimSpace(rawOutput) == "" {
		return ContentResult("No files found matching pattern."), nil
	}

	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(rawOutput))
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")
		if line == "" {
			continue
		}

		// Convert to workspace-relative path with forward slashes
		relPath := toRelativePath(line, t.workspace)

		// Preserve trailing separator for directories
		if strings.HasSuffix(line, string(os.PathSeparator)) || strings.HasSuffix(line, "/") {
			if !strings.HasSuffix(relPath, "/") {
				relPath += "/"
			}
		}

		paths = append(paths, relPath)
	}

	if len(paths) == 0 {
		return ContentResult("No files found matching pattern."), nil
	}

	result := strings.Join(paths, "\n")

	// Byte-level truncation
	truncatedByBytes := false
	if len(result) > FindMaxOutputBytes {
		result = result[:FindMaxOutputBytes]
		// Find last newline to avoid cutting mid-line
		if lastNL := strings.LastIndex(result, "\n"); lastNL > 0 {
			result = result[:lastNL]
		}
		truncatedByBytes = true
	}

	// Append truncation notices
	if len(paths) >= limit {
		result += fmt.Sprintf("\n\n[Showing first %d results. Use a more specific pattern or path to narrow results.]", limit)
	}
	if truncatedByBytes {
		result += "\n\n[Output truncated at 50KB. Use a more specific pattern or path to narrow results.]"
	}

	// Details for UI: prepend search summary
	relPath := toRelativePath(resolvedPath, t.workspace)
	summary := fmt.Sprintf("find %q in %s", pattern, relPath)
	return &ToolResult{
		Success: true,
		Content: result,
		Details: summary + "\n" + result,
	}, nil
}

