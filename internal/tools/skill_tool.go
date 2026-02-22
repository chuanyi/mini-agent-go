package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill represents a skill loaded from SKILL.md file
type Skill struct {
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Content      string            `yaml:"-"` // Not from YAML
	License      string            `yaml:"license,omitempty"`
	AllowedTools []string          `yaml:"allowed-tools,omitempty"`
	Metadata     map[string]string `yaml:"metadata,omitempty"`
	SkillPath    string            `yaml:"-"`
}

// ToPrompt converts skill to prompt format for LLM
func (s *Skill) ToPrompt() string {
	return fmt.Sprintf(`# Skill: %s

%s

---

%s`, s.Name, s.Description, s.Content)
}

// SkillLoader manages loading and caching of skills
type SkillLoader struct {
	skillsDir    string
	loadedSkills map[string]*Skill
}

// NewSkillLoader creates a new skill loader
func NewSkillLoader(skillsDir string) *SkillLoader {
	return &SkillLoader{
		skillsDir:    skillsDir,
		loadedSkills: make(map[string]*Skill),
	}
}

// LoadSkill loads a single skill from SKILL.md file
func (l *SkillLoader) LoadSkill(skillPath string) (*Skill, error) {
	// Read file content
	content, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file: %w", err)
	}

	// Parse YAML frontmatter (support both \n and \r\n line endings)
	frontmatterRegex := regexp.MustCompile(`(?s)^---\r?\n(.*?)\r?\n---\r?\n(.*)$`)
	matches := frontmatterRegex.FindSubmatch(content)

	if matches == nil || len(matches) < 3 {
		return nil, fmt.Errorf("missing YAML frontmatter in %s", skillPath)
	}

	frontmatterText := matches[1]
	skillContent := strings.TrimSpace(string(matches[2]))

	// Parse YAML
	var skill Skill
	if err := yaml.Unmarshal(frontmatterText, &skill); err != nil {
		return nil, fmt.Errorf("failed to parse YAML frontmatter: %w", err)
	}

	// Validate required fields
	if skill.Name == "" || skill.Description == "" {
		return nil, fmt.Errorf("missing required fields (name or description) in %s", skillPath)
	}

	// Get skill directory (parent of SKILL.md)
	skillDir := filepath.Dir(skillPath)

	// Process skill paths to convert relative paths to absolute paths
	processedContent := l.processSkillPaths(skillContent, skillDir)

	// Set content and path
	skill.Content = processedContent
	skill.SkillPath = skillPath

	return &skill, nil
}

// processSkillPaths processes skill content to replace relative paths with absolute paths
func (l *SkillLoader) processSkillPaths(content string, skillDir string) string {
	// Pattern 1: Directory-based paths (scripts/, examples/, templates/, reference/)
	dirPattern := regexp.MustCompile(`(python\s+|` + "`" + `)((?:scripts|examples|templates|reference)/[^\s` + "`" + `\)]+)`)
	content = dirPattern.ReplaceAllStringFunc(content, func(match string) string {
		groups := dirPattern.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		prefix := groups[1]
		relPath := groups[2]

		absPath := filepath.Join(skillDir, relPath)
		if _, err := os.Stat(absPath); err == nil {
			return prefix + absPath
		}
		return match
	})

	// Pattern 2: Direct markdown/document references (forms.md, reference.md, etc.)
	docPattern := regexp.MustCompile(`(?i)(see|read|refer to|check)\s+([a-zA-Z0-9_-]+\.(?:md|txt|json|yaml))([.,;\s])`)
	content = docPattern.ReplaceAllStringFunc(content, func(match string) string {
		groups := docPattern.FindStringSubmatch(match)
		if len(groups) < 4 {
			return match
		}
		prefix := groups[1]
		filename := groups[2]
		suffix := groups[3]

		absPath := filepath.Join(skillDir, filename)
		if _, err := os.Stat(absPath); err == nil {
			return fmt.Sprintf("%s `%s` (use read_file to access)%s", prefix, absPath, suffix)
		}
		return match
	})

	// Pattern 3: Markdown links
	linkPattern := regexp.MustCompile(`(?i)(?:(Read|See|Check|Refer to|Load|View)\s+)?\[(` + "`" + `?[^` + "`" + `\]]+` + "`" + `?)\]\(((?:\./)?[^)]+\.(?:md|txt|json|yaml|js|py|html))\)`)
	content = linkPattern.ReplaceAllStringFunc(content, func(match string) string {
		groups := linkPattern.FindStringSubmatch(match)
		if len(groups) < 4 {
			return match
		}
		prefix := ""
		if groups[1] != "" {
			prefix = groups[1] + " "
		}
		linkText := groups[2]
		filepath_str := groups[3]

		// Remove leading ./ if present
		cleanPath := strings.TrimPrefix(filepath_str, "./")

		absPath := filepath.Join(skillDir, cleanPath)
		if _, err := os.Stat(absPath); err == nil {
			return fmt.Sprintf("%s[%s](`%s`) (use read_file to access)", prefix, linkText, absPath)
		}
		return match
	})

	return content
}

