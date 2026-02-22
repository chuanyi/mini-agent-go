package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WebDownloadTool downloads files from URLs and saves them locally
type WebDownloadTool struct {
	*BaseTool
	workspace string
}

func NewWebDownloadTool(workspace string) *WebDownloadTool {
	return &WebDownloadTool{
		BaseTool: &BaseTool{
			NameValue: "web_download",
			DescriptionValue: `Download a file from URL and save to local path. Creates parent directories automatically.
Max 100MB. HTTP/HTTPS only. Overwrites existing files.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to download from",
					},
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "The local file path where the downloaded content should be saved (absolute or relative to workspace)",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Optional timeout in seconds (default: 300, max: 600)",
					},
				},
				"required": []string{"url", "file_path"},
			},
		},
		workspace: workspace,
	}
}

func (t *WebDownloadTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Extract URL parameter
	targetURL, ok := params["url"].(string)
	if !ok || targetURL == "" {
		return ErrorResult("url parameter is required"), nil
	}

	// Extract file_path parameter
	filePath, ok := params["file_path"].(string)
	if !ok || filePath == "" {
		return ErrorResult("file_path parameter is required"), nil
	}

	// Extract timeout parameter (default: 300 seconds)
	timeout := 300
	if timeoutVal, ok := params["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	// Perform download
	absPath, bytesWritten, contentType, err := t.download(ctx, targetURL, filePath, timeout)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Download failed: %v", err)), nil
	}

	// Format result
	message := fmt.Sprintf("Successfully downloaded %d bytes to %s", bytesWritten, absPath)
	if contentType != "" {
		message += fmt.Sprintf("\nContent-Type: %s", contentType)
	}

	return ContentResult(message), nil
}

// download retrieves content from a URL and saves it to a file
func (t *WebDownloadTool) download(ctx context.Context, targetURL, filePath string, timeoutSeconds int) (string, int64, string, error) {
	// Validate URL
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return "", 0, "", fmt.Errorf("URL must start with http:// or https://")
	}

	// Resolve path and validate within workspace
	absPath, err := ResolvePath(t.workspace, filePath)
	if err != nil {
		return "", 0, "", err
	}

	// Create parent directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", 0, "", fmt.Errorf("failed to create parent directories: %w", err)
	}

	// Set timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300 // Default 5 minutes
	}
	if timeoutSeconds > 600 {
		timeoutSeconds = 600 // Max 10 minutes
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Mini-Agent-Go/1.0)")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to download from URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, "", fmt.Errorf("download failed with status code: %d", resp.StatusCode)
	}

	// Check content length if available
	maxSize := int64(100 * 1024 * 1024) // 100MB
	if resp.ContentLength > maxSize {
		return "", 0, "", fmt.Errorf("file too large: %d bytes (max %d bytes)", resp.ContentLength, maxSize)
	}

	// Create the output file
	outFile, err := os.Create(absPath)
	if err != nil {
		return "", 0, "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer outFile.Close()

	// Copy data with size limit
	limitedReader := io.LimitReader(resp.Body, maxSize)
	bytesWritten, err := io.Copy(outFile, limitedReader)
	if err != nil {
		// Clean up the file on error
		os.Remove(absPath)
		return "", 0, "", fmt.Errorf("failed to write file: %w", err)
	}

	// Check if we hit the size limit
	if bytesWritten == maxSize {
		// Check if there's more data
		buf := make([]byte, 1)
		n, _ := resp.Body.Read(buf)
		if n > 0 {
			// There's more data, clean up and return error
			os.Remove(absPath)
			return "", 0, "", fmt.Errorf("file too large: exceeded %d bytes limit", maxSize)
		}
	}

	contentType := resp.Header.Get("Content-Type")

	return absPath, bytesWritten, contentType, nil
}
