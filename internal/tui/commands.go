package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	qrcode "github.com/skip2/go-qrcode"
)

// handleCommand 处理用户输入的命令
func (m Model) handleCommand(input string) (tea.Model, tea.Cmd) {
	switch input {
	case "/help", "help":
		m.showHelp()
		return m, nil

	case "/clear", "clear":
		m.clearHistory()
		return m, nil

	case "/stats", "stats":
		m.showStats()
		return m, nil

	case "/history", "history":
		m.showHistory()
		return m, nil

	case "/prompt", "prompt":
		m.showPrompt()
		return m, nil

	case "/qrcode", "qrcode":
		m.showQRCode()
		return m, nil

	case "/quit", "/exit", "quit", "exit":
		return m, tea.Quit

	default:
		m.addSystemMessage(fmt.Sprintf("Unknown command: %s. Type /help for available commands.", input))
		return m, nil
	}
}

// showHelp 显示帮助信息
func (m *Model) showHelp() {
	helpText := `Available Commands:
  /help      - Show this help message
  /clear     - Clear session history
  /history   - Show current session message count
  /stats     - Show session statistics
  /prompt    - Show current system prompt
  /qrcode    - Show QR code for project URL
  /quit      - Exit program (also: /exit)

Usage:
  - Enter your task directly, Agent will help you complete it
  - Agent remembers all conversation content in this session
  - Use /clear to start a new session
  - Press Enter to submit your message`

	m.addSystemMessage(helpText)
}

// clearHistory 清除会话历史
func (m *Model) clearHistory() {
	if m.agent == nil {
		m.addSystemMessage("Agent not initialized")
		return
	}

	oldCount := len(m.agent.GetHistory())

	// Clear agent conversation history (keep tools, config, event channel intact)
	m.agent.ClearHistory()

	// Clear UI messages
	m.messages = []Message{}
	m.viewport.SetContent("")

	// Push clear event to Web
	m.pushClearToWeb()

	m.addSystemMessage(fmt.Sprintf("Cleared %d messages from history", oldCount-1))
}

// showStats 显示会话统计
func (m *Model) showStats() {
	if m.agent == nil {
		m.addSystemMessage("Agent not initialized")
		return
	}

	stats := m.agent.GetStats()
	statsText := fmt.Sprintf(`Session Statistics:
  Total Messages: %v
  User Messages: %v
  Assistant Replies: %v
  Tool Calls: %v
  Available Tools: %v
  API Tokens: %v`,
		stats["total_messages"],
		stats["user_messages"],
		stats["assistant_msgs"],
		stats["tool_calls"],
		stats["tools_count"],
		stats["api_tokens"])

	m.addSystemMessage(statsText)
}

// showHistory 显示历史消息数量
func (m *Model) showHistory() {
	if m.agent == nil {
		m.addSystemMessage("Agent not initialized")
		return
	}

	history := m.agent.GetHistory()
	m.addSystemMessage(fmt.Sprintf("Current session message count: %d", len(history)))
}

// showPrompt 显示当前系统提示词
func (m *Model) showPrompt() {
	if m.systemPrompt == "" {
		m.addSystemMessage("No system prompt configured")
		return
	}

	// 限制显示长度
	prompt := m.systemPrompt
	if len(prompt) > 1000 {
		prompt = prompt[:1000] + "\n... (truncated)"
	}

	m.addSystemMessage(fmt.Sprintf("=== System Prompt ===\n%s\n===================", prompt))
}

// showQRCode 显示 Web 控制 URL 的 QR code（如果启用），否则显示项目 URL
func (m *Model) showQRCode() {
	var url string
	var description string

	// 如果 Web 控制启用，显示 Web 控制 URL
	if m.web != nil && m.web.enabled {
		url = m.GetWebURL()
		description = "Scan to open Web remote control"
	} else {
		url = "https://github.com/anthropics/claude-code"
		description = "Scan to visit project page"
	}

	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		m.addSystemMessage(fmt.Sprintf("Failed to generate QR code: %v", err))
		return
	}

	text := qr.ToSmallString(false)
	m.addSystemMessage(fmt.Sprintf("=== QR Code ===\n%s\n%s: %s", text, description, url))
}
