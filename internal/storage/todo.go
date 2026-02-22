package storage

import (
	"fmt"
	"sync"
)

// TodoItem represents a single todo task
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"` // pending, in_progress, completed
}

// TodoStorage manages todo items with thread-safe operations
type TodoStorage struct {
	items []TodoItem
	mu    sync.RWMutex
}

// NewTodoStorage creates a new TodoStorage instance
func NewTodoStorage() *TodoStorage {
	return &TodoStorage{
		items: make([]TodoItem, 0),
	}
}

// Set replaces all todo items with the provided list
func (ts *TodoStorage) Set(items []TodoItem) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.items = items
}

// Get returns a copy of all todo items
func (ts *TodoStorage) Get() []TodoItem {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Return a copy to prevent external modification
	result := make([]TodoItem, len(ts.items))
	copy(result, ts.items)
	return result
}

// Clear removes all todo items
func (ts *TodoStorage) Clear() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.items = make([]TodoItem, 0)
}

// Count returns the number of todo items
func (ts *TodoStorage) Count() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.items)
}

// Validate performs validation on the todo items
func (ts *TodoStorage) Validate(items []TodoItem) error {
	// Check for duplicate IDs
	idMap := make(map[string]bool)
	for _, item := range items {
		if idMap[item.ID] {
			return fmt.Errorf("duplicate todo ID found: %s", item.ID)
		}
		idMap[item.ID] = true
	}

	// Check for multiple in_progress tasks
	inProgressCount := 0
	var inProgressIDs []string
	for _, item := range items {
		if item.Status == "in_progress" {
			inProgressCount++
			inProgressIDs = append(inProgressIDs, item.ID)
		}
	}
	if inProgressCount > 1 {
		return fmt.Errorf("only one task can be in_progress at a time, found %d: %v", inProgressCount, inProgressIDs)
	}

	// Validate each todo
	for _, item := range items {
		// Check for empty content
		if item.Content == "" {
			return fmt.Errorf("todo with ID '%s' has empty content", item.ID)
		}

		// Check for valid status
		if item.Status != "pending" && item.Status != "in_progress" && item.Status != "completed" {
			return fmt.Errorf("invalid status '%s' for todo '%s' (must be pending, in_progress, or completed)", item.Status, item.ID)
		}

		// Check for empty ID
		if item.ID == "" {
			return fmt.Errorf("todo with content '%s' has empty ID", item.Content)
		}
	}

	return nil
}

// GetStats returns statistics about the todo items
func (ts *TodoStorage) GetStats() map[string]int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	stats := map[string]int{
		"total":       len(ts.items),
		"pending":     0,
		"in_progress": 0,
		"completed":   0,
	}

	for _, item := range ts.items {
		switch item.Status {
		case "pending":
			stats["pending"]++
		case "in_progress":
			stats["in_progress"]++
		case "completed":
			stats["completed"]++
		}
	}

	return stats
}
