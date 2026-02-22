package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	ExternalToolMaxTimeout    = 600 // seconds
	ExternalToolDefaultTimeout = 60  // seconds
	ExternalToolMaxBytes      = 1048576 // 1MB
)

// ExternalToolParam defines a single parameter in an external tool YAML
type ExternalToolParam struct {
	Type        string      `yaml:"type"`
	Description string      `yaml:"description"`
	Required    bool        `yaml:"required"`
	Default     interface{} `yaml:"default,omitempty"`
	Enum        []string    `yaml:"enum,omitempty"`
}

// ExternalToolOutput defines output capture settings
type ExternalToolOutput struct {
	Capture  string `yaml:"capture"`   // stdout | stderr | both
	MaxBytes int    `yaml:"max_bytes"` // max capture bytes
}

// ExternalToolDef represents the YAML definition of an external tool
type ExternalToolDef struct {
	Name        string                       `yaml:"name"`
	Description string                       `yaml:"description"`
	Enabled     *bool                        `yaml:"enabled,omitempty"` // pointer to distinguish unset from false
	Params      map[string]*ExternalToolParam `yaml:"params,omitempty"`
	Command     string                       `yaml:"command"`
	Mode        string                       `yaml:"mode,omitempty"`    // exec | shell
	Timeout     int                          `yaml:"timeout,omitempty"` // seconds
	Output      ExternalToolOutput           `yaml:"output,omitempty"`
	WorkDir     string                       `yaml:"workdir,omitempty"`
	Env         map[string]string            `yaml:"env,omitempty"`
}

// IsEnabled returns whether the tool is enabled (defaults to true)
func (d *ExternalToolDef) IsEnabled() bool {
	if d.Enabled == nil {
		return true
	}
	return *d.Enabled
}

// ExternalTool implements the Tool interface for externally defined tools
type ExternalTool struct {
	*BaseTool
	commandTemplate string
	mode            string // "exec" | "shell"
	timeout         int    // seconds
	outputCapture   string // "stdout" | "stderr" | "both"
	maxBytes        int
	workDir         string
	env             map[string]string
	workspace       string
	toolsDir        string
	paramDefs       map[string]*ExternalToolParam
}

// NewExternalTool creates an ExternalTool from a parsed YAML definition
func NewExternalTool(def *ExternalToolDef, workspace, toolsDir string) *ExternalTool {
	// Build JSON Schema parameters from param definitions
	properties := make(map[string]interface{})
	required := make([]string, 0)

	for name, param := range def.Params {
		prop := map[string]interface{}{
			"type":        param.Type,
			"description": param.Description,
		}
		if len(param.Enum) > 0 {
			prop["enum"] = param.Enum
		}
		if param.Default != nil {
			prop["default"] = param.Default
		}
		properties[name] = prop
		if param.Required {
			required = append(required, name)
		}
	}

	parametersValue := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		parametersValue["required"] = required
	}

	// Resolve defaults
	mode := def.Mode
	if mode == "" {
		mode = "exec"
	}
	timeout := def.Timeout
	if timeout <= 0 {
		timeout = ExternalToolDefaultTimeout
	}
	if timeout > ExternalToolMaxTimeout {
		timeout = ExternalToolMaxTimeout
	}
	capture := def.Output.Capture
	if capture == "" {
		capture = "stdout"
	}
	maxBytes := def.Output.MaxBytes
	if maxBytes <= 0 {
		maxBytes = ExternalToolMaxBytes
	}

	workDir := def.WorkDir
	if workDir == "" {
		workDir = workspace
	}

	return &ExternalTool{
		BaseTool: &BaseTool{
			NameValue:        def.Name,
			DescriptionValue: def.Description,
			ParametersValue:  parametersValue,
		},
		commandTemplate: def.Command,
		mode:            mode,
		timeout:         timeout,
		outputCapture:   capture,
		maxBytes:        maxBytes,
		workDir:         workDir,
		env:             def.Env,
		workspace:       workspace,
		toolsDir:        toolsDir,
		paramDefs:       def.Params,
	}
}

