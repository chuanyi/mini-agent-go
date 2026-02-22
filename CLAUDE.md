# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Mini Agent is a Go-based AI agent framework that implements a complete agent execution loop with tool calling, persistent memory, intelligent context management, and token summarization. It supports both Anthropic and OpenAI-compatible APIs.

## Build & Development Commands

### Build
```bash
go mod tidy
go build -o mini-agent-go ./cmd
# or on Windows: go build -o mini-agent-go.exe ./cmd
```

**Note**: After making changes, only verify compilation passes (`go build`). Do NOT run `go test` unless explicitly requested.

### Run
```bash
# Run with default workspace
./mini-agent-go

# Run with custom workspace
./mini-agent-go /path/to/workspace
```

### Test
```bash
# Run all tests
go test ./...

# Run tests for specific package
go test ./internal/tools -v
go test ./internal/agent -v

# Run specific test
go test ./internal/tools -run TestSkill -v
```

## Architecture Overview

### Core Agent Loop (internal/agent/agent.go)

The `Agent` struct orchestrates the entire execution loop:
- **Messages**: Maintains conversation history with system, user, assistant, and tool messages
- **Token Management**: Estimates token usage and triggers summarization when approaching limits (default: 80,000 tokens)
- **Summarization**: When token limit is approached, summarizes past execution rounds while preserving system prompt and recent context
- **Tool Execution**: Executes tools sequentially, adding tool results back to conversation
- **Max Steps**: Prevents infinite loops with configurable step limit (default: 50)

Key methods:
- `Run(ctx, prompt)`: Main execution loop - adds user message, iterates through LLM calls and tool executions
- `maybeSummarize(ctx)`: Checks token usage and performs multi-round summarization if needed
- `executeTool(ctx, toolCall)`: Executes a single tool and adds result to messages

### LLM Abstraction (internal/llm/)

The `Client` interface abstracts LLM providers:
- **client.go**: Defines `Client` interface with `Generate()` method
- **anthropic.go**: Anthropic-compatible client (used for MiniMax M2 and Claude)
- **openai.go**: OpenAI-compatible client

Both implementations handle:
- Message format conversion (internal schema → provider format)
- Tool schema conversion (unified schema → provider-specific format)
- Response parsing (provider format → internal schema)
- Retry logic with exponential backoff

### Tool System (internal/tools/)

**Tool Interface** (base.go):
```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]interface{}
    Execute(ctx, params) (*ToolResult, error)
    ToSchema() map[string]interface{}      // Anthropic format
    ToOpenAISchema() map[string]interface{} // OpenAI format
}
```

**Tool Registry**:
- Centralized tool management (`ToolRegistry`)
- Converts tools to provider-specific schemas
- Retrieves tools by name during execution

**Default Tools**:
- **File Tools**: `ls_tool`, `read_tool`, `write_tool`, `edit_tool`, `multiedit_tool`
- **Bash Tools**: `bash_tool` (execute), `bash_output_tool` (read output), `bash_kill_tool` (kill process)
- **Note Tools**: `record_note_tool`, `recall_note_tool` (persistent memory in `.agent_memory.json`)
- **Web Tools**: `web_search_tool`, `web_fetch_tool`, `web_download_tool`

**Skill System** (skill_tool.go):
- Implements progressive disclosure pattern (3 levels)
- Level 1: Skills metadata injected into system prompt at startup
- Level 2: `get_skill` tool loads full skill content on demand
- Level 3: Relative paths in skills converted to absolute paths
- Skills directory: `./skills/` with `SKILL.md` files following Agent Skills Spec 1.0

### Message Flow

1. **User Input** → User Message added to `agent.messages`
2. **LLM Call** → Converts messages to schema format, calls LLM with tool schemas
3. **Response** → Assistant Message (content + tool calls) added to `agent.messages`
4. **Tool Execution** → For each tool call, execute and add Tool Message to `agent.messages`
5. **Loop** → Repeat steps 2-4 until no tool calls (task complete) or max steps reached

### Configuration (internal/config/)

Configuration loaded from (in priority order):
1. `mini_agent/config/config.yaml` (development)
2. `~/.mini-agent/config/config.yaml` (user home)
3. `config/config.yaml` (relative to executable)

Key settings:
- `llm.provider`: "anthropic" or "openai"
- `llm.model`: Model name (e.g., "MiniMax-M2", "claude-sonnet-4-5-20250929")
- `agent.max_steps`: Maximum execution steps
- `agent.token_limit`: Token limit before summarization
- `agent.workspace_dir`: Working directory for file operations
- `agent.system_prompt_path`: Path to system prompt file

### Shell Integration (internal/shell/)

