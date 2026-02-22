package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	testWorkspace string
)

func TestMain(m *testing.M) {
	// Setup test workspace
	tmpDir, err := os.MkdirTemp("", "bash_test_workspace")
	if err != nil {
		panic(fmt.Sprintf("Failed to create test workspace: %v", err))
	}
	testWorkspace = tmpDir

	// Run tests
	code := m.Run()

	// Cleanup
	os.RemoveAll(testWorkspace)
	os.Exit(code)
}

func TestBashTool_BasicCommand(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Test basic echo command
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo Hello World",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if !strings.Contains(result.Content, "Hello World") {
		t.Errorf("Expected output to contain 'Hello World', got: %s", result.Content)
	}
}

func TestBashTool_WorkingDirectory(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Test pwd command
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "pwd",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Working directory should be in Details (not Content)
	if !strings.Contains(result.Details, "cwd:") {
		t.Errorf("Expected Details to contain cwd, got: %s", result.Details)
	}
}

func TestBashTool_PersistentState(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Set environment variable
	result1, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "export TEST_VAR=hello",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result1.Success {
		t.Fatalf("Expected success, got error: %s", result1.Error)
	}

	// Check if the variable persists
	result2, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo $TEST_VAR",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result2.Success {
		t.Fatalf("Expected success, got error: %s", result2.Error)
	}

	if !strings.Contains(result2.Content, "hello") {
		t.Errorf("Expected environment variable to persist, got: %s", result2.Content)
	}
}

func TestBashTool_DirectoryNavigation(t *testing.T) {
	// Create a subdirectory
	subDir := "testdir_nav"
	err := os.Mkdir(filepath.Join(testWorkspace, subDir), 0755)
	if err != nil && !os.IsExist(err) {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	tool := NewBashTool(testWorkspace)

	// Test: cd and pwd in single command should work
	result1, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cd " + subDir + " && pwd",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result1.Success {
		t.Fatalf("Expected success, got error: %s", result1.Error)
	}

	if !strings.Contains(result1.Content, subDir) {
		t.Errorf("Expected current directory to contain '%s', got: %s", subDir, result1.Content)
	}

	// Verify: next Execute should reset to workspace (not persist cd)
	result2, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "pwd",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result2.Success {
		t.Fatalf("Expected success, got error: %s", result2.Error)
	}

	// Should be back at workspace root, not in subDir
	if strings.Contains(result2.Content, subDir) {
		t.Errorf("Expected to be reset to workspace root, but still in '%s': %s", subDir, result2.Content)
	}
}

func TestBashTool_ExitCode(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Test command with non-zero exit code
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "exit 42",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Errorf("Expected failure for non-zero exit code")
	}

	// Check that error message contains exit code information
	if !strings.Contains(result.Content, "Exit code 42") && !strings.Contains(result.Error, "Exit code 42") {
		t.Logf("Expected exit code 42 in output, got: %+v", result)
	}
}

func TestBashTool_Timeout(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Test command that should timeout (sleep for 5 seconds with 1 second timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, map[string]interface{}{
		"command": "sleep 5",
		"timeout": float64(1), // 1 second timeout
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Errorf("Expected command to fail due to timeout")
	}

	if !strings.Contains(result.Content, "aborted") && !strings.Contains(result.Error, "aborted") {
		t.Logf("Timeout result: %+v", result)
	}
}

func TestBashTool_EmptyCommand(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Test empty command
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Success {
		t.Errorf("Expected failure for empty command")
	}

	if !strings.Contains(result.Error, "required") {
		t.Errorf("Expected error message about required command, got: %s", result.Error)
	}
}

func TestBashTool_MultipleCommands(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Test multiple commands with &&
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo first && echo second",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	if !strings.Contains(result.Content, "first") || !strings.Contains(result.Content, "second") {
		t.Errorf("Expected output to contain both 'first' and 'second', got: %s", result.Content)
	}
}

func TestBashTool_FileCreation(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	// Create a file using echo
	result1, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo 'test content' > testfile.txt",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result1.Success {
		t.Fatalf("Expected success, got error: %s", result1.Error)
	}

	// Verify file was created
	result2, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cat testfile.txt",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result2.Success {
		t.Fatalf("Expected success, got error: %s", result2.Error)
	}

	if !strings.Contains(result2.Content, "test content") {
		t.Errorf("Expected file content 'test content', got: %s", result2.Content)
	}
}

// TestBashTool_BlockedCommands removed — YOLO mode, no command blocking.

