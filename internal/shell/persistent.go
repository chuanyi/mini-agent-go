package shell

import (
	"sync"
)

// PersistentShell is a singleton shell instance that maintains state across the application
type PersistentShell struct {
	*Shell
	workspace string // The initial workspace directory to reset to before each execution
}

var (
	once          sync.Once
	shellInstance *PersistentShell
)

// GetPersistentShell returns the singleton persistent shell instance
// This maintains backward compatibility with the existing API
func GetPersistentShell(cwd string) *PersistentShell {
	once.Do(func() {
		shellInstance = &PersistentShell{
			Shell: NewShell(&Options{
				WorkingDir: cwd,
				Logger:     noopLogger{}, // Use noop logger to prevent output interference with TUI
			}),
			workspace: cwd,
		}
	})
	return shellInstance
}

// ResetToWorkspace resets the shell's working directory to the initial workspace
func (p *PersistentShell) ResetToWorkspace() {
	if p.workspace != "" {
		p.Shell.mu.Lock()
		p.Shell.cwd = p.workspace
		p.Shell.mu.Unlock()
	}
}

// loggingAdapter adapts the internal slog package to the Logger interface
// DEPRECATED: This causes output to terminal which interferes with TUI
// Use noopLogger instead for TUI mode
type loggingAdapter struct{}

func (l *loggingAdapter) InfoPersist(msg string, keysAndValues ...any) {
	// Disabled to prevent TUI interference
	// slog.Info(msg, keysAndValues...)
}
