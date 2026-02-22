package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/shared"

	"mini-agent-go/internal/schema"
)

// OpenAISDKClient wraps the official OpenAI SDK
type OpenAISDKClient struct {
	config   Config
	client   *openai.Client
	debugLog *os.File
}

// NewOpenAISDKClient creates a new OpenAI client using the official SDK
func NewOpenAISDKClient(config Config) (*OpenAISDKClient, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key is required")
	}

	if config.APIBase == "" {
		config.APIBase = "https://api.openai.com/v1"
	}

	if config.Model == "" {
		config.Model = "gpt-4"
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

	client := openai.NewClient(opts...)

	// Create debug log file
	logDir := config.LogDir
	if logDir == "" {
		logDir = "logs"
	}
	debugLogPath := filepath.Join(logDir, "llm_openai_sdk_debug.log")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}
	debugLog, err := os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open debug log: %w", err)
	}

	return &OpenAISDKClient{
		config:   config,
		client:   &client,
		debugLog: debugLog,
	}, nil
}

// Close releases resources held by the OpenAI SDK client.
func (c *OpenAISDKClient) Close() error {
	if c.debugLog != nil {
		return c.debugLog.Close()
	}
	return nil
}

// GenerateStream generates a response from the LLM with streaming support
func (c *OpenAISDKClient) GenerateStream(
	ctx context.Context,
	messages []*schema.Message,
	tools []map[string]interface{},
	callback StreamCallback,
) (*schema.LLMResponse, error) {
	// Convert messages to OpenAI SDK format
	msgParams := c.convertMessages(messages)
	toolParams := c.convertTools(tools)

	// Prepare request
	maxTokens := c.config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4096
	}

	// Build request using SDK
	req := openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(c.config.Model),
		Messages:            msgParams,
		MaxCompletionTokens: param.NewOpt(int64(maxTokens)),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		},
	}

	// Add temperature if specified
	if c.config.Temperature >= 0 {
		req.Temperature = param.NewOpt(c.config.Temperature)
	}

	// Add tools if provided
	if len(toolParams) > 0 {
		req.Tools = toolParams
		req.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("auto"),
		}
	}

	// Create streaming request
	stream := c.client.Chat.Completions.NewStreaming(ctx, req)
	defer stream.Close()

	// Process the stream
	return c.processStream(stream, callback)
}

// convertMessages converts internal schema messages to OpenAI SDK format
func (c *OpenAISDKClient) convertMessages(messages []*schema.Message) []openai.ChatCompletionMessageParamUnion {
	var result []openai.ChatCompletionMessageParamUnion

	for _, msg := range messages {
		switch msg.Role {
		case "system":
			result = append(result, openai.SystemMessage(msg.Content))

		case "user":
			result = append(result, openai.UserMessage(msg.Content))

		case "assistant":
			if len(msg.ToolCalls) > 0 {
				// Assistant message with tool calls
				asst := openai.ChatCompletionAssistantMessageParam{}
				if msg.Content != "" {
					asst.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: param.NewOpt(msg.Content),
					}
				}
				// Convert tool calls
				for _, tc := range msg.ToolCalls {
					argsJSON, _ := json.Marshal(tc.Function.Arguments)
					asst.ToolCalls = append(asst.ToolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: tc.ID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      tc.Function.Name,
							Arguments: string(argsJSON),
						},
					})
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{OfAssistant: &asst})
			} else {
				result = append(result, openai.AssistantMessage(msg.Content))
			}

		case "tool":
			result = append(result, openai.ToolMessage(msg.Content, msg.ToolCallID))
		}
	}

	return result
}

// convertTools converts tool definitions to OpenAI SDK format.
// Accepts both OpenAI format {"type":"function","function":{...}} and
// Anthropic flat format {"name":"...","description":"...","input_schema":{...}}.
func (c *OpenAISDKClient) convertTools(tools []map[string]interface{}) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}

	var result []openai.ChatCompletionToolParam
	for _, t := range tools {
		var name, desc string
		var params map[string]interface{}

		// Try OpenAI format first: {"type":"function","function":{"name":...}}
		if funcMap, ok := t["function"].(map[string]interface{}); ok {
			name, _ = funcMap["name"].(string)
			desc, _ = funcMap["description"].(string)
			params, _ = funcMap["parameters"].(map[string]interface{})
		} else {
			// Anthropic flat format: {"name":"...","description":"...","input_schema":{...}}
			name, _ = t["name"].(string)
			desc, _ = t["description"].(string)
			if p, ok := t["input_schema"].(map[string]interface{}); ok {
				params = p
			} else {
				params, _ = t["parameters"].(map[string]interface{})
			}
		}

		if name == "" {
			continue
		}

		toolParam := openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        name,
				Description: param.NewOpt(desc),
				Parameters:  shared.FunctionParameters(params),
			},
		}

		result = append(result, toolParam)
	}
	return result
}

// openaiStreamState manages state during streaming response
type openaiStreamState struct {
	textContent  strings.Builder
	toolCalls    []schema.ToolCall
	usage        *schema.Usage
	finishReason string
	// Track tool call builders by index for incremental accumulation
	toolCallBuilders map[int64]*openaiToolCallBuilder
}

// openaiToolCallBuilder accumulates partial data for a single tool call
type openaiToolCallBuilder struct {
	id        string
	name      string
	argsJSON  strings.Builder
	index     int64
	started   bool
}

