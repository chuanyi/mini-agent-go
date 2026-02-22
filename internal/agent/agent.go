package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mini-agent-go/internal/llm"
	"mini-agent-go/internal/schema"
	"mini-agent-go/internal/storage"
	"mini-agent-go/internal/tools"
	"mini-agent-go/internal/utils"
)

// ResolveAgentDir resolves the .agent/ directory path.
// Priority: exe directory first, then CWD as fallback.
func ResolveAgentDir() string {
	subPath := ".agent"

	// Try executable directory first
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidate := filepath.Join(exeDir, subPath)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}

	// Fallback to CWD
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, subPath)
	}

	return subPath
}

// ResolveExternalToolsDir resolves the .agent/tools directory path.
func ResolveExternalToolsDir() string {
	return filepath.Join(ResolveAgentDir(), "tools")
}

// Agent represents the main agent that orchestrates LLM calls and tool execution
type Agent struct {
	model        llm.Client
	toolRegistry *tools.ToolRegistry
	maxSteps     int
	tokenLimit   int
	workspace    string
	todoStorage  *storage.TodoStorage
	logger       *utils.Logger
	systemPrompt string
	messages     []*Message
	lastInputTokens        int    // last LLM-reported input token count
	lastSummarizedStep     int    // step number of last successful summarization (-1 = none)
	lastCompactionSummary  string // previous compaction summary for incremental updates
	keepRecentTokens       int    // tokens to preserve at tail (default 20000)
	skillLoader            *tools.SkillLoader

	// Pending user messages buffer (for runtime message injection)
	pendingMessages    []string
	pendingMessagesMux sync.Mutex

	// Event channel for notifying TUI about updates
	eventCh chan<- interface{}
}

// Option is a function that configures the Agent
type Option func(*Agent)

// WithMaxSteps sets the maximum number of steps
func WithMaxSteps(maxSteps int) Option {
	return func(a *Agent) {
		a.maxSteps = maxSteps
	}
}

// WithTokenLimit sets the token limit for summarization
func WithTokenLimit(tokenLimit int) Option {
	return func(a *Agent) {
		a.tokenLimit = tokenLimit
	}
}

// WithWorkspace sets the workspace directory
func WithWorkspace(workspace string) Option {
	return func(a *Agent) {
		a.workspace = workspace
	}
}

// WithSystemPrompt sets the system prompt
func WithSystemPrompt(prompt string) Option {
	return func(a *Agent) {
		a.systemPrompt = prompt
	}
}

// WithSkillLoader sets the skill loader
func WithSkillLoader(loader *tools.SkillLoader) Option {
	return func(a *Agent) {
		a.skillLoader = loader
	}
}

// WithEventCh sets the event channel for TUI updates
func WithEventCh(eventCh chan<- interface{}) Option {
	return func(a *Agent) {
		a.eventCh = eventCh
	}
}

// New creates a new Agent instance
func New(model llm.Client, options ...Option) *Agent {
	a := &Agent{
		model:              model,
		toolRegistry:       tools.NewToolRegistry(),
		maxSteps:           200,
		tokenLimit:         80000,
		workspace:          "", // Must be set via WithWorkspace option
		todoStorage:        storage.NewTodoStorage(),
		logger:             utils.NewLogger(),
		messages:           make([]*Message, 0),
		lastSummarizedStep: -1,
		keepRecentTokens:   20000,
	}

	// Apply options
	for _, opt := range options {
		opt(a)
	}

	// Set log directory to .agent/logs/
	a.logger.SetLogDir(filepath.Join(ResolveAgentDir(), "logs"))

	// Inject skills metadata into system prompt if skill loader is provided
	if a.skillLoader != nil {
		skillsMetadata := a.skillLoader.GetSkillsMetadataPrompt()
		if skillsMetadata != "" {
			a.systemPrompt = a.systemPrompt + "\n\n" + skillsMetadata
		}
	}

	// Inject todo tool behavioral guidance into system prompt
	if a.systemPrompt != "" {
		a.systemPrompt = a.systemPrompt + "\n\n" + tools.TodoPrompt
	}

	// Inject workspace and OS information into system prompt
	if a.systemPrompt != "" {
		osInfo := getOSInfo()
		now := time.Now()
		contextInfo := fmt.Sprintf("\n\n## Current Environment\n"+
			"**Current Date/Time**: %s (%s)\n"+
			"**Operating System**: %s (%s)\n"+
			"**Workspace Directory**: `%s`\n\n"+
			"All relative paths will be resolved relative to the workspace directory.\n"+
			"This system uses a cross-platform POSIX shell (bash tool). Use forward slashes `/` for all paths.",
			now.Format("2006-01-02 15:04:05"), now.Format("Monday"),
			osInfo.Name, osInfo.GOOS, a.workspace)

		a.systemPrompt = a.systemPrompt + contextInfo
	}

	// Initialize with system message
	if a.systemPrompt != "" {
		a.messages = append(a.messages, NewSystemMessage(a.systemPrompt))
	}

	return a
}

