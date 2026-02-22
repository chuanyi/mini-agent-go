// Package shell provides cross-platform shell execution capabilities.
//
// This package offers two main types:
// - Shell: A general-purpose shell executor for one-off or managed commands
// - PersistentShell: A singleton shell that maintains state across the application
//
// WINDOWS COMPATIBILITY:
// This implementation provides POSIX shell emulation (mvdan.cc/sh/v3),
// even on Windows. Commands should use forward slashes (/) as path separators
// to work correctly on all platforms.
package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"mini-agent-go/internal/shell/coreutils"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"
)

// ShellType represents the type of shell to use
type ShellType int

const (
	ShellTypePOSIX ShellType = iota
	ShellTypeCmd
	ShellTypePowerShell
)

// Logger interface for optional logging
type Logger interface {
	InfoPersist(msg string, keysAndValues ...any)
}

// noopLogger is a logger that does nothing
type noopLogger struct{}

func (noopLogger) InfoPersist(msg string, keysAndValues ...any) {}

// Shell provides cross-platform shell execution with optional state persistence
type Shell struct {
	env    []string
	cwd    string
	mu     sync.Mutex
	logger Logger
}

// Options for creating a new shell
type Options struct {
	WorkingDir string
	Env        []string
	Logger     Logger
}

// NewShell creates a new shell instance with the given options
func NewShell(opts *Options) *Shell {
	if opts == nil {
		opts = &Options{}
	}

	cwd := opts.WorkingDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	env := opts.Env
	if env == nil {
		env = os.Environ()
	}

	logger := opts.Logger
	if logger == nil {
		logger = noopLogger{}
	}

	return &Shell{
		cwd:    cwd,
		env:    env,
		logger: logger,
	}
}

// Exec executes a command in the shell
func (s *Shell) Exec(ctx context.Context, command string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.execPOSIX(ctx, command)
}

// GetWorkingDir returns the current working directory
func (s *Shell) GetWorkingDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// SetWorkingDir sets the working directory
func (s *Shell) SetWorkingDir(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify the directory exists
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("directory does not exist: %w", err)
	}

	s.cwd = dir
	return nil
}

// GetEnv returns a copy of the environment variables
func (s *Shell) GetEnv() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	env := make([]string, len(s.env))
	copy(env, s.env)
	return env
}

// SetEnv sets an environment variable
func (s *Shell) SetEnv(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update or add the environment variable
	keyPrefix := key + "="
	for i, env := range s.env {
		if strings.HasPrefix(env, keyPrefix) {
			s.env[i] = keyPrefix + value
			return
		}
	}
	s.env = append(s.env, keyPrefix+value)
}

// execPOSIX executes commands using POSIX shell emulation (cross-platform)
func (s *Shell) execPOSIX(ctx context.Context, command string) (string, string, error) {
	line, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return "", "", fmt.Errorf("could not parse command: %w", err)
	}

	// Temporarily redirect os.Stdout and os.Stderr to prevent
	// external commands from interfering with TUI display
	// This is critical for commands like go build, npm, etc.
	originalStdout := os.Stdout
	originalStderr := os.Stderr

	// Create devnull for redirection
	devNull, devNullErr := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if devNullErr == nil {
		os.Stdout = devNull
		os.Stderr = devNull
		defer func() {
			devNull.Close()
			os.Stdout = originalStdout
			os.Stderr = originalStderr
		}()
	}

	var stdout, stderr bytes.Buffer
	runner, err := interp.New(
		interp.StdIO(nil, &stdout, &stderr),
		interp.Interactive(false),
		interp.Env(expand.ListEnviron(s.env...)),
		interp.Dir(s.cwd),
		interp.ExecHandlers(coreutils.ExecHandler),
	)
	if err != nil {
		return "", "", fmt.Errorf("could not run command: %w", err)
	}

	err = runner.Run(ctx, line)
	s.cwd = runner.Dir
	s.env = []string{}
	for name, vr := range runner.Vars {
		s.env = append(s.env, fmt.Sprintf("%s=%s", name, vr.Str))
	}
	s.logger.InfoPersist("POSIX command finished", "command", command, "err", err)
	return stdout.String(), stderr.String(), err
}

// IsInterrupt checks if an error is due to interruption
func IsInterrupt(err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

// ExitCode extracts the exit code from an error
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr interp.ExitStatus
	if errors.As(err, &exitErr) {
		return int(exitErr)
	}
	return 1
}
