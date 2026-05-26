package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTestSkill(dir, name, description, trigger string) {
	skillDir := filepath.Join(dir, name)
	os.MkdirAll(skillDir, 0755)
	content := "---\nname: " + name + "\ndescription: " + description + "\n"
	if trigger != "" {
		content += "trigger: " + trigger + "\n"
	}
	content += "---\n\nThis is the " + name + " skill prompt."
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
}

func TestSkillRegistry_BuildIndex(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	userDir := filepath.Join(t.TempDir(), "user")
	os.MkdirAll(projectDir, 0755)
	os.MkdirAll(userDir, 0755)

	createTestSkill(projectDir, "code-review", "Review code", "When reviewing PRs")
	createTestSkill(userDir, "tdd", "Test driven development", "When writing tests")

	reg := NewSkillRegistry(projectDir, userDir)
	err := reg.BuildIndex()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	index := reg.GetIndex()
	if len(index) != 2 {
		t.Fatalf("expected 2 skills in index, got %d", len(index))
	}
}

func TestSkillRegistry_ProjectOverridesUser(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	userDir := filepath.Join(t.TempDir(), "user")
	os.MkdirAll(projectDir, 0755)
	os.MkdirAll(userDir, 0755)

	createTestSkill(projectDir, "review", "Project review", "project trigger")
	createTestSkill(userDir, "review", "User review", "user trigger")

	reg := NewSkillRegistry(projectDir, userDir)
	reg.BuildIndex()

	index := reg.GetIndex()
	if len(index) != 1 {
		t.Fatalf("expected 1 skill after dedup, got %d", len(index))
	}
	if index[0].Description != "Project review" {
		t.Errorf("expected project version, got: %s", index[0].Description)
	}
	if index[0].Source != SourceProject {
		t.Error("expected SourceProject")
	}
}

func TestSkillRegistry_Activate(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(projectDir, 0755)
	createTestSkill(projectDir, "debug", "Debug skill", "")

	reg := NewSkillRegistry(projectDir, "")
	reg.BuildIndex()

	skill, err := reg.Activate("debug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill.Name != "debug" {
		t.Errorf("expected name 'debug', got '%s'", skill.Name)
	}
	if skill.Prompt == "" {
		t.Error("expected non-empty prompt after activation")
	}
}

func TestSkillRegistry_Activate_NotFound(t *testing.T) {
	reg := NewSkillRegistry("", "")
	reg.BuildIndex()

	_, err := reg.Activate("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
}

func TestSkillRegistry_ActiveSkills(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(projectDir, 0755)
	createTestSkill(projectDir, "s1", "Skill 1", "")
	createTestSkill(projectDir, "s2", "Skill 2", "")

	reg := NewSkillRegistry(projectDir, "")
	reg.BuildIndex()

	if len(reg.ActiveSkills()) != 0 {
		t.Error("expected 0 active skills before activation")
	}

	reg.Activate("s1")
	if len(reg.ActiveSkills()) != 1 {
		t.Error("expected 1 active skill after activation")
	}

	reg.Activate("s1") // duplicate
	if len(reg.ActiveSkills()) != 1 {
		t.Error("expected still 1 active skill (no duplicates)")
	}
}

func TestSkillRegistry_BuildIndexPrompt(t *testing.T) {
	projectDir := filepath.Join(t.TempDir(), "project")
	os.MkdirAll(projectDir, 0755)
	createTestSkill(projectDir, "review", "Review code", "When reviewing")

	reg := NewSkillRegistry(projectDir, "")
	reg.BuildIndex()

	prompt := reg.BuildIndexPrompt()
	if prompt == "" {
		t.Fatal("expected non-empty index prompt")
	}
	if !strings.Contains(prompt, "review") {
		t.Error("expected 'review' in index prompt")
	}
	if !strings.Contains(prompt, "activate_skill") {
		t.Error("expected 'activate_skill' reference in prompt")
	}
}

func TestSkillRegistry_EmptyDirs(t *testing.T) {
	reg := NewSkillRegistry("/nonexistent/path", "/also/nonexistent")
	err := reg.BuildIndex()
	if err != nil {
		t.Fatalf("empty/missing dirs should not error: %v", err)
	}
	if len(reg.GetIndex()) != 0 {
		t.Error("expected empty index")
	}
}
