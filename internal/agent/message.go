package agent

import (
	"fmt"
	"time"

	"mini-agent-go/internal/schema"
)

// Message represents a chat message with execution context
type Message struct {
	*schema.Message
}

// NewSystemMessage creates a new system message
func NewSystemMessage(content string) *Message {
	return &Message{
		Message: &schema.Message{
			Role:      "system",
			Content:   content,
			Timestamp: time.Now(),
		},
	}
}

// NewUserMessage creates a new user message
func NewUserMessage(content string) *Message {
	return &Message{
		Message: &schema.Message{
			Role:      "user",
			Content:   content,
			Timestamp: time.Now(),
		},
	}
}

// NewAssistantMessage creates a new assistant message
func NewAssistantMessage(content string, thinking string, toolCalls []schema.ToolCall) *Message {
	return &Message{
		Message: &schema.Message{
			Role:      "assistant",
			Content:   content,
			Thinking:  thinking,
			ToolCalls: toolCalls,
			Timestamp: time.Now(),
		},
	}
}

// NewToolMessage creates a new tool message
func NewToolMessage(name, content string, toolCallID string) *Message {
	return &Message{
		Message: &schema.Message{
			Role:       "tool",
			Content:    content,
			ToolCallID: toolCallID,
			Name:       name,
			Timestamp:  time.Now(),
		},
	}
}

// ToSchema converts Message to schema.Message
func (m *Message) ToSchema() *schema.Message {
	return m.Message
}

// String returns a string representation of the message
func (m *Message) String() string {
	return fmt.Sprintf("[%s] %s", m.Role, m.Content)
}

// IsSystem checks if message is a system message
func (m *Message) IsSystem() bool {
	return m.Role == "system"
}

// IsUser checks if message is a user message
func (m *Message) IsUser() bool {
	return m.Role == "user"
}

// IsAssistant checks if message is an assistant message
func (m *Message) IsAssistant() bool {
	return m.Role == "assistant"
}

// IsTool checks if message is a tool message
func (m *Message) IsTool() bool {
	return m.Role == "tool"
}

// HasToolCalls checks if message has tool calls
func (m *Message) HasToolCalls() bool {
	return len(m.ToolCalls) > 0
}