// Execute runs the external command with the given parameters
func (t *ExternalTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Merge defaults into params
	mergedParams := t.mergeDefaults(params)

	// Resolve workdir with built-in variables
	workDir := t.resolveBuiltins(t.workDir, mergedParams)

	// Create context with timeout
	timeout := time.Duration(t.timeout) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute based on mode
	var stdout, stderr bytes.Buffer
	var cmd *exec.Cmd

	if t.mode == "shell" {
		// Shell mode: build full command string, pass to sh/cmd
		cmdStr, err := t.buildCommand(mergedParams)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to build command: %v", err)), nil
		}
		cmd = t.buildShellCmd(ctx, cmdStr)
	} else {
		// Exec mode: use placeholder-based argv building to avoid
		// parameter values (containing quotes etc.) breaking shellSplit
		argv, err := t.buildArgvExec(mergedParams)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to build command: %v", err)), nil
		}
		if len(argv) == 0 {
			return ErrorResult("empty command after expansion"), nil
		}
		cmd = exec.CommandContext(ctx, argv[0], argv[1:]...)
	}

	cmd.Dir = workDir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set environment variables
	if len(t.env) > 0 {
		cmd.Env = append(os.Environ(), t.buildEnvVars()...)
	}

	// Run command
	runErr := cmd.Run()

	// Determine output based on capture mode
	var output string
	switch t.outputCapture {
	case "stderr":
		output = stderr.String()
	case "both":
		output = stdout.String() + stderr.String()
	default: // "stdout"
		output = stdout.String()
	}

	// Truncate if needed
	if len(output) > t.maxBytes {
		output = output[:t.maxBytes] + "\n[truncated]"
	}

	// Check timeout
	if ctx.Err() == context.DeadlineExceeded {
		return &ToolResult{
			Success: false,
			Content: output,
			Error:   fmt.Sprintf("timeout after %ds", t.timeout),
		}, nil
	}

	// Check exit code
	if runErr != nil {
		errMsg := stderr.String()
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		return &ToolResult{
			Success: false,
			Content: output,
			Error:   errMsg,
		}, nil
	}

	if output == "" {
		output = "(no output)"
	}

	return ContentResult(output), nil
}

// mergeDefaults merges default values for params not provided by the LLM
func (t *ExternalTool) mergeDefaults(params map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{})
	for k, v := range params {
		merged[k] = v
	}
	for name, def := range t.paramDefs {
		if _, exists := merged[name]; !exists && def.Default != nil {
			merged[name] = def.Default
		}
	}
	return merged
}

// buildCommand processes the command template: optional segments, param substitution, built-in vars
func (t *ExternalTool) buildCommand(params map[string]interface{}) (string, error) {
	cmd := t.commandTemplate

	// Step 1: Evaluate optional segments [...]
	cmd = t.evaluateOptionalSegments(cmd, params)

	// Step 2: Replace built-in variables
	cmd = t.resolveBuiltins(cmd, params)

	// Step 3: Replace {param} with values
	if t.mode == "shell" {
		cmd = t.substituteParamsShell(cmd, params)
	} else {
		cmd = t.substituteParamsExec(cmd, params)
	}

	// Check for unresolved required params
	unresolvedPattern := regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	matches := unresolvedPattern.FindAllStringSubmatch(cmd, -1)
	for _, match := range matches {
		paramName := match[1]
		// Skip built-in variables (already resolved)
		if strings.HasPrefix(paramName, "_") {
			continue
		}
		if def, exists := t.paramDefs[paramName]; exists && def.Required {
			return "", fmt.Errorf("required parameter '%s' not provided", paramName)
		}
	}

	return cmd, nil
}

// evaluateOptionalSegments processes [...] segments in the command template
// A segment is kept only if ALL {param} references within it are provided
func (t *ExternalTool) evaluateOptionalSegments(cmd string, params map[string]interface{}) string {
	optionalPattern := regexp.MustCompile(`\[([^\[\]]+)\]`)

	return optionalPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		// Extract content inside brackets
		inner := match[1 : len(match)-1]

		// Find all param references in this segment
		paramPattern := regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*?)(?::raw)?\}`)
		paramRefs := paramPattern.FindAllStringSubmatch(inner, -1)

		// Check if all non-builtin params are provided
		for _, ref := range paramRefs {
			paramName := ref[1]
			if strings.HasPrefix(paramName, "_") {
				continue // built-in, always available
			}
			if _, exists := params[paramName]; !exists {
				return "" // param not provided, remove segment
			}
		}

		// All params provided, keep the content (without brackets)
		return inner
	})
}

// resolveBuiltins replaces built-in variables {_var} in the string
func (t *ExternalTool) resolveBuiltins(s string, params map[string]interface{}) string {
	builtins := map[string]string{
		"_workspace": filepath.ToSlash(t.workspace),
		"_tools_dir": filepath.ToSlash(t.toolsDir),
		"_os":        runtime.GOOS,
		"_arch":      runtime.GOARCH,
		"_separator": string(os.PathSeparator),
	}

	for key, val := range builtins {
		s = strings.ReplaceAll(s, "{"+key+"}", val)
	}
	return s
}

// substituteParamsExec replaces {param} with raw values (for exec mode)
func (t *ExternalTool) substituteParamsExec(cmd string, params map[string]interface{}) string {
	// Replace {param:raw} and {param} identically in exec mode (no shell, so no quoting needed)
	paramPattern := regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)(?::raw)?\}`)
	return paramPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		sub := paramPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		paramName := sub[1]
		if strings.HasPrefix(paramName, "_") {
			return match // built-in, already resolved
		}
		if val, exists := params[paramName]; exists {
			return fmt.Sprintf("%v", val)
		}
		return match
	})
}

