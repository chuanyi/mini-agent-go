package llm

import (
	"context"
	"fmt"

	"mini-agent-go/internal/schema"
)

// StreamCallback receives streaming events during LLM generation
type StreamCallback interface {
	// OnContentDelta is called when text content arrives
	OnContentDelta(delta string) error

	// OnThinkingDelta is called when thinking content arrives
	OnThinkingDelta(delta string) error

	// OnToolCallStart is called when a tool call begins
	OnToolCallStart(id, name string) error

	// OnToolCallDelta is called with partial JSON for tool arguments
	OnToolCallDelta(id string, partialJSON string) error

	// OnToolCallComplete is called when a tool call is complete with full arguments
	OnToolCallComplete(id string, input map[string]interface{}) error

	// OnUsageUpdate is called with token usage information
	OnUsageUpdate(usage *schema.Usage) error
}

// Client is the interface for LLM clients
type Client interface {
	GenerateStream(ctx context.Context, messages []*schema.Message, tools []map[string]interface{}, callback StreamCallback) (*schema.LLMResponse, error)
	// Close releases resources held by the client (file handles, connections, etc.).
	Close() error
}

// Config represents LLM client configuration
type Config struct {
	APIKey      string
	APIBase     string
	Model       string
	Provider    schema.LLMProvider
	MaxRetries  int
	MaxTokens   int     // Max output tokens per request (0 = provider default)
	Temperature float64 // Sampling temperature (-1 = provider default)
	LogDir      string  // Directory for debug logs (default: "logs")
}

// NewClient creates a new LLM client based on provider
func NewClient(config Config) (Client, error) {
	switch config.Provider {
	case schema.ProviderAnthropic:
		return NewAnthropicSDKClient(config)
	case schema.ProviderOpenAI:
		return NewOpenAISDKClient(config)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", config.Provider)
	}
}