- **shell.go**: Manages persistent shell sessions for bash tool
- **persistent.go**: Process lifecycle management (start, read, write, kill)
- **coreutils/**: Go-native implementations of core Unix utilities (ls, cat, etc.)

## Important Implementation Details

### Token Summarization Strategy

When token limit is exceeded:
1. Keep system prompt (always preserved)
2. For each user message round (user message + assistant responses + tool results):
   - Keep the user message
   - Summarize the execution (assistant responses + tool results) using LLM
   - Replace execution with summary message
3. Result: System + User1 + Summary1 + User2 + Summary2 + ... + CurrentRound (unsummarized)

### Tool Schema Conversion

Tools define parameters once, then convert to provider-specific formats:
- **Anthropic**: `{"name": "...", "description": "...", "input_schema": {...}}`
- **OpenAI**: `{"type": "function", "function": {"name": "...", "description": "...", "parameters": {...}}}`

### Path Resolution

All file tools use workspace-relative paths:
- Absolute paths: Used directly
- Relative paths: Resolved relative to `agent.workspace`
- Security: Tools validate paths are within workspace (prevent traversal attacks)

### Skills Loading

At startup, if `./skills/` exists:
1. `SkillLoader` recursively discovers all `SKILL.md` files
2. Parses YAML frontmatter (name, description, allowed-tools, metadata)
3. Generates metadata prompt injected into system prompt
4. Registers `get_skill` tool for on-demand loading
5. Skill content paths converted to absolute (e.g., `scripts/` → `<skill-dir>/scripts/`)

### Logging

All execution logged to `logs/` directory:
- Filename: `mini-agent-YYYYMMDD-HHMMSS.jsonl`
- Format: JSON Lines (one JSON object per line)
- Content: Requests (messages + tools), responses, tool executions

## Platform-Specific Considerations

The agent auto-detects OS and injects appropriate command examples into system prompt:
- **Windows**: `dir`, `mkdir`, `del`, `copy`, etc.
- **macOS/Linux**: `ls`, `mkdir`, `rm`, `cp`, etc.

Bash tool executes commands using `mvdan.cc/sh/v3` (pure Go shell implementation), which provides cross-platform compatibility.

## Development Workflow

### Bug Fix / Feature Implementation Process

**Every bug fix or feature task MUST follow this workflow — analyze first, code second:**

1. **Problem Analysis / Feature Design** — Before writing any code, first present:
   - **Bug fix**: Locate the root cause, explain what's wrong and why, propose the fix approach
   - **New feature**: Describe the implementation plan — which files to add/modify, key design decisions, how it integrates with existing code
2. **Wait for Confirmation** — Do NOT start coding until the user explicitly approves the approach
3. **Execute** — Implement according to the approved plan
4. **Verify** — Build passes (`go build ./cmd`); run tests only if requested

Keep proposals simple, direct, and practical. Avoid over-analysis — a few bullet points are enough.

## Common Workflows

### Adding a New Tool

1. Create file in `internal/tools/` (e.g., `my_tool.go`)
2. Define struct implementing `Tool` interface
3. Implement required methods: `Name()`, `Description()`, `Parameters()`, `Execute()`
4. Use `BaseTool` for schema conversion (inherit `ToSchema()`, `ToOpenAISchema()`)
5. Register in `agent.RegisterDefaultTools()` or via `agent.RegisterTool()`

### Adding a New LLM Provider

1. Create file in `internal/llm/` (e.g., `myprovider.go`)
2. Implement `Client` interface
3. Handle message conversion (internal schema ↔ provider format)
4. Handle tool schema conversion
5. Add provider to `NewClient()` switch case in `client.go`
6. Add provider constant to `internal/schema/schema.go`

### Testing the Agent

Integration test approach in `internal/llm/integration_test.go`:
1. Uses `test_helper.go` mock client
2. Simulates LLM responses with tool calls
3. Verifies agent loop, tool execution, message flow
4. Can be adapted for testing new tools or scenarios

## External Dependencies

- `mvdan.cc/sh/v3`: Shell interpreter for bash tool
- `github.com/aymanbagabas/go-udiff`: Unified diff for edit tool
- `gopkg.in/yaml.v3`: YAML parsing for config and skills
- `github.com/u-root/u-root`: Coreutils implementation
- `github.com/JohannesKaufmann/html-to-markdown`: Web fetch tool
- `github.com/PuerkitoBio/goquery`: HTML parsing

## Skills Directory

The `skills/` directory contains Claude Code-compatible skills following Agent Skills Spec 1.0. Skills are auto-loaded at startup and made available via the `get_skill` tool. See `SKILL_TOOL_IMPLEMENTATION.md` for details on the skill system architecture.
