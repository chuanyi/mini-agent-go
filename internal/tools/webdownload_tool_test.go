package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWebDownloadTool_Name(t *testing.T) {
	tool := NewWebDownloadTool(".")
	if tool.Name() != "web_download" {
		t.Errorf("Expected name 'web_download', got '%s'", tool.Name())
	}
}

func TestWebDownloadTool_Description(t *testing.T) {
	tool := NewWebDownloadTool(".")
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
}

func TestWebDownloadTool_Parameters(t *testing.T) {
	tool := NewWebDownloadTool(".")
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

	// Check if file_path parameter exists
	if _, ok := props["file_path"]; !ok {
		t.Error("Parameters should include 'file_path'")
	}

	// Check if timeout parameter exists
	if _, ok := props["timeout"]; !ok {
		t.Error("Parameters should include 'timeout'")
	}
}

func TestWebDownloadTool_Execute_MissingURL(t *testing.T) {
	tool := NewWebDownloadTool(".")
	ctx := context.Background()

	// Test with missing url
	params := map[string]interface{}{
		"file_path": "test.txt",
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

func TestWebDownloadTool_Execute_MissingFilePath(t *testing.T) {
	tool := NewWebDownloadTool(".")
	ctx := context.Background()

	// Test with missing file_path
	params := map[string]interface{}{
		"url": "https://example.com",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when file_path is missing")
	}

	if result.Error == "" {
		t.Error("Error message should not be empty when file_path is missing")
	}
}

func TestWebDownloadTool_Execute_EmptyURL(t *testing.T) {
	tool := NewWebDownloadTool(".")
	ctx := context.Background()

	// Test with empty url
	params := map[string]interface{}{
		"url":       "",
		"file_path": "test.txt",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when url is empty")
	}
}

func TestWebDownloadTool_Execute_EmptyFilePath(t *testing.T) {
	tool := NewWebDownloadTool(".")
	ctx := context.Background()

	// Test with empty file_path
	params := map[string]interface{}{
		"url":       "https://example.com",
		"file_path": "",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when file_path is empty")
	}
}

func TestWebDownloadTool_Execute_InvalidURL(t *testing.T) {
	tool := NewWebDownloadTool(".")
	ctx := context.Background()

	// Test with invalid url (missing protocol)
	params := map[string]interface{}{
		"url":       "invalid-url",
		"file_path": "test.txt",
	}
	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if result.Success {
		t.Error("Expected failure when url is invalid")
	}
}

func TestWebDownloadTool_Execute_ValidDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary directory for test
	tmpDir := t.TempDir()

	tool := NewWebDownloadTool(tmpDir)
	ctx := context.Background()

	// Test with valid parameters
	testFile := filepath.Join(tmpDir, "test.html")
	params := map[string]interface{}{
		"url":       "https://example.com",
		"file_path": "test.html",
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	if result.Content == "" {
		t.Error("Content should not be empty for successful download")
	}

	// Verify file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Downloaded file should exist")
	}

	// Clean up
	os.Remove(testFile)
}

func TestWebDownloadTool_Execute_AbsolutePath(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary directory for test
	tmpDir := t.TempDir()

	tool := NewWebDownloadTool(".")
	ctx := context.Background()

	// Test with absolute path
	testFile := filepath.Join(tmpDir, "test_absolute.html")
	params := map[string]interface{}{
		"url":       "https://example.com",
		"file_path": testFile,
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Verify file was created
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Downloaded file should exist")
	}

	// Clean up
	os.Remove(testFile)
}

func TestWebDownloadTool_Execute_CreateParentDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary directory for test
	tmpDir := t.TempDir()

	tool := NewWebDownloadTool(tmpDir)
	ctx := context.Background()

	// Test with nested path (parent dirs should be created)
	params := map[string]interface{}{
		"url":       "https://example.com",
		"file_path": "subdir/test.html",
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Verify directory and file were created
	testFile := filepath.Join(tmpDir, "subdir", "test.html")
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Downloaded file should exist in created subdirectory")
	}

	// Clean up
	os.RemoveAll(filepath.Join(tmpDir, "subdir"))
}

func TestWebDownloadTool_Execute_CustomTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary directory for test
	tmpDir := t.TempDir()

	tool := NewWebDownloadTool(tmpDir)
	ctx := context.Background()

	// Test with custom timeout
	params := map[string]interface{}{
		"url":       "https://example.com",
		"file_path": "test_timeout.html",
		"timeout":   float64(10),
	}

	result, err := tool.Execute(ctx, params)

	if err != nil {
		t.Errorf("Execute should not return error, got: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Clean up
	os.Remove(filepath.Join(tmpDir, "test_timeout.html"))
}

func TestWebDownloadTool_ToSchema(t *testing.T) {
	tool := NewWebDownloadTool(".")
	schema := tool.ToSchema()

	if schema == nil {
		t.Fatal("Schema should not be nil")
	}

	if schema["name"] != "web_download" {
		t.Errorf("Schema name should be 'web_download', got '%v'", schema["name"])
	}

	if schema["description"] == nil || schema["description"] == "" {
		t.Error("Schema should have a description")
	}

	if schema["input_schema"] == nil {
		t.Error("Schema should have input_schema")
	}
}

func TestWebDownloadTool_ToOpenAISchema(t *testing.T) {
	tool := NewWebDownloadTool(".")
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

	if function["name"] != "web_download" {
		t.Errorf("Function name should be 'web_download', got '%v'", function["name"])
	}
}
