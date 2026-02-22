package llm

import (
	"context"
	"fmt"
	"os"
	"testing"

	"mini-agent-go/internal/schema"
)

// TestAnthropicIntegration tests actual Anthropic API calls
// This test requires ANTHROPIC_API_KEY environment variable
func TestAnthropicIntegration(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: ANTHROPIC_API_KEY not set")
	}

	t.Run("Simple generation", func(t *testing.T) {
		config := Config{
			APIKey:     apiKey,
			APIBase:    "https://api.minimaxi.com/anthropic/v1",
			Model:      "MiniMax-M2",
			MaxRetries: 2,
		}

		client, err := NewAnthropicSDKClient(config)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		messages := []*schema.Message{
			{
				Role:    "user",
				Content: "Say hello in exactly 3 words",
			},
		}

		ctx := context.Background()
		response, err := client.GenerateStream(ctx, messages, nil, nil)
		if err != nil {
			t.Fatalf("GenerateStream failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response to be non-nil")
		}

		if response.Content == "" {
			t.Error("Expected non-empty content")
		}

		if response.Usage == nil {
			t.Error("Expected usage information")
		} else {
			if response.Usage.TotalTokens <= 0 {
				t.Error("Expected positive token count")
			}
		}

		t.Logf("Response: %s", response.Content)
		if response.Usage != nil {
			t.Logf("Tokens: %d", response.Usage.TotalTokens)
		}
	})

	t.Run("Generation with system prompt", func(t *testing.T) {
		config := Config{
			APIKey:     apiKey,
			APIBase:    "https://api.minimaxi.com/anthropic/v1",
			Model:      "MiniMax-M2",
			MaxRetries: 2,
		}

		client, err := NewAnthropicSDKClient(config)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		messages := []*schema.Message{
			{
				Role:    "system",
				Content: "You are a helpful assistant that always responds with exactly one emoji",
			},
			{
				Role:    "user",
				Content: "How are you?",
			},
		}

		ctx := context.Background()
		response, err := client.GenerateStream(ctx, messages, nil, nil)
		if err != nil {
			t.Fatalf("GenerateStream failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response to be non-nil")
		}

		if response.Content == "" {
			t.Error("Expected non-empty content")
		}

		t.Logf("Response: %s", response.Content)
	})

	t.Run("Generation with tools", func(t *testing.T) {
		config := Config{
			APIKey:     apiKey,
			APIBase:    "https://api.minimaxi.com/anthropic/v1",
			Model:      "MiniMax-M2",
			MaxRetries: 2,
		}

		client, err := NewAnthropicSDKClient(config)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		messages := []*schema.Message{
			{
				Role:    "user",
				Content: "Can you help me with a file operation?",
			},
		}

		tools := []map[string]interface{}{
			{
				"name":        "file_tool",
				"description": "File operation tool for reading and writing files",
				"input_schema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"action": map[string]interface{}{
							"type": "string",
							"enum": []string{"read", "write"},
						},
						"path": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []string{"action", "path"},
				},
			},
		}

		ctx := context.Background()
		response, err := client.GenerateStream(ctx, messages, tools, nil)
		if err != nil {
			t.Fatalf("GenerateStream failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response to be non-nil")
		}

		t.Logf("Response: %s", response.Content)
		if response.Usage != nil {
			t.Logf("Tokens: %d", response.Usage.TotalTokens)
		}
	})

	t.Run("Multiple messages", func(t *testing.T) {
		config := Config{
			APIKey:     apiKey,
			APIBase:    "https://api.minimaxi.com/anthropic/v1",
			Model:      "MiniMax-M2",
			MaxRetries: 2,
		}

		client, err := NewAnthropicSDKClient(config)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		messages := []*schema.Message{
			{
				Role:    "user",
				Content: "My name is Alice",
			},
			{
				Role:    "assistant",
				Content: "Hello Alice! Nice to meet you.",
			},
			{
				Role:    "user",
				Content: "What's my name?",
			},
		}

		ctx := context.Background()
		response, err := client.GenerateStream(ctx, messages, nil, nil)
		if err != nil {
			t.Fatalf("GenerateStream failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response to be non-nil")
		}

		if response.Content == "" {
			t.Error("Expected non-empty content")
		}

		t.Logf("Response: %s", response.Content)
	})
}