// Close releases resources held by the agent (log file handles, etc.).
func (a *Agent) Close() {
	a.logger.Close()
}

// RegisterTool registers a tool with the agent
func (a *Agent) RegisterTool(tool tools.Tool) {
	a.toolRegistry.Register(tool)
}

// UnregisterTool removes a tool by name
func (a *Agent) UnregisterTool(name string) {
	a.toolRegistry.Unregister(name)
}

// LoadExternalTools discovers and loads external tool definitions from the given directory.
// toolsDir should be the .agent/tools path relative to the executable location.
func (a *Agent) LoadExternalTools(toolsDir string) {
	loader := tools.NewExternalToolLoader(toolsDir, a.workspace)

	// Collect existing tool names to detect conflicts
	existingNames := make(map[string]bool)
	for _, t := range a.toolRegistry.List() {
		existingNames[t.Name()] = true
	}

	loadedTools, _ := loader.LoadTools(existingNames)
	for _, t := range loadedTools {
		a.toolRegistry.Register(t)
	}
}

// RegisterDefaultTools registers all default tools
func (a *Agent) RegisterDefaultTools() {
	// Register file tools
	a.RegisterTool(tools.NewLSTool(a.workspace))
	a.RegisterTool(tools.NewReadTool(a.workspace))
	a.RegisterTool(tools.NewWriteTool(a.workspace))
	a.RegisterTool(tools.NewEditTool(a.workspace))
	a.RegisterTool(tools.NewMultiEditTool(a.workspace))
	a.RegisterTool(tools.NewGrepTool(a.workspace))
	a.RegisterTool(tools.NewFindTool(a.workspace))

	// Register bash tool
	a.RegisterTool(tools.NewBashTool(a.workspace))

	// Register note tools
	memoryFile := filepath.Join(ResolveAgentDir(), "memory.json")
	a.RegisterTool(tools.NewRecordNoteTool(memoryFile))
	a.RegisterTool(tools.NewRecallNoteTool(memoryFile))

	// Register web tools
	//a.RegisterTool(tools.NewWebSearchTool())
	a.RegisterTool(tools.NewWebFetchTool())
	a.RegisterTool(tools.NewWebDownloadTool(a.workspace))

	// Register todo tool
	a.RegisterTool(tools.NewTodoTool(a.todoStorage, a.eventCh))
}

// AddUserMessage adds a user message to the conversation
func (a *Agent) AddUserMessage(content string) {
	a.messages = append(a.messages, NewUserMessage(content))
}

// Run executes the agent loop until task is complete or max steps reached (no streaming callbacks).
func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {
	return a.runLoop(ctx, prompt, nil)
}

// RunWithCallback executes the agent loop with streaming and tool callbacks for TUI mode.
func (a *Agent) RunWithCallback(ctx context.Context, prompt string, callback AgentCallback) error {
	_, err := a.runLoop(ctx, prompt, &callback)
	return err
}

