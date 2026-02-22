package llm

import (
	"fmt"
	"regexp"
	"strings"

	"mini-agent-go/internal/schema"
)

// MiniMax M2/M2.5 returns tool calls as XML in content text instead of
// standard OpenAI tool_calls JSON, when accessed via the official API.
//
// Format:
//   <minimax:tool_call>
//   <invoke name="function_name">
//   <parameter name="key">value</parameter>
//   </invoke>
//   </minimax:tool_call>
//
// It also wraps thinking in <think>...</think> tags inside content.

var (
	reThinkBlock    = regexp.MustCompile(`(?s)<think>(.*?)</think>`)
	reToolCallBlock = regexp.MustCompile(`(?s)<minimax:tool_call>(.*?)</minimax:tool_call>`)
	reInvoke        = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)">(.*?)</invoke>`)
	reParameter     = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)">(.*?)</parameter>`)
)

// extractMiniMaxToolCalls parses MiniMax XML tool calls and thinking from
// the content field of an LLM response. It modifies the response in-place:
//   - Extracts <think> blocks into resp.Thinking
//   - Extracts <minimax:tool_call> blocks into resp.ToolCalls
//   - Cleans the content text
//
// This is a no-op if the content contains no MiniMax XML tags.
func extractMiniMaxToolCalls(resp *schema.LLMResponse) {
	if resp == nil || resp.Content == "" {
		return
	}

	content := resp.Content

	// Extract thinking blocks
	if matches := reThinkBlock.FindStringSubmatch(content); len(matches) > 1 {
		if resp.Thinking == "" {
			resp.Thinking = strings.TrimSpace(matches[1])
		}
		content = reThinkBlock.ReplaceAllString(content, "")
	}

	// Extract tool call blocks
	toolBlockMatches := reToolCallBlock.FindAllStringSubmatch(content, -1)
	if len(toolBlockMatches) == 0 {
		resp.Content = strings.TrimSpace(content)
		return
	}

	// Only override if no standard tool_calls were already parsed
	if len(resp.ToolCalls) > 0 {
		resp.Content = strings.TrimSpace(content)
		return
	}

	toolCallIndex := 0
	for _, blockMatch := range toolBlockMatches {
		blockContent := blockMatch[1]

		// Find all <invoke> elements within this tool_call block
		invokeMatches := reInvoke.FindAllStringSubmatch(blockContent, -1)
		for _, invokeMatch := range invokeMatches {
			funcName := invokeMatch[1]
			invokeBody := invokeMatch[2]

			// Parse parameters
			args := make(map[string]interface{})
			paramMatches := reParameter.FindAllStringSubmatch(invokeBody, -1)
			for _, pm := range paramMatches {
				args[pm[1]] = pm[2]
			}

			resp.ToolCalls = append(resp.ToolCalls, schema.ToolCall{
				ID:   fmt.Sprintf("minimax_call_%d", toolCallIndex),
				Type: "function",
				Function: schema.FunctionCall{
					Name:      funcName,
					Arguments: args,
				},
			})
			toolCallIndex++
		}
	}

	// Update finish reason if we found tool calls
	if len(resp.ToolCalls) > 0 {
		resp.FinishReason = "tool_calls"
	}

	// Remove tool call XML from content
	content = reToolCallBlock.ReplaceAllString(content, "")
	resp.Content = strings.TrimSpace(content)
}

// convertToolsToOpenAIFormat converts tool schemas to OpenAI format.
// Handles both Anthropic flat format {"name","description","input_schema"}
// and already-correct OpenAI format {"type":"function","function":{...}}.
func convertToolsToOpenAIFormat(tools []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		// Already in OpenAI format
		if _, ok := t["function"]; ok {
			result = append(result, t)
			continue
		}

		// Convert from Anthropic flat format
		name, _ := t["name"].(string)
		desc, _ := t["description"].(string)
		var params interface{}
		if p, ok := t["input_schema"]; ok {
			params = p
		} else {
			params = t["parameters"]
		}

		result = append(result, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        name,
				"description": desc,
				"parameters":  params,
			},
		})
	}
	return result
}
