package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLSTool(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "ls_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some test files and directories
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "file2.txt"), []byte("test"), 0644)

	tool := NewLSTool(tmpDir)

	// Test listing current directory
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": tmpDir,
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if !strings.Contains(result.Content, "file1.txt") {
		t.Errorf("Expected output to contain 'file1.txt', got: %s", result.Content)
	}

	if !strings.Contains(result.Content, "subdir") {
		t.Errorf("Expected output to contain 'subdir', got: %s", result.Content)
	}
}

func TestReadTool(t *testing.T) {
	// Create temp file for testing
	tmpDir, err := os.MkdirTemp("", "read_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "line 1\nline 2\nline 3\nline 4\nline 5"
	os.WriteFile(testFile, []byte(testContent), 0644)

	tool := NewReadTool(tmpDir)

	// Test reading entire file
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": testFile,
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if !strings.Contains(result.Content, "line 1") {
		t.Errorf("Expected output to contain 'line 1', got: %s", result.Content)
	}

	// Test reading with offset (1-indexed) and limit
	// offset=3 means start from line 3 ("line 3"), limit=2 means read 2 lines
	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"path":   testFile,
		"offset": float64(3),
		"limit":  float64(2),
	})

	if err != nil {
		t.Fatalf("Execute with offset failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if !strings.Contains(result.Content, "line 3") {
		t.Errorf("Expected output to contain 'line 3' with offset 3, got: %s", result.Content)
	}

	// Test reading non-existent file
	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"path": filepath.Join(tmpDir, "nonexistent.txt"),
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Errorf("Expected failure for non-existent file")
	}
}

func TestWriteTool(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "write_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tool := NewWriteTool(tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "test content"

	// Test writing new file
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":    testFile,
		"content": testContent,
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify file was created
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Expected content '%s', got '%s'", testContent, string(content))
	}

	// Test overwriting existing file
	newContent := "new content"
	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"path":    testFile,
		"content": newContent,
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify file was updated
	content, err = os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	if string(content) != newContent {
		t.Errorf("Expected content '%s', got '%s'", newContent, string(content))
	}
}

func TestEditTool(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "edit_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tool := NewEditTool(tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "hello world\nhello universe\nhello galaxy"

	// Create test file
	os.WriteFile(testFile, []byte(originalContent), 0644)

	// Test replacing content
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"path":  testFile,
		"old_string": "world",
		"new_string": "earth",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify content was changed
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expectedContent := "hello earth\nhello universe\nhello galaxy"
	if string(content) != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, string(content))
	}

	// Test replace_all
	os.WriteFile(testFile, []byte(originalContent), 0644)

	result, err = tool.Execute(context.Background(), map[string]interface{}{
		"path":   testFile,
		"old_string":  "hello",
		"new_string":  "hi",
		"replace_all": true,
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	content, err = os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expectedContent = "hi world\nhi universe\nhi galaxy"
	if string(content) != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, string(content))
	}
}

func TestMultiEditTool(t *testing.T) {
	// Create temp directory for testing
	tmpDir, err := os.MkdirTemp("", "multiedit_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tool := NewMultiEditTool(tmpDir)
	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "hello world\nfoo bar\nbaz qux"

	// Create test file
	os.WriteFile(testFile, []byte(originalContent), 0644)

	// Test multiple edits
	edits := []interface{}{
		map[string]interface{}{
			"old_string": "world",
			"new_string": "earth",
		},
		map[string]interface{}{
			"old_string": "foo",
			"new_string": "FOO",
		},
	}

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"path": testFile,
		"edits":     edits,
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Verify content was changed
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expectedContent := "hello earth\nFOO bar\nbaz qux"
	if string(content) != expectedContent {
		t.Errorf("Expected content '%s', got '%s'", expectedContent, string(content))
	}
}

func TestDiffUtils(t *testing.T) {
	before := "line 1\nline 2\nline 3"
	after := "line 1\nline 2 modified\nline 3\nline 4"

	diff, additions, removals := GenerateDiff(before, after, "test.txt")

	if additions == 0 {
		t.Errorf("Expected additions > 0, got %d", additions)
	}

	if removals == 0 {
		t.Errorf("Expected removals > 0, got %d", removals)
	}

	if !strings.Contains(diff, "test.txt") {
		t.Errorf("Expected diff to contain filename, got: %s", diff)
	}
}

func TestFsextUtils(t *testing.T) {
	// Test ToUnixLineEndings
	windowsText := "line 1\r\nline 2\r\nline 3"
	unixText, hasCRLF := ToUnixLineEndings(windowsText)
	if !hasCRLF {
		t.Error("Expected hasCRLF to be true")
	}
	if strings.Contains(unixText, "\r\n") {
		t.Error("Expected no CRLF in converted text")
	}

	// Test ToWindowsLineEndings
	unixText = "line 1\nline 2\nline 3"
	windowsText, converted := ToWindowsLineEndings(unixText)
	if !converted {
		t.Error("Expected conversion to be true")
	}
	if !strings.Contains(windowsText, "\r\n") {
		t.Error("Expected CRLF in converted text")
	}

	// Test Expand
	expanded, err := Expand("~/test")
	if err != nil {
		t.Fatalf("Expand failed: %v", err)
	}
	if strings.HasPrefix(expanded, "~") {
		t.Error("Expected tilde to be expanded")
	}
}
