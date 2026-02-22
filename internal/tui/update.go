package tui

import (
	"fmt"
	"strings"

	"mini-agent-go/internal/storage"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// Update 处理消息并更新 Model（Bubbletea 必需方法）
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	// 窗口大小变化
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		if !m.ready {
			// 首次初始化组件大小
			statusHeightAdjust := 0
			if m.isRunning {
				statusHeightAdjust = statusHeight
			}
			pendingHeightAdjust := m.getPendingHeight()
			todoHeightAdjust := m.getTodoHeight()
			viewportHeight := msg.Height - headerHeight - getInputHeight() - footerHeight - statusHeightAdjust - pendingHeightAdjust - todoHeightAdjust + 1
			m.viewport = viewport.New(msg.Width, viewportHeight)
			// 移除绝对定位，使用流式布局避免滚动时渲染冲突
			// m.viewport.YPosition = headerHeight
			m.ready = true

			// 显示欢迎消息
			m.addSystemMessage(fmt.Sprintf("Welcome to MiniK! Model: %s  Tools: %d", m.config.LLM.Model, m.GetToolCount()))
			m.addSystemMessage("Type your message and press Enter to submit. Type /help for available commands.")

			// Display any startup errors/warnings collected before TUI started
			for _, msg := range m.startupMessages {
				m.addSystemMessage(msg)
			}
			m.startupMessages = nil

			// 如果配置了启动时显示 QR code，则显示
			if m.ShouldShowQRCodeOnStart() {
				m.showQRCode()
			}
		} else {
			m.viewport.Width = msg.Width
			statusHeightAdjust := 0
			if m.isRunning {
				statusHeightAdjust = statusHeight
			}
			pendingHeightAdjust := m.getPendingHeight()
			todoHeightAdjust := m.getTodoHeight()
			m.viewport.Height = msg.Height - headerHeight - getInputHeight() - footerHeight - statusHeightAdjust - pendingHeightAdjust - todoHeightAdjust + 1

			// 窗口大小改变时，重新渲染所有消息以适应新宽度
			content := m.renderMessages()
			m.viewport.SetContent(content)
		}

		m.textarea.SetWidth(msg.Width - 2)

	// 键盘事件
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEsc:
			// ESC: Cancel Agent execution if running, otherwise quit
			if m.isRunning && m.cancelFunc != nil {
				m.cancelFunc()
				m.addSystemMessage("⚠️ Execution cancelled by user")
				m.isRunning = false
				m.statusText = ""
				// Clear pending queue
				m.pendingMutex.Lock()
				m.pendingUserMessages = nil
				m.pendingMutex.Unlock()
				// Clear todo list (execution cancelled)
				m.todosMutex.Lock()
				m.todos = nil
				m.todosMutex.Unlock()
				// Adjust viewport height (status bar, pending bar, and todo list disappear)
				pendingHeightAdjust := m.getPendingHeight()
				todoHeightAdjust := m.getTodoHeight()
				m.viewport.Height = m.height - headerHeight - getInputHeight() - footerHeight - pendingHeightAdjust - todoHeightAdjust + 1
			} else {
				return m, tea.Quit
			}

		case tea.KeyEnter:
			// 提交输入
			return m.handleSubmit()
		}

	// Agent 执行事件
	case agentStartMsg:
		m.isRunning = true
		m.addSystemMessage("Agent started...")

		// 推送状态到 Web
		m.pushStatusToWeb(true, "Agent started")

		// 清空 todo 列表（新的执行开始）
		m.todosMutex.Lock()
		m.todos = nil
		m.todosMutex.Unlock()

		// 调整 viewport 高度（因为状态栏出现，todo 列表被清空）
		pendingHeightAdjust := m.getPendingHeight()
		todoHeightAdjust := m.getTodoHeight()
		m.viewport.Height = m.height - headerHeight - getInputHeight() - footerHeight - statusHeight - pendingHeightAdjust - todoHeightAdjust + 1

	case agentStepMsg:
		m.currentStep = msg.step
		m.statusText = fmt.Sprintf("Step %d/%d", msg.step, msg.maxSteps)
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentThinkingMsg:
		m.addMessage(Message{
			Type:    MessageTypeThinking,
			Content: msg.content,
		})
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentResponseMsg:
		m.addMessage(Message{
			Type:    MessageTypeAssistant,
			Content: msg.content,
		})
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentToolCallMsg:
		m.addMessage(Message{
			Type:     MessageTypeToolCall,
			ToolName: msg.toolName,
			ToolArgs: msg.args,
		})
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentToolResultMsg:
		m.addMessage(Message{
			Type:     MessageTypeToolResult,
			ToolName: msg.toolName,
			Success:  msg.success,
			Content:  msg.details,
		})
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentCompleteMsg:
		m.isRunning = false
		m.statusText = ""
		m.addSystemMessage("✅ Task completed")

		// 推送状态到 Web
		m.pushStatusToWeb(false, "")

		// 检查是否所有 todos 都已完成，如果是则清空列表
		m.todosMutex.Lock()
		allCompleted := true
		if len(m.todos) > 0 {
			for _, todo := range m.todos {
				if todo.Status != "completed" {
					allCompleted = false
					break
				}
			}
			if allCompleted {
				// 所有任务都完成了，清空列表
				m.todos = nil
			}
		}
		m.todosMutex.Unlock()

		// 推送 todos 更新到 Web
		m.pushTodosToWeb()

		// 调整 viewport 高度（因为状态栏消失，todo 可能被清空）
		pendingHeightAdjust := m.getPendingHeight()
		todoHeightAdjust := m.getTodoHeight()
		m.viewport.Height = m.height - headerHeight - getInputHeight() - footerHeight - pendingHeightAdjust - todoHeightAdjust + 1
		// 不再监听事件

	case agentErrorMsg:
		m.isRunning = false
		m.err = msg.err
		m.addSystemMessage(fmt.Sprintf("❌ Error: %v", msg.err))

		// 推送状态到 Web
		m.pushStatusToWeb(false, "")

		// 清空 todo 列表（任务失败，清空旧的 todos）
		m.todosMutex.Lock()
		m.todos = nil
		m.todosMutex.Unlock()

		// 推送 todos 更新到 Web
		m.pushTodosToWeb()

		// 调整 viewport 高度（因为状态栏消失，todo 列表被清空）
		pendingHeightAdjust := m.getPendingHeight()
		todoHeightAdjust := m.getTodoHeight()
		m.viewport.Height = m.height - headerHeight - getInputHeight() - footerHeight - pendingHeightAdjust - todoHeightAdjust + 1
		// 不再监听事件

	// 流式事件处理（新增）
	case agentContentDeltaMsg:
		if msg.isFirst {
			// 首个chunk：创建新的Assistant消息
			m.addMessage(Message{
				Type:    MessageTypeAssistant,
				Content: msg.delta,
			})
			m.currentStreamingIndex = len(m.messages) - 1
			m.streamingContentBuf.Reset()
			m.streamingContentBuf.WriteString(msg.delta)
		} else {
			// 后续chunk：累积并原地更新
			m.streamingContentBuf.WriteString(msg.delta)
			m.updateLastMessageWithDebounce(m.streamingContentBuf.String())
		}
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentThinkingDeltaMsg:
		if msg.isFirst {
			// 首个chunk：创建新的Thinking消息
			m.addMessage(Message{
				Type:    MessageTypeThinking,
				Content: msg.delta,
			})
			m.streamingThinkingBuf.Reset()
			m.streamingThinkingBuf.WriteString(msg.delta)
		} else {
			// 后续chunk：累积并原地更新
			m.streamingThinkingBuf.WriteString(msg.delta)
			m.updateLastMessageWithDebounce(m.streamingThinkingBuf.String())
		}
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case agentStreamCompleteMsg:
		// 流式完成：强制最后一次渲染（不带防抖）
		if m.currentStreamingIndex >= 0 && m.currentStreamingIndex < len(m.messages) {
			m.updateLastMessage(m.streamingContentBuf.String())
		}
		// 清理流式状态
		m.currentStreamingIndex = -1
		m.streamingContentBuf.Reset()
		m.streamingThinkingBuf.Reset()
		// 继续监听下一个事件
		return m, waitForAgentEvent(m.agentEventCh)

	case userMessageInjectedMsg:
		// User message was injected from pending queue into Agent
		// Remove from pending queue
		m.pendingMutex.Lock()
		if len(m.pendingUserMessages) > 0 {
			m.pendingUserMessages = m.pendingUserMessages[1:]
		}
		m.pendingMutex.Unlock()
		// Continue listening
		return m, waitForAgentEvent(m.agentEventCh)

	case todoUpdateMsg:
		// Todo list update event from agent
		m.todosMutex.Lock()
		// Convert interface{} to TodoItem
		m.todos = make([]storage.TodoItem, len(msg.todos))
		for i, todo := range msg.todos {
			if todoItem, ok := todo.(storage.TodoItem); ok {
				m.todos[i] = todoItem
			}
		}
		m.todosMutex.Unlock()

		// Push to Web client
		m.pushTodosToWeb()

		// Adjust viewport height if todo list size changed
		statusHeightAdjust := 0
		if m.isRunning {
			statusHeightAdjust = statusHeight
		}
		pendingHeightAdjust := m.getPendingHeight()
		todoHeightAdjust := m.getTodoHeight()
		m.viewport.Height = m.height - headerHeight - getInputHeight() - footerHeight - statusHeightAdjust - pendingHeightAdjust - todoHeightAdjust + 1

		// Continue listening if agent is running
		if m.isRunning {
			return m, waitForAgentEvent(m.agentEventCh)
		}

	// Web 远程控制消息处理
	case webInputMsg:
		// Web 用户输入 - 设置输入框内容并提交（与键盘输入一致）
		m.textarea.SetValue(msg.content)
		return m.handleSubmit()

	case webCommandMsg:
		// Web 命令 - 确保带有 / 前缀
		cmdName := msg.name
		if !strings.HasPrefix(cmdName, "/") {
			cmdName = "/" + cmdName
		}
		return m.handleCommand(cmdName)

	case webConnectedMsg:
		// Web 客户端连接
		m.addSystemMessage("📱 Web client connected")
		// 发送当前状态快照到 Web（在 Update 中调用以访问正确的 Model）
		m.sendSnapshotToWeb()

	case webDisconnectedMsg:
		// Web 客户端断开
		m.addSystemMessage("📱 Web client disconnected")
	}

	// 更新子组件 - textarea 应该始终接收输入（即使 Agent 运行中）
	m.textarea, cmd = m.textarea.Update(msg)
	cmds = append(cmds, cmd)

	// viewport 更新：过滤掉普通字符输入，避免在输入框输入时触发 viewport 滚动
	shouldUpdateViewport := true
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		// 如果是普通字符输入（KeyRunes），不传给 viewport
		// 保留所有功能键（方向键、翻页键等）的传递
		if keyMsg.Type == tea.KeyRunes {
			shouldUpdateViewport = false
		}
	}

	if shouldUpdateViewport {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// handleSubmit 处理用户提交输入
func (m Model) handleSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" {
		return m, nil
	}

	// 添加用户消息到会话
	m.addMessage(Message{
		Type:    MessageTypeUser,
		Content: input,
	})

	// 清空输入框
	m.textarea.Reset()

	// 处理命令 (支持 /command 或 command 格式)
	if isCommand(input) {
		return m.handleCommand(input)
	}

	if m.isRunning {
		// Agent is running: add to pending queue
		m.pendingMutex.Lock()
		m.pendingUserMessages = append(m.pendingUserMessages, input)
		m.pendingMutex.Unlock()

		// Notify Agent that new message is available
		m.agent.AppendUserMessage(input)

		return m, nil
	}

	// Agent not running: start execution
	return m, m.runAgent(input)
}

// isCommand 检查输入是否为命令（支持带或不带 '/' 前缀）
func isCommand(input string) bool {
	// Commands with '/' prefix
	if strings.HasPrefix(input, "/") {
		return true
	}

	// Commands without '/' prefix
	switch input {
	case "help", "clear", "stats", "history", "prompt", "quit", "exit":
		return true
	}

	return false
}

// waitForActivity 等待 Agent 执行事件
func waitForActivity(sub <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}
