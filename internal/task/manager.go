package task

import (
	"context"
	"fmt"
	"sync"
)

// TaskRunner is a function that executes a task description and returns a result.
// It is provided by the caller (e.g., cmd/main.go) to avoid circular imports.
type TaskRunner func(ctx context.Context, description string) (result string, err error)

// TaskManager manages task lifecycle: queuing, dispatching, and execution.
type TaskManager struct {
	store       *TaskStore
	runner      TaskRunner
	maxWorkers  int
	sem         chan struct{}   // worker pool semaphore
	wg          sync.WaitGroup // tracks running workers
	ctx         context.Context
	cancel      context.CancelFunc
	notify      chan struct{} // signals new tasks to dispatcher
	cancelsMu   sync.Mutex
	cancels     map[string]context.CancelFunc // per-task cancel funcs
	OnTaskComplete func(description, result string) // optional completion callback
}

// NewTaskManager creates a new TaskManager.
// parentCtx allows the manager to respond to application-level cancellation.
func NewTaskManager(parentCtx context.Context, store *TaskStore, runner TaskRunner, maxWorkers int) *TaskManager {
	if maxWorkers <= 0 {
		maxWorkers = 3
	}
	ctx, cancel := context.WithCancel(parentCtx)
	return &TaskManager{
		store:      store,
		runner:     runner,
		maxWorkers: maxWorkers,
		sem:        make(chan struct{}, maxWorkers),
		ctx:        ctx,
		cancel:     cancel,
		notify:     make(chan struct{}, 1),
		cancels:    make(map[string]context.CancelFunc),
	}
}

// Start launches the dispatcher goroutine.
func (m *TaskManager) Start() {
	go m.dispatcher()
}

// Stop cancels all running tasks and waits for them to finish.
func (m *TaskManager) Stop() {
	m.cancel()
	m.wg.Wait()
}

// AddTask creates a new pending task and notifies the dispatcher.
func (m *TaskManager) AddTask(description, cronID string) (string, error) {
	t, err := m.store.Add(description, cronID)
	if err != nil {
		return "", err
	}
	// Non-blocking send to notify dispatcher
	select {
	case m.notify <- struct{}{}:
	default:
	}
	return t.ID, nil
}

// CancelTask cancels a pending or running task.
func (m *TaskManager) CancelTask(id string) error {
	t, err := m.store.Get(id)
	if err != nil {
		return err
	}

	if t.Status == StatusRunning {
		// Cancel the running goroutine
		m.cancelsMu.Lock()
		cancelFn, ok := m.cancels[id]
		m.cancelsMu.Unlock()
		if ok {
			cancelFn()
		}
	}

	return m.store.Cancel(id)
}

// GetTask returns a task by ID.
func (m *TaskManager) GetTask(id string) (*Task, error) {
	return m.store.Get(id)
}

// ListTasks returns tasks, optionally filtered by status.
func (m *TaskManager) ListTasks(status TaskStatus) []*Task {
	return m.store.List(status)
}

// DeleteTask deletes a single finished task by ID.
func (m *TaskManager) DeleteTask(id string) error {
	return m.store.Delete(id)
}

// DeleteFinishedTasks deletes all finished tasks and returns the count deleted.
func (m *TaskManager) DeleteFinishedTasks() (int, error) {
	return m.store.DeleteFinished()
}

// dispatcher runs in a goroutine, polling for pending tasks.
func (m *TaskManager) dispatcher() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.notify:
			m.dispatchPending()
		}
	}
}

// dispatchPending picks up all pending tasks and dispatches them to workers.
func (m *TaskManager) dispatchPending() {
	pending := m.store.ListPending()
	for _, t := range pending {
		select {
		case <-m.ctx.Done():
			return
		case m.sem <- struct{}{}: // acquire worker slot
			m.wg.Add(1)
			go m.runTask(t.ID, t.Description)
		}
	}
}

// runTask executes a single task in a worker goroutine.
func (m *TaskManager) runTask(id, description string) {
	defer func() {
		<-m.sem // release worker slot
		m.wg.Done()

		m.cancelsMu.Lock()
		delete(m.cancels, id)
		m.cancelsMu.Unlock()
	}()

	// Mark running
	if err := m.store.MarkRunning(id); err != nil {
		return // task may have been cancelled
	}

	// Create per-task context with cancel
	taskCtx, taskCancel := context.WithCancel(m.ctx)
	defer taskCancel()

	m.cancelsMu.Lock()
	m.cancels[id] = taskCancel
	m.cancelsMu.Unlock()

	// Execute the runner
	result, err := m.runner(taskCtx, description)
	if err != nil {
		// Check if it was a cancellation
		if taskCtx.Err() != nil {
			// Already cancelled via CancelTask — store.Cancel was called
			return
		}
		m.store.MarkFailed(id, fmt.Errorf("%v", err))
		return
	}

	m.store.MarkCompleted(id, result)

	if m.OnTaskComplete != nil {
		go m.OnTaskComplete(description, result)
	}
}
