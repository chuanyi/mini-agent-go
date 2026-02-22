package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillLoader(t *testing.T) {
	// Create a temporary skills directory for testing
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}

	// Create a test SKILL.md file
	skillContent := `---
name: test-skill
description: A test skill for unit testing
license: MIT
allowed-tools:
  - read_file
  - write_file
metadata:
  author: test
---

# Test Skill

This is a test skill for unit testing.

## Instructions

1. Read the file
2. Process it
3. Write the result
`

	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	// Test skill loader
	loader := NewSkillLoader(tempDir)
	skills, err := loader.DiscoverSkills()
	if err != nil {
		t.Fatalf("Failed to discover skills: %v", err)
	}

	if len(skills) != 1 {
		t.Fatalf("Expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Name != "test-skill" {
		t.Errorf("Expected skill name 'test-skill', got '%s'", skill.Name)
	}

	if skill.Description != "A test skill for unit testing" {
		t.Errorf("Expected description 'A test skill for unit testing', got '%s'", skill.Description)
	}

	if skill.License != "MIT" {
		t.Errorf("Expected license 'MIT', got '%s'", skill.License)
	}

	if len(skill.AllowedTools) != 2 {
		t.Errorf("Expected 2 allowed tools, got %d", len(skill.AllowedTools))
	}

	// Test GetSkill
	loadedSkill := loader.GetSkill("test-skill")
	if loadedSkill == nil {
		t.Fatal("Failed to get skill by name")
	}

	if loadedSkill.Name != skill.Name {
		t.Errorf("GetSkill returned wrong skill")
	}

	// Test ListSkills
	skillNames := loader.ListSkills()
	if len(skillNames) != 1 {
		t.Fatalf("Expected 1 skill name, got %d", len(skillNames))
	}

	if skillNames[0] != "test-skill" {
		t.Errorf("Expected skill name 'test-skill', got '%s'", skillNames[0])
	}

	// Test GetSkillsMetadataPrompt
	prompt := loader.GetSkillsMetadataPrompt()
	if prompt == "" {
		t.Error("Expected non-empty metadata prompt")
	}

	t.Logf("Metadata prompt:\n%s", prompt)
}

func TestGetSkillTool(t *testing.T) {
	// Create a temporary skills directory for testing
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}

	// Create a test SKILL.md file
	skillContent := `---
name: test-skill
description: A test skill for unit testing
---

# Test Skill

This is a test skill content.
`

	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write skill file: %v", err)
	}

	// Create skill loader and tool
	loader := NewSkillLoader(tempDir)
	_, err := loader.DiscoverSkills()
	if err != nil {
		t.Fatalf("Failed to discover skills: %v", err)
	}

	tool := NewGetSkillTool(loader)

	// Test successful skill retrieval
	ctx := context.Background()
	params := map[string]interface{}{
		"skill_name": "test-skill",
	}

	result, err := tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}

	if result.Content == "" {
		t.Error("Expected non-empty content")
	}

	t.Logf("Skill content:\n%s", result.Content)

	// Test non-existent skill
	params = map[string]interface{}{
		"skill_name": "non-existent",
	}

	result, err = tool.Execute(ctx, params)
	if err != nil {
		t.Fatalf("Tool execution failed: %v", err)
	}

	if result.Success {
		t.Error("Expected failure for non-existent skill")
	}

	if result.Error == "" {
		t.Error("Expected error message")
	}

	t.Logf("Error message: %s", result.Error)
}

func TestCreateSkillTools(t *testing.T) {
	// Test with non-existent directory
	tool, loader, err := CreateSkillTools("./non-existent-dir")
	if err != nil {
		t.Fatalf("CreateSkillTools should not fail with non-existent dir: %v", err)
	}

	if tool == nil {
		t.Error("Expected non-nil tool")
	}

	if loader == nil {
		t.Error("Expected non-nil loader")
	}

	// Should have 0 skills
	skills := loader.ListSkills()
	if len(skills) != 0 {
		t.Errorf("Expected 0 skills, got %d", len(skills))
	}
}