// runLoop is the unified execution loop shared by Run and RunWithCallback.
// When callback is nil, no streaming adapter or event callbacks are fired.
func (a *Agent) runLoop(ctx context.Context, prompt string, callback *AgentCallback) (string, error) {
	a.AddUserMessage(prompt)
	a.logger.StartNewRun()

	if len(a.toolRegistry.List()) == 0 {
		a.RegisterDefaultTools()
	}

	for step := 0; step < a.maxSteps; step++ {
		// Inject pending messages (TUI mode only)
		if callback != nil {
			a.injectPendingMessages(*callback)
		}

		// Step callback
		if callback != nil && callback.OnStep != nil {
			callback.OnStep(step+1, a.maxSteps)
		}

		// Check and summarize message history
		if err := a.maybeSummarize(ctx, step); err != nil {
			return "", fmt.Errorf("summarization failed: %w", err)
		}

		// Log LLM request
		toolList := a.toolRegistry.List()
		a.logger.LogRequest(a.convertMessages(a.messages), a.convertTools(toolList))

		// Build streaming adapter if callback provided
		var streamCB llm.StreamCallback
		if callback != nil {
			streamCB = newAgentStreamAdapter(*callback)
		}

		// Call LLM
		toolSchemas := a.toolRegistry.GetSchemas()
		response, err := a.model.GenerateStream(ctx, a.toSchemaMessages(), toolSchemas, streamCB)
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		// Update token usage (use input tokens for accurate threshold)
		if response.Usage != nil {
			a.lastInputTokens = response.Usage.InputTokens
		}

		// Log LLM response
		a.logger.LogResponse(a.convertResponse(response))

		// Stream complete callback
		if callback != nil && callback.OnStreamComplete != nil {
			callback.OnStreamComplete(response.Content)
		}

		// Create assistant message
		assistantMsg := NewAssistantMessage(response.Content, response.Thinking, response.ToolCalls)
		a.messages = append(a.messages, assistantMsg)

		// Check if task is complete (no tool calls)
		if len(response.ToolCalls) == 0 {
			return response.Content, nil
		}

		// Execute tool calls (with inter-tool steering check)
		for i, toolCall := range response.ToolCalls {
			// Check for pending user messages before each tool execution
			if callback != nil && a.GetPendingMessageCount() > 0 {
				// Inject pending messages
				a.injectPendingMessages(*callback)
				// Add "Skipped" results for remaining tool calls so the LLM knows
				for _, skipped := range response.ToolCalls[i:] {
					toolMsg := NewToolMessage(skipped.Function.Name, "[Skipped: user sent new message]", skipped.ID)
					a.messages = append(a.messages, toolMsg)
				}
				break // immediately proceed to next LLM call
			}

			// Tool call callback
			if callback != nil && callback.OnToolCall != nil {
				callback.OnToolCall(toolCall.Function.Name, toolCall.Function.Arguments)
			}

			result, err := a.executeTool(ctx, toolCall)
			if err != nil {
				errMsg := fmt.Sprintf("Tool execution failed: %v", err)
				if callback != nil && callback.OnToolResult != nil {
					callback.OnToolResult(toolCall.Function.Name, false, errMsg)
				}
				return "", fmt.Errorf("tool execution failed: %w", err)
			}

			// Tool result callback (send Details only — Content is for LLM only)
			if callback != nil && callback.OnToolResult != nil {
				callback.OnToolResult(toolCall.Function.Name, result.Success, result.Details)
			}
		}
	}

	return "", fmt.Errorf("task couldn't be completed after %d steps", a.maxSteps)
}

