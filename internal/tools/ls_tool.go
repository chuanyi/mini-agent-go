package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxLSFiles = 1000
)

// TreeNode represents a node in the directory tree
type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"` // "file" or "directory"
	Children []*TreeNode `json:"children,omitempty"`
}

// LSTool provides directory listing functionality
type LSTool struct {
	*BaseTool
	workspace string
}

// NewLSTool creates a new directory listing tool
func NewLSTool(workspace string) *LSTool {
	return &LSTool{
		BaseTool: &BaseTool{
			NameValue: "ls",
			DescriptionValue: `List files and subdirectories in a tree structure.
Skips hidden files and common noise directories (__pycache__, node_modules).
Results limited to 1000 files.`,
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the directory to list (defaults to workspace)",
					},
					"ignore": map[string]interface{}{
						"type":        "array",
						"description": "List of glob patterns to ignore",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
		workspace: workspace,
	}
}

func (t *LSTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Get path parameter
	searchPath := t.workspace
	if pathVal, ok := params["path"].(string); ok && pathVal != "" {
		searchPath = pathVal
	}

	// Expand path
	expandedPath, err := Expand(searchPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("error expanding path: %v", err)), nil
	}
	searchPath = expandedPath

	// Resolve and validate path within workspace
	resolvedPath, err := ResolvePath(t.workspace, searchPath)
	if err != nil {
		return ErrorResult(err.Error()), nil
	}
	searchPath = resolvedPath

	// Get ignore patterns
	var ignore []string
	if ignoreVal, ok := params["ignore"].([]interface{}); ok {
		for _, pattern := range ignoreVal {
			if str, ok := pattern.(string); ok {
				ignore = append(ignore, str)
			}
		}
	}

	// Check if directory exists
	if _, err := os.Stat(searchPath); os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("path does not exist: %s", searchPath)), nil
	}

	// List directory
	files, truncated, err := ListDirectory(searchPath, ignore, MaxLSFiles)
	if err != nil {
		return ErrorResult(fmt.Sprintf("error listing directory: %v", err)), nil
	}

	// Create tree
	tree := createFileTree(files, searchPath)
	output := printTree(tree, searchPath)

	// Add truncation note if needed
	if truncated {
		output = fmt.Sprintf("There are more than %d files in the directory. Use a more specific path or ignore patterns. The first %d files and directories are included below:\n\n%s",
			MaxLSFiles, MaxLSFiles, output)
	}

	// Content for LLM: tree structure
	// Details for UI: include path header
	relPath := strings.TrimPrefix(searchPath, t.workspace+string(filepath.Separator))
	if relPath == searchPath {
		relPath = "."
	}
	return &ToolResult{
		Success: true,
		Content: output,
		Details: fmt.Sprintf("%s\n%s", relPath, output),
	}, nil
}

func createFileTree(sortedPaths []string, rootPath string) []*TreeNode {
	root := []*TreeNode{}
	pathMap := make(map[string]*TreeNode)

	for _, path := range sortedPaths {
		relativePath := strings.TrimPrefix(path, rootPath)
		parts := strings.Split(relativePath, string(filepath.Separator))
		currentPath := ""
		var parentPath string

		// Clean up empty parts
		var cleanParts []string
		for _, part := range parts {
			if part != "" {
				cleanParts = append(cleanParts, part)
			}
		}
		parts = cleanParts

		if len(parts) == 0 {
			continue
		}

		for i, part := range parts {
			if currentPath == "" {
				currentPath = part
			} else {
				currentPath = filepath.Join(currentPath, part)
			}

			if _, exists := pathMap[currentPath]; exists {
				parentPath = currentPath
				continue
			}

			isLastPart := i == len(parts)-1
			isDir := !isLastPart || strings.HasSuffix(relativePath, string(filepath.Separator))
			nodeType := "file"
			if isDir {
				nodeType = "directory"
			}

			newNode := &TreeNode{
				Name:     part,
				Path:     currentPath,
				Type:     nodeType,
				Children: []*TreeNode{},
			}

			pathMap[currentPath] = newNode

			if i > 0 && parentPath != "" {
				if parent, ok := pathMap[parentPath]; ok {
					parent.Children = append(parent.Children, newNode)
				}
			} else {
				root = append(root, newNode)
			}

			parentPath = currentPath
		}
	}

	return root
}

func printTree(tree []*TreeNode, rootPath string) string {
	var result strings.Builder

	result.WriteString("- ")
	result.WriteString(rootPath)
	if len(rootPath) > 0 && rootPath[len(rootPath)-1] != filepath.Separator {
		result.WriteByte(filepath.Separator)
	}
	result.WriteByte('\n')

	for _, node := range tree {
		printNode(&result, node, 1)
	}

	return result.String()
}

func printNode(builder *strings.Builder, node *TreeNode, level int) {
	indent := strings.Repeat("  ", level)

	nodeName := node.Name
	if node.Type == "directory" {
		nodeName = nodeName + string(filepath.Separator)
	}

	fmt.Fprintf(builder, "%s- %s\n", indent, nodeName)

	if node.Type == "directory" && len(node.Children) > 0 {
		for _, child := range node.Children {
			printNode(builder, child, level+1)
		}
	}
}
