package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"mini-agent-go/internal/schema"
)

// AnthropicSDKClient wraps the official Anthropic SDK
type AnthropicSDKClient struct {
	config   Config
	client   *anthropic.Client
	debugLog *os.File
}

// NewAnthropicSDKClient creates a new Anthropic client using the official SDK
func NewAnthropicSDKClient(config Config) (*AnthropicSDKClient, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if config.APIBase == "" {
		config.APIBase = "https://api.minimaxi.com/anthropic/v1"
	}

	if config.Model == "" {
		config.Model = "MiniMax-M2.5"
	}

	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	// Create SDK client with custom base URL and retries
	opts := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
		option.WithBaseURL(config.APIBase),
		option.WithMaxRetries(config.MaxRetries),
	}

	client := anthropic.NewClient(opts...)

	// Create debug log file
	logDir := config.LogDir
	if logDir == "" {
		logDir = "logs"
	}
	debugLogPath := filepath.Join(logDir, "llm_sdk_debug.log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}
	debugLog, err := os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open debug log: %w", err)
	}

	return &AnthropicSDKClient{
		config:   config,
		client:   &client,
		debugLog: debugLog,
	}, nil
}

// Close releases resources held by the Anthropic SDK client.
func (c *AnthropicSDKClient) Close() error {
	if c.debugLog != nil {
		return c.debugLog.Close()
	}
	return nil
}

// GenerateStream generates a response from the LLM with streaming support
func (c *AnthropicSDKClient) GenerateStream(
	ctx context.Context,
	messages []*schema.Message,
	tools []map[string]interface{},
	callback StreamCallback,
) (*schema.LLMResponse, error) {
	// Convert messages to Anthropic format
	msgParams, systemText := c.convertMessages(messages)
	toolParams := c.convertTools(tools)

	// Prepare request
	maxTokens := c.config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 16384
	}

	// Build request using SDK
	req := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.config.Model),
		MaxTokens: int64(maxTokens),
		Messages:  msgParams,
		System:    []anthropic.TextBlockParam{{Text: systemText}},
	}

	// Add tools if provided
	if len(toolParams) > 0 {
		req.Tools = toolParams
	}

	// Create streaming request
	stream := c.client.Messages.NewStreaming(ctx, req)
	defer stream.Close()

	// Process the stream
	return c.processStream(stream, callback)
}

// convertMessages converts internal schema messages to Anthropic SDK format
func (c *AnthropicSDKClient) convertMessages(messages []*schema.Message) ([]anthropic.MessageParam, string) {
	var systemPrompt string
	var anthropicMessages []anthropic.MessageParam

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			systemPrompt = msg.Content

		case "user":
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(msg.Content),
			))

		case "assistant":
			// Handle assistant messages with thinking or tool calls
			if msg.Thinking != "" || len(msg.ToolCalls) > 0 {
				var blocks []anthropic.ContentBlockParamUnion

				// Add text content if present
				if msg.Content != "" {
					blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
				}

				// Add tool use blocks for tool calls
				for _, tc := range msg.ToolCalls {
					blocks = append(blocks, anthropic.NewToolUseBlock(
						tc.ID,
						tc.Function.Arguments,
						tc.Function.Name,
					))
				}

				if len(blocks) > 0 {
					anthropicMessages = append(anthropicMessages,
						anthropic.NewAssistantMessage(blocks...))
				}
			} else {
				// Simple text message - use content array format
				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleAssistant,
					Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(msg.Content)},
				})
			}

		case "tool":
			anthropicMessages = append(anthropicMessages,
				anthropic.NewUserMessage(anthropic.NewToolResultBlock(
					msg.ToolCallID,
					msg.Content,
					false,
				)))
		}
	}

	return anthropicMessages, systemPrompt
}

