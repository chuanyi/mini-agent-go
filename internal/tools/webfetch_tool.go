package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
)

// WebFetchTool fetches content from URLs and converts it to various formats
type WebFetchTool struct {
	*BaseTool
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		BaseTool: &BaseTool{
			NameValue: "web_fetch",
			DescriptionValue: `Fetch content from a URL. Returns text, markdown, or HTML format.
Max 5MB response, truncated at 500KB. HTTP/HTTPS only.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch content from",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"text", "markdown", "html"},
						"description": "Output format: 'text' (plain text), 'markdown' (markdown), or 'html' (HTML body)",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Optional timeout in seconds (default: 30, max: 120)",
					},
				},
				"required": []string{"url", "format"},
			},
		},
	}
}

func (t *WebFetchTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Extract URL parameter
	targetURL, ok := params["url"].(string)
	if !ok || targetURL == "" {
		return ErrorResult("url parameter is required"), nil
	}

	// Extract format parameter
	format, ok := params["format"].(string)
	if !ok || format == "" {
		format = "text" // Default to text format
	}

	// Extract timeout parameter (default: 30 seconds)
	timeout := 30
	if timeoutVal, ok := params["timeout"].(float64); ok {
		timeout = int(timeoutVal)
	}

	// Perform fetch
	content, contentType, statusCode, err := t.fetch(ctx, targetURL, format, timeout)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Fetch failed: %v", err)), nil
	}

	// Content for LLM: full fetched content
	llmContent := fmt.Sprintf("Status Code: %d\nContent-Type: %s\n\n%s", statusCode, contentType, content)

	// Details for UI: truncate long content for display
	displayContent := content
	if len(displayContent) > 2000 {
		displayContent = displayContent[:2000] + "\n...[truncated for display]"
	}
	uiDetails := fmt.Sprintf("GET %s → %d (%s)\n%s", targetURL, statusCode, contentType, displayContent)

	return &ToolResult{
		Success: true,
		Content: llmContent,
		Details: uiDetails,
	}, nil
}

// fetch retrieves content from a URL and formats it
func (t *WebFetchTool) fetch(ctx context.Context, targetURL, format string, timeoutSeconds int) (string, string, int, error) {
	// Validate URL
	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		return "", "", 0, fmt.Errorf("URL must start with http:// or https://")
	}

	// Validate format
	format = strings.ToLower(format)
	if format != "text" && format != "markdown" && format != "html" {
		return "", "", 0, fmt.Errorf("format must be one of: text, markdown, html")
	}

	// Set timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 30
	}
	if timeoutSeconds > 120 {
		timeoutSeconds = 120
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
		return "", "", 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Mini-Agent-Go/1.0)")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Read response body with size limit (5MB)
	maxSize := int64(5 * 1024 * 1024)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSize))
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to read response: %w", err)
	}

	content := string(body)

	// Validate UTF-8
	if !utf8.ValidString(content) {
		return "", "", 0, fmt.Errorf("response content is not valid UTF-8")
	}

	contentType := resp.Header.Get("Content-Type")

	// Process based on format
	if strings.Contains(contentType, "text/html") {
		switch format {
		case "text":
			text, err := extractTextFromHTML(content)
			if err != nil {
				return "", "", 0, fmt.Errorf("failed to extract text from HTML: %w", err)
			}
			content = text

		case "markdown":
			markdown, err := convertHTMLToMarkdown(content)
			if err != nil {
				return "", "", 0, fmt.Errorf("failed to convert HTML to markdown: %w", err)
			}
			content = markdown

		case "html":
			// Extract body content
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(content))
			if err != nil {
				return "", "", 0, fmt.Errorf("failed to parse HTML: %w", err)
			}
			bodyHTML, err := doc.Find("body").Html()
			if err != nil {
				return "", "", 0, fmt.Errorf("failed to extract body from HTML: %w", err)
			}
			if bodyHTML == "" {
				return "", "", 0, fmt.Errorf("no body content found in HTML")
			}
			content = "<html>\n<body>\n" + bodyHTML + "\n</body>\n</html>"
		}
	}

	// Truncate if too large (500KB for practical purposes)
	maxContentSize := 500 * 1024
	if len(content) > maxContentSize {
		content = content[:maxContentSize]
		content += fmt.Sprintf("\n\n[Content truncated at %d bytes]", maxContentSize)
	}

	return content, contentType, resp.StatusCode, nil
}

// extractTextFromHTML extracts plain text from HTML
func extractTextFromHTML(html string) (string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", err
	}

	// Remove script and style elements
	doc.Find("script, style").Remove()

	text := doc.Find("body").Text()
	// Normalize whitespace
	text = strings.Join(strings.Fields(text), " ")

	return text, nil
}

// convertHTMLToMarkdown converts HTML to Markdown
func convertHTMLToMarkdown(html string) (string, error) {
	converter := md.NewConverter("", true, nil)

	markdown, err := converter.ConvertString(html)
	if err != nil {
		return "", err
	}

	return markdown, nil
}
