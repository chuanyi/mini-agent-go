package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Note represents a session note with timestamp, category and content
type Note struct {
	Timestamp string `json:"timestamp"`
	Category  string `json:"category"`
	Content   string `json:"content"`
}

// noteStorage manages the shared note storage file
type noteStorage struct {
	memoryFile string
}

func (s *noteStorage) load() ([]Note, error) {
	// If file doesn't exist, return empty slice (lazy loading)
	if _, err := os.Stat(s.memoryFile); os.IsNotExist(err) {
		return []Note{}, nil
	}

	data, err := os.ReadFile(s.memoryFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) == 0 {
		return []Note{}, nil
	}

	var notes []Note
	if err := json.Unmarshal(data, &notes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notes: %w", err)
	}

	return notes, nil
}

func (s *noteStorage) save(notes []Note) error {
	// Create directory if it doesn't exist (lazy initialization)
	dir := filepath.Dir(s.memoryFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := json.MarshalIndent(notes, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal notes: %w", err)
	}

	if err := os.WriteFile(s.memoryFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// RecordNoteTool allows the agent to record important information as session notes
type RecordNoteTool struct {
	*BaseTool
	storage *noteStorage
}

// NewRecordNoteTool creates a new tool for recording session notes
func NewRecordNoteTool(memoryFile string) *RecordNoteTool {
	return &RecordNoteTool{
		BaseTool: &BaseTool{
			NameValue: "record_note",
			DescriptionValue: "Record a timestamped note for future reference. Use for key facts, preferences, decisions, or context to recall later.",
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The information to record as a note. Be concise but specific.",
					},
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Optional category/tag for this note (e.g., 'user_preference', 'project_info', 'decision')",
					},
				},
				"required": []string{"content"},
			},
		},
		storage: &noteStorage{memoryFile: memoryFile},
	}
}

// Execute records a new session note
func (t *RecordNoteTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Get content parameter (required)
	content, ok := params["content"].(string)
	if !ok || content == "" {
		return ErrorResult("content parameter is required"), nil
	}

	// Get category parameter (optional, defaults to "general")
	category := "general"
	if categoryVal, ok := params["category"].(string); ok && categoryVal != "" {
		category = categoryVal
	}

	// Load existing notes
	notes, err := t.storage.load()
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to load notes: %v", err)), nil
	}

	// Add new note with timestamp
	note := Note{
		Timestamp: time.Now().Format(time.RFC3339),
		Category:  category,
		Content:   content,
	}
	notes = append(notes, note)

	// Save back to file
	if err := t.storage.save(notes); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to record note: %v", err)), nil
	}

	return ContentResult(fmt.Sprintf("Recorded note: %s (category: %s)", content, category)), nil
}

// RecallNoteTool allows the agent to recall previously recorded session notes
type RecallNoteTool struct {
	*BaseTool
	storage *noteStorage
}

// NewRecallNoteTool creates a new tool for recalling session notes
func NewRecallNoteTool(memoryFile string) *RecallNoteTool {
	return &RecallNoteTool{
		BaseTool: &BaseTool{
			NameValue: "recall_notes",
			DescriptionValue: "Recall previously recorded session notes. Optionally filter by category.",
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Optional: filter notes by category",
					},
				},
			},
		},
		storage: &noteStorage{memoryFile: memoryFile},
	}
}

// Execute recalls session notes with optional category filter
func (t *RecallNoteTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Get optional category filter
	var categoryFilter string
	if category, ok := params["category"].(string); ok {
		categoryFilter = category
	}

	// Load notes from file
	notes, err := t.storage.load()
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to recall notes: %v", err)), nil
	}

	// Check if there are no notes
	if len(notes) == 0 {
		return ContentResult("No notes recorded yet."), nil
	}

	// Filter by category if specified
	if categoryFilter != "" {
		filtered := make([]Note, 0)
		for _, note := range notes {
			if note.Category == categoryFilter {
				filtered = append(filtered, note)
			}
		}
		notes = filtered

		if len(notes) == 0 {
			return ContentResult(fmt.Sprintf("No notes found in category: %s", categoryFilter)), nil
		}
	}

	// Format notes for display
	var formatted strings.Builder
	formatted.WriteString("Recorded Notes:\n")
	for idx, note := range notes {
		formatted.WriteString(fmt.Sprintf("%d. [%s] %s\n   (recorded at %s)\n",
			idx+1, note.Category, note.Content, note.Timestamp))
	}

	return ContentResult(formatted.String()), nil
}