// convertTools converts tool definitions to Anthropic SDK format
func (c *AnthropicSDKClient) convertTools(tools []map[string]interface{}) []anthropic.ToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	var result []anthropic.ToolUnionParam
	for _, t := range tools {
		name, ok := t["name"].(string)
		if !ok {
			continue
		}

		desc, _ := t["description"].(string)
		inputSchemaRaw, _ := t["input_schema"].(map[string]interface{})

		// Extract properties from input_schema - it's nested under "properties" key
		// input_schema structure: {"type": "object", "properties": {...}, "required": [...]}
		// We need just the "properties" part for the SDK
		var properties map[string]interface{}
		if props, ok := inputSchemaRaw["properties"].(map[string]interface{}); ok {
			properties = props
		} else {
			// Fallback: if no nested properties, use the raw input_schema as properties
			properties = inputSchemaRaw
		}

		// Convert input_schema to SDK format
		inputSchema := anthropic.ToolInputSchemaParam{
			Type:        "object",
			Properties:  properties,
		}

		toolParam := anthropic.ToolParam{
			Name:        name,
			Description: param.NewOpt(desc),
			InputSchema: inputSchema,
		}

		result = append(result, anthropic.ToolUnionParam{OfTool: &toolParam})
	}
	return result
}

// sdkStreamState manages state during streaming response
type sdkStreamState struct {
	textContent     strings.Builder
	thinkingContent strings.Builder
	toolCalls       []schema.ToolCall
	currentToolCall *sdkToolCallBuilder
	usage           *schema.Usage
	finishReason    string
	messageID       string
	currentIndex    int64
}

// sdkToolCallBuilder accumulates partial JSON for tool arguments
type sdkToolCallBuilder struct {
	id        string
	name      string
	inputJSON strings.Builder
	index     int64
}

// processStream handles the SDK streaming response
func (c *AnthropicSDKClient) processStream(
	stream *ssestream.Stream[anthropic.MessageStreamEventUnion],
	callback StreamCallback,
) (*schema.LLMResponse, error) {
	state := &sdkStreamState{
		currentIndex: -1,
	}

	for stream.Next() {
		event := stream.Current()

		if err := c.handleStreamEvent(event, state, callback); err != nil {
			return nil, err
		}
	}

	if stream.Err() != nil {
		return nil, fmt.Errorf("stream error: %w", stream.Err())
	}

	return c.buildFinalResponse(state), nil
}

// handleStreamEvent processes a single streaming event from the SDK
func (c *AnthropicSDKClient) handleStreamEvent(
	event anthropic.MessageStreamEventUnion,
	state *sdkStreamState,
	callback StreamCallback,
) error {
	// Check event type using the Type field
	switch event.Type {
	case "message_start":
		state.messageID = event.Message.ID
		if event.Message.Usage.InputTokens > 0 {
			state.usage = &schema.Usage{
				InputTokens:  int(event.Message.Usage.InputTokens),
				PromptTokens: int(event.Message.Usage.InputTokens),
			}
		}

	case "content_block_start":
		state.currentIndex = event.Index
		// Check if this is a tool_use block
		if event.ContentBlock.Type == "tool_use" {
			state.currentToolCall = &sdkToolCallBuilder{
				id:    event.ContentBlock.ID,
				name:  event.ContentBlock.Name,
				index: event.Index,
			}
			if callback != nil {
				callback.OnToolCallStart(event.ContentBlock.ID, event.ContentBlock.Name)
			}
		}

	case "content_block_delta":
		return c.handleContentBlockDelta(event, state, callback)

	case "content_block_stop":
		// If we were building a tool call, finalize it
		if state.currentToolCall != nil && state.currentToolCall.index == event.Index {
			inputJSON := state.currentToolCall.inputJSON.String()
			var input map[string]interface{}
			if inputJSON != "" {
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					c.debugLogf("Failed to parse tool input JSON for tool '%s' (ID: %s): %v",
						state.currentToolCall.name, state.currentToolCall.id, err)
					return fmt.Errorf("failed to parse tool input JSON: %w", err)
				}
			}

			state.toolCalls = append(state.toolCalls, schema.ToolCall{
				ID:   state.currentToolCall.id,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      state.currentToolCall.name,
					Arguments: input,
				},
			})

			if callback != nil {
				callback.OnToolCallComplete(state.currentToolCall.id, input)
			}

			state.currentToolCall = nil
		}

	case "message_delta":
		if event.Delta.StopReason != "" {
			state.finishReason = string(event.Delta.StopReason)
		}
		if event.Usage.OutputTokens > 0 {
			if state.usage == nil {
				state.usage = &schema.Usage{}
			}
			state.usage.OutputTokens = int(event.Usage.OutputTokens)
			state.usage.CompletionTokens = int(event.Usage.OutputTokens)
			state.usage.TotalTokens = state.usage.InputTokens + state.usage.OutputTokens
			if callback != nil {
				callback.OnUsageUpdate(state.usage)
			}
		}

	case "message_stop":
		// Stream complete

	case "ping":
		// Ignore keep-alive

	case "error":
		// Handle error event
		c.debugLogf("Received error event from stream")

	default:
		// Unknown event type, ignore
	}

	return nil
}

