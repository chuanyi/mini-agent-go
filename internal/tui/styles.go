package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// 定义颜色
var (
	colorPrimary   = lipgloss.Color("39")  // 蓝色
	colorSuccess   = lipgloss.Color("42")  // 绿色
	colorWarning   = lipgloss.Color("220") // 黄色
	colorError     = lipgloss.Color("196") // 红色
	colorSecondary = lipgloss.Color("243") // 灰色
	colorAccent    = lipgloss.Color("208") // 橙色
	colorPurple    = lipgloss.Color("62")  // 紫色
	colorBgDark    = lipgloss.Color("235") // 深灰背景
	colorBgMedium  = lipgloss.Color("236") // 中灰背景

	// Todo 状态颜色
	colorTodoCompleted  = lipgloss.Color("243") // 灰色 - completed
	colorTodoInProgress = lipgloss.Color("42")  // 绿色 - in_progress
	colorTodoNext       = lipgloss.Color("141") // 紫色 - next pending
	colorTodoPending    = lipgloss.Color("250") // 浅灰色 - other pending
)

// 样式定义
var (
	// 标题栏样式
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary).
			Background(colorBgDark).
			Padding(0, 1)

	// 状态栏样式
	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorWarning).
			Background(colorBgMedium).
			Padding(0, 1)

	// 输入框样式
	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Padding(0, 0)

	// 消息样式
	userStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	assistantStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSuccess)

	thinkingStyle = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Italic(true)

	toolCallStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent)

	toolResultStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	toolErrorStyle = lipgloss.NewStyle().
			Foreground(colorError)

	systemStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(colorSecondary)

	// 帮助文本样式
	helpStyle = lipgloss.NewStyle().
			Foreground(colorSecondary)

	// Todo 样式
	todoCompletedStyle = lipgloss.NewStyle().
				Foreground(colorTodoCompleted).
				Strikethrough(true)

	todoInProgressStyle = lipgloss.NewStyle().
				Foreground(colorTodoInProgress).
				Bold(true)

	todoNextStyle = lipgloss.NewStyle().
			Foreground(colorTodoNext).
			Bold(true)

	todoPendingStyle = lipgloss.NewStyle().
				Foreground(colorTodoPending)

	todoBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorSecondary).
			Padding(0, 1)
)