// maybeSummarize checks if summarization is needed and performs it.
// Uses Pi-style cut-point algorithm: accumulate tokens from the tail,
// find a safe cut point (not in the middle of a tool call/result pair),
// compress everything before the cut point into a single structured summary.
func (a *Agent) maybeSummarize(ctx context.Context, currentStep int) error {
	// Skip check on the step immediately following a successful summarization
	if a.lastSummarizedStep >= 0 && currentStep == a.lastSummarizedStep+1 {
		return nil
	}

	// Use LLM-reported input tokens as primary signal, fall back to estimation
	tokenCount := a.lastInputTokens
	if tokenCount == 0 {
		tokenCount = utils.EstimateTokens(a.toSchemaMessages())
	}

	if tokenCount <= a.tokenLimit {
		return nil
	}

	// Find cut point: walk backwards from end, accumulating tokens until >= keepRecentTokens.
	// Only cut at user or assistant boundaries (never split tool call/result pairs).
	cutIdx := -1
	tailTokens := 0
	for i := len(a.messages) - 1; i > 0; i-- {
		msg := a.messages[i]
		tailTokens += utils.EstimateTokens([]*schema.Message{msg.ToSchema()})
		if tailTokens >= a.keepRecentTokens {
			// Walk forward to find a safe boundary (user or assistant, not tool)
			for j := i; j < len(a.messages); j++ {
				if a.messages[j].IsUser() || a.messages[j].IsAssistant() {
					cutIdx = j
					break
				}
			}
			break
		}
	}

	// No valid cut point found or nothing to compress
	if cutIdx <= 1 {
		return nil
	}

	// Messages to compress: everything between system prompt and cut point
	toCompress := a.messages[1:cutIdx]
	if len(toCompress) == 0 {
		return nil
	}

	// Generate structured summary
	summaryText, err := a.createSummary(ctx, toCompress)
	if err != nil {
		return nil // silently skip — don't lose messages on summary failure
	}

	// Track file operations in compressed messages
	fileTracker := a.trackFileOps(toCompress)
	if fileTracker != "" {
		summaryText += "\n\n" + fileTracker
	}

	// Build new message list: [System] + [Summary as User] + [Recent messages]
	newMessages := make([]*Message, 0, 2+len(a.messages)-cutIdx)
	newMessages = append(newMessages, a.messages[0]) // system prompt
	newMessages = append(newMessages, NewUserMessage("[Compaction Summary]\n\n"+summaryText))
	newMessages = append(newMessages, a.messages[cutIdx:]...) // preserved tail

	a.messages = newMessages
	a.lastCompactionSummary = summaryText
	a.lastSummarizedStep = currentStep
	a.lastInputTokens = 0 // reset so next step re-estimates

	return nil
}

// createSummary generates a structured checkpoint summary via a single LLM call.
// When a previous compaction summary exists, uses an incremental UPDATE prompt.
func (a *Agent) createSummary(ctx context.Context, messages []*Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// Serialize messages to compact text for the summary prompt
	var sb strings.Builder
	for _, msg := range messages {
		switch {
		case msg.IsUser():
			sb.WriteString("User: ")
			sb.WriteString(utils.TruncateText(msg.Content, 500))
			sb.WriteString("\n")
		case msg.IsAssistant():
			if msg.Content != "" {
				sb.WriteString("Assistant: ")
				sb.WriteString(utils.TruncateText(msg.Content, 500))
				sb.WriteString("\n")
			}
			for _, tc := range msg.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Function.Arguments)
				sb.WriteString(fmt.Sprintf("  → %s(%s)\n", tc.Function.Name, utils.TruncateText(string(argsJSON), 200)))
			}
		case msg.IsTool():
			sb.WriteString(fmt.Sprintf("  ← [%s]: %s\n", msg.Name, utils.TruncateText(msg.Content, 300)))
		}
	}
	conversation := sb.String()

	var prompt string
	if a.lastCompactionSummary == "" {
		// Initial summary — generate structured checkpoint
		prompt = fmt.Sprintf(`Summarize this conversation into a structured checkpoint. Be concise but preserve all important context.

<conversation>
%s
</conversation>

Use EXACTLY this format:

## Goal
[What the user is trying to accomplish]

## Progress
- [Completed step 1]
- [Completed step 2]

## Key Decisions
- [Important decision or finding]

## Current State
[What was just done or is in progress]

## Next Steps
- [What remains to be done]`, conversation)
	} else {
		// Incremental update — merge new activity into existing summary
		prompt = fmt.Sprintf(`Update the existing summary with new conversation activity. Merge new information into the existing structure. Remove completed next-steps, add new progress.

<existing-summary>
%s
</existing-summary>

<new-activity>
%s
</new-activity>

Output the UPDATED summary using the same format:

## Goal
## Progress
## Key Decisions
## Current State
## Next Steps`, a.lastCompactionSummary, conversation)
	}

	summaryMessages := []*schema.Message{
		{
			Role:    "system",
			Content: "You produce concise, structured summaries of agent execution history. Preserve critical details (file paths, error messages, key values) but omit verbose tool output.",
		},
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, err := a.model.GenerateStream(ctx, summaryMessages, nil, nil)
	if err != nil {
		return "", fmt.Errorf("summary LLM call failed: %w", err)
	}

	return response.Content, nil
}