// handleContentBlockDelta processes content delta events
func (c *AnthropicSDKClient) handleContentBlockDelta(
	event anthropic.MessageStreamEventUnion,
	state *sdkStreamState,
	callback StreamCallback,
) error {
	// Delta is a struct, we need to check its Type field
	delta := event.Delta

	switch delta.Type {
	case "text_delta":
		// Get text from Delta - it's in a nested field
		if text := c.extractTextFromDelta(event); text != "" {
			state.textContent.WriteString(text)
			if callback != nil {
				callback.OnContentDelta(text)
			}
		}

	case "thinking_delta":
		if thinking := c.extractThinkingFromDelta(event); thinking != "" {
			state.thinkingContent.WriteString(thinking)
			if callback != nil {
				callback.OnThinkingDelta(thinking)
			}
		}

	case "input_json_delta":
		if state.currentToolCall != nil {
			partialJSON := c.extractPartialJSONFromDelta(event)
			if partialJSON != "" {
				state.currentToolCall.inputJSON.WriteString(partialJSON)
				if callback != nil {
					callback.OnToolCallDelta(state.currentToolCall.id, partialJSON)
				}
			}
		}
	}

	return nil
}

// extractTextFromDelta extracts text from delta event
func (c *AnthropicSDKClient) extractTextFromDelta(event anthropic.MessageStreamEventUnion) string {
	// The delta data is embedded in the event, need to parse it
	// Use JSON to extract the text field from the raw delta
	var deltaRaw map[string]interface{}
	if data, err := json.Marshal(event.Delta); err == nil {
		if err := json.Unmarshal(data, &deltaRaw); err == nil {
			if text, ok := deltaRaw["text"].(string); ok {
				return text
			}
		}
	}
	return ""
}

// extractThinkingFromDelta extracts thinking from delta event
func (c *AnthropicSDKClient) extractThinkingFromDelta(event anthropic.MessageStreamEventUnion) string {
	var deltaRaw map[string]interface{}
	if data, err := json.Marshal(event.Delta); err == nil {
		if err := json.Unmarshal(data, &deltaRaw); err == nil {
			if thinking, ok := deltaRaw["thinking"].(string); ok {
				return thinking
			}
		}
	}
	return ""
}

// extractPartialJSONFromDelta extracts partial JSON from delta event
func (c *AnthropicSDKClient) extractPartialJSONFromDelta(event anthropic.MessageStreamEventUnion) string {
	var deltaRaw map[string]interface{}
	if data, err := json.Marshal(event.Delta); err == nil {
		if err := json.Unmarshal(data, &deltaRaw); err == nil {
			if partialJSON, ok := deltaRaw["partial_json"].(string); ok {
				return partialJSON
			}
		}
	}
	return ""
}

// buildFinalResponse constructs the final LLMResponse from stream state
func (c *AnthropicSDKClient) buildFinalResponse(state *sdkStreamState) *schema.LLMResponse {
	return &schema.LLMResponse{
		Content:      state.textContent.String(),
		Thinking:     state.thinkingContent.String(),
		ToolCalls:    state.toolCalls,
		FinishReason: state.finishReason,
		Usage:        state.usage,
	}
}

// debugLogf writes a formatted debug message to the debug log file
func (c *AnthropicSDKClient) debugLogf(format string, args ...interface{}) {
	if c.debugLog == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.debugLog, "[%s] %s\n", timestamp, msg)
	c.debugLog.Sync()
}
