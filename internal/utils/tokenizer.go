package utils

import (
	"encoding/json"
	"strings"

	"mini-agent-go/internal/schema"
)

// EstimateTokens estimates the number of tokens in a list of messages
func EstimateTokens(messages []*schema.Message) int {
	totalTokens := 0

	for _, msg := range messages {
		// Count text content
		totalTokens += estimateTextTokens(msg.Content)

		// Count thinking
		if msg.Thinking != "" {
			totalTokens += estimateTextTokens(msg.Thinking)
		}

		// Count tool calls
		for _, tc := range msg.ToolCalls {
			totalTokens += estimateTextTokens(tc.Function.Name)
			if tc.Function.Arguments != nil {
				if argBytes, err := json.Marshal(tc.Function.Arguments); err == nil {
					totalTokens += len(argBytes) / 4
				}
			}
			totalTokens += 10 // overhead per tool call (id, type, structure)
		}

		// Metadata overhead per message (approximately 4 tokens)
		totalTokens += 4
	}

	return totalTokens
}

// estimateTextTokens estimates tokens in text using simple chars/4 heuristic
func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// TruncateText truncates text by token count
func TruncateText(text string, maxTokens int) string {
	tokens := estimateTextTokens(text)
	if tokens <= maxTokens {
		return text
	}

	// Calculate approximate characters per token (uniform ~4)
	maxChars := maxTokens * 4
	if maxChars >= len(text) {
		return text
	}

	// Keep head and tail mode: allocate half space for each (with 5% safety margin)
	charsPerHalf := int(float64(maxChars) / 2.0 * 0.95)

	// Truncate front part
	headPart := text[:charsPerHalf]
	lastNewlineHead := strings.LastIndex(headPart, "\n")
	if lastNewlineHead > 0 {
		headPart = headPart[:lastNewlineHead]
	}

	// Truncate back part
	tailPart := text[len(text)-charsPerHalf:]
	firstNewlineTail := strings.Index(tailPart, "\n")
	if firstNewlineTail > 0 {
		tailPart = tailPart[firstNewlineTail+1:]
	}

	// Combine result
	truncationNote := "\n\n... [Content truncated] ...\n\n"
	return headPart + truncationNote + tailPart
}
