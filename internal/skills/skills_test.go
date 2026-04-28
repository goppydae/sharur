package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkill(t *testing.T) {
	content := `---
name: test-skill
description: A test skill
---
Body of the skill.
`
	s := parse(content, "/path/to/test.md")
	if s.Name != "test-skill" {
		t.Errorf("expected name test-skill, got %s", s.Name)
	}
	if s.Description != "A test skill" {
		t.Errorf("expected description 'A test skill', got %s", s.Description)
	}
	if s.Content != "Body of the skill." {
		t.Errorf("expected content 'Body of the skill.', got %s", s.Content)
	}
}

func TestParseSkill_NoFrontmatter(t *testing.T) {
	content := "Body only."
	s := parse(content, "/path/to/test.md")
	if s.Name != "test" {
		t.Errorf("expected name test, got %s", s.Name)
	}
	if s.Content != "Body only." {
		t.Errorf("expected content 'Body only.', got %s", s.Content)
	}
}

func TestDiscover(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a skill file
	skillDir := filepath.Join(tmpDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("--- \n name: my-skill \n --- \n skill content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create another skill file
	otherSkillPath := filepath.Join(tmpDir, "other.md")
	if err := os.WriteFile(otherSkillPath, []byte("other content"), 0644); err != nil {
		t.Fatal(err)
	}

	skills, err := Discover(tmpDir)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skills))
	}

	foundMySkill := false
	for _, s := range skills {
		if s.Name == "my-skill" {
			foundMySkill = true
			break
		}
	}
	if !foundMySkill {
		t.Error("my-skill not discovered")
	}
}

func TestFormatSkillsForPrompt(t *testing.T) {
	skills := []*Skill{
		{Name: "s1", Description: "d1", Path: "/p1"},
	}
	formatted := FormatSkillsForPrompt(skills)
	if !strings.Contains(formatted, "<available_skills>") {
		t.Error("expected <available_skills> in prompt")
	}
	if !strings.Contains(formatted, "s1: d1") {
		t.Error("expected skill name and description in prompt")
	}
}
