package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	input := `---
name: code-review
description: Use when reviewing code
---

# Code Review

Review the code carefully.
`
	fm, body, err := ParseFrontmatter([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fm.Name != "code-review" {
		t.Errorf("expected name 'code-review', got '%s'", fm.Name)
	}
	if fm.Description != "Use when reviewing code" {
		t.Errorf("expected description 'Use when reviewing code', got '%s'", fm.Description)
	}
	if body != "# Code Review\n\nReview the code carefully.\n" {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestLoadSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0755)
	os.MkdirAll(filepath.Join(skillDir, "scripts"), 0755)

	skillMD := `---
name: test-skill
description: A test skill
---

# Test Skill

Do the thing. See checklist.md for details.
`
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644)
	os.WriteFile(filepath.Join(skillDir, "checklist.md"), []byte("# Checklist\n- item 1\n"), 0644)
	os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte("#!/bin/bash\necho hello\n"), 0755)

	s, err := LoadSkill(skillDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got '%s'", s.Name)
	}
	if s.Dir != skillDir {
		t.Errorf("expected dir '%s', got '%s'", skillDir, s.Dir)
	}
	if s.Prompt != "# Test Skill\n\nDo the thing. See checklist.md for details.\n" {
		t.Errorf("unexpected prompt: %q", s.Prompt)
	}
}

func TestLoadAllSkills(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"skill-a", "skill-b"} {
		skillDir := filepath.Join(dir, name)
		os.MkdirAll(skillDir, 0755)
		content := "---\nname: " + name + "\ndescription: " + name + " desc\n---\n\n# " + name + "\n"
		os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
	}

	os.MkdirAll(filepath.Join(dir, "not-a-skill"), 0755)

	skills, err := LoadAllSkills(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skills))
	}
}