// trackFileOps scans messages for file-related tool calls and returns a summary.
func (a *Agent) trackFileOps(messages []*Message) string {
	fileTools := map[string]bool{"read": true, "write": true, "edit": true, "multiedit": true}
	// tool name -> set of file paths
	ops := make(map[string]map[string]int)

	for _, msg := range messages {
		if !msg.IsAssistant() {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if !fileTools[tc.Function.Name] {
				continue
			}
			toolName := tc.Function.Name
			if ops[toolName] == nil {
				ops[toolName] = make(map[string]int)
			}
			// Extract file path from arguments
			if path, ok := tc.Function.Arguments["file_path"]; ok {
				if pathStr, ok := path.(string); ok {
					ops[toolName][pathStr]++
				}
			} else if path, ok := tc.Function.Arguments["path"]; ok {
				if pathStr, ok := path.(string); ok {
					ops[toolName][pathStr]++
				}
			}
		}
	}

	if len(ops) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Files Touched")
	for toolName, files := range ops {
		var paths []string
		for path, count := range files {
			if count > 1 {
				paths = append(paths, fmt.Sprintf("%s (%d times)", path, count))
			} else {
				paths = append(paths, path)
			}
		}
		sb.WriteString(fmt.Sprintf("\n- %s: %s", toolName, strings.Join(paths, ", ")))
	}
	return sb.String()
}

// GetToolNames returns the list of registered tool names (exported for diagnostics).
func (a *Agent) GetToolNames() []string {
	tools := a.toolRegistry.List()
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	return names
}

// executeTool executes a tool call, logs the result, and appends the tool message.
func (a *Agent) executeTool(ctx context.Context, toolCall schema.ToolCall) (*tools.ToolResult, error) {
	var result *tools.ToolResult
	tool, exists := a.toolRegistry.Get(toolCall.Function.Name)
	if !exists {
		availableTools := a.GetToolNames()
		errorMsg := fmt.Sprintf("Error: Unknown tool '%s'.\n\nAvailable tools: %v\n\nThis usually means the tool was not registered. Please check the tool registration in your code.",
			toolCall.Function.Name,
			availableTools)
		result = tools.ErrorResult(errorMsg)
	} else {
		var err error
		result, err = tool.Execute(ctx, toolCall.Function.Arguments)
		if err != nil {
			result = tools.ErrorResult(fmt.Sprintf("Tool execution failed: %v", err))
		}
	}

	// Log tool execution result
	liteResult := &utils.ToolResultLite{
		Success: result.Success,
		Content: result.Content,
		Details: result.Details,
		Error:   result.Error,
	}
	a.logger.LogToolResult(toolCall.Function.Name, toolCall.Function.Arguments, liteResult)

	// Add tool result message — use error as content when tool failed
	content := result.Content
	if !result.Success && content == "" {
		content = "Error: " + result.Error
	}
	toolMsg := NewToolMessage(toolCall.Function.Name, content, toolCall.ID)
	a.messages = append(a.messages, toolMsg)

	return result, nil
}

// GetHistory returns the message history
func (a *Agent) GetHistory() []*Message {
	return append([]*Message(nil), a.messages...)
}

