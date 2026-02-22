package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// CronEntry represents a scheduled cron job.
type CronEntry struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	CronExpr    string     `json:"cron_expr"`
	Description string     `json:"description"` // prompt template for spawned tasks
	Enabled     bool       `json:"enabled"`
	NextRunAt   time.Time  `json:"next_run_at"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastTaskID  string     `json:"last_task_id,omitempty"`
}

// cronParser is reused for validation and schedule computation.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// CronStore persists cron entries to a JSON file.
type CronStore struct {
	mu       sync.RWMutex
	entries  map[string]*CronEntry
	filePath string
	nextID   int
}

// NewCronStore creates a new CronStore. dir is the directory where cron.json will live.
func NewCronStore(dir string) (*CronStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create cron store dir: %w", err)
	}

	s := &CronStore{
		entries:  make(map[string]*CronEntry),
		filePath: filepath.Join(dir, "cron.json"),
		nextID:   1,
	}

	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load cron entries: %w", err)
	}

	return s, nil
}

// Add creates a new enabled cron entry after validating the expression.
func (s *CronStore) Add(name, cronExpr, description string) (*CronEntry, error) {
	// Validate cron expression
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry := &CronEntry{
		ID:          fmt.Sprintf("cron-%03d", s.nextID),
		Name:        name,
		CronExpr:    cronExpr,
		Description: description,
		Enabled:     true,
		NextRunAt:   sched.Next(time.Now()),
	}
	s.nextID++
	s.entries[entry.ID] = entry

	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return entry, nil
}

// Remove deletes a cron entry by ID.
func (s *CronStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.entries[id]; !ok {
		return fmt.Errorf("cron entry not found: %s", id)
	}
	delete(s.entries, id)
	return s.saveLocked()
}

// List returns copies of all cron entries.
func (s *CronStore) List() []*CronEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*CronEntry, 0, len(s.entries))
	for _, e := range s.entries {
		cp := *e
		result = append(result, &cp)
	}
	return result
}

// GetDueEntries returns entries whose NextRunAt is before or at now, and updates their schedule.
func (s *CronStore) GetDueEntries() ([]*CronEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var due []*CronEntry
	changed := false

	for _, e := range s.entries {
		if !e.Enabled {
			continue
		}
		if e.NextRunAt.Before(now) || e.NextRunAt.Equal(now) {
			cp := *e
			due = append(due, &cp)

			// Update next run and last run
			sched, err := cronParser.Parse(e.CronExpr)
			if err != nil {
				continue // skip broken entries
			}
			e.LastRunAt = &now
			e.NextRunAt = sched.Next(now)
			changed = true
		}
	}

	if changed {
		if err := s.saveLocked(); err != nil {
			return due, err
		}
	}
	return due, nil
}

// UpdateLastTask records the last spawned task ID for a cron entry.
func (s *CronStore) UpdateLastTask(cronID, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[cronID]
	if !ok {
		return fmt.Errorf("cron entry not found: %s", cronID)
	}
	e.LastTaskID = taskID
	return s.saveLocked()
}

func (s *CronStore) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}

	var entries []*CronEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse cron.json: %w", err)
	}

	s.entries = make(map[string]*CronEntry, len(entries))
	maxID := 0
	for _, e := range entries {
		s.entries[e.ID] = e
		var num int
		if _, err := fmt.Sscanf(e.ID, "cron-%d", &num); err == nil && num >= maxID {
			maxID = num
		}
	}
	s.nextID = maxID + 1
	return nil
}

func (s *CronStore) saveLocked() error {
	entries := make([]*CronEntry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cron entries: %w", err)
	}

	tmpFile := s.filePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpFile, s.filePath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}

// CronManager periodically checks for due cron entries and spawns tasks.
type CronManager struct {
	cronStore    *CronStore
	taskMgr      *TaskManager
	tickInterval time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewCronManager creates a new CronManager with a default 30-second tick interval.
// parentCtx allows the manager to respond to application-level cancellation.
func NewCronManager(parentCtx context.Context, cronStore *CronStore, taskMgr *TaskManager) *CronManager {
	ctx, cancel := context.WithCancel(parentCtx)
	return &CronManager{
		cronStore:    cronStore,
		taskMgr:      taskMgr,
		tickInterval: 30 * time.Second,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start launches the tick loop goroutine.
func (cm *CronManager) Start() {
	go cm.loop()
}

// Stop cancels the tick loop.
func (cm *CronManager) Stop() {
	cm.cancel()
}

func (cm *CronManager) loop() {
	ticker := time.NewTicker(cm.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			cm.tick()
		}
	}
}

func (cm *CronManager) tick() {
	due, err := cm.cronStore.GetDueEntries()
	if err != nil {
		return // silently ignore persistence errors during tick
	}

	for _, entry := range due {
		taskID, err := cm.taskMgr.AddTask(entry.Description, entry.ID)
		if err != nil {
			continue
		}
		cm.cronStore.UpdateLastTask(entry.ID, taskID)
	}
}
