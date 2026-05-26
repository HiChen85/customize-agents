package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Trigger     string
	Prompt      string
	Dir         string
	Docs        map[string]string
}

func LoadSkill(dir string) (*Skill, error) {
	skillPath := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}

	fm, body, err := ParseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse SKILL.md: %w", err)
	}

	return &Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Trigger:     fm.Trigger,
		Prompt:      body,
		Dir:         dir,
		Docs:        make(map[string]string),
	}, nil
}

func (s *Skill) LoadDoc(name string) (string, error) {
	if doc, ok := s.Docs[name]; ok {
		return doc, nil
	}

	data, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return "", fmt.Errorf("read doc %s: %w", name, err)
	}

	s.Docs[name] = string(data)
	return string(data), nil
}

func LoadAllSkills(baseDir string) ([]*Skill, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("read skills directory: %w", err)
	}

	var skills []*Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(baseDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			continue
		}
		s, err := LoadSkill(filepath.Join(baseDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("load skill %s: %w", entry.Name(), err)
		}
		skills = append(skills, s)
	}

	return skills, nil
}

func FindSkillByName(skills []*Skill, name string) *Skill {
	for _, s := range skills {
		if strings.EqualFold(s.Name, name) {
			return s
		}
	}
	return nil
}