// TestOpenAIIntegration tests actual OpenAI API calls
// This test requires OPENAI_API_KEY environment variable
func TestOpenAIIntegration(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("Skipping integration test: OPENAI_API_KEY not set")
	}

	t.Run("Simple generation", func(t *testing.T) {
		config := Config{
			APIKey:     apiKey,
			APIBase:    "https://api.openai.com/v1",
			Model:      "gpt-3.5-turbo",
			MaxRetries: 2,
		}

		client, err := NewOpenAISDKClient(config)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		messages := []*schema.Message{
			{
				Role:    "user",
				Content: "Say hello in exactly 3 words",
			},
		}

		ctx := context.Background()
		response, err := client.GenerateStream(ctx, messages, nil, nil)
		if err != nil {
			t.Fatalf("GenerateStream failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response to be non-nil")
		}

		if response.Content == "" {
			t.Error("Expected non-empty content")
		}

		if response.Usage == nil {
			t.Error("Expected usage information")
		} else {
			if response.Usage.TotalTokens <= 0 {
				t.Error("Expected positive token count")
			}
		}

		t.Logf("Response: %s", response.Content)
		if response.Usage != nil {
			t.Logf("Tokens: %d", response.Usage.TotalTokens)
		}
	})

	t.Run("Generation with tools", func(t *testing.T) {
		config := Config{
			APIKey:     apiKey,
			APIBase:    "https://api.openai.com/v1",
			Model:      "gpt-3.5-turbo",
			MaxRetries: 2,
		}

		client, err := NewOpenAISDKClient(config)
		if err != nil {
			t.Fatalf("Failed to create client: %v", err)
		}
		defer client.Close()

		messages := []*schema.Message{
			{
				Role:    "user",
				Content: "Can you help me with a file operation?",
			},
		}

		tools := []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "file_tool",
					"description": "File operation tool for reading and writing files",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"action": map[string]interface{}{
								"type": "string",
								"enum": []string{"read", "write"},
							},
							"path": map[string]interface{}{
								"type": "string",
							},
						},
						"required": []string{"action", "path"},
					},
				},
			},
		}

		ctx := context.Background()
		response, err := client.GenerateStream(ctx, messages, tools, nil)
		if err != nil {
			t.Fatalf("GenerateStream failed: %v", err)
		}

		if response == nil {
			t.Fatal("Expected response to be non-nil")
		}

		t.Logf("Response: %s", response.Content)
		if response.Usage != nil {
			t.Logf("Tokens: %d", response.Usage.TotalTokens)
		}
	})
}

// Benchmark tests for performance measurement
func BenchmarkAnthropicGenerate(b *testing.B) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		b.Skip("Skipping benchmark: ANTHROPIC_API_KEY not set")
	}

	config := Config{
		APIKey:     apiKey,
		APIBase:    "https://api.minimaxi.com/anthropic/v1",
		Model:      "MiniMax-M2",
		MaxRetries: 1,
	}

	client, err := NewAnthropicSDKClient(config)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	messages := []*schema.Message{
		{
			Role:    "user",
			Content: "Generate a short response",
		},
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := client.GenerateStream(ctx, messages, nil, nil)
		if err != nil {
			b.Fatalf("GenerateStream failed: %v", err)
		}
	}
}

func BenchmarkOpenAIGenerate(b *testing.B) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		b.Skip("Skipping benchmark: OPENAI_API_KEY not set")
	}

	config := Config{
		APIKey:     apiKey,
		APIBase:    "https://api.openai.com/v1",
		Model:      "gpt-3.5-turbo",
		MaxRetries: 1,
	}

	client, err := NewOpenAISDKClient(config)
	if err != nil {
		b.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	messages := []*schema.Message{
		{
			Role:    "user",
			Content: "Generate a short response",
		},
	}

	ctx := context.Background()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := client.GenerateStream(ctx, messages, nil, nil)
		if err != nil {
			b.Fatalf("GenerateStream failed: %v", err)
		}
	}
}

// Example tests demonstrating usage
func ExampleNewAnthropicSDKClient() {
	config := Config{
		APIKey:     "your-api-key",
		APIBase:    "https://api.minimaxi.com/anthropic/v1",
		Model:      "MiniMax-M2",
		MaxRetries: 3,
	}

	client, err := NewAnthropicSDKClient(config)
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		return
	}
	defer client.Close()

	fmt.Printf("Client created: %v\n", client != nil)
	// Output: Client created: true
}
