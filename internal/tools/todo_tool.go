package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"mini-agent-go/internal/storage"
)

// TodoTool implements the todo tool for tracking multi-step progress
type TodoTool struct {
	BaseTool
	storage *storage.TodoStorage
	eventCh chan<- interface{} // Channel to notify TUI about updates
}

// NewTodoTool creates a new TodoTool instance
func NewTodoTool(todoStorage *storage.TodoStorage, eventCh chan<- interface{}) *TodoTool {
	return &TodoTool{
		BaseTool: BaseTool{
			NameValue:        "todo",
			DescriptionValue: todoDescription,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todos": map[string]interface{}{
						"type":        "array",
						"description": "The updated todo list",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":        "string",
									"description": "Unique identifier for the task",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "The task description or content",
								},
								"status": map[string]interface{}{
									"type":        "string",
									"enum":        []string{"pending", "in_progress", "completed"},
									"description": "Current status of the task",
								},
							},
							"required": []string{"id", "content", "status"},
						},
					},
				},
				"required": []string{"todos"},
			},
		},
		storage: todoStorage,
		eventCh: eventCh,
	}
}

// Execute executes the todo tool
func (t *TodoTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Extract todos from parameters
	todosRaw, ok := params["todos"]
	if !ok {
		return ErrorResult("missing 'todos' parameter"), nil
	}

	// Convert to []TodoItem
	var todos []storage.TodoItem
	todosJSON, err := json.Marshal(todosRaw)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to serialize todos: %v", err)), nil
	}

	err = json.Unmarshal(todosJSON, &todos)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse todos: %v", err)), nil
	}

	// Validate todos
	if err := t.storage.Validate(todos); err != nil {
		return ErrorResult(fmt.Sprintf("validation failed: %v", err)), nil
	}

	// Store previous todos for comparison
	previousTodos := t.storage.Get()

	// Update the storage
	t.storage.Set(todos)

	// Get statistics
	stats := t.storage.GetStats()

	// Notify TUI about the update (non-blocking)
	if t.eventCh != nil {
		select {
		case t.eventCh <- TodoUpdateEvent{
			PreviousTodos: previousTodos,
			NewTodos:      todos,
		}:
		default:
			// Channel full or closed, skip notification
		}
	}

	// Content for LLM: concise stats
	content := fmt.Sprintf("Updated %d todos (%d pending, %d in_progress, %d completed).",
		stats["total"], stats["pending"], stats["in_progress"], stats["completed"])

	return ContentResult(content), nil
}

// TodoUpdateEvent represents a todo update event sent to the TUI
type TodoUpdateEvent struct {
	PreviousTodos []storage.TodoItem
	NewTodos      []storage.TodoItem
}
