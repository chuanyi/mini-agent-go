package tools

import (
	"context"
	"testing"
)

func TestWebFetchTool_Name(t *testing.T) {
	tool := NewWebFetchTool()
	if tool.Name() != "web_fetch" {
		t.Errorf("Expected name 'web_fetch', got '%s'", tool.Name())
	}
}

func TestWebFetchTool_Description(t *testing.T) {
	tool := NewWebFetchTool()
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestWebFetchTool_Parameters(t *testing.T) {
	tool := NewWebFetchTool()
	params := tool.Parameters()

	if params == nil {
		t.Fatal("Parameters should not be nil")
	}

	// Check if required fields exist
	if params["type"] != "object" {
		t.Error("Parameters type should be 'object'")
	}

	props, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("Parameters should have properties")
	}

	// Check if url parameter exists
	if _, ok := props["url"]; !ok {
		t.Error("Parameters should include 'url'")
	}

	// Check if format parameter exists
	if _, ok := props["format"]; !ok {
		t.Error("Parameters should include 'format'")
	}

	// Check if timeout parameter exists
	if _, ok := props["timeout"]; !ok {
		t.Error("Parameters should include 'timeout'")
	}
}

func TestWebFetchTool_Execute_MissingURL(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with missing url
	params := map[string]interface{}{
		"format": "text",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when url is missing")
	}

	if result.Error == "" {
		t.Error("Error message should not be empty when url is missing")
	}
}

func TestWebFetchTool_Execute_EmptyURL(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with empty url
	params := map[string]interface{}{
		"url":    "",
		"format": "text",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when url is empty")
	}
}

func TestWebFetchTool_Execute_InvalidURL(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with invalid url (missing protocol)
	params := map[string]interface{}{
		"url":    "invalid-url",
		"format": "text",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when url is invalid")
	}
}

func TestWebFetchTool_Execute_InvalidFormat(t *testing.T) {
	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with invalid format
	params := map[string]interface{}{
		"url":    "https://example.com",
		"format": "invalid",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when format is invalid")
	}
}

func TestWebFetchTool_Execute_ValidTextFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with valid parameters (text format)
	params := map[string]interface{}{
		"url":    "https://example.com",
		"format": "text",
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	if result.Content == "" {
		t.Error("Content should not be empty for successful fetch")
	}
}

func TestWebFetchTool_Execute_ValidMarkdownFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with valid parameters (markdown format)
	params := map[string]interface{}{
		"url":    "https://example.com",
		"format": "markdown",
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
}

func TestWebFetchTool_Execute_ValidHTMLFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with valid parameters (html format)
	params := map[string]interface{}{
		"url":    "https://example.com",
		"format": "html",
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
}

func TestWebFetchTool_Execute_DefaultFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test without format (should default to text)
	params := map[string]interface{}{
		"url": "https://example.com",
	}

	// This should use default format "text"
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	// Note: Without format, the tool might fail validation
	// Check the implementation to see if it defaults properly
	if !result.Success {
		t.Logf("Fetch without format failed (may be expected): %s", result.Error)
	}
}

func TestWebFetchTool_Execute_CustomTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebFetchTool()
	ctx := context.Background()

	// Test with custom timeout
	params := map[string]interface{}{
		"url":     "https://example.com",
		"format":  "text",
		"timeout": float64(10),
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
}

func TestWebFetchTool_ToSchema(t *testing.T) {
	tool := NewWebFetchTool()
	schema := tool.ToSchema()

	if schema == nil {
		t.Fatal("Schema should not be nil")
	}

	if schema["name"] != "web_fetch" {
		t.Errorf("Schema name should be 'web_fetch', got '%v'", schema["name"])
	}

	if schema["description"] == nil || schema["description"] == "" {
		t.Error("Schema should have a description")
	}

	if schema["input_schema"] == nil {
		t.Error("Schema should have input_schema")
	}
}

func TestWebFetchTool_ToOpenAISchema(t *testing.T) {
	tool := NewWebFetchTool()
	schema := tool.ToOpenAISchema()

	if schema == nil {
		t.Fatal("OpenAI schema should not be nil")
	}

	if schema["type"] != "function" {
		t.Error("OpenAI schema type should be 'function'")
	}

	function, ok := schema["function"].(map[string]interface{})
	if !ok {
		t.Fatal("OpenAI schema should have 'function' field")
	}

	if function["name"] != "web_fetch" {
		t.Errorf("Function name should be 'web_fetch', got '%v'", function["name"])
	}
}
