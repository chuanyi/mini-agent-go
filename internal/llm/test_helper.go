package llm

import (
	"fmt"
	"strings"

	"mini-agent-go/internal/schema"
)

// Helper functions for testing

// CreateTestMessages creates a set of test messages for testing
func CreateTestMessages() []*schema.Message {
	return []*schema.Message{
		{
			Role:    "system",
			Content: "You are a helpful assistant.",
		},
		{
			Role:    "user",
			Content: "Hello! How are you?",
		},
		{
			Role:    "assistant",
			Content: "I'm doing well, thank you!",
		},
		{
			Role:    "user",
			Content: "Can you help me with a task?",
		},
	}
}

// CreateTestTools creates a set of test tools for testing
func CreateTestTools() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"name":        "file_tool",
			"description": "File operation tool",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type": "string",
						"enum": []string{"read", "write", "edit"},
					},
					"path": map[string]interface{}{
						"type": "string",
					},
					"content": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"action", "path"},
			},
		},
		{
			"name":        "bash",
			"description": "Bash command execution tool",
			"input_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type": "string",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

// AssertResponse checks if a response meets expected criteria
func AssertResponse(t interface {
	Errorf(format string, args ...interface{})
}, response *schema.LLMResponse, expectedContent string) {
	if response == nil {
		t.Errorf("Expected response to be non-nil")
		return
	}

	if response.Content == "" {
		t.Errorf("Expected non-empty content")
	}

	if expectedContent != "" && response.Content != expectedContent {
		t.Errorf("Expected content '%s', got: '%s'", expectedContent, response.Content)
	}

	if response.Usage == nil {
		t.Errorf("Expected usage to be non-nil")
	}

	if response.Usage != nil && response.Usage.TotalTokens <= 0 {
		t.Errorf("Expected positive token count, got: %d", response.Usage.TotalTokens)
	}
}

// PrintTestSummary prints a summary of test results
func PrintTestSummary(testName string, success bool, response *schema.LLMResponse) {
	prefix := "✅"
	if !success {
		prefix = "❌"
	}

	fmt.Printf("%s %s\n", prefix, testName)

	if response != nil {
		fmt.Printf("   Content: %s\n", response.Content)
		if response.Usage != nil {
			fmt.Printf("   Tokens: %d (in: %d, out: %d)\n",
				response.Usage.TotalTokens,
				response.Usage.InputTokens,
				response.Usage.OutputTokens)
		}
		if response.FinishReason != "" {
			fmt.Printf("   Finish Reason: %s\n", response.FinishReason)
		}
	}
	fmt.Println()
}

// CreateLargeMessage creates a large message for testing token limits
func CreateLargeMessage(sizeKB int) *schema.Message {
	charCount := sizeKB * 1024
	content := strings.Repeat("This is a test message for token estimation. ", charCount/50)

	return &schema.Message{
		Role:    "user",
		Content: content,
	}
}