// buildArgvExec builds an argv slice for exec mode using placeholder-based substitution.
// Instead of substituting param values directly into the command string (which breaks
// shellSplit when values contain quotes), it:
//  1. Replaces {param} with safe placeholders
//  2. Runs shellSplit on the placeholder-safe string (template quoting intact)
//  3. Replaces placeholders with actual values in the resulting argv
func (t *ExternalTool) buildArgvExec(params map[string]interface{}) ([]string, error) {
	cmd := t.commandTemplate

	// Step 1: Evaluate optional segments [...]
	cmd = t.evaluateOptionalSegments(cmd, params)

	// Step 2: Replace built-in variables
	cmd = t.resolveBuiltins(cmd, params)

	// Step 3: Replace {param} with unique placeholders, store mapping
	placeholders := make(map[string]string) // placeholder → actual value
	paramPattern := regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)(?::raw)?\}`)
	idx := 0
	cmd = paramPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		sub := paramPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		paramName := sub[1]
		if strings.HasPrefix(paramName, "_") {
			return match // built-in, already resolved
		}
		if val, exists := params[paramName]; exists {
			placeholder := fmt.Sprintf("__PARAM_%d__", idx)
			idx++
			placeholders[placeholder] = fmt.Sprintf("%v", val)
			return placeholder
		}
		return match
	})

	// Step 4: Check for unresolved required params
	unresolvedPattern := regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	matches := unresolvedPattern.FindAllStringSubmatch(cmd, -1)
	for _, match := range matches {
		paramName := match[1]
		if strings.HasPrefix(paramName, "_") {
			continue
		}
		if def, exists := t.paramDefs[paramName]; exists && def.Required {
			return nil, fmt.Errorf("required parameter '%s' not provided", paramName)
		}
	}

	// Step 5: shellSplit the command (placeholders contain no special chars)
	argv, err := shellSplit(cmd)
	if err != nil {
		return nil, err
	}

	// Step 6: Replace placeholders in each argv element with actual values
	for i, arg := range argv {
		for placeholder, value := range placeholders {
			if strings.Contains(arg, placeholder) {
				argv[i] = strings.ReplaceAll(argv[i], placeholder, value)
			}
		}
	}

	return argv, nil
}

// substituteParamsShell replaces {param} with shell-escaped values (for shell mode)
func (t *ExternalTool) substituteParamsShell(cmd string, params map[string]interface{}) string {
	// {param:raw} → raw injection (no quoting)
	rawPattern := regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*):raw\}`)
	cmd = rawPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		sub := rawPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		paramName := sub[1]
		if val, exists := params[paramName]; exists {
			return fmt.Sprintf("%v", val)
		}
		return match
	})

	// {param} → shell-quoted
	paramPattern := regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)\}`)
	cmd = paramPattern.ReplaceAllStringFunc(cmd, func(match string) string {
		sub := paramPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		paramName := sub[1]
		if strings.HasPrefix(paramName, "_") {
			return match
		}
		if val, exists := params[paramName]; exists {
			return shellQuote(fmt.Sprintf("%v", val))
		}
		return match
	})

	return cmd
}

// buildShellCmd creates an exec.Cmd for shell mode
func (t *ExternalTool) buildShellCmd(ctx context.Context, cmdStr string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", cmdStr)
	}
	return exec.CommandContext(ctx, "sh", "-c", cmdStr)
}

// buildEnvVars converts env map to KEY=VALUE slice
func (t *ExternalTool) buildEnvVars() []string {
	envVars := make([]string, 0, len(t.env))
	for k, v := range t.env {
		envVars = append(envVars, k+"="+v)
	}
	return envVars
}

// shellQuote quotes a string for safe shell usage
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// If string contains no special characters, return as-is
	safe := true
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '-' && c != '_' && c != '.' && c != '/' && c != ':' {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	// Use single quotes, escaping any embedded single quotes
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// shellSplit splits a command string into argv tokens, respecting quotes
func shellSplit(s string) ([]string, error) {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' && !inSingle {
			escaped = true
			continue
		}

		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if (c == ' ' || c == '\t') && !inSingle && !inDouble {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(c)
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("unterminated quote in command")
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args, nil
}

// --- ExternalToolLoader ---

// ExternalToolLoader handles discovery and loading of external tool definitions
type ExternalToolLoader struct {
	toolsDir  string
	workspace string
}

// NewExternalToolLoader creates a new external tool loader
func NewExternalToolLoader(toolsDir, workspace string) *ExternalToolLoader {
	return &ExternalToolLoader{
		toolsDir:  toolsDir,
		workspace: workspace,
	}
}

// LoadTools discovers and loads all external tool YAML files
// Returns loaded tools and any warnings (validation failures are warnings, not errors)
func (l *ExternalToolLoader) LoadTools(existingNames map[string]bool) ([]*ExternalTool, []string) {
	var loadedTools []*ExternalTool
	var warnings []string

	// Check if tools directory exists
	if _, err := os.Stat(l.toolsDir); os.IsNotExist(err) {
		return loadedTools, warnings
	}

	// Read directory entries
	entries, err := os.ReadDir(l.toolsDir)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to read tools directory: %v", err))
		return loadedTools, warnings
	}

	for _, entry := range entries {
		// Skip directories (including _disabled/)
		if entry.IsDir() {
			continue
		}

		// Skip non-YAML files
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		// Skip files starting with _
		if strings.HasPrefix(name, "_") {
			continue
		}

		filePath := filepath.Join(l.toolsDir, name)
		tool, err := l.loadToolFile(filePath, existingNames)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipping %s: %v", name, err))
			continue
		}

		if tool != nil {
			loadedTools = append(loadedTools, tool)
			existingNames[tool.Name()] = true
		}
	}

	return loadedTools, warnings
}

// loadToolFile loads and validates a single YAML tool definition
func (l *ExternalToolLoader) loadToolFile(filePath string, existingNames map[string]bool) (*ExternalTool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	var def ExternalToolDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("YAML parse failed: %w", err)
	}

	// Validate required fields
	if def.Name == "" {
		return nil, fmt.Errorf("missing required field: name")
	}
	if def.Description == "" {
		return nil, fmt.Errorf("missing required field: description")
	}
	if def.Command == "" {
		return nil, fmt.Errorf("missing required field: command")
	}

	// Check enabled
	if !def.IsEnabled() {
		return nil, nil // disabled, skip silently
	}

	// Check name conflicts with built-in tools
	if existingNames[def.Name] {
		return nil, fmt.Errorf("name '%s' conflicts with an existing tool", def.Name)
	}

	// Validate mode
	if def.Mode != "" && def.Mode != "exec" && def.Mode != "shell" {
		return nil, fmt.Errorf("invalid mode '%s' (must be 'exec' or 'shell')", def.Mode)
	}

	// Validate output capture
	if def.Output.Capture != "" && def.Output.Capture != "stdout" && def.Output.Capture != "stderr" && def.Output.Capture != "both" {
		return nil, fmt.Errorf("invalid output.capture '%s' (must be 'stdout', 'stderr', or 'both')", def.Output.Capture)
	}

	// Validate param types
	validTypes := map[string]bool{"string": true, "number": true, "integer": true, "boolean": true}
	for pname, param := range def.Params {
		if param.Type == "" {
			return nil, fmt.Errorf("param '%s' missing type", pname)
		}
		if !validTypes[param.Type] {
			return nil, fmt.Errorf("param '%s' has invalid type '%s'", pname, param.Type)
		}
	}

	absToolsDir, _ := filepath.Abs(l.toolsDir)
	tool := NewExternalTool(&def, l.workspace, absToolsDir)
	return tool, nil
}
