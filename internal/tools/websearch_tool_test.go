package tools

import (
	"context"
	"testing"
)

func TestWebSearchTool_Name(t *testing.T) {
	tool := NewWebSearchTool()
	if tool.Name() != "web_search" {
		t.Errorf("Expected name 'web_search', got '%s'", tool.Name())
	}
}

func TestWebSearchTool_Description(t *testing.T) {
	tool := NewWebSearchTool()
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestWebSearchTool_Parameters(t *testing.T) {
	tool := NewWebSearchTool()
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

	// Check if query parameter exists
	if _, ok := props["query"]; !ok {
		t.Error("Parameters should include 'query'")
	}

	// Check if max_results parameter exists
	if _, ok := props["max_results"]; !ok {
		t.Error("Parameters should include 'max_results'")
	}
}

func TestWebSearchTool_Execute_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool()
	ctx := context.Background()

	// Test with missing query
	params := map[string]interface{}{}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when query is missing")
	}

	if result.Error == "" {
		t.Error("Error message should not be empty when query is missing")
	}
}

func TestWebSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := NewWebSearchTool()
	ctx := context.Background()

	// Test with empty query
	params := map[string]interface{}{
		"query": "",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when query is empty")
	}
}

func TestWebSearchTool_Execute_ValidQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebSearchTool()
	ctx := context.Background()

	// Test with valid query
	params := map[string]interface{}{
		"query":       "golang programming",
		"max_results": float64(3),
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	// Note: This test may fail if DuckDuckGo is not accessible or changes their HTML
	// In that case, we should mock the HTTP client
	if !result.Success {
		t.Logf("Search failed (may be expected): %s", result.Error)
	}
}

func TestWebSearchTool_Execute_MaxResultsDefault(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tool := NewWebSearchTool()
	ctx := context.Background()

	// Test without max_results (should use default)
	params := map[string]interface{}{
		"query": "golang",
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Logf("Search failed (may be expected): %s", result.Error)
	}
}

func TestWebSearchTool_Execute_MaxResultsValidation(t *testing.T) {
	tool := NewWebSearchTool()
	ctx := context.Background()

	tests := []struct {
		name       string
		maxResults float64
		// We can't easily check the actual number without mocking
		// but we can verify it doesn't crash
	}{
		{"Negative", float64(-1)},
		{"Zero", float64(0)},
		{"Valid", float64(5)},
		{"Over limit", float64(100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]interface{}{
				"query":       "test",
				"max_results": tt.maxResults,
			}

			// Should not crash
			_, err := tool.Execute(ctx, params)
			if err != nil {
				t.Errorf("Execute should not return error, got: %v", err)
			}
		})
	}
}

func TestWebSearchTool_ToSchema(t *testing.T) {
	tool := NewWebSearchTool()
	schema := tool.ToSchema()

	if schema == nil {
		t.Fatal("Schema should not be nil")
	}

	if schema["name"] != "web_search" {
		t.Errorf("Schema name should be 'web_search', got '%v'", schema["name"])
	}

	if schema["description"] == nil || schema["description"] == "" {
		t.Error("Schema should have a description")
	}

	if schema["input_schema"] == nil {
		t.Error("Schema should have input_schema")
	}
}

func TestWebSearchTool_ToOpenAISchema(t *testing.T) {
	tool := NewWebSearchTool()
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

	if function["name"] != "web_search" {
		t.Errorf("Function name should be 'web_search', got '%v'", function["name"])
	}
}
