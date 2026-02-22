package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mini-agent-go/internal/shell"
)

const (
	DefaultBashTimeout = 60      // seconds
	MaxBashTimeout     = 600     // seconds
	MaxOutputLines     = 500     // tail truncation: max lines
	MaxOutputBytes     = 30 * 1024 // tail truncation: 30KB
	BashNoOutput       = "(no output)"
)

// BashTool executes bash commands using a persistent shell
type BashTool struct {
	*BaseTool
	workspace string
}

// NewBashTool creates a new bash tool instance
func NewBashTool(workspace string) *BashTool {
	// Initialize persistent shell
	shell.GetPersistentShell(workspace)

	return &BashTool{
		BaseTool: &BaseTool{
			NameValue: "bash",
			DescriptionValue: `Execute a bash command in the current working directory. Returns stdout and stderr.
Output is truncated to last 500 lines or 30KB (whichever is hit first).
If truncated, full output is saved to a temp file. Optionally provide a timeout in seconds.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Bash command to execute",
					},
					"timeout": map[string]interface{}{
						"type":        "number",
						"description": "Timeout in seconds (default 60, max 600)",
					},
				},
				"required": []string{"command"},
			},
		},
		workspace: workspace,
	}
}

func (t *BashTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	command, ok := params["command"].(string)
	if !ok || command == "" {
		return ErrorResult("command parameter is required"), nil
	}

	// Get timeout in seconds
	timeout := DefaultBashTimeout
	if timeoutVal, ok := params["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}
	if timeout > MaxBashTimeout {
		timeout = MaxBashTimeout
	} else if timeout <= 0 {
		timeout = DefaultBashTimeout
	}

	// Create context with timeout
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Execute command
	persistentShell := shell.GetPersistentShell(t.workspace)
	persistentShell.ResetToWorkspace()
	stdout, stderr, err := persistentShell.Exec(ctx, command)

	currentWorkingDir := persistentShell.GetWorkingDir()
	interrupted := shell.IsInterrupt(err)
	exitCode := shell.ExitCode(err)

	// Handle internal execution errors (not command failures)
	if exitCode == 0 && !interrupted && err != nil {
		return ErrorResult(fmt.Sprintf("error executing command: %v", err)), nil
	}

	// Combine stdout + stderr for output
	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}

	// Add error/status info
	if interrupted {
		output += "\nCommand was aborted (timeout or cancelled)"
	} else if exitCode != 0 {
		output += fmt.Sprintf("\nExit code %d", exitCode)
	}

	// Tail truncation with temp file for full output
	output, tempFile := tailTruncate(output)

	if output == "" {
		output = BashNoOutput
	}

	// Content for LLM: output + truncation note
	llmContent := output
	if tempFile != "" {
		llmContent += fmt.Sprintf("\n[Output truncated. Full output saved to: %s]", tempFile)
	}

	// Details for UI: output + cwd
	uiDetails := output
	if tempFile != "" {
		uiDetails += fmt.Sprintf("\n[Full output: %s]", tempFile)
	}
	uiDetails += fmt.Sprintf("\n\ncwd: %s", currentWorkingDir)

	if exitCode == 0 && !interrupted {
		return &ToolResult{Success: true, Content: llmContent, Details: uiDetails}, nil
	}
	return &ToolResult{Success: false, Content: llmContent, Details: uiDetails, Error: llmContent}, nil
}

// tailTruncate keeps the last MaxOutputLines or MaxOutputBytes of output.
// If truncated, saves full output to a temp file and returns its path.
func tailTruncate(output string) (string, string) {
	lines := strings.Split(output, "\n")
	totalBytes := len(output)

	// Check if truncation is needed
	if len(lines) <= MaxOutputLines && totalBytes <= MaxOutputBytes {
		return output, ""
	}

	// Save full output to temp file
	tempFile := saveTempOutput(output)

	// Take last N lines, respecting byte limit
	truncLines := lines
	if len(truncLines) > MaxOutputLines {
		truncLines = truncLines[len(truncLines)-MaxOutputLines:]
	}

	// Further trim if still over byte limit
	result := strings.Join(truncLines, "\n")
	for len(result) > MaxOutputBytes && len(truncLines) > 1 {
		truncLines = truncLines[1:]
		result = strings.Join(truncLines, "\n")
	}

	droppedLines := len(lines) - len(truncLines)
	return fmt.Sprintf("[%d lines truncated]\n%s", droppedLines, result), tempFile
}

// saveTempOutput writes content to a temp file and returns the path.
func saveTempOutput(content string) string {
	tmpFile, err := os.CreateTemp("", "bash-output-*.txt")
	if err != nil {
		return ""
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return ""
	}

	// Convert to forward slashes for cross-platform display
	return filepath.ToSlash(tmpFile.Name())
}
