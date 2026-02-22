package tools

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Expand expands the tilde in path to the user's home directory
func Expand(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path, err
		}
		path = filepath.Join(home, path[1:])
	}
	return path, nil
}

// ListDirectory lists all files in a directory recursively
// Returns a sorted list of file paths, whether the list was truncated, and any error
func ListDirectory(rootPath string, ignore []string, maxFiles int) ([]string, bool, error) {
	var files []string
	truncated := false

	// Default ignore patterns
	defaultIgnore := []string{
		".*",           // Hidden files/directories
		"__pycache__", // Python cache
		"node_modules", // Node.js modules
		".git",         // Git directory
	}
	ignore = append(ignore, defaultIgnore...)

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if we've hit the max files limit
		if len(files) >= maxFiles {
			truncated = true
			return filepath.SkipAll
		}

		// Skip the root path itself
		if path == rootPath {
			return nil
		}

		// Get the relative path
		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}

		// Check ignore patterns
		parts := strings.Split(relPath, string(filepath.Separator))
		for _, part := range parts {
			for _, pattern := range ignore {
				matched, _ := filepath.Match(pattern, part)
				if matched {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
		}

		// Add the path
		if info.IsDir() {
			files = append(files, path+string(filepath.Separator))
		} else {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return nil, false, err
	}

	// Sort files
	sort.Strings(files)

	return files, truncated, nil
}

// ToUnixLineEndings converts Windows line endings to Unix
// Returns the converted string and whether the original had CRLF endings
func ToUnixLineEndings(s string) (string, bool) {
	hasCRLF := strings.Contains(s, "\r\n")
	if hasCRLF {
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	return s, hasCRLF
}

// ToWindowsLineEndings converts Unix line endings to Windows
// Returns the converted string and whether the conversion was made
func ToWindowsLineEndings(s string) (string, bool) {
	// Only convert if there are LF but not CRLF
	if strings.Contains(s, "\n") && !strings.Contains(s, "\r\n") {
		s = strings.ReplaceAll(s, "\n", "\r\n")
		return s, true
	}
	return s, false
}