package tui

import (
	"time"
)

// MessageType 表示消息的类型
type MessageType int

const (
	MessageTypeUser MessageType = iota
	MessageTypeAssistant
	MessageTypeThinking
	MessageTypeToolCall
	MessageTypeToolResult
	MessageTypeSystem
)

// Message 表示会话区域中的一条消息
type Message struct {
	Type      MessageType
	Content   string // display text (for tool results, this is Details from ToolResult)
	Timestamp time.Time
	// 工具调用相关字段
	ToolName string
	ToolArgs map[string]interface{}
	Success  bool // 对于 ToolResult，表示是否成功
}

// Bubbletea 消息类型（事件）

// windowSizeMsg 窗口大小变化事件
type windowSizeMsg struct {
	width, height int
}

// agentStartMsg Agent 开始执行事件
type agentStartMsg struct{}

// agentStepMsg Agent 执行步骤更新事件
type agentStepMsg struct {
	step     int
	maxSteps int
}

// agentThinkingMsg Agent 思考内容事件
type agentThinkingMsg struct {
	content string
}

// agentResponseMsg Agent 响应内容事件
type agentResponseMsg struct {
	content string
}

// agentToolCallMsg Agent 工具调用事件
type agentToolCallMsg struct {
	toolName string
	args     map[string]interface{}
}

// agentToolResultMsg Agent 工具结果事件
type agentToolResultMsg struct {
	toolName string
	success  bool
	details  string // UI display text (Content is LLM-only, not sent to TUI)
}

// agentCompleteMsg Agent 完成执行事件
type agentCompleteMsg struct {
	result string
}

// agentErrorMsg Agent 执行错误事件
type agentErrorMsg struct {
	err error
}

// userMessageInjectedMsg 用户消息注入事件（从 pending 队列注入到 Agent）
type userMessageInjectedMsg struct {
	content string
}

// 流式响应事件（新增）

// agentContentDeltaMsg Agent 内容增量事件（流式）
type agentContentDeltaMsg struct {
	delta   string
	isFirst bool
}

// agentThinkingDeltaMsg Agent 思考增量事件（流式）
type agentThinkingDeltaMsg struct {
	delta   string
	isFirst bool
}

// agentStreamCompleteMsg Agent 流式响应完成事件
type agentStreamCompleteMsg struct {
	finalContent string
}

// todoUpdateMsg Todo 列表更新事件
type todoUpdateMsg struct {
	todos []interface{} // TodoItem from tools package
}

// Web 远程控制消息类型

// webInputMsg Web 用户输入事件
type webInputMsg struct {
	content string
}

// webCommandMsg Web 命令事件
type webCommandMsg struct {
	name string
}

// webConnectedMsg Web 连接建立事件
type webConnectedMsg struct{}

// webDisconnectedMsg Web 连接断开事件
type webDisconnectedMsg struct{}