func TestBashTool_OutputFormat(t *testing.T) {
	tool := NewBashTool(testWorkspace)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "echo test",
	})

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	// Check that Details contains working directory
	if !strings.Contains(result.Details, "cwd:") {
		t.Error("Expected Details to contain cwd")
	}

	// Check that output contains the command result
	if !strings.Contains(result.Content, "test") {
		t.Errorf("Expected output to contain 'test', got: %s", result.Content)
	}
}

func TestBashTool_LocalExecutable_WithDotSlash(t *testing.T) {
	// Create a subdirectory with an executable script
	subDir := filepath.Join(testWorkspace, "execdir")
	err := os.MkdirAll(subDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	// Create a simple shell script
	scriptPath := filepath.Join(subDir, "testscript.sh")
	scriptContent := "#!/bin/sh\necho 'Script executed successfully'\n"
	err = os.WriteFile(scriptPath, []byte(scriptContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create script: %v", err)
	}

	tool := NewBashTool(testWorkspace)

	// Test 1: Execute with ./ prefix (should succeed)
	result1, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cd execdir && ./testscript.sh",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result1.Success {
		t.Errorf("Expected success with ./ prefix, got error: %s\nContent: %s", result1.Error, result1.Content)
	}
	if !strings.Contains(result1.Content, "Script executed successfully") {
		t.Errorf("Expected script output, got: %s", result1.Content)
	}

	// Test 2: Execute without ./ prefix (should fail with "command not found")
	result2, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cd execdir && testscript.sh",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result2.Success {
		t.Errorf("Expected failure without ./ prefix, but command succeeded")
	}
	if !strings.Contains(result2.Content, "not found") && !strings.Contains(result2.Error, "not found") {
		t.Errorf("Expected 'not found' error, got: %s", result2.Content)
	}
}

func TestBashTool_PathSeparators(t *testing.T) {
	// Create nested directories
	nestedDir := filepath.Join(testWorkspace, "level1", "level2")
	err := os.MkdirAll(nestedDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create nested dir: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(nestedDir, "testfile.txt")
	err = os.WriteFile(testFile, []byte("nested content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tool := NewBashTool(testWorkspace)

	// Test 1: Using forward slashes (should succeed)
	result1, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cat level1/level2/testfile.txt",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result1.Success {
		t.Errorf("Expected success with forward slashes, got error: %s", result1.Error)
	}
	if !strings.Contains(result1.Content, "nested content") {
		t.Errorf("Expected file content, got: %s", result1.Content)
	}

	// Test 2: Using backslashes (should fail on POSIX shell)
	// Note: Backslash is escape character in POSIX shell
	result2, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cat level1\\level2\\testfile.txt",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	// This should fail because backslashes are escape characters
	if result2.Success {
		t.Logf("Note: Backslash command unexpectedly succeeded. This may vary by shell implementation.")
	}
}

func TestBashTool_CdAndExecute(t *testing.T) {
	// This is the main test case for the reported issue:
	// "cd xxxx && rodagent -h" type commands
	//
	// IMPORTANT: Use a fresh tool instance to avoid state pollution from other tests
	tool := NewBashTool(testWorkspace)

	// Setup: Create a test directory structure
	result1, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "mkdir -p cdtest && echo 'test content' > cdtest/data.txt",
	})
	if err != nil || !result1.Success {
		t.Fatalf("Setup failed: %v, %s", err, result1.Error)
	}

	// Test 1: cd && command combination in single call
	result2, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cd cdtest && cat data.txt",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result2.Success {
		t.Errorf("Expected 'cd && cat' to succeed, got error: %s\nContent: %s", result2.Error, result2.Content)
	}
	if !strings.Contains(result2.Content, "test content") {
		t.Errorf("Expected file content, got: %s", result2.Content)
	}
	// Verify working directory changed (cwd is in Details now)
	if !strings.Contains(result2.Details, "cdtest") {
		t.Errorf("Expected Details cwd to contain 'cdtest', got: %s", result2.Details)
	}

	// Test 2: Verify the shell resets to workspace between calls
	// After the previous cd, we should be back at workspace root (not in cdtest)
	result3, err := tool.Execute(context.Background(), map[string]interface{}{
		"command": "cat cdtest/data.txt", // Need full path since we're back at workspace root
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result3.Success {
		t.Errorf("Expected cat to work with relative path from workspace, got: %s", result3.Error)
	}
	if !strings.Contains(result3.Content, "test content") {
		t.Errorf("Expected file content, got: %s", result3.Content)
	}

	t.Logf("✓ Test passed: 'cd xxx && command' pattern works correctly")
	t.Logf("✓ Shell resets to workspace between Execute calls")
}
