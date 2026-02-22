package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MessageLite is a lightweight representation of a message for logging
type MessageLite struct {
	Role      string      `json:"role"`
	Content   string      `json:"content"`
	ToolCalls interface{} `json:"tool_calls,omitempty"`
}

// ToolLite is a lightweight representation of a tool for logging
type ToolLite struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// UsageLite is a lightweight representation of usage for logging
type UsageLite struct {
	InputTokens     int `json:"input_tokens,omitempty"`
	OutputTokens    int `json:"output_tokens,omitempty"`
	TotalTokens     int `json:"total_tokens,omitempty"`
	PromptTokens    int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
}

// LLMResponseLite is a lightweight representation of an LLM response for logging
type LLMResponseLite struct {
	Content      string      `json:"content"`
	Thinking     string      `json:"thinking,omitempty"`
	ToolCalls    interface{} `json:"tool_calls,omitempty"`
	FinishReason string      `json:"finish_reason,omitempty"`
	Usage        *UsageLite  `json:"usage,omitempty"`
}

// ToolResultLite is a lightweight representation of a tool result for logging
type ToolResultLite struct {
	Success bool   `json:"success"`
	Content string `json:"content"`
	Details string `json:"details,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Logger represents the agent logger
type Logger struct {
	logFilePath string
	logFile     *os.File
	logDir      string // base directory for log files
}

// NewLogger creates a new Logger instance
func NewLogger() *Logger {
	return &Logger{
		logDir: "logs", // default fallback
	}
}

// SetLogDir sets the base directory for log files
func (l *Logger) SetLogDir(dir string) {
	l.logDir = dir
}

// StartNewRun starts a new logging run
func (l *Logger) StartNewRun() {
	// Create logs directory
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		// Failed to create log directory - TUI mode should not print
		return
	}

	// Create log file with timestamp
	timestamp := time.Now().Format("20060102_150405")
	l.logFilePath = filepath.Join(l.logDir, fmt.Sprintf("agent_%s.log", timestamp))

	var err error
	l.logFile, err = os.Create(l.logFilePath)
	if err != nil {
		// Failed to create log file - TUI mode should not print
	}
}

// LogRequest logs an LLM request
func (l *Logger) LogRequest(messages []MessageLite, tools []ToolLite) {
	if l.logFile == nil {
		return
	}

	logEntry := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"type":      "request",
		"messages":  messages,
		"tools":     tools,
	}

	l.writeLog(logEntry)
}

// LogResponse logs an LLM response
func (l *Logger) LogResponse(response *LLMResponseLite) {
	if l.logFile == nil {
		return
	}

	logEntry := map[string]interface{}{
		"timestamp":     time.Now().Format(time.RFC3339),
		"type":          "response",
		"content":       response.Content,
		"thinking":      response.Thinking,
		"tool_calls":    response.ToolCalls,
		"finish_reason": response.FinishReason,
		"usage":         response.Usage,
	}

	l.writeLog(logEntry)
}

// LogToolResult logs a tool execution result
func (l *Logger) LogToolResult(toolName string, arguments map[string]interface{}, result *ToolResultLite) {
	if l.logFile == nil {
		return
	}

	logEntry := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"type":      "tool_result",
		"tool":      toolName,
		"arguments": arguments,
		"success":   result.Success,
		"content":   result.Content,
		"error":     result.Error,
	}

	l.writeLog(logEntry)
}

// writeLog writes a log entry to the file
func (l *Logger) writeLog(entry map[string]interface{}) {
	if l.logFile == nil {
		return
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	fmt.Fprintf(l.logFile, "%s\n", string(data))
	l.logFile.Sync()
}

// GetLogFilePath returns the path of the log file
func (l *Logger) GetLogFilePath() string {
	return l.logFilePath
}

// Close closes the log file
func (l *Logger) Close() {
	if l.logFile != nil {
		l.logFile.Close()
		l.logFile = nil
	}
}