// ClearHistory clears the conversation history and resets token counter,
// preserving the system prompt as the first message.
func (a *Agent) ClearHistory() {
	a.messages = nil
	if a.systemPrompt != "" {
		a.messages = append(a.messages, NewSystemMessage(a.systemPrompt))
	}
	a.lastInputTokens = 0
	a.lastCompactionSummary = ""
}

// GetStats returns agent statistics
func (a *Agent) GetStats() map[string]interface{} {
	userCount := 0
	assistantCount := 0
	toolCount := 0

	for _, msg := range a.messages {
		switch msg.Role {
		case "user":
			userCount++
		case "assistant":
			assistantCount++
		case "tool":
			toolCount++
		}
	}

	return map[string]interface{}{
		"total_messages": len(a.messages),
		"user_messages":  userCount,
		"assistant_msgs": assistantCount,
		"tool_calls":     toolCount,
		"tools_count":    len(a.toolRegistry.List()),
		"api_tokens":     a.lastInputTokens,
	}
}

// convertMessages converts agent messages to lite version for logging
func (a *Agent) convertMessages(messages []*Message) []utils.MessageLite {
	liteMessages := make([]utils.MessageLite, len(messages))
	for i, msg := range messages {
		liteMessages[i] = utils.MessageLite{
			Role:      msg.Role,
			Content:   msg.Content,
			ToolCalls: msg.ToolCalls,
		}
	}
	return liteMessages
}

// convertTools converts tools to lite version for logging
func (a *Agent) convertTools(tools []tools.Tool) []utils.ToolLite {
	liteTools := make([]utils.ToolLite, len(tools))
	for i, tool := range tools {
		liteTools[i] = utils.ToolLite{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		}
	}
	return liteTools
}

// convertResponse converts LLM response to lite version for logging
func (a *Agent) convertResponse(response *schema.LLMResponse) *utils.LLMResponseLite {
	liteUsage := (*utils.UsageLite)(nil)
	if response.Usage != nil {
		liteUsage = &utils.UsageLite{
			InputTokens:      response.Usage.InputTokens,
			OutputTokens:     response.Usage.OutputTokens,
			TotalTokens:      response.Usage.TotalTokens,
			PromptTokens:     response.Usage.PromptTokens,
			CompletionTokens: response.Usage.CompletionTokens,
		}
	}

	return &utils.LLMResponseLite{
		Content:      response.Content,
		Thinking:     response.Thinking,
		ToolCalls:    response.ToolCalls,
		FinishReason: response.FinishReason,
		Usage:        liteUsage,
	}
}

// toSchemaMessages converts agent messages to schema messages
func (a *Agent) toSchemaMessages() []*schema.Message {
	schemaMessages := make([]*schema.Message, len(a.messages))
	for i, msg := range a.messages {
		schemaMessages[i] = msg.ToSchema()
	}
	return schemaMessages
}

// OSInfo contains operating system information
type OSInfo struct {
	Name string
	GOOS string
}

// getOSInfo returns information about the current operating system
func getOSInfo() OSInfo {
	goos := runtime.GOOS
	osName := goos

	// Map GOOS to friendly names
	switch goos {
	case "windows":
		osName = "Windows"
	case "darwin":
		osName = "macOS"
	case "linux":
		osName = "Linux"
	}

	return OSInfo{
		Name: osName,
		GOOS: goos,
	}
}

// AppendUserMessage appends a user message to the pending buffer (thread-safe)
// This is called by TUI when Agent is running to queue messages for injection
func (a *Agent) AppendUserMessage(content string) {
	a.pendingMessagesMux.Lock()
	defer a.pendingMessagesMux.Unlock()
	a.pendingMessages = append(a.pendingMessages, content)
}

// GetPendingMessageCount returns the count of pending messages (thread-safe)
func (a *Agent) GetPendingMessageCount() int {
	a.pendingMessagesMux.Lock()
	defer a.pendingMessagesMux.Unlock()
	return len(a.pendingMessages)
}

// GetTodoStorage returns the todo storage instance
func (a *Agent) GetTodoStorage() *storage.TodoStorage {
	return a.todoStorage
}
