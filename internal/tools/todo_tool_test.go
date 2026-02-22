package tools

import (
	"context"
	"testing"

	"mini-agent-go/internal/storage"
)

func TestTodoTool_Execute(t *testing.T) {
	// Create todo storage
	todoStorage := storage.NewTodoStorage()

	// Create event channel
	eventCh := make(chan interface{}, 10)

	// Create tool
	tool := NewTodoTool(todoStorage, eventCh)

	// Test basic execution
	params := map[string]interface{}{
		"todos": []map[string]interface{}{
			{
				"id":      "1",
				"content": "Step 1",
				"status":  "pending",
			},
			{
				"id":      "2",
				"content": "Step 2",
				"status":  "in_progress",
			},
		},
	}

	result, err := tool.Execute(context.Background(), params)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	// Verify storage
	todos := todoStorage.Get()
	if len(todos) != 2 {
		t.Errorf("Expected 2 todos, got %d", len(todos))
	}

	// Verify event was sent
	select {
	case event := <-eventCh:
		if _, ok := event.(TodoUpdateEvent); !ok {
			t.Errorf("Expected TodoUpdateEvent, got %T", event)
		}
	default:
		t.Error("Expected event to be sent")
	}
}

func TestTodoTool_Validation(t *testing.T) {
	todoStorage := storage.NewTodoStorage()
	eventCh := make(chan interface{}, 10)
	tool := NewTodoTool(todoStorage, eventCh)

	tests := []struct {
		name      string
		params    map[string]interface{}
		shouldErr bool
		errMsg    string
	}{
		{
			name: "duplicate IDs",
			params: map[string]interface{}{
				"todos": []map[string]interface{}{
					{"id": "1", "content": "Step 1", "status": "pending"},
					{"id": "1", "content": "Step 2", "status": "pending"},
				},
			},
			shouldErr: true,
			errMsg:    "duplicate todo ID",
		},
		{
			name: "multiple in_progress",
			params: map[string]interface{}{
				"todos": []map[string]interface{}{
					{"id": "1", "content": "Step 1", "status": "in_progress"},
					{"id": "2", "content": "Step 2", "status": "in_progress"},
				},
			},
			shouldErr: true,
			errMsg:    "only one task can be in_progress",
		},
		{
			name: "empty content",
			params: map[string]interface{}{
				"todos": []map[string]interface{}{
					{"id": "1", "content": "", "status": "pending"},
				},
			},
			shouldErr: true,
			errMsg:    "empty content",
		},
		{
			name: "invalid status",
			params: map[string]interface{}{
				"todos": []map[string]interface{}{
					{"id": "1", "content": "Step 1", "status": "invalid"},
				},
			},
			shouldErr: true,
			errMsg:    "invalid status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.params)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if tt.shouldErr {
				if result.Success {
					t.Error("Expected validation error, got success")
				}
				if result.Error == "" {
					t.Error("Expected error message")
				}
			} else {
				if !result.Success {
					t.Errorf("Expected success, got error: %s", result.Error)
				}
			}
		})
	}
}

func TestTodoTool_ToolInterface(t *testing.T) {
	todoStorage := storage.NewTodoStorage()
	tool := NewTodoTool(todoStorage, nil)

	// Verify tool implements Tool interface
	var _ Tool = tool

	// Check tool properties
	if tool.Name() != "todo" {
		t.Errorf("Expected name 'todo', got %s", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}

	params := tool.Parameters()
	if params == nil {
		t.Error("Parameters should not be nil")
	}

	// Check schema conversion
	schema := tool.ToSchema()
	if schema["name"] != "todo" {
		t.Error("Schema name mismatch")
	}

	openAISchema := tool.ToOpenAISchema()
	if openAISchema["type"] != "function" {
		t.Error("OpenAI schema type should be 'function'")
	}
}
