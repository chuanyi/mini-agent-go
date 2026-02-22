package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskStatus represents the state of a task.
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"
	StatusRunning   TaskStatus = "running"
	StatusCompleted TaskStatus = "completed"
	StatusFailed    TaskStatus = "failed"
	StatusCancelled TaskStatus = "cancelled"
)

// Task represents an async background task.
type Task struct {
	ID          string     `json:"id"`
	Status      TaskStatus `json:"status"`
	Description string     `json:"description"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CronID      string     `json:"cron_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// TaskStore persists tasks to a JSON file with mutex protection.
type TaskStore struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	filePath string
	nextID   int
}

// NewTaskStore creates a new TaskStore. dir is the directory where tasks.json will live.
func NewTaskStore(dir string) (*TaskStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create task store dir: %w", err)
	}

	s := &TaskStore{
		tasks:    make(map[string]*Task),
		filePath: filepath.Join(dir, "tasks.json"),
		nextID:   1,
	}

	// Load existing tasks if file exists
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load tasks: %w", err)
	}

	return s, nil
}

// Add creates a new pending task and persists it.
func (s *TaskStore) Add(description, cronID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := &Task{
		ID:          fmt.Sprintf("task-%03d", s.nextID),
		Status:      StatusPending,
		Description: description,
		CronID:      cronID,
		CreatedAt:   time.Now(),
	}
	s.nextID++
	s.tasks[t.ID] = t

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return t, nil
}

// Get returns a copy of the task with the given ID.
func (s *TaskStore) Get(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	cp := *t
	return &cp, nil
}

// List returns copies of tasks, optionally filtered by status.
// If status is empty, all tasks are returned.
func (s *TaskStore) List(status TaskStatus) []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Task
	for _, t := range s.tasks {
		if status == "" || t.Status == status {
			cp := *t
			result = append(result, &cp)
		}
	}
	return result
}

// ListPending returns all tasks with pending status.
func (s *TaskStore) ListPending() []*Task {
	return s.List(StatusPending)
}

// MarkRunning transitions a task from pending to running.
func (s *TaskStore) MarkRunning(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.Status != StatusPending {
		return fmt.Errorf("cannot start task %s: status is %s, expected pending", id, t.Status)
	}
	now := time.Now()
	t.Status = StatusRunning
	t.StartedAt = &now
	return s.saveLocked()
}

// MarkCompleted transitions a task from running to completed.
func (s *TaskStore) MarkCompleted(id, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.Status != StatusRunning {
		return fmt.Errorf("cannot complete task %s: status is %s, expected running", id, t.Status)
	}
	now := time.Now()
	t.Status = StatusCompleted
	t.Result = result
	t.FinishedAt = &now
	return s.saveLocked()
}

// MarkFailed transitions a task from running to failed.
func (s *TaskStore) MarkFailed(id string, taskErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.Status != StatusRunning {
		return fmt.Errorf("cannot fail task %s: status is %s, expected running", id, t.Status)
	}
	now := time.Now()
	t.Status = StatusFailed
	t.Error = taskErr.Error()
	t.FinishedAt = &now
	return s.saveLocked()
}

// Cancel transitions a pending or running task to cancelled.
func (s *TaskStore) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if t.Status != StatusPending && t.Status != StatusRunning {
		return fmt.Errorf("cannot cancel task %s: status is %s", id, t.Status)
	}
	now := time.Now()
	t.Status = StatusCancelled
	t.FinishedAt = &now
	return s.saveLocked()
}

// isTerminal returns true if the task status is a terminal state (completed, failed, cancelled).
func isTerminal(s TaskStatus) bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

// Delete removes a single finished task by ID. Only terminal tasks can be deleted.
func (s *TaskStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if !isTerminal(t.Status) {
		return fmt.Errorf("cannot delete task %s: status is %s (only finished tasks can be deleted)", id, t.Status)
	}
	delete(s.tasks, id)
	return s.saveLocked()
}

// DeleteFinished removes all tasks in terminal states and returns the count deleted.
func (s *TaskStore) DeleteFinished() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for id, t := range s.tasks {
		if isTerminal(t.Status) {
			delete(s.tasks, id)
			count++
		}
	}
	if count > 0 {
		if err := s.saveLocked(); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// RecoverStaleTasks marks any tasks stuck in "running" as "failed".
// This handles crash recovery on startup.
func (s *TaskStore) RecoverStaleTasks() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	now := time.Now()
	for _, t := range s.tasks {
		if t.Status == StatusRunning {
			t.Status = StatusFailed
			t.Error = "recovered: agent was terminated while task was running"
			t.FinishedAt = &now
			changed = true
		}
	}
	if changed {
		return s.saveLocked()
	}
	return nil
}

// load reads tasks from disk. Must be called without lock held (only used in constructor).
func (s *TaskStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var tasks []*Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return fmt.Errorf("parse tasks.json: %w", err)
	}

	s.tasks = make(map[string]*Task, len(tasks))
	maxID := 0
	for _, t := range tasks {
		s.tasks[t.ID] = t
		// Parse ID number to set nextID correctly
		var num int
		if _, err := fmt.Sscanf(t.ID, "task-%d", &num); err == nil && num >= maxID {
			maxID = num
		}
	}
	s.nextID = maxID + 1
	return nil
}

// saveLocked writes all tasks to disk atomically. Caller must hold s.mu.
func (s *TaskStore) saveLocked() error {
	tasks := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tasks: %w", err)
	}

	// Atomic write: write to temp file then rename
	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpFile, s.filePath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
