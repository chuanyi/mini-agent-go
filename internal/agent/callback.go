package agent

import (
	"strings"

	"mini-agent-go/internal/schema"
)

// AgentCallback defines callback functions for Agent execution events.
type AgentCallback struct {
	OnStep func(step, maxSteps int)

	// Streaming callbacks
	OnContentDelta        func(delta string, isFirst bool)
	OnThinkingDelta       func(delta string, isFirst bool)
	OnStreamComplete      func(finalContent string)

	// Tool callbacks
	OnToolCall            func(name string, args map[string]interface{})
	OnToolResult          func(name string, success bool, details string)
	OnUserMessageInjected func(content string) // Called when a pending user message is injected
}

// injectPendingMessages injects all pending user messages into the conversation.
// Called at the start of each step, before LLM call.
func (a *Agent) injectPendingMessages(callback AgentCallback) {
	a.pendingMessagesMux.Lock()
	defer a.pendingMessagesMux.Unlock()

	for _, msg := range a.pendingMessages {
		a.messages = append(a.messages, NewUserMessage(msg))

		if callback.OnUserMessageInjected != nil {
			callback.OnUserMessageInjected(msg)
		}
	}

	a.pendingMessages = nil
}

// agentStreamAdapter adapts LLM StreamCallback to AgentCallback
type agentStreamAdapter struct {
	agentCallback    AgentCallback
	isFirstContent   bool
	isFirstThinking  bool
	contentBuf       *strings.Builder
	thinkingBuf      *strings.Builder
}

func newAgentStreamAdapter(callback AgentCallback) *agentStreamAdapter {
	return &agentStreamAdapter{
		agentCallback:   callback,
		isFirstContent:  true,
		isFirstThinking: true,
		contentBuf:      &strings.Builder{},
		thinkingBuf:     &strings.Builder{},
	}
}

func (a *agentStreamAdapter) OnContentDelta(delta string) error {
	a.contentBuf.WriteString(delta)
	if a.agentCallback.OnContentDelta != nil {
		a.agentCallback.OnContentDelta(delta, a.isFirstContent)
	}
	a.isFirstContent = false
	return nil
}

func (a *agentStreamAdapter) OnThinkingDelta(delta string) error {
	a.thinkingBuf.WriteString(delta)
	if a.agentCallback.OnThinkingDelta != nil {
		a.agentCallback.OnThinkingDelta(delta, a.isFirstThinking)
	}
	a.isFirstThinking = false
	return nil
}

func (a *agentStreamAdapter) OnToolCallStart(id, name string) error {
	return nil
}

func (a *agentStreamAdapter) OnToolCallDelta(id string, partialJSON string) error {
	return nil
}

func (a *agentStreamAdapter) OnToolCallComplete(id string, input map[string]interface{}) error {
	return nil
}

func (a *agentStreamAdapter) OnUsageUpdate(usage *schema.Usage) error {
	return nil
}