// processStream handles the SDK streaming response
func (c *OpenAISDKClient) processStream(
	stream *ssestream.Stream[openai.ChatCompletionChunk],
	callback StreamCallback,
) (*schema.LLMResponse, error) {
	state := &openaiStreamState{
		toolCallBuilders: make(map[int64]*openaiToolCallBuilder),
	}

	acc := openai.ChatCompletionAccumulator{}

	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)

		if err := c.handleChunk(chunk, state, callback); err != nil {
			return nil, err
		}

		// Check if a tool call just finished via the accumulator
		if tc, ok := acc.JustFinishedToolCall(); ok {
			c.finalizeToolCall(tc, state, callback)
		}
	}

	if stream.Err() != nil {
		return nil, fmt.Errorf("stream error: %w", stream.Err())
	}

	// Finalize any remaining tool call builders that weren't caught by the accumulator.
	// This happens when the last streaming chunk contains the final tool call delta
	// and JustFinishedToolCall() doesn't fire because there's no subsequent chunk.
	c.finalizeRemainingToolCalls(state, callback)

	resp := c.buildResponse(state)

	// Post-process: extract MiniMax XML tool calls from content if present
	extractMiniMaxToolCalls(resp)

	return resp, nil
}

// handleChunk processes a single streaming chunk
func (c *OpenAISDKClient) handleChunk(
	chunk openai.ChatCompletionChunk,
	state *openaiStreamState,
	callback StreamCallback,
) error {
	// Process choices
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		delta := choice.Delta

		// Content delta
		if delta.Content != "" {
			state.textContent.WriteString(delta.Content)
			if callback != nil {
				callback.OnContentDelta(delta.Content)
			}
		}

		// Tool call deltas
		for _, tc := range delta.ToolCalls {
			builder, exists := state.toolCallBuilders[tc.Index]
			if !exists {
				// New tool call starting
				builder = &openaiToolCallBuilder{
					id:    tc.ID,
					name:  tc.Function.Name,
					index: tc.Index,
				}
				state.toolCallBuilders[tc.Index] = builder
			}

			// Update ID and name if provided (only in first chunk)
			if tc.ID != "" {
				builder.id = tc.ID
			}
			if tc.Function.Name != "" {
				builder.name = tc.Function.Name
			}

			// Notify tool call start on first encounter
			if !builder.started {
				builder.started = true
				if callback != nil {
					callback.OnToolCallStart(builder.id, builder.name)
				}
			}

			// Accumulate partial arguments
			if tc.Function.Arguments != "" {
				builder.argsJSON.WriteString(tc.Function.Arguments)
				if callback != nil {
					callback.OnToolCallDelta(builder.id, tc.Function.Arguments)
				}
			}
		}

		// Finish reason
		if choice.FinishReason != "" {
			state.finishReason = string(choice.FinishReason)
		}
	}

	// Usage info (final chunk with StreamOptions.IncludeUsage=true)
	if chunk.Usage.TotalTokens > 0 {
		state.usage = &schema.Usage{
			InputTokens:      int(chunk.Usage.PromptTokens),
			OutputTokens:     int(chunk.Usage.CompletionTokens),
			TotalTokens:      int(chunk.Usage.TotalTokens),
			PromptTokens:     int(chunk.Usage.PromptTokens),
			CompletionTokens: int(chunk.Usage.CompletionTokens),
		}
		if callback != nil {
			callback.OnUsageUpdate(state.usage)
		}
	}

	return nil
}

// finalizeToolCall processes a completed tool call from the accumulator
func (c *OpenAISDKClient) finalizeToolCall(
	tc openai.FinishedChatCompletionToolCall,
	state *openaiStreamState,
	callback StreamCallback,
) {
	var args map[string]interface{}
	if tc.Arguments != "" {
		if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
			c.debugLogf("Failed to parse tool arguments for %s (ID: %s): %v",
				tc.Name, tc.ID, err)
			// Use empty args on parse failure
			args = make(map[string]interface{})
		}
	}

	state.toolCalls = append(state.toolCalls, schema.ToolCall{
		ID:   tc.ID,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      tc.Name,
			Arguments: args,
		},
	})

	if callback != nil {
		callback.OnToolCallComplete(tc.ID, args)
	}
}

// finalizeRemainingToolCalls converts any un-finalized tool call builders into
// completed tool calls. This catches tool calls that the accumulator's
// JustFinishedToolCall() missed (e.g. when the last chunk ends the stream).
func (c *OpenAISDKClient) finalizeRemainingToolCalls(state *openaiStreamState, callback StreamCallback) {
	// Build a set of already-finalized tool call IDs to avoid duplicates
	finalized := make(map[string]bool, len(state.toolCalls))
	for _, tc := range state.toolCalls {
		finalized[tc.ID] = true
	}

	for _, builder := range state.toolCallBuilders {
		if finalized[builder.id] {
			continue
		}

		argsJSON := builder.argsJSON.String()
		var args map[string]interface{}
		if argsJSON != "" {
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				c.debugLogf("Failed to parse tool arguments for %s (ID: %s): %v",
					builder.name, builder.id, err)
				args = make(map[string]interface{})
			}
		}

		state.toolCalls = append(state.toolCalls, schema.ToolCall{
			ID:   builder.id,
			Type: "function",
			Function: schema.FunctionCall{
				Name:      builder.name,
				Arguments: args,
			},
		})

		if callback != nil {
			callback.OnToolCallComplete(builder.id, args)
		}
	}
}

// buildResponse constructs the final LLMResponse from stream state
func (c *OpenAISDKClient) buildResponse(state *openaiStreamState) *schema.LLMResponse {
	return &schema.LLMResponse{
		Content:      state.textContent.String(),
		ToolCalls:    state.toolCalls,
		FinishReason: state.finishReason,
		Usage:        state.usage,
	}
}

// debugLogf writes a formatted debug message to the debug log file
func (c *OpenAISDKClient) debugLogf(format string, args ...interface{}) {
	if c.debugLog == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(c.debugLog, "[%s] %s\n", timestamp, msg)
	c.debugLog.Sync()
}
