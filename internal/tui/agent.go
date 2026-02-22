package tui

import (
	"context"

	"mini-agent-go/internal/agent"
	"mini-agent-go/internal/tools"

	tea "github.com/charmbracelet/bubbletea"
)

// runAgent 在后台运行 Agent 并返回初始命令
func (m *Model) runAgent(prompt string) tea.Cmd {
	// 创建可取消的 context
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelFunc = cancel

	// 创建事件 channel
	m.agentEventCh = make(chan tea.Msg, 10)

	// 启动 goroutine 监听 Agent 内部事件 (todo updates, etc.)
	go func() {
		for {
			select {
			case <-ctx.Done():
				// Context cancelled, stop listening
				return
			case event, ok := <-m.agentInternalEventCh:
				if !ok {
					// Channel closed
					return
				}
				// Convert tool events to TUI messages
				switch e := event.(type) {
				case tools.TodoUpdateEvent:
					// Convert TodoItem to interface{} slice
					todos := make([]interface{}, len(e.NewTodos))
					for i, todo := range e.NewTodos {
						todos[i] = todo
					}
					// Safe send with timeout to avoid blocking
					select {
					case m.agentEventCh <- todoUpdateMsg{todos: todos}:
						// Sent successfully
					case <-ctx.Done():
						// Context cancelled, abort send
						return
					}
				}
			}
		}
	}()

	// 启动 Agent 执行的 goroutine
	go func() {
		defer close(m.agentEventCh)
		defer cancel() // Cancel context when agent completes
		defer func() {
			m.cancelFunc = nil
		}()

		// 使用带回调的 Agent 执行（流式）
		err := m.agent.RunWithCallback(ctx, prompt, agent.AgentCallback{
			OnStep: func(step, maxSteps int) {
				m.agentEventCh <- agentStepMsg{step, maxSteps}
			},
			// 流式回调
			OnContentDelta: func(delta string, isFirst bool) {
				m.agentEventCh <- agentContentDeltaMsg{delta, isFirst}
			},
			OnThinkingDelta: func(delta string, isFirst bool) {
				m.agentEventCh <- agentThinkingDeltaMsg{delta, isFirst}
			},
			OnStreamComplete: func(finalContent string) {
				m.agentEventCh <- agentStreamCompleteMsg{finalContent}
			},
			// 工具回调
			OnToolCall: func(name string, args map[string]interface{}) {
				m.agentEventCh <- agentToolCallMsg{name, args}
			},
			OnToolResult: func(name string, success bool, details string) {
				m.agentEventCh <- agentToolResultMsg{name, success, details}
			},
			OnUserMessageInjected: func(content string) {
				m.agentEventCh <- userMessageInjectedMsg{content}
			},
		})

		if err != nil {
			// Check if error is due to context cancellation
			if err == context.Canceled {
				// User cancelled, don't send error
				return
			}
			m.agentEventCh <- agentErrorMsg{err}
		} else {
			m.agentEventCh <- agentCompleteMsg{""}
		}
	}()

	// 返回一个批量命令：先发送开始消息，然后开始监听事件
	return tea.Batch(
		func() tea.Msg { return agentStartMsg{} },
		waitForAgentEvent(m.agentEventCh),
	)
}

// waitForAgentEvent 等待 Agent 执行事件
func waitForAgentEvent(eventCh <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-eventCh
		if !ok {
			// Channel 已关闭
			return nil
		}
		return msg
	}
}
