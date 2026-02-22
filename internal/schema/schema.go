package schema

import "time"

// Message represents a chat message
type Message struct {
	Role       string                 `json:"role"`       // "system", "user", "assistant", "tool"
	Content    string                 `json:"content"`
	Thinking   string                 `json:"thinking,omitempty"`
	ToolCalls  []ToolCall             `json:"tool_calls,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"` // For tool role messages
	Name       string                 `json:"name,omitempty"`          // For tool role
	Timestamp  time.Time              `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// FunctionCall represents the function part of a tool call
type FunctionCall struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolCall represents a tool invocation
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// LLMProvider represents the LLM provider type
type LLMProvider string

const (
	ProviderAnthropic LLMProvider = "anthropic"
	ProviderOpenAI    LLMProvider = "openai"
)

// Usage represents token usage information
type Usage struct {
	InputTokens     int `json:"input_tokens,omitempty"`
	OutputTokens    int `json:"output_tokens,omitempty"`
	TotalTokens     int `json:"total_tokens,omitempty"`
	PromptTokens    int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
}

// LLMResponse represents a response from the LLM
type LLMResponse struct {
	Content      string       `json:"content"`
	Thinking     string       `json:"thinking,omitempty"`
	ToolCalls    []ToolCall   `json:"tool_calls,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
	Usage        *Usage       `json:"usage,omitempty"`
}