// DiscoverSkills discovers and loads all skills in the skills directory
func (l *SkillLoader) DiscoverSkills() ([]*Skill, error) {
	skills := make([]*Skill, 0)

	// Check if skills directory exists
	if _, err := os.Stat(l.skillsDir); os.IsNotExist(err) {
		// Skills directory does not exist - TUI mode should not print
		return skills, nil
	}

	// Recursively find all SKILL.md files
	err := filepath.Walk(l.skillsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if file is SKILL.md
		if !info.IsDir() && info.Name() == "SKILL.md" {
			skill, loadErr := l.LoadSkill(path)
			if loadErr != nil {
				// Failed to load skill - TUI mode should not print
				return nil // Continue walking
			}

			skills = append(skills, skill)
			l.loadedSkills[skill.Name] = skill
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk skills directory: %w", err)
	}

	return skills, nil
}

// GetSkill returns a loaded skill by name
func (l *SkillLoader) GetSkill(name string) *Skill {
	return l.loadedSkills[name]
}

// ListSkills returns a list of all loaded skill names
func (l *SkillLoader) ListSkills() []string {
	names := make([]string, 0, len(l.loadedSkills))
	for name := range l.loadedSkills {
		names = append(names, name)
	}
	return names
}

// GetSkillsMetadataPrompt generates a prompt containing only metadata (name + description)
// This implements Progressive Disclosure - Level 1
func (l *SkillLoader) GetSkillsMetadataPrompt() string {
	if len(l.loadedSkills) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("## Available Skills\n\n")
	builder.WriteString("You have access to specialized skills. Each skill provides expert guidance for specific tasks.\n")
	builder.WriteString("Load a skill's full content using the get_skill tool when needed.\n\n")

	// List all skills with their descriptions
	for _, skill := range l.loadedSkills {
		builder.WriteString(fmt.Sprintf("- `%s`: %s\n", skill.Name, skill.Description))
	}

	return builder.String()
}

// GetSkillTool is a tool that allows the agent to load full skill content on-demand
type GetSkillTool struct {
	*BaseTool
	loader *SkillLoader
}

// NewGetSkillTool creates a new get_skill tool
func NewGetSkillTool(loader *SkillLoader) *GetSkillTool {
	return &GetSkillTool{
		BaseTool: &BaseTool{
			NameValue:       "get_skill",
			DescriptionValue: "Get complete content and guidance for a specified skill, used for executing specific types of tasks",
			ParametersValue: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"skill_name": map[string]interface{}{
						"type":        "string",
						"description": "Name of the skill to retrieve (use the Available Skills list in system prompt to view available skills)",
					},
				},
				"required": []string{"skill_name"},
			},
		},
		loader: loader,
	}
}

// Execute retrieves the full content of a skill
func (t *GetSkillTool) Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error) {
	// Get skill_name parameter
	skillName, ok := params["skill_name"].(string)
	if !ok || skillName == "" {
		return ErrorResult("skill_name parameter is required"), nil
	}

	// Get skill from loader
	skill := t.loader.GetSkill(skillName)
	if skill == nil {
		available := strings.Join(t.loader.ListSkills(), ", ")
		return ErrorResult(fmt.Sprintf("Skill '%s' does not exist. Available skills: %s", skillName, available)), nil
	}

	// Content for LLM: full skill prompt
	// Details for UI: just the skill name (no need to display full prompt)
	return &ToolResult{
		Success: true,
		Content: skill.ToPrompt(),
		Details: fmt.Sprintf("Loaded skill: %s", skillName),
	}, nil
}

// CreateSkillTools creates skill loader and tool
// Returns the tool, loader, and any error
func CreateSkillTools(skillsDir string) (Tool, *SkillLoader, error) {
	// Create skill loader
	loader := NewSkillLoader(skillsDir)

	// Discover and load skills
	_, err := loader.DiscoverSkills()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to discover skills: %w", err)
	}

	// Discovered skills - TUI mode should not print to console

	// Create get_skill tool (Progressive Disclosure Level 2)
	tool := NewGetSkillTool(loader)

	return tool, loader, nil
}
