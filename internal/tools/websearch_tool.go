package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// WebSearchResult represents a single search result
type WebSearchResult struct {
	Title       string
	Description string
	URL         string
}

// WebSearchTool performs web searches using DuckDuckGo
type WebSearchTool struct {
	*BaseTool
	httpClient *http.Client
	userAgent  string
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		BaseTool: &BaseTool{
			NameValue: "web_search",
			DescriptionValue: `Search the web via DuckDuckGo. Returns results with title, description, and URL.
Max 20 results per query (default: 5).`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query string",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of search results to return (default: 5, max: 20)",
					},
				},
				"required": []string{"query"},
			},
		},
		httpClient: &http.Client{},
		userAgent:  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36",
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Extract query parameter
	query, ok := params["query"].(string)
	if !ok || query == "" {
		return ErrorResult("query parameter is required"), nil
	}

	// Extract max_results parameter (default: 5)
	maxResults := 5
	if maxResultsVal, ok := params["max_results"].(float64); ok {
		maxResults = int(maxResultsVal)
	}

	// Validate and set defaults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}

	// Perform search
	results, err := t.search(ctx, query, maxResults)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Web search failed: %v", err)), nil
	}

	// Format results
	formattedResults := t.formatSearchResults(results)

	return &ToolResult{
		Success: true,
		Content: formattedResults,
		Details: fmt.Sprintf("search: %s\n%s", query, formattedResults),
	}, nil
}

// search performs a DuckDuckGo search and returns results
func (t *WebSearchTool) search(ctx context.Context, query string, maxResults int) ([]WebSearchResult, error) {
	// Build search URL
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set comprehensive headers to mimic real Chrome browser and avoid anti-bot detection
	if t.userAgent != "" {
		req.Header.Set("User-Agent", t.userAgent)
	}

	// Accept headers
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	//req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

	// Cache control
	req.Header.Set("Cache-Control", "max-age=0")

	// Sec-CH-UA headers (Chrome client hints)
	req.Header.Set("Sec-Ch-Ua", `"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`)
	req.Header.Set("Sec-Ch-Ua-Mobile", "?0")
	req.Header.Set("Sec-Ch-Ua-Platform", `"Windows"`)

	// Sec-Fetch headers (fetch metadata)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")

	// Other browser headers
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Priority", "u=0, i")

	// Execute request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse HTML response
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract search results
	var results []WebSearchResult
	sel := doc.Find(".web-result")

	for i := range sel.Nodes {
		if len(results) >= maxResults {
			break
		}

		node := sel.Eq(i)
		titleNode := node.Find(".result__a")

		title := strings.TrimSpace(titleNode.Text())
		description := strings.TrimSpace(node.Find(".result__snippet").Text())
		resultURL := ""

		// Extract URL from href attribute
		if len(titleNode.Nodes) > 0 {
			// Find href attribute
			var href string
			for _, attr := range titleNode.Nodes[0].Attr {
				if attr.Key == "href" {
					href = attr.Val
					break
				}
			}

			if href != "" {
				// Check if this is a DuckDuckGo redirect URL
				if strings.Contains(href, "duckduckgo.com") {
					// Parse the href URL to extract query parameters
					parsedURL, err := url.Parse(href)
					if err != nil {
						continue
					}

					// Extract the 'uddg' parameter which contains the real URL
					uddg := parsedURL.Query().Get("uddg")
					if uddg != "" {
						resultURL = uddg
					}
				} else {
					// Direct URL, use as-is
					resultURL = href
				}
			}
		}

		if title != "" && resultURL != "" {
			results = append(results, WebSearchResult{
				Title:       title,
				Description: description,
				URL:         resultURL,
			})
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no search results found for query: %s", query)
	}

	return results, nil
}

// formatSearchResults formats results into a readable string
func (t *WebSearchTool) formatSearchResults(results []WebSearchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d search result(s):\n\n", len(results)))

	for i, result := range results {
		sb.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, result.Title))
		if result.Description != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", result.Description))
		}
		sb.WriteString(fmt.Sprintf("   URL: %s\n\n", result.URL))
	}

	return sb.String()
}
