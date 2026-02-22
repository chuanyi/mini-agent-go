package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Success bool   `json:"success"`
	Content string `json:"content"`            // concise text for LLM
	Details string `json:"details,omitempty"`   // rich text for UI (defaults to Content)
	Error   string `json:"error,omitempty"`
}

// ContentResult creates a successful ToolResult where Details defaults to Content.
func ContentResult(content string) *ToolResult {
	return &ToolResult{Success: true, Content: content, Details: content}
}

// ErrorResult creates a failed ToolResult where Details defaults to the error message.
func ErrorResult(errMsg string) *ToolResult {
	return &ToolResult{Success: false, Error: errMsg, Details: errMsg}
}

// Tool interface that all tools must implement
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error)
	ToSchema() map[string]interface{}
	ToOpenAISchema() map[string]interface{}
}

// BaseTool provides common functionality for all tools
type BaseTool struct {
	NameValue        string
	DescriptionValue string
	ParametersValue  map[string]interface{}
}

func (b *BaseTool) Name() string {
	return b.NameValue
}

func (b *BaseTool) Description() string {
	return b.DescriptionValue
}

func (b *BaseTool) Parameters() map[string]interface{} {
	return b.ParametersValue
}

func (b *BaseTool) ToSchema() map[string]interface{} {
	return map[string]interface{}{
		"name":        b.Name(),
		"description": b.Description(),
		"input_schema": b.Parameters(),
	}
}

func (b *BaseTool) ToOpenAISchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        b.Name(),
			"description": b.Description(),
			"parameters":  b.Parameters(),
		},
	}
}

// ToolRegistry manages all available tools
type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

func (r *ToolRegistry) Register(tool Tool) {
	r.tools[tool.Name()] = tool
}

func (r *ToolRegistry) Unregister(name string) {
	delete(r.tools, name)
}

func (r *ToolRegistry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

func (r *ToolRegistry) List() []Tool {
	tools := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

func (r *ToolRegistry) GetSchemas() []map[string]interface{} {
	schemas := make([]map[string]interface{}, 0, len(r.tools))
	for _, tool := range r.tools {
		schemas = append(schemas, tool.ToSchema())
	}
	return schemas
}

func (r *ToolRegistry) GetOpenAISchemas() []map[string]interface{} {
	schemas := make([]map[string]interface{}, 0, len(r.tools))
	for _, tool := range r.tools {
		schemas = append(schemas, tool.ToOpenAISchema())
	}
	return schemas
}

// ResolvePath resolves a file path relative to the workspace and validates
// that the result stays within the workspace boundary (path traversal protection).
func ResolvePath(workspace, filePath string) (string, error) {
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(workspace, filePath)
	}
	resolved, err := filepath.Abs(filepath.Clean(filePath))
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	wsAbs, err := filepath.Abs(filepath.Clean(workspace))
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace: %w", err)
	}
	// Allow exact match (workspace root) or child paths
	if resolved != wsAbs && !strings.HasPrefix(resolved, wsAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("access denied: path %q is outside workspace %q", filePath, workspace)
	}
	return resolved, nil
}

// --- Parameter extraction helpers ---
// These reduce boilerplate type assertions in tool Execute methods.

// RequireString extracts a required string parameter.
// Returns an error message suitable for ToolResult.Error if missing or wrong type.
func RequireString(params map[string]interface{}, key string) (string, error) {
	val, ok := params[key].(string)
	if !ok || val == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return val, nil
}

// OptionalString extracts an optional string parameter, returning defaultVal if absent.
func OptionalString(params map[string]interface{}, key, defaultVal string) string {
	if val, ok := params[key].(string); ok && val != "" {
		return val
	}
	return defaultVal
}

// OptionalFloat64 extracts an optional float64 parameter (JSON numbers decode as float64).
func OptionalFloat64(params map[string]interface{}, key string, defaultVal float64) float64 {
	if val, ok := params[key].(float64); ok {
		return val
	}
	return defaultVal
}

// OptionalBool extracts an optional bool parameter, returning defaultVal if absent.
func OptionalBool(params map[string]interface{}, key string, defaultVal bool) bool {
	if val, ok := params[key].(bool); ok {
		return val
	}
	return defaultVal
}

// OptionalInt extracts an optional int parameter.
// JSON numbers are float64, so this handles the conversion.
func OptionalInt(params map[string]interface{}, key string, defaultVal int) int {
	if val, ok := params[key].(float64); ok {
		return int(val)
	}
	return defaultVal
}

// resolveBinaryPath finds a tool binary by name (e.g. "rg", "fd").
// Search order: <exe-dir>/.agent/tools/bin/<name>, <cwd>/.agent/tools/bin/<name>, system PATH.
func resolveBinaryPath(name string) (string, error) {
	binaryName := name
	if runtime.GOOS == "windows" {
		binaryName = name + ".exe"
	}

	subPath := filepath.Join(".agent", "tools", "bin", binaryName)

	// Try executable directory first
	if exePath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), subPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Try CWD
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, subPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// Try system PATH
	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("%s not found. Looked in:\n  1. <exe-dir>/%s\n  2. <cwd>/%s\n  3. System PATH\nPlease install %s or place the binary in .agent/tools/bin/", name, subPath, subPath, name)
}
